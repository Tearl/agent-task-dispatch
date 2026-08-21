export type PublicSession = {
  sessionId: string;
  userId: string;
  walletAddress: string;
  roles: string[];
  expiresAt: string;
};

export type ActionReason = { code: string; message: string };
export type ActionDecision = { action: string; allowed: boolean; reasons: ActionReason[] };
export type AvailableActions = { aggregateVersion: number; actions: ActionDecision[] };

export class PlatformAPIError extends Error {
  readonly status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}

export class ActionBlockedError extends Error {
  readonly action: string;
  readonly reasons: ActionReason[];
  constructor(action: string, reasons: ActionReason[]) {
    super(reasons.map((reason) => reason.message).join(" ") || `${action} is not allowed`);
    this.action = action;
    this.reasons = reasons;
  }
}

export type WalletProvider = {
  request(input: { method: string; params?: unknown[] }): Promise<unknown>;
};

export type ClientRole = "publisher" | "agent" | "arbitrator";

export function clientRolesForEngineRoles(roles: readonly string[]): ClientRole[] {
  const result: ClientRole[] = [];
  if (roles.includes("publisher")) result.push("publisher");
  if (roles.includes("agent_provider")) result.push("agent");
  if (roles.includes("arbitrator")) result.push("arbitrator");
  return result;
}

export async function authenticateWallet(provider: WalletProvider, request: typeof fetch = fetch): Promise<PublicSession> {
  const accounts = await provider.request({ method: "eth_requestAccounts" });
  const address = Array.isArray(accounts) && typeof accounts[0] === "string" ? accounts[0] : "";
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) throw new Error("钱包未返回有效账户。");
  const challenge = await apiRequest<{ message: string }>("/api/auth/nonce", { method: "POST", body: { walletAddress: address } }, request);
  const signature = await provider.request({ method: "personal_sign", params: [challenge.message, address] });
  if (typeof signature !== "string" || signature.length === 0) throw new Error("钱包未返回有效签名。");
  return apiRequest<PublicSession>("/api/auth/verify", { method: "POST", body: { message: challenge.message, signature } }, request);
}

export async function readSession(request: typeof fetch = fetch): Promise<PublicSession | null> {
  try { return await apiRequest<PublicSession>("/api/auth/session", {}, request); }
  catch (error) { if (error instanceof PlatformAPIError && error.status === 401) return null; throw error; }
}

export async function revokeSession(request: typeof fetch = fetch): Promise<void> {
  await apiRequest<unknown>("/api/auth/session", { method: "DELETE" }, request, true);
}

export type AgentOnboardingInput = {
  operationId: string;
  name: string;
  category: string;
  tagline: string;
  endpointUrl: string;
  capabilities: string[];
  controllerAddress: string;
  maxConcurrency: number;
  credentialType: "api_key" | "bearer_token" | "oauth_client_secret";
  secret: string;
  overviewPrice: string;
  formalPrice: string;
};

export async function onboardAgent(input: AgentOnboardingInput, request: typeof fetch = fetch) {
  validateAgentOnboardingInput(input);
  const agent = await mutation<{ id: string; aggregateVersion: number }>("/api/agents", `${input.operationId}:create`, {
    name: input.name,
    category: input.category,
    tags: input.capabilities,
    capabilities: input.tagline,
    languages: ["zh-CN"],
    estimatedDurationSeconds: 3600,
    authorBio: input.tagline,
    endpointUrl: input.endpointUrl,
    controllerAddress: input.controllerAddress,
    payoutAddress: input.controllerAddress,
    maxConcurrency: input.maxConcurrency,
  }, request);
  const credential = await mutation<{ agentAggregateVersion: number }>(`/api/agents/${encodeURIComponent(agent.id)}/credentials`, `${input.operationId}:credential`, {
    credentialType: input.credentialType,
    label: "Primary integration credential",
    secret: input.secret,
    expectedVersion: agent.aggregateVersion,
  }, request);
  const price = await mutation<{ agentAggregateVersion: number }>(`/api/agents/${encodeURIComponent(agent.id)}/prices`, `${input.operationId}:price`, {
    overviewPrice: input.overviewPrice,
    formalPackageGrossPrice: input.formalPrice,
    additionalVersionPrice: "0",
    externalCostCap: "0",
    expectedVersion: credential.agentAggregateVersion,
  }, request);
  type AgentView = { agent: { id: string; aggregateVersion: number; status: string }; availableActions: AvailableActions };
  const path = `/api/agents/${encodeURIComponent(agent.id)}`;
  let view = await apiRequest<AgentView>(path, {}, request);
  if (view.agent.status === "active") return view.agent;

  let activate = view.availableActions.actions.find((item) => item.action === "activate");
  const needsHealthRefresh = !activate?.allowed && activate?.reasons.some((reason) => reason.code === "healthy_status_required" || reason.code === "health_check_expired");
  if (needsHealthRefresh) {
    const refreshVersion = view.agent.aggregateVersion;
    await mutation<{ aggregateVersion: number }>(`${path}/health`, `${input.operationId}:health:${refreshVersion}`, {
      expectedVersion: refreshVersion,
    }, request);
    view = await apiRequest<AgentView>(path, {}, request);
    if (view.agent.status === "active") return view.agent;
    activate = view.availableActions.actions.find((item) => item.action === "activate");
  }
  if (!activate || !activate.allowed) throw new ActionBlockedError("activate", activate?.reasons ?? [{ code: "action_unavailable", message: "Engine 未返回上线资格。" }]);
  return mutation<{ id: string; status: string; aggregateVersion: number }>(`/api/agents/${encodeURIComponent(agent.id)}/lifecycle`, `${input.operationId}:activate`, {
    status: "active",
    expectedVersion: view.agent.aggregateVersion,
  }, request);
}

export type TaskPublishInput = {
  operationId: string;
  title: string;
  description: string;
  category: string;
  amount: string;
  deadline: string;
  criteria: string[];
};

export async function createAndPublishTask(input: TaskPublishInput, request: typeof fetch = fetch) {
  validateTaskPublishInput(input);
  const criteria = input.criteria.filter((item) => item.trim());
  const weightBase = Math.floor(100 / criteria.length);
  const draft = await mutation<{ id: string; aggregateVersion: number }>("/api/tasks", `${input.operationId}:create`, {
    title: input.title,
    description: input.description,
    expertType: input.category,
    language: "zh-CN",
    overviewBudget: input.amount,
    formalBudget: input.amount,
    externalCostCap: "0",
    deadline: new Date(`${input.deadline}T23:59:59Z`).toISOString(),
    inputs: ["用户提交的任务描述"],
    allowedTools: ["平台批准的只读工具"],
    exclusions: ["未在任务描述中明确授权的操作"],
    deliveryFormat: "按任务描述提交结构化成果",
    acceptanceCriteria: criteria.map((item, index) => ({ id: `AC-${index + 1}`, title: item, description: item, weight: index === criteria.length - 1 ? 100 - weightBase * index : weightBase })),
  }, request);
  const view = await apiRequest<{ task: { aggregateVersion: number; status: string }; availableActions: AvailableActions }>(`/api/tasks/${encodeURIComponent(draft.id)}`, {}, request);
  const publish = view.availableActions.actions.find((item) => item.action === "publish");
  if (view.task.status !== "pending_escrow" && (!publish || !publish.allowed)) throw new ActionBlockedError("publish", publish?.reasons ?? [{ code: "action_unavailable", message: "Engine 未返回发布资格。" }]);
  // The creation replay returns the original draft version. Reusing it here
  // lets Engine replay a committed publication whose response was lost.
  return mutation<{ task: { id: string; status: string }; spec: { contentHash: string }; acceptance: { contentHash: string } }>(`/api/tasks/${encodeURIComponent(draft.id)}/publish`, `${input.operationId}:publish`, { expectedVersion: draft.aggregateVersion }, request);
}

export function validateAgentOnboardingInput(input: AgentOnboardingInput): void {
  if (!input.operationId || !input.name.trim() || input.name.length > 200 || !input.category.trim() || input.category.length > 100 || !input.tagline.trim() || input.tagline.length > 5_000) throw new Error("Agent 基本信息不完整或过长。");
  let endpoint: URL;
  try { endpoint = new URL(input.endpointUrl); } catch { throw new Error("健康检查地址必须是有效的 HTTPS URL。"); }
  if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || input.endpointUrl.length > 2_048) throw new Error("健康检查地址必须是不含凭证、查询或片段的 HTTPS URL。");
  if (!/^0x[0-9a-fA-F]{40}$/.test(input.controllerAddress)) throw new Error("当前会话钱包地址无效。");
  if (!Number.isInteger(input.maxConcurrency) || input.maxConcurrency < 1 || input.maxConcurrency > 10_000) throw new Error("并发上限必须为 1–10000。");
  if (input.capabilities.length === 0 || input.capabilities.length > 50 || input.capabilities.some((item) => !item.trim() || item.length > 2_000)) throw new Error("请提供有效的能力标签。");
  if (!input.secret || input.secret.length > 16_384) throw new Error("凭证不能为空且不得超过 16384 字符。");
  if (!canonicalAmount(input.overviewPrice) || !canonicalAmount(input.formalPrice) || BigInt(input.overviewPrice) > BigInt(input.formalPrice)) throw new Error("概览价格必须是非负整数且不得高于正式套餐总价。");
}

export function validateTaskPublishInput(input: TaskPublishInput): void {
  if (!input.operationId || !input.title.trim() || input.title.length > 200 || !input.description.trim() || input.description.length > 50_000) throw new Error("任务标题或描述不完整或过长。");
  if (!canonicalAmount(input.amount)) throw new Error("预算必须是不含前导零的非负整数。");
  const deadline = new Date(`${input.deadline}T23:59:59Z`);
  if (!Number.isFinite(deadline.getTime()) || deadline.getTime() <= Date.now()) throw new Error("截止日期必须晚于当前时间。");
  const criteria = input.criteria.filter((item) => item.trim());
  if (criteria.length === 0 || criteria.length > 100 || criteria.some((item) => item.length > 2_000)) throw new Error("请提供 1–100 条有效验收标准。");
}

function canonicalAmount(value: string): boolean {
  return value.length <= 78 && /^(?:0|[1-9]\d*)$/.test(value);
}

export function requireAllowed(actions: AvailableActions, action: string): ActionDecision {
  const decision = actions.actions.find((item) => item.action === action);
  if (!decision || !decision.allowed) throw new ActionBlockedError(action, decision?.reasons ?? [{ code: "action_unavailable", message: "Engine 未返回该操作资格。" }]);
  return decision;
}

async function mutation<T>(path: string, idempotencyKey: string, body: unknown, request: typeof fetch) {
  return apiRequest<T>(path, { method: "POST", body, idempotencyKey }, request);
}

async function apiRequest<T>(path: string, options: { method?: string; body?: unknown; idempotencyKey?: string } = {}, request: typeof fetch = fetch, allowEmpty = false): Promise<T> {
  const headers = new Headers({ accept: "application/json" });
  if (options.body !== undefined) headers.set("content-type", "application/json");
  if (options.idempotencyKey) headers.set("idempotency-key", options.idempotencyKey);
  const response = await request(path, { method: options.method ?? "GET", headers, body: options.body === undefined ? undefined : JSON.stringify(options.body), credentials: "include", cache: "no-store" });
  if (allowEmpty && response.ok && response.status === 204) return undefined as T;
  let value: unknown;
  try { value = await response.json(); } catch { throw new PlatformAPIError(response.status || 502, "服务返回了无效响应。"); }
  if (!response.ok) {
    const message = value && typeof value === "object" && typeof (value as { error?: unknown }).error === "string" ? (value as { error: string }).error : "请求失败，请稍后重试。";
    throw new PlatformAPIError(response.status, message);
  }
  return value as T;
}
