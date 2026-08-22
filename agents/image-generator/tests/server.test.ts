import assert from "node:assert/strict";
import { mkdtemp } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import type { AsyncJobRecord } from "@agent-platform/agent-runtime";
import type { GeneratedImage, ImageRequest } from "../src/domain.ts";
import { ImageStore } from "../src/image-store.ts";
import { createImageAgentServer } from "../src/server.ts";

test("HTTP API validates jobs and protects generated image bytes", async (context) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-http-"));
  const images = new ImageStore(directory);
  const stored = await images.write("job-http", Buffer.from("png"));
  const now = new Date().toISOString();
  const job: AsyncJobRecord<ImageRequest, GeneratedImage> = {
    id: "11111111-1111-4111-8111-111111111111",
    status: "queued",
    createdAt: now,
    updatedAt: now,
    request: { prompt: "test", size: "1280x1280", quality: "hd" },
  };
  const server = createImageAgentServer({
    async submit(request) {
      return { ...job, request };
    },
    async get() {
      return job;
    },
  }, images, "secret-token");
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => new Promise<void>((resolve) => server.close(() => resolve())));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

  const unauthorized = await fetch(`${origin}${stored.url}`);
  assert.equal(unauthorized.status, 401);

  const image = await fetch(`${origin}${stored.url}`, {
    headers: { Authorization: "Bearer secret-token" },
  });
  assert.equal(image.status, 200);
  assert.equal(image.headers.get("content-type"), "image/png");
  assert.equal(Buffer.from(await image.arrayBuffer()).toString(), "png");

  const submitted = await fetch(`${origin}/v1/image-generation/jobs`, {
    method: "POST",
    headers: {
      Authorization: "Bearer secret-token",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ prompt: "一只橘猫" }),
  });
  assert.equal(submitted.status, 202);
  assert.equal((await submitted.json() as { status: string }).status, "queued");
});
