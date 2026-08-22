import assert from "node:assert/strict";
import { mkdtemp } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { OneSentenceImageGenerator, type MastraImageAgent } from "../src/generator.ts";
import { ImageStore } from "../src/image-store.ts";

test("stores the image emitted by Mastra's imageGeneration tool", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const expected = Buffer.from("fake png bytes");
  const agent: MastraImageAgent = {
    async stream() {
      return {
        fullStream: (async function* () {
          yield { type: "text-delta", payload: { text: "正在生成" } };
          yield {
            type: "tool-result",
            payload: { toolName: "image_generation", result: { result: expected.toString("base64") } },
          };
        })(),
      };
    },
  };
  const images = new ImageStore(directory, "https://agent.example.com");
  const generator = new OneSentenceImageGenerator(() => agent, images);

  const result = await generator.generate(
    { prompt: "一只在月球喝咖啡的橘猫", size: "1280x1280", quality: "hd" },
    "job-1",
  );

  assert.equal(result.bytes, expected.byteLength);
  assert.match(result.imageUrl, /^https:\/\/agent\.example\.com\/v1\/images\/[a-f0-9]{64}$/u);
  assert.deepEqual(await images.read(result.imageUrl.split("/").at(-1)!), expected);
});

test("accepts the file event emitted by newer Mastra streams", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const expected = Buffer.from("file event png");
  const agent: MastraImageAgent = {
    async stream() {
      return {
        fullStream: (async function* () {
          yield { type: "file", payload: { mimeType: "image/png", base64: expected.toString("base64") } };
        })(),
      };
    },
  };
  const images = new ImageStore(directory);
  const result = await new OneSentenceImageGenerator(() => agent, images).generate(
    { prompt: "test", size: "1280x1280", quality: "hd" },
    "job-file",
  );
  assert.deepEqual(await images.read(result.imageUrl.split("/").at(-1)!), expected);
});

test("accepts images exposed through the Mastra output files promise", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const expected = Buffer.from("files promise png");
  const agent: MastraImageAgent = {
    async stream() {
      return {
        fullStream: (async function* () { yield { type: "finish", payload: {} }; })(),
        files: Promise.resolve([
          { type: "file", payload: { mimeType: "image/png", data: expected.toString("base64") } },
        ]),
      };
    },
  };
  const images = new ImageStore(directory);
  const result = await new OneSentenceImageGenerator(() => agent, images).generate(
    { prompt: "test", size: "1280x1280", quality: "hd" },
    "job-files-promise",
  );
  assert.deepEqual(await images.read(result.imageUrl.split("/").at(-1)!), expected);
});

test("surfaces the provider's image tool error", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const agent: MastraImageAgent = {
    async stream() {
      return {
        fullStream: (async function* () {
          yield { type: "tool-error", payload: { toolName: "image_generation", error: { message: "rate limit" } } };
        })(),
      };
    },
  };
  await assert.rejects(
    new OneSentenceImageGenerator(() => agent, new ImageStore(directory)).generate(
      { prompt: "test", size: "1280x1280", quality: "hd" },
      "job-error",
    ),
    /rate limit/u,
  );
});

test("surfaces a model error emitted before the image tool is called", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const agent: MastraImageAgent = {
    async stream() {
      return {
        fullStream: (async function* () {
          yield { type: "error", payload: { error: { message: "model not found" } } };
        })(),
        files: Promise.resolve([]),
      };
    },
  };
  await assert.rejects(
    new OneSentenceImageGenerator(() => agent, new ImageStore(directory)).generate(
      { prompt: "test", size: "1280x1280", quality: "hd" },
      "job-model-error",
    ),
    /model not found/u,
  );
});

test("rejects a completed run without an image tool result", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-"));
  const agent: MastraImageAgent = {
    async stream() {
      return { fullStream: (async function* () { yield { type: "text-delta" }; })() };
    },
  };
  const generator = new OneSentenceImageGenerator(() => agent, new ImageStore(directory));
  await assert.rejects(
    generator.generate({ prompt: "test", size: "1280x1280", quality: "hd" }, "job-2"),
    /without an image/u,
  );
});
