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

const categories = ["数据分析", "翻译", "图像生成", "代码开发", "市场研究", "智能审计"];

export class InvalidTaskAnalysisInputError extends Error {}
export class InvalidTaskAnalysisResponseError extends Error {}
export class TaskAnalysisProviderError extends Error {}

export function resolveDeepSeekConfig(
  environment: Readonly<Record<string, string | undefined>> = process.env,
): DeepSeekConfig {
  const apiKey = environment.DEEPSEEK_API_KEY?.trim() ?? "";
  if (!apiKey) throw new Error("DEEPSEEK_API_KEY is required");
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
  if (!categories.includes(category)) throw new InvalidTaskAnalysisResponseError("invalid analysis category");
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
