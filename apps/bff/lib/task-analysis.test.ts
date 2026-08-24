import assert from "node:assert/strict";
import test from "node:test";
import {
  generateTaskAnalysis,
  InvalidTaskAnalysisInputError,
  InvalidTaskAnalysisResponseError,
  parseTaskAnalysisInput,
  resolveDeepSeekConfig,
  type DeepSeekConfig,
} from "./task-analysis.ts";

const config: DeepSeekConfig = { apiKey: "test-key", baseUrl: "https://api.deepseek.test", model: "deepseek-v4-flash", timeoutMs: 5_000 };
const analysis = {
  title: "竞品价格采集",
  summary: "采集并核验三个竞品官网的公开价格。",
  category: "数据分析",
  depth: "深度",
  budget: 1200,
  deliveryDays: 3,
  tags: ["数据采集", "质量校验"],
  deliverables: ["结构化价格表"],
  acceptanceCriteria: ["每个价格均附来源链接和采集时间"],
  risk: "部分网站可能限制自动访问。",
};

test("DeepSeek task analysis uses server credentials and JSON output", async () => {
  let requestBody: Record<string, unknown> | undefined;
  const request: typeof fetch = async (input, init) => {
    assert.equal(String(input), "https://api.deepseek.test/chat/completions");
    assert.equal(new Headers(init?.headers).get("authorization"), "Bearer test-key");
    requestBody = JSON.parse(String(init?.body)) as Record<string, unknown>;
    return Response.json({ model: "deepseek-v4-flash", choices: [{ finish_reason: "stop", message: { content: JSON.stringify(analysis) } }] });
  };
  const result = await generateTaskAnalysis(parseTaskAnalysisInput({ prompt: "采集三个竞品官网价格", category: "数据分析", depth: "深度" }), { fetch: request, config });
  assert.deepEqual(result, { analysis, model: "deepseek-v4-flash" });
  assert.deepEqual(requestBody?.response_format, { type: "json_object" });
  assert.equal(requestBody?.model, "deepseek-v4-flash");
});

test("task analysis refinement sends the current version and instruction", async () => {
  const input = parseTaskAnalysisInput({ prompt: "采集价格", currentAnalysis: analysis, instruction: "预算调整为 1500 USDC" });
  let body = "";
  await generateTaskAnalysis(input, { config, fetch: async (_input, init) => {
    body = String(init?.body);
    return Response.json({ choices: [{ finish_reason: "stop", message: { content: JSON.stringify({ ...analysis, budget: 1500 }) } }] });
  } });
  assert.match(body, /预算调整为 1500 USDC/);
  assert.match(body, /现有分析/);
});

test("task analysis rejects invalid input and unexpected model fields", async () => {
  assert.throws(() => parseTaskAnalysisInput({ prompt: "x", instruction: "change" }), InvalidTaskAnalysisInputError);
  await assert.rejects(() => generateTaskAnalysis({ prompt: "x" }, { config, fetch: async () => Response.json({ choices: [{ finish_reason: "stop", message: { content: JSON.stringify({ ...analysis, privateKey: "unsafe" }) } }] }) }), InvalidTaskAnalysisResponseError);
});

test("DeepSeek configuration remains server-only and validates its origin", () => {
  assert.deepEqual(resolveDeepSeekConfig({ DEEPSEEK_API_KEY: "key" }), { apiKey: "key", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", timeoutMs: 45_000 });
  assert.throws(() => resolveDeepSeekConfig({}), /DEEPSEEK_API_KEY/);
  assert.throws(() => resolveDeepSeekConfig({ DEEPSEEK_API_KEY: "key", DEEPSEEK_BASE_URL: "http://api.deepseek.com" }), /DEEPSEEK_BASE_URL/);
});
