import assert from "node:assert/strict";
import test from "node:test";
import { planTask } from "./graph.ts";

const agent = (agentId: string, category: string) => ({ agentId, category, tags: [category], capabilities: [category.toLowerCase()] });

test("LangGraph planner chooses a single node for one capability loop", async () => {
  const plan = await planTask({ task: { id: "task-1", specHash: `sha256:${"1".repeat(64)}`, title: "翻译文档", description: "将中文翻译成英文", category: "翻译", language: "zh", deliverables: ["译文"], allowedTools: [] }, agents: [agent("agent-1", "翻译")] });
  assert.equal(plan.mode, "single"); assert.equal(plan.steps.length, 1); assert.equal(plan.graphVersion, "task-orchestration-langgraph-v1");
});

test("LangGraph planner creates an acyclic multi-agent DAG for dependent phases", async () => {
  const plan = await planTask({ task: { id: "task-2", specHash: `sha256:${"2".repeat(64)}`, title: "竞品研究与验证", description: "抓取竞品数据，分析并复核结论", category: "市场研究", language: "zh", deliverables: ["数据", "报告", "复核记录"], allowedTools: ["web"] }, agents: [agent("agent-1", "采集"), agent("agent-2", "分析"), agent("agent-3", "验证")] });
  assert.equal(plan.mode, "multi"); assert.ok(plan.steps.length >= 2); assert.deepEqual(plan.steps[1]?.dependsOn, ["step-1"]);
});

test("LangGraph planner analyzes an escrowed task even when no agents are currently available", async () => {
  const plan = await planTask({ task: { id: "task-3", specHash: `sha256:${"3".repeat(64)}`, title: "门户网站设计与开发", description: "设计科技风格页面，使用 React 开发并完成响应式测试", category: "代码开发", language: "zh", deliverables: ["设计稿", "源代码", "测试报告"], allowedTools: [] }, agents: [] });
  assert.equal(plan.mode, "multi");
  assert.ok(plan.steps.length >= 2);
  assert.ok(plan.steps.every((step) => step.requiredCapabilities.length > 0));
});
