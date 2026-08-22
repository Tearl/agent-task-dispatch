import assert from "node:assert/strict";
import test from "node:test";
import { GlmImageClient } from "../src/glm-image-client.ts";

test("GLM-Image client generates and immediately downloads the image", async () => {
  const imageBytes = Buffer.from("generated image");
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = input.toString();
    requests.push({ url, init });
    if (url.endsWith("/images/generations")) {
      return Response.json({ data: [{ url: "https://cdn.bigmodel.cn/generated.png" }] });
    }
    return new Response(imageBytes, { headers: { "Content-Type": "image/png" } });
  };
  const client = new GlmImageClient({
    apiKey: "zai-secret",
    baseUrl: "https://open.bigmodel.cn/api/paas/v4",
    fetchImpl,
    validateImageUrl: async (url) => new URL(url),
  });

  const result = await client.generate("一只橘猫", "1280x1280");

  assert.deepEqual(result, imageBytes);
  assert.equal(requests.length, 2);
  const body = JSON.parse(String(requests[0]?.init?.body)) as Record<string, unknown>;
  assert.deepEqual(body, {
    model: "glm-image",
    prompt: "一只橘猫",
    quality: "hd",
    size: "1280x1280",
    watermark_enabled: true,
  });
  assert.equal((requests[0]?.init?.headers as Record<string, string>).Authorization, "Bearer zai-secret");
});

test("GLM-Image client exposes bounded provider errors", async () => {
  const client = new GlmImageClient({
    apiKey: "zai-secret",
    baseUrl: "https://open.bigmodel.cn/api/paas/v4",
    fetchImpl: async () => Response.json({ error: { message: "余额不足" } }, { status: 429 }),
  });

  await assert.rejects(client.generate("test", "1280x1280"), /429: 余额不足/u);
});
