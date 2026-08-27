export type TaskAnalysis = {
  title: string;
  summary: string;
  category: string;
  depth: string;
  budget: number;
  deliveryDays: number;
  tags: string[];
  deliverables: string[];
  acceptanceCriteria: string[];
  risk: string;
};

export type TaskAnalysisInput = {
  prompt: string;
  category?: string;
  depth?: string;
  currentAnalysis?: TaskAnalysis;
  instruction?: string;
};

export type DeepSeekConfig = {
  apiKey: string;
  baseUrl: string;
  model: string;
  timeoutMs: number;
};

type ChatCompletion = {
  model?: unknown;
  choices?: Array<{ finish_reason?: unknown; message?: { content?: unknown } }>;
};

const categories = ["数据分析", "翻译", "图像生成", "代码开发", "市场研究", "智能审计"] as const;
type TaskCategory = typeof categories[number];

export class InvalidTaskAnalysisInputError extends Error {}
export class InvalidTaskAnalysisResponseError extends Error {}
export class TaskAnalysisProviderError extends Error {}
export class TaskAnalysisConfigurationError extends Error {}

export function resolveDeepSeekConfig(
  environment: Readonly<Record<string, string | undefined>> = process.env,
): DeepSeekConfig {
  const apiKey = environment.DEEPSEEK_API_KEY?.trim() ?? "";
  if (!apiKey) throw new TaskAnalysisConfigurationError("DEEPSEEK_API_KEY is required");
  const parsed = new URL(environment.DEEPSEEK_BASE_URL?.trim() || "https://api.deepseek.com");
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("invalid DEEPSEEK_BASE_URL");
  }
  const model = environment.DEEPSEEK_MODEL?.trim() || "deepseek-v4-flash";
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(model)) throw new Error("invalid DEEPSEEK_MODEL");
  const timeoutMs = Number(environment.DEEPSEEK_TIMEOUT_MS ?? "45000");
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1_000 || timeoutMs > 120_000) {
    throw new Error("invalid DEEPSEEK_TIMEOUT_MS");
  }
  return { apiKey, baseUrl: parsed.toString().replace(/\/$/, ""), model, timeoutMs };
}

export function generateLocalTaskAnalysis(input: TaskAnalysisInput): { analysis: TaskAnalysis; model: string } {
  if (input.currentAnalysis && input.instruction) {
    const instruction = input.instruction;
    const analysis: TaskAnalysis = {
      ...input.currentAnalysis,
      tags: [...input.currentAnalysis.tags],
      deliverables: [...input.currentAnalysis.deliverables],
      acceptanceCriteria: [...input.currentAnalysis.acceptanceCriteria],
    };
    const budget = instruction.match(/(?:预算|控制在|不超过)\D{0,8}(\d{1,9})/);
    const deliveryDays = instruction.match(/(?:周期|交付|完成|改为|调整为)\D{0,8}(\d{1,4})\s*天/);
    if (budget) analysis.budget = boundedInteger(Number(budget[1]), 1, 1_000_000_000);
    if (deliveryDays) analysis.deliveryDays = boundedInteger(Number(deliveryDays[1]), 1, 3_650);
    if (!budget && !deliveryDays) analysis.acceptanceCriteria = appendUnique(analysis.acceptanceCriteria, instruction.slice(0, 1_000), 20);
    analysis.summary = boundedString(`${input.currentAnalysis.summary.replace(/\n补充要求：.*$/s, "")}\n补充要求：${instruction}`.slice(0, 5_000), 1, 5_000);
    return { analysis: validateAnalysis(analysis), model: "local-rules-v1" };
  }

  const prompt = input.prompt.trim();
  const category = inferCategory(prompt, input.category);
  const defaults = localCategoryDefaults[category];
  const depth = input.depth?.trim() || "深度";
  return {
    analysis: validateAnalysis({
      title: prompt.length > 200 ? `${prompt.slice(0, 199)}…` : prompt,
      summary: `基于用户描述，完成“${prompt.slice(0, 4_800)}”，并以可验证、可复用的形式交付结果。`,
      category,
      depth,
      budget: defaults.budget,
      deliveryDays: depth === "专家" ? 5 : depth === "标准" ? 2 : 3,
      tags: defaults.tags,
      deliverables: defaults.deliverables,
      acceptanceCriteria: defaults.acceptanceCriteria,
      risk: defaults.risk,
    }),
    model: "local-rules-v1",
  };
}

const localCategoryDefaults: Record<TaskCategory, Pick<TaskAnalysis, "budget" | "tags" | "deliverables" | "acceptanceCriteria" | "risk">> = {
  数据分析: { budget: 1200, tags: ["数据采集", "结构化分析", "可验证交付"], deliverables: ["结构化数据文件", "分析摘要", "数据来源说明"], acceptanceCriteria: ["数据字段完整", "结果可复核", "交付格式可直接使用"], risk: "公开数据可能存在缺失、更新延迟或访问限制。" },
  翻译: { budget: 800, tags: ["专业翻译", "术语一致", "质量校对"], deliverables: ["完整译文", "术语表", "校对说明"], acceptanceCriteria: ["无明显漏译错译", "术语前后一致", "保留原文结构"], risk: "专业术语和上下文不足可能影响翻译准确度。" },
  图像生成: { budget: 1000, tags: ["图像生成", "视觉设计", "多版本交付"], deliverables: ["最终图像文件", "候选版本", "生成参数说明"], acceptanceCriteria: ["尺寸与格式符合要求", "主题和风格匹配", "不存在明显视觉瑕疵"], risk: "复杂文字排版和特定人物一致性可能需要多轮调整。" },
  代码开发: { budget: 2500, tags: ["代码开发", "自动化测试", "部署文档"], deliverables: ["可运行源代码", "自动化测试", "部署与使用说明"], acceptanceCriteria: ["核心功能按需求运行", "类型检查与测试通过", "不存在高危安全问题"], risk: "需要明确现有代码仓库、运行环境和第三方依赖版本。" },
  市场研究: { budget: 1500, tags: ["市场洞察", "多源验证", "研究报告"], deliverables: ["研究报告", "数据来源清单", "关键结论与行动建议"], acceptanceCriteria: ["关键结论有来源支撑", "覆盖约定市场与竞品", "数据口径和时间范围明确"], risk: "部分市场数据可能依赖付费数据库或存在时效限制。" },
  智能审计: { budget: 3200, tags: ["安全审计", "风险分级", "修复建议"], deliverables: ["安全审计报告", "问题复现说明", "分级修复建议"], acceptanceCriteria: ["覆盖约定代码范围", "风险可复现且分级明确", "高危问题提供可执行修复方案"], risk: "审计结论依赖冻结的代码版本，后续代码变更需重新评估。" },
};

function inferCategory(prompt: string, requested?: string): TaskCategory {
  if (requested && categories.includes(requested as TaskCategory)) return requested as TaskCategory;
  const rules: Array<[TaskCategory, string[]]> = [
    ["翻译", ["翻译", "译文", "多语言"]],
    ["图像生成", ["图片", "图像", "海报", "视觉"]],
    ["代码开发", ["代码", "开发", "接口", "网站", "应用"]],
    ["市场研究", ["市场", "竞品", "调研", "行业"]],
    ["智能审计", ["审计", "安全", "漏洞", "合约"]],
  ];
  return rules.find(([, keywords]) => keywords.some((keyword) => prompt.includes(keyword)))?.[0] ?? "数据分析";
}

function appendUnique(items: string[], value: string, maximum: number): string[] {
  if (items.includes(value)) return items;
  return [...items.slice(0, maximum - 1), value];
}

export function parseTaskAnalysisInput(value: unknown): TaskAnalysisInput {
  try {
    const input = record(value);
    if (!input) throw new InvalidTaskAnalysisInputError("invalid request");
    const prompt = boundedString(input.prompt, 1, 50_000);
    const category = optionalString(input.category, 100);
    const depth = optionalString(input.depth, 50);
    const instruction = optionalString(input.instruction, 10_000);
    const currentAnalysis = input.currentAnalysis === undefined ? undefined : validateAnalysis(input.currentAnalysis);
    if (Boolean(instruction) !== Boolean(currentAnalysis)) {
      throw new InvalidTaskAnalysisInputError("instruction and currentAnalysis must be provided together");
    }
    return { prompt, ...(category ? { category } : {}), ...(depth ? { depth } : {}), ...(currentAnalysis ? { currentAnalysis } : {}), ...(instruction ? { instruction } : {}) };
  } catch (error) {
    if (error instanceof InvalidTaskAnalysisInputError) throw error;
    throw new InvalidTaskAnalysisInputError("invalid request", { cause: error });
  }
}

export async function generateTaskAnalysis(
  input: TaskAnalysisInput,
  options: { fetch?: typeof fetch; config?: DeepSeekConfig } = {},
): Promise<{ analysis: TaskAnalysis; model: string }> {
  const config = options.config ?? resolveDeepSeekConfig();
  const request = options.fetch ?? fetch;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), config.timeoutMs);
  let response: Response;
  try {
    response = await request(`${config.baseUrl}/chat/completions`, {
      method: "POST",
      headers: { authorization: `Bearer ${config.apiKey}`, "content-type": "application/json", accept: "application/json" },
      body: JSON.stringify({
        model: config.model,
        temperature: 0.2,
        max_tokens: 2_500,
        response_format: { type: "json_object" },
        messages: [
          { role: "system", content: systemPrompt },
          { role: "user", content: userPrompt(input) },
        ],
      }),
      cache: "no-store",
      signal: controller.signal,
    });
  } catch (error) {
    throw new TaskAnalysisProviderError(error instanceof Error && error.name === "AbortError" ? "DeepSeek request timed out" : "DeepSeek request failed", { cause: error });
  } finally {
    clearTimeout(timeout);
  }
  if (!response.ok) throw new TaskAnalysisProviderError(`DeepSeek returned ${response.status}`);
  let completion: ChatCompletion;
  try {
    completion = await response.json() as ChatCompletion;
  } catch (error) {
    throw new InvalidTaskAnalysisResponseError("DeepSeek returned invalid JSON", { cause: error });
  }
  const choice = completion.choices?.[0];
  if (choice?.finish_reason && choice.finish_reason !== "stop") {
    throw new InvalidTaskAnalysisResponseError("DeepSeek response was incomplete");
  }
  if (typeof choice?.message?.content !== "string" || !choice.message.content.trim()) {
    throw new InvalidTaskAnalysisResponseError("DeepSeek returned empty content");
  }
  let raw: unknown;
  try {
    raw = JSON.parse(stripCodeFence(choice.message.content));
  } catch (error) {
    throw new InvalidTaskAnalysisResponseError("DeepSeek returned malformed analysis", { cause: error });
  }
  return { analysis: validateAnalysis(raw), model: typeof completion.model === "string" ? completion.model : config.model };
}

const systemPrompt = `你是 Agent 任务平台的发布需求分析助手。请把用户需求转换为可验证的任务草稿。
你只能输出一个 JSON 对象，不得输出 Markdown、解释或额外字段。JSON 格式必须严格为：
{"title":"字符串","summary":"字符串","category":"数据分析|翻译|图像生成|代码开发|市场研究|智能审计","depth":"字符串","budget":1200,"deliveryDays":3,"tags":["字符串"],"deliverables":["字符串"],"acceptanceCriteria":["字符串"],"risk":"字符串"}
规则：预算是正整数 USDC；周期是正整数天；交付物和验收标准必须具体、可检查；不要声称已经访问网站、文件或执行任务；信息不足时写入 risk，不要捏造事实。`;

function userPrompt(input: TaskAnalysisInput): string {
  if (input.currentAnalysis && input.instruction) {
    return `请按补充要求修改现有任务分析，并输出完整 JSON。未被要求修改的内容应尽量保持不变。\n原始需求：${input.prompt}\n现有分析：${JSON.stringify(input.currentAnalysis)}\n补充要求：${input.instruction}`;
  }
  return `请分析以下任务需求并输出 JSON。\n用户选择的分类：${input.category || "未指定"}\n分析深度：${input.depth || "深度"}\n原始需求：${input.prompt}`;
}

function validateAnalysis(value: unknown): TaskAnalysis {
  const analysis = record(value);
  if (!analysis) throw new InvalidTaskAnalysisResponseError("analysis must be an object");
  const expectedKeys = ["acceptanceCriteria", "budget", "category", "deliverables", "deliveryDays", "depth", "risk", "summary", "tags", "title"];
  if (Object.keys(analysis).sort().join("|") !== expectedKeys.join("|")) throw new InvalidTaskAnalysisResponseError("analysis has unexpected fields");
  const category = boundedString(analysis.category, 1, 100);
  if (!categories.includes(category as TaskCategory)) throw new InvalidTaskAnalysisResponseError("invalid analysis category");
  return {
    title: boundedString(analysis.title, 1, 200),
    summary: boundedString(analysis.summary, 1, 5_000),
    category,
    depth: boundedString(analysis.depth, 1, 50),
    budget: boundedInteger(analysis.budget, 1, 1_000_000_000),
    deliveryDays: boundedInteger(analysis.deliveryDays, 1, 3_650),
    tags: stringArray(analysis.tags, 1, 12, 100),
    deliverables: stringArray(analysis.deliverables, 1, 20, 1_000),
    acceptanceCriteria: stringArray(analysis.acceptanceCriteria, 1, 20, 1_000),
    risk: boundedString(analysis.risk, 1, 2_000),
  };
}

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function boundedString(value: unknown, minimum: number, maximum: number): string {
  if (typeof value !== "string") throw new InvalidTaskAnalysisResponseError("invalid string field");
  const normalized = value.trim();
  if (normalized.length < minimum || normalized.length > maximum) throw new InvalidTaskAnalysisResponseError("string field is out of bounds");
  return normalized;
}

function optionalString(value: unknown, maximum: number): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  try { return boundedString(value, 1, maximum); }
  catch (error) { throw new InvalidTaskAnalysisInputError("invalid optional string", { cause: error }); }
}

function boundedInteger(value: unknown, minimum: number, maximum: number): number {
  if (!Number.isSafeInteger(value) || (value as number) < minimum || (value as number) > maximum) {
    throw new InvalidTaskAnalysisResponseError("invalid integer field");
  }
  return value as number;
}

function stringArray(value: unknown, minimum: number, maximum: number, itemMaximum: number): string[] {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) throw new InvalidTaskAnalysisResponseError("invalid array field");
  return value.map((item) => boundedString(item, 1, itemMaximum));
}

function stripCodeFence(value: string): string {
  return value.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
}
