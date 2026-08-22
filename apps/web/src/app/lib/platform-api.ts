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
export type ChainPresentation = { submission: "not_submitted" | "submitted"; confirmation: "not_observed" | "pending" | "confirmed" | "failed" | "orphaned" };
export type PublisherFinanceView = {
  asOf: string;
  totals: { discovery: string; formal: string; changeOrders: string; disputeFees: string; refundable: string; refunded: string };
  tasks: Array<{ taskId: string; title: string; asset: string; lifecycle: string; discovery: string; formal: string; changeOrders: string; disputeFees: string; refundable: string; refundStatus: "available" | "pending" | "confirmed" | "unavailable"; terminal: boolean; chain: ChainPresentation; transactionHash?: string; updatedAt: string }>;
  ledger: Array<{ id: string; taskId?: string; type: string; amount: string; asset: string; reasonCode: string; transactionHash?: string; createdAt: string }>;
};
export type AgentFinanceView = {
  asOf: string;
  totals: { overviewReceivable: string; formalClaimable: string; totalAvailable: string };
  positions: Array<{ agentId: string; agentName: string; controller: string; payout: string; asset: string; overviewReceivable: string; formalClaimable: string; chainClaimable: string; chain: ChainPresentation }>;
  records: PublisherFinanceView["ledger"];
};
export type ReconciliationFinanceView = {
  asOf: string;
  runs: Array<{ id: string; chainId: string; contract: string; safeBlock: number; status: "matched" | "difference_detected"; startedAt: string; finishedAt: string; differences: Array<{ category: string; resourceId: string; expected: string; observed: string; severity: "warning" | "critical" }> }>;
};
export type MatchingView = {
  asOf: string;
  task: { id: string; title: string; status: string; specHash: string };
  snapshot?: { id: string; revision: number; algorithmVersion: string; ruleVersion: string; modelVersion: string; seedDigest: string; explorationTriggered: boolean; createdAt: string; degradations: Array<{ dependency: string; code: string; message: string }>; candidates: Array<{ agentId: string; name: string; category: string; tags: string[]; estimatedDurationSeconds: number; position: number; exploration: boolean; overviewPrice: string; formalPrice: string; externalCostCap: string; score: { taskMatch: number; reputation: number; priceTime: number; availability: number; rule: number; modelDelta: number; ranking: number }; overview?: { slotId: string; status: string; billingStatus: string; validationCodes: string[]; contentHash?: string; replacement: boolean } }> };
  batch?: { id: string; status: string; deadline: string; replacementUsed: boolean; replacementExhausted: boolean };
  reservation?: { id: string; agentId: string; slotId: string; status: string; transactionHash?: string };
};
export type SelectionProof = { taskId: string; assignmentId: string; agentController: string; payout: string; overviewId: string; allocationId: string; quoteHash: string; taskSpecHash: string; matchRevision: number; priceVersion: number; overviewPrice: string; formalGrossPrice: string; overviewCredit: string; policyHash: string; nonce: string; deadline: number };
export type SelectionIntent = { reservation: { id: string; publisherWallet: string; taskId: string; batchId: string; slotId: string; agentId: string; chainId: string; contractAddress: string; proof: SelectionProof; formalPayable: string; status: string; transactionHash?: string }; platformSignature: string };

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

export type ClientRole = "publisher" | "agent" | "arbitrator" | "admin";

export function clientRolesForEngineRoles(roles: readonly string[]): ClientRole[] {
  const result: ClientRole[] = [];
  if (roles.includes("publisher")) result.push("publisher");
  if (roles.includes("agent_provider")) result.push("agent");
  if (roles.includes("arbitrator")) result.push("arbitrator");
  if (roles.includes("admin")) result.push("admin");
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

export function readPublisherFinance(request: typeof fetch = fetch) { return apiRequest<PublisherFinanceView>("/api/finance/publisher", {}, request); }
export function readAgentFinance(request: typeof fetch = fetch) { return apiRequest<AgentFinanceView>("/api/finance/agent", {}, request); }
export function readReconciliationFinance(request: typeof fetch = fetch) { return apiRequest<ReconciliationFinanceView>("/api/finance/reconciliation", {}, request); }
export function readMatchingView(taskID: string, request: typeof fetch = fetch) { return apiRequest<MatchingView>(`/api/tasks/${encodeURIComponent(taskID)}/matching`, {}, request); }
export function reserveSelection(taskID: string, batchID: string, slotID: string, operationID: string, request: typeof fetch = fetch) { return mutation<SelectionIntent>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations`, operationID, { batchId: batchID, slotId: slotID }, request); }
export function readSelection(taskID: string, reservationID: string, request: typeof fetch = fetch) { return apiRequest<SelectionIntent>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations/${encodeURIComponent(reservationID)}`, {}, request); }
export function reconcileSelection(taskID: string, reservationID: string, transactionHash: string, request: typeof fetch = fetch) { return mutation<{ reservation: SelectionIntent["reservation"]; assignment: { id: string; workNonce: number } | null }>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations/${encodeURIComponent(reservationID)}/reconcile`, `${reservationID}:reconcile:${transactionHash.toLowerCase()}`, { transactionHash }, request); }

export async function submitSelectionTransaction(provider: WalletProvider, intent: SelectionIntent): Promise<string> {
  if (!intent.platformSignature || intent.reservation.status !== "reserved") throw new Error("选择证明当前不可提交。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || !/^0x[0-9a-fA-F]+$/.test(walletChain) || BigInt(walletChain).toString(10) !== intent.reservation.chainId) throw new Error(`请将钱包切换到链 ${intent.reservation.chainId}。`);
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from: intent.reservation.publisherWallet, to: intent.reservation.contractAddress, data: encodeSelectionCall(intent.reservation.proof, intent.platformSignature), value: "0x0" }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效交易哈希。");
  return transactionHash.toLowerCase();
}

function encodeSelectionCall(proof: SelectionProof, signature: string): string {
  const words = [proof.taskId, proof.assignmentId, addressWord(proof.agentController), addressWord(proof.payout), proof.overviewId, proof.allocationId, proof.quoteHash, proof.taskSpecHash, uintWord(proof.matchRevision), uintWord(proof.priceVersion), uintWord(proof.overviewPrice), uintWord(proof.formalGrossPrice), uintWord(proof.overviewCredit), proof.policyHash, proof.nonce, uintWord(proof.deadline)].map(normalizeWord);
  const rawSignature = signature.replace(/^0x/, "");
  if (!/^[0-9a-fA-F]{130}$/.test(rawSignature)) throw new Error("平台选择签名无效。");
  const offset = uintWord(17 * 32);
  const length = uintWord(rawSignature.length / 2);
  const paddedSignature = rawSignature.toLowerCase().padEnd(Math.ceil(rawSignature.length / 64) * 64, "0");
  return `0xa2dfc191${words.join("")}${offset}${length}${paddedSignature}`;
}

function normalizeWord(value: string): string {
  const raw = value.replace(/^0x/, "").toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(raw)) throw new Error("选择证明字段无效。");
  return raw;
}
function addressWord(value: string): string {
  const raw = value.replace(/^0x/, "").toLowerCase();
  if (!/^[0-9a-f]{40}$/.test(raw)) throw new Error("选择地址无效。");
  return raw.padStart(64, "0");
}
function uintWord(value: string | number): string {
  let encoded: string;
  try { encoded = BigInt(value).toString(16); } catch { throw new Error("选择数值无效。"); }
  if (encoded.length > 64 || encoded.startsWith("-")) throw new Error("选择数值超出范围。");
  return encoded.padStart(64, "0");
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
