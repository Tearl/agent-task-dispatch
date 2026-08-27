import assert from "node:assert/strict";
import test from "node:test";
import { SeedreamClient, visualDesignPrompt } from "../src/seedream-client.ts";

test("Seedream client requests one 2K PNG as base64", async () => {
  let request: { url: string; init?: RequestInit } | undefined;
  const expected = Buffer.from("seedream png");
  const client = new SeedreamClient({
    apiKey: "secret", baseUrl: "https://ark.example/api/v3", model: "doubao-seedream-5-0-lite-260128",
    fetchImpl: async (input, init) => { request = { url: input.toString(), init }; return Response.json({ data: [{ b64_json: expected.toString("base64") }] }); },
  });
  assert.deepEqual(await client.generate("开发者工具首页"), expected);
  const body = JSON.parse(String(request?.init?.body)) as Record<string, any>;
  assert.equal(request?.url, "https://ark.example/api/v3/images/generations");
  assert.equal(body.size, "2K");
  assert.equal(body.response_format, "b64_json");
  assert.equal(body.sequential_image_generation, "disabled");
  assert.match(body.prompt, /品牌视觉/u);
});

test("visual design prompt retains the product requirement", () => assert.match(visualDesignPrompt("代码托管产品"), /代码托管产品/u));

test("Seedream client reports bounded provider errors", async () => {
  const client = new SeedreamClient({ apiKey: "secret", baseUrl: "https://ark.example/api/v3", model: "doubao-seedream-5-0-lite-260128", fetchImpl: async () => Response.json({ error: { code: "RateLimit", message: "稍后再试" } }, { status: 429 }) });
  await assert.rejects(client.generate("test"), /429: RateLimit: 稍后再试/u);
});
