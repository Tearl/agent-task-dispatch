import { Annotation, END, START, StateGraph } from "@langchain/langgraph";
import { z } from "zod";

const agentSchema = z.object({
  agentId: z.string().min(1).max(128),
  category: z.string().min(1).max(100),
  tags: z.array(z.string().max(100)).max(50),
  capabilities: z.array(z.string().max(200)).max(100),
});

export const planRequestSchema = z.object({
  task: z.object({
    id: z.string().min(1).max(128),
    specHash: z.string().regex(/^sha256:[0-9a-f]{64}$/),
    title: z.string().min(1).max(200),
    description: z.string().min(1).max(10_000),
    category: z.string().min(1).max(100),
    language: z.string().min(1).max(50),
    deliverables: z.array(z.string().max(1_000)).max(50),
    allowedTools: z.array(z.string().max(100)).max(50),
  }),
  agents: z.array(agentSchema).max(1_000),
});

export type PlanRequest = z.infer<typeof planRequestSchema>;
export type PlanStep = {
  id: string;
  title: string;
  objective: string;
  requiredCapabilities: string[];
  dependsOn: string[];
  output: string;
};
export type OrchestrationPlan = {
  mode: "single" | "multi";
  summary: string;
  rationale: string[];
  confidence: number;
  steps: PlanStep[];
  model: string;
  graphVersion: "task-orchestration-langgraph-v1";
};

const modelPlanSchema = z.object({
  mode: z.enum(["single", "multi"]),
  summary: z.string().min(1).max(2_000),
  rationale: z.array(z.string().min(1).max(1_000)).min(1).max(12),
  confidence: z.number().min(0).max(1),
  steps: z.array(z.object({
    id: z.string().regex(/^step-[1-9][0-9]{0,2}$/),
    title: z.string().min(1).max(200),
    objective: z.string().min(1).max(2_000),
    requiredCapabilities: z.array(z.string().min(1).max(200)).min(1).max(20),
    dependsOn: z.array(z.string()).max(20),
    output: z.string().min(1).max(1_000),
  })).min(1).max(20),
});

const GraphState = Annotation.Root({
  input: Annotation<PlanRequest>(),
  capabilityIndex: Annotation<string[]>(),
  draft: Annotation<z.infer<typeof modelPlanSchema>>(),
  result: Annotation<OrchestrationPlan>(),
});

export type PlannerOptions = {
  apiKey?: string;
  baseUrl?: string;
  model?: string;
  fetch?: typeof fetch;
};

export function createPlanningGraph(options: PlannerOptions = {}) {
  return new StateGraph(GraphState)
    .addNode("normalize", async (state) => ({
      capabilityIndex: [...new Set(state.input.agents.flatMap((agent) => [agent.category, ...agent.tags, ...agent.capabilities]).map(normalize).filter(Boolean))].sort(),
    }))
    .addNode("decompose", async (state) => ({ draft: await decompose(state.input, state.capabilityIndex, options) }))
    .addNode("validate", async (state) => ({ result: validatePlan(state.draft, options.apiKey ? (options.model ?? "deepseek-chat") : "local-rules-v1") }))
    .addEdge(START, "normalize")
    .addEdge("normalize", "decompose")
    .addEdge("decompose", "validate")
    .addEdge("validate", END)
    .compile();
}

export async function planTask(input: unknown, options: PlannerOptions = {}): Promise<OrchestrationPlan> {
  const parsed = planRequestSchema.parse(input);
  const state = await createPlanningGraph(options).invoke({ input: parsed });
  return state.result;
}

async function decompose(input: PlanRequest, capabilities: string[], options: PlannerOptions) {
  if (!options.apiKey) return localPlan(input, capabilities);
  const response = await (options.fetch ?? fetch)(`${(options.baseUrl ?? "https://api.deepseek.com").replace(/\/$/, "")}/chat/completions`, {
    method: "POST",
    headers: { authorization: `Bearer ${options.apiKey}`, "content-type": "application/json" },
    body: JSON.stringify({
      model: options.model ?? "deepseek-chat",
      temperature: 0.1,
      response_format: { type: "json_object" },
      messages: [
        { role: "system", content: systemPrompt },
        { role: "user", content: JSON.stringify({ task: input.task, availableCapabilities: capabilities }) },
      ],
    }),
    signal: AbortSignal.timeout(45_000),
  });
  if (!response.ok) throw new Error(`planner model returned ${response.status}`);
  const body = await response.json() as { choices?: Array<{ message?: { content?: string } }> };
  const content = body.choices?.[0]?.message?.content;
  if (!content) throw new Error("planner model returned empty content");
  return modelPlanSchema.parse(JSON.parse(content.replace(/^```(?:json)?\s*|\s*```$/g, "")));
}

function localPlan(input: PlanRequest, capabilities: string[]): z.infer<typeof modelPlanSchema> {
  const text = `${input.task.title} ${input.task.description} ${input.task.deliverables.join(" ")}`;
  const phases = [
    ["research", ["调研", "抓取", "采集", "研究", "竞品"]],
    ["analysis", ["分析", "洞察", "对比", "审计"]],
    ["build", ["开发", "代码", "实现", "生成", "设计"]],
    ["verify", ["测试", "验证", "校对", "复核", "验收"]],
  ].filter(([, words]) => (words as string[]).some((word) => text.includes(word))) as Array<[string, string[]]>;
  const multi = phases.length >= 2 || input.task.deliverables.length >= 3;
  if (!multi) {
    const capability = bestCapability(input.task.category, capabilities);
    return { mode: "single", summary: "该任务的输入、执行与交付可以由一种 Agent 能力闭环完成。", rationale: ["任务只有一个主要能力域", "交付物之间不存在必须跨 Agent 的前置依赖"], confidence: 0.72, steps: [{ id: "step-1", title: "完成任务并交付", objective: input.task.description, requiredCapabilities: [capability], dependsOn: [], output: input.task.deliverables.join("、") || "任务交付物" }] };
  }
  const labels: Record<string, [string, string]> = { research: ["信息采集", "形成可追溯的原始资料与结构化输入"], analysis: ["专业分析", "基于上游输入形成结论与决策依据"], build: ["内容或成果生产", "生成任务要求的核心成果"], verify: ["质量验证", "独立复核成果并输出验收证据"] };
  const selected = phases.length >= 2 ? phases : [["analysis", []], ["verify", []]] as Array<[string, string[]]>;
  const steps = selected.map(([phase], index) => {
    const [title, objective] = labels[phase] ?? [phase, `${phase}阶段任务`];
    return { id: `step-${index + 1}`, title, objective, requiredCapabilities: [bestCapability(title, capabilities)], dependsOn: index === 0 ? [] : [`step-${index}`], output: index === selected.length - 1 ? (input.task.deliverables.join("、") || "最终交付物") : `${title}阶段产物` };
  });
  return { mode: "multi", summary: `任务需要 ${steps.length} 个有依赖关系的能力阶段，建议使用多 Agent DAG 执行。`, rationale: ["任务覆盖多个能力阶段", "后续阶段需要消费前序阶段的结构化产物", "独立验证节点可降低交付风险"], confidence: 0.78, steps };
}

function validatePlan(draft: z.infer<typeof modelPlanSchema>, model: string): OrchestrationPlan {
  const parsed = modelPlanSchema.parse(draft);
  if (parsed.mode === "single" && parsed.steps.length !== 1) throw new Error("single plan must contain exactly one step");
  if (parsed.mode === "multi" && parsed.steps.length < 2) throw new Error("multi plan must contain at least two steps");
  const ids = new Set(parsed.steps.map((step) => step.id));
  for (const step of parsed.steps) {
    if (step.dependsOn.some((id) => !ids.has(id) || id === step.id)) throw new Error("plan contains invalid dependency");
  }
  assertAcyclic(parsed.steps);
  return { ...parsed, model, graphVersion: "task-orchestration-langgraph-v1" };
}

function assertAcyclic(steps: PlanStep[]) {
  const visiting = new Set<string>(); const visited = new Set<string>(); const byId = new Map(steps.map((step) => [step.id, step]));
  const visit = (id: string) => { if (visiting.has(id)) throw new Error("plan contains dependency cycle"); if (visited.has(id)) return; visiting.add(id); for (const dep of byId.get(id)?.dependsOn ?? []) visit(dep); visiting.delete(id); visited.add(id); };
  for (const step of steps) visit(step.id);
}

function bestCapability(hint: string, available: string[]) { const normalized = normalize(hint); return available.find((item) => item.includes(normalized) || normalized.includes(item)) ?? available[0] ?? "general"; }
function normalize(value: string) { return value.trim().toLowerCase(); }

const systemPrompt = `你是任务编排规划器。判断任务能否由一种 Agent 能力闭环完成；只有存在真实的能力边界或产物依赖时才使用 multi。输出严格 JSON：mode、summary、rationale、confidence、steps。steps 的 id 必须为 step-1 起的连续编号；availableCapabilities 只是当前供给参考，不是任务分析边界，缺少合适供给时应自行给出简洁、稳定的 requiredCapabilities 能力标签；dependsOn 只能引用前序步骤；图必须无环。不要选择具体 Agent ID。`;
