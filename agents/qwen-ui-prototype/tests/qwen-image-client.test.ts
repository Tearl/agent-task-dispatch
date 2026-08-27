import assert from "node:assert/strict";
import test from "node:test";
import { QwenImageClient, uiPrototypePrompt } from "../src/qwen-image-client.ts";

test("Qwen client sends a UI-specific request and persists the downloaded result", async () => {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = input.toString(); calls.push({ url, init });
    if (url.includes("multimodal-generation")) return Response.json({ output: { choices: [{ message: { content: [{ image: "https://result.example/ui.png" }] } }] } });
    return new Response(Buffer.from("qwen png"), { headers: { "Content-Type": "image/png" } });
  };
  const client = new QwenImageClient({ apiKey: "secret", baseUrl: "https://workspace.example", model: "qwen-image-3.0-pro", fetchImpl, validateImageUrl: async (url) => new URL(url) });
  assert.deepEqual(await client.generate("开发者任务看板", "1568x1056"), Buffer.from("qwen png"));
  const body = JSON.parse(String(calls[0]?.init?.body)) as any;
  assert.equal(body.model, "qwen-image-3.0-pro");
  assert.equal(body.parameters.size, "1568*1056");
  assert.equal(body.parameters.prompt_extend_mode, "agent");
  assert.match(body.input.messages[0].content[0].text, /前端工程师/u);
});

test("UI prompt retains the product requirement", () => assert.match(uiPrototypePrompt("代码审查页面"), /代码审查页面/u));

test("Qwen client reports bounded provider errors", async () => {
  const client = new QwenImageClient({ apiKey: "secret", baseUrl: "https://workspace.example", model: "qwen-image-3.0-pro", fetchImpl: async () => Response.json({ code: "Throttled", message: "稍后再试" }, { status: 429 }) });
  await assert.rejects(client.generate("test", "1280x1280"), /429: Throttled: 稍后再试/u);
});
