export type EngineResourceKind = "agents" | "tasks";
export type EngineFinanceKind = "publisher" | "agent" | "reconciliation";
export type EngineWorkspaceKind = "tasks" | "agents" | "marketplace" | "notifications";

export type EngineAggregateResult = {
  status: number;
  body: Record<string, unknown>;
};

export type EngineMutationInput = {
  path: string;
  body: string;
  idempotencyKey: string;
  sessionToken: string;
};

export class InvalidResourceIdError extends Error {}
export class InvalidEngineResponseError extends Error {}

const maxEngineResponseBytes = 1_048_576;
const sensitiveKeys = new Set([
  "authorization",
  "accesstoken",
  "apikey",
  "ciphertext",
  "credential",
  "credentialvalue",
	"callbackkey",
	"callbacknonce",
  "plaintext",
  "privatekey",
  "password",
	"inputref",
  "refreshtoken",
  "secret",
  "sessiontoken",
  "signature",
  "token",
  "wrappeddatakey",
]);

export function resolveEngineBaseUrl(environment: Readonly<Record<string, string | undefined>> = process.env): string {
  const raw = environment.ENGINE_BASE_URL ?? "http://localhost:8080";
  const value = new URL(raw);
  if ((value.protocol !== "http:" && value.protocol !== "https:") || value.username || value.password || value.search || value.hash) {
    throw new Error("invalid ENGINE_BASE_URL");
  }
  return value.toString().replace(/\/$/, "");
}

export async function aggregateEngineResource(
  kind: EngineResourceKind,
  id: string,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(id)) throw new InvalidResourceIdError("invalid resource id");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const encodedID = encodeURIComponent(id);
  const headers = { authorization: `Bearer ${sessionToken}`, accept: "application/json" };
  const singular = kind === "agents" ? "agent" : "task";
  const response = await request(`${baseUrl}/v1/${kind}/${encodedID}/view`, { headers, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const view = sanitizePayload(await readEngineJSON(response));
  if (!view || typeof view !== "object" || Array.isArray(view)) throw new InvalidEngineResponseError("invalid engine view response");
  const resource = (view as Record<string, unknown>)[singular];
  const availableActions = (view as Record<string, unknown>).availableActions;
  if (!validResource(resource, id) || !validActions(availableActions, singular, id) || resource.aggregateVersion !== availableActions.aggregateVersion) {
    throw new InvalidEngineResponseError("invalid engine view snapshot");
  }
  return { status: 200, body: { [singular]: resource, availableActions } };
}

export async function aggregateEngineFinance(
  kind: EngineFinanceKind,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const response = await request(`${baseUrl}/v1/finance/${kind}`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  if (!validFinanceView(kind, value)) throw new InvalidEngineResponseError("invalid engine finance response");
  return { status: 200, body: value as Record<string, unknown> };
}

export async function aggregateEngineMatching(
  taskID: string,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!validID(taskID)) throw new InvalidResourceIdError("invalid resource id");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const response = await request(`${baseUrl}/v1/tasks/${encodeURIComponent(taskID)}/matching-view`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  if (!validMatchingView(taskID, value)) throw new InvalidEngineResponseError("invalid engine matching response");
  return { status: 200, body: value as Record<string, unknown> };
}

export async function aggregateEngineExecutions(
	taskID: string,
	sessionToken: string,
	options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
	if (!validID(taskID)) throw new InvalidResourceIdError("invalid resource id");
	if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
	const response = await (options.fetch ?? fetch)(`${options.engineBaseUrl ?? resolveEngineBaseUrl()}/v1/tasks/${encodeURIComponent(taskID)}/executions`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
	if (!response.ok) return engineError(response);
	const value = sanitizePayload(await readEngineJSON(response));
	if (!Array.isArray(value) || !value.every(validExecutionView)) throw new InvalidEngineResponseError("invalid engine execution response");
	return { status: 200, body: { executions: value } };
}

export async function aggregateEngineWorkspace(kind: EngineWorkspaceKind, sessionToken: string, options: { fetch?: typeof fetch; engineBaseUrl?: string } = {}): Promise<EngineAggregateResult> {
	if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
	const response = await (options.fetch ?? fetch)(`${options.engineBaseUrl ?? resolveEngineBaseUrl()}/v1/workspace/${kind}`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
	if (!response.ok) return engineError(response);
	const value = sanitizePayload(await readEngineJSON(response));
	const body = record(value);
	if (!body || !Array.isArray(body[kind])) throw new InvalidEngineResponseError("invalid engine workspace response");
	return { status: 200, body };
}

export async function aggregateEngineFormalDelivery(
  taskID: string,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!validID(taskID)) throw new InvalidResourceIdError("invalid resource id");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const response = await request(`${baseUrl}/v1/tasks/${encodeURIComponent(taskID)}/formal-package`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  if (!validFormalDelivery(taskID, value)) throw new InvalidEngineResponseError("invalid engine formal delivery response");
  return { status: 200, body: value as Record<string, unknown> };
}

export async function aggregateEngineDisputes(
  caseID: string | undefined,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (caseID !== undefined && !/^sha256:[0-9a-f]{64}$/.test(caseID)) throw new InvalidResourceIdError("invalid dispute id");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const path = caseID === undefined ? "/v1/disputes" : `/v1/disputes/${encodeURIComponent(caseID)}`;
  const response = await (options.fetch ?? fetch)(`${options.engineBaseUrl ?? resolveEngineBaseUrl()}${path}`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  const valid = caseID === undefined ? Boolean(record(value) && Array.isArray(record(value)?.cases) && (record(value)?.cases as unknown[]).every(validDisputeView)) : validDisputeView(value);
  if (!valid) throw new InvalidEngineResponseError("invalid engine dispute response");
  return { status: 200, body: value as Record<string, unknown> };
}

export async function forwardEngineRead(path: string, sessionToken: string, options: { fetch?: typeof fetch; engineBaseUrl?: string } = {}): Promise<EngineAggregateResult> {
  if (!/^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/selection-reservations\/sha256(?::|%3A)[0-9a-f]{64}$/.test(path)) throw new InvalidResourceIdError("invalid read path");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const response = await (options.fetch ?? fetch)(`${options.engineBaseUrl ?? resolveEngineBaseUrl()}${path}`, { headers: { authorization: `Bearer ${sessionToken}`, accept: "application/json" }, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  if (!record(value)) throw new InvalidEngineResponseError("invalid engine read response");
  return { status: 200, body: value as Record<string, unknown> };
}

export async function forwardEngineMutation(
  input: EngineMutationInput,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!validMutationPath(input.path)) throw new InvalidResourceIdError("invalid mutation path");
  if (!input.sessionToken) return { status: 401, body: { error: "unauthorized" } };
  if (!input.idempotencyKey || input.idempotencyKey.length > 200) return { status: 400, body: { error: "invalid idempotency key" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const response = await request(`${baseUrl}${input.path}`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${input.sessionToken}`,
      "content-type": "application/json",
      "idempotency-key": input.idempotencyKey,
    },
    body: input.body,
    cache: "no-store",
  });
  if (!response.ok) return engineError(response);
  const value = sanitizePayload(await readEngineJSON(response));
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new InvalidEngineResponseError("invalid engine mutation response");
  return { status: response.status, body: value as Record<string, unknown> };
}

function validMutationPath(path: string): boolean {
  if (path === "/v1/agents" || path === "/v1/tasks") return true;
	return /^\/v1\/agents\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/(?:credentials|health|prices|lifecycle)$/.test(path)
		|| /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/(?:matching-runs|overview-batches)$/.test(path)
		|| /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/overview-batches\/sha256(?::|%3A)[0-9a-f]{64}\/slots\/sha256(?::|%3A)[0-9a-f]{64}\/finalize$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/publish$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/formal-packages\/start$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/formal-feedback$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/formal-change-orders(?:\/sha256(?::|%3A)[0-9a-f]{64}\/(?:decision|accept|activate))?$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/formal-acceptance-intents(?:\/sha256(?::|%3A)[0-9a-f]{64}\/(?:submit|reconcile))?$/.test(path)
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/disputes$/.test(path)
    || /^\/v1\/disputes\/sha256(?::|%3A)[0-9a-f]{64}\/(?:claims|freeze-submission|freeze-reconcile|evidence|evidence-access|assignments|decisions|settlements|reviews|finalize)$/.test(path)
    || path === "/v1/admin/operations"
    || /^\/v1\/tasks\/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\/selection-reservations(?:\/sha256(?::|%3A)[0-9a-f]{64}\/reconcile)?$/.test(path);
}

function validExecutionView(value: unknown): boolean {
	const item = record(value);
	return Boolean(item && typeof item.logicalExecutionId === "string" && typeof item.stage === "string" && typeof item.agentId === "string" && typeof item.status === "string" && Number.isSafeInteger(item.currentAttempt) && typeof item.usedCost === "string" && typeof item.costCap === "string" && typeof item.deadline === "string" && typeof item.createdAt === "string" && typeof item.updatedAt === "string");
}

function validID(value: string): boolean { return /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(value); }

async function engineError(response: Response): Promise<EngineAggregateResult> {
  try {
    const value = await readEngineJSON(response);
    if (value && typeof value === "object" && !Array.isArray(value) && typeof (value as Record<string, unknown>).error === "string") {
      return { status: response.status, body: { error: (value as Record<string, string>).error } };
    }
  } catch {
    // Replace malformed upstream errors with a stable BFF error contract.
  }
  return { status: response.status >= 400 && response.status <= 599 ? response.status : 502, body: { error: "engine request failed" } };
}

async function readEngineJSON(response: Response): Promise<unknown> {
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > maxEngineResponseBytes) throw new InvalidEngineResponseError("engine response too large");
  if (!response.body) throw new InvalidEngineResponseError("empty engine response");
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxEngineResponseBytes) {
      await reader.cancel();
      throw new InvalidEngineResponseError("engine response too large");
    }
    chunks.push(value);
  }
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw new InvalidEngineResponseError("invalid engine encoding");
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new InvalidEngineResponseError("invalid engine JSON");
  }
}

function sanitizePayload(value: unknown, depth = 0): unknown {
  if (depth > 20) throw new InvalidEngineResponseError("engine response too deeply nested");
  if (Array.isArray(value)) return value.map((item) => sanitizePayload(item, depth + 1));
  if (!value || typeof value !== "object") return value;
  const result: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    if (!sensitiveKeys.has(key.toLowerCase())) result[key] = sanitizePayload(item, depth + 1);
  }
  return result;
}

function validResource(value: unknown, id: string): value is Record<string, unknown> & { aggregateVersion: number } {
  return Boolean(value && typeof value === "object" && !Array.isArray(value) && (value as Record<string, unknown>).id === id && Number.isSafeInteger((value as Record<string, unknown>).aggregateVersion));
}

function validActions(value: unknown, resourceType: string, id: string): value is Record<string, unknown> & { aggregateVersion: number } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const response = value as Record<string, unknown>;
  if (response.resourceType !== resourceType || response.resourceId !== id || !Number.isSafeInteger(response.aggregateVersion) || !Array.isArray(response.actions)) return false;
  return response.actions.every((item) => {
    if (!item || typeof item !== "object" || Array.isArray(item)) return false;
    const decision = item as Record<string, unknown>;
    return typeof decision.action === "string" && typeof decision.allowed === "boolean" && Array.isArray(decision.reasons) && decision.reasons.every((reason) => Boolean(reason && typeof reason === "object" && !Array.isArray(reason) && typeof (reason as Record<string, unknown>).code === "string" && typeof (reason as Record<string, unknown>).message === "string"));
  });
}

function validFinanceView(kind: EngineFinanceKind, value: unknown): value is Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value) || typeof (value as Record<string, unknown>).asOf !== "string") return false;
  const view = value as Record<string, unknown>;
  if (kind === "publisher") return validTotals(view.totals, ["discovery", "formal", "changeOrders", "disputeFees", "refundable", "refunded"]) && Array.isArray(view.tasks) && view.tasks.every(validTaskFunds) && Array.isArray(view.ledger) && view.ledger.every(validLedgerRecord);
  if (kind === "agent") return validTotals(view.totals, ["overviewReceivable", "formalClaimable", "totalAvailable"]) && Array.isArray(view.positions) && view.positions.every(validEarningPosition) && Array.isArray(view.records) && view.records.every(validLedgerRecord);
  return Array.isArray(view.runs) && view.runs.every(validReconciliationRun);
}

function validMatchingView(taskID: string, value: unknown): value is Record<string, unknown> {
  const view = record(value); const task = record(view?.task);
  if (!view || !text(view.asOf) || !task || task.id !== taskID || !text(task.title) || !text(task.status) || !text(task.specHash)) return false;
  if (view.snapshot === undefined) return true;
  const snapshot = record(view.snapshot);
  return Boolean(snapshot && text(snapshot.id) && Number.isSafeInteger(snapshot.revision) && text(snapshot.algorithmVersion) && text(snapshot.seedDigest) && Array.isArray(snapshot.degradations) && snapshot.degradations.every((item)=>{const d=record(item);return Boolean(d&&text(d.dependency)&&text(d.code)&&text(d.message));}) && Array.isArray(snapshot.candidates) && snapshot.candidates.every(validMatchingCandidate));
}

function validMatchingCandidate(value: unknown): boolean {
  const item=record(value); const score=record(item?.score);
  return Boolean(item&&text(item.agentId)&&text(item.name)&&text(item.category)&&Array.isArray(item.tags)&&item.tags.every(text)&&Number.isSafeInteger(item.position)&&typeof item.exploration==="boolean"&&money(item.overviewPrice)&&money(item.formalPrice)&&money(item.externalCostCap)&&score&&["taskMatch","reputation","priceTime","availability","rule","modelDelta","ranking"].every((key)=>Number.isSafeInteger(score[key])));
}

function validFormalDelivery(taskID: string, value: unknown): boolean {
  const view = record(value); const formalPackage = record(view?.package); const scope = record(view?.scope); const chain=record(view?.chain);
  if (!view || !formalPackage || formalPackage.taskId !== taskID || !text(formalPackage.id) || !text(formalPackage.assignmentId)
    || !Number.isSafeInteger(formalPackage.aggregateVersion) || !Number.isSafeInteger(formalPackage.allocatedVersion)
    || formalPackage.includedVersions !== 3 || formalPackage.maximumVersions !== 5 || !scope || !text(scope.id)
    || !text(scope.contentHash) || !text(scope.taskSpecHash) || !chain || !text(chain.chainId) || !/^0x[0-9a-f]{40}$/.test(String(chain.contractAddress)) || !/^0x[0-9a-f]{40}$/.test(String(chain.publisherWallet)) || !/^0x[0-9a-f]{64}$/.test(String(chain.taskId)) || !/^0x[0-9a-f]{64}$/.test(String(chain.assignmentId)) || !Number.isSafeInteger(chain.workNonce) || !Array.isArray(view.versions) || !Array.isArray(view.feedback) || !Array.isArray(view.changeOrders) || !Array.isArray(view.acceptances)) return false;
  if (!view.feedback.every(validFormalFeedback)) return false;
  if (!view.changeOrders.every(validFormalChangeOrder)) return false;
  if (!view.acceptances.every(validFormalAcceptance)) return false;
  return view.versions.every((item) => {
    const version = record(item);
    return Boolean(version && version.packageId === formalPackage.id && Number.isSafeInteger(version.number)
      && Number.isSafeInteger(version.aggregateVersion) && text(version.scopeHash) && Number.isSafeInteger(version.workNonce)
      && text(version.logicalExecutionId) && ["allocated", "generating", "review", "failed"].includes(String(version.status))
      && money(version.usedCost) && (version.proof === undefined || validFormalProof(version.proof))
      && (version.feedbackResponses === undefined || Array.isArray(version.feedbackResponses))
      && (version.changes === undefined || Array.isArray(version.changes)));
  });
}

function validFormalAcceptance(value: unknown): boolean {
  const intent=record(value); const eligibility=record(intent?.settlementEligibility);
  return Boolean(intent&&text(intent.id)&&text(intent.packageId)&&text(intent.taskId)&&Number.isSafeInteger(intent.formalVersion)
    && text(intent.contentHash)&&text(intent.proofDigest)&&Number.isSafeInteger(intent.workNonce)&&Number.isSafeInteger(intent.packageAggregateVersion)
    && Number.isSafeInteger(intent.aggregateVersion)&&["intent_recorded","pending_confirmation","confirmed","orphaned"].includes(String(intent.state))
    && text(intent.chainId)&&/^0x[0-9a-f]{40}$/.test(String(intent.contractAddress))&&/^0x[0-9a-f]{40}$/.test(String(intent.publisherWallet))&&/^0x[0-9a-f]{64}$/.test(String(intent.chainTaskId))
    && eligibility&&typeof eligibility.eligible==="boolean"&&(eligibility.reasonCode===undefined||text(eligibility.reasonCode))
    && text(intent.createdAt)&&text(intent.updatedAt));
}

function validFormalChangeOrder(value: unknown): boolean {
  const order=record(value);
  return Boolean(order&&text(order.id)&&text(order.packageId)&&text(order.taskId)&&[4,5].includes(Number(order.targetVersion))
    && Number.isSafeInteger(order.triggerVersion)&&text(order.triggerContentHash)&&text(order.feedbackSetId)&&text(order.feedbackDigest)
    && text(order.baseScopeHash)&&text(order.newSpecHash)&&text(order.differenceDigest)&&Array.isArray(order.differences)
    && money(order.requestedPrice)&&money(order.authorizedPrice)&&Number.isSafeInteger(order.packageAggregateVersion)
    && Number.isSafeInteger(order.aggregateVersion)&&["responsibility_pending","awaiting_acceptance","awaiting_funding","ready_to_activate","effective","consumed"].includes(String(order.status))&&text(order.deadline));
}

function validFormalFeedback(value: unknown): boolean {
  const feedback = record(value);
  return Boolean(feedback && text(feedback.id) && text(feedback.packageId) && Number.isSafeInteger(feedback.parentVersion)
    && text(feedback.parentContentHash) && text(feedback.scopeHash) && text(feedback.digest)
    && Number.isSafeInteger(feedback.packageAggregateVersion) && text(feedback.createdAt) && Array.isArray(feedback.items)
    && feedback.items.every((raw) => { const item=record(raw); return Boolean(item&&text(item.id)&&Number.isSafeInteger(item.ordinal)&&text(item.criterionId)&&text(item.category)&&text(item.priority)&&text(item.target)&&text(item.description)&&text(item.expectedOutcome)&&text(item.scopeClaim)); }));
}

function validFormalProof(value: unknown): boolean {
  const recordValue=record(value); const proof=record(recordValue?.proof);
  return Boolean(recordValue&&proof&&proof.version==="formal-proof-v1"&&text(recordValue.payloadHash)&&text(recordValue.digest)&&/^0x[0-9a-f]{130}$/.test(String(recordValue.signature))&&text(proof.contentHash)&&Number.isSafeInteger(proof.formalVersion)&&Number.isSafeInteger(proof.workNonce)&&Number.isSafeInteger(proof.packageAggregateVersion));
}

function validDisputeView(value: unknown): boolean {
  const view=record(value);const caseRecord=record(view?.case);const context=record(view?.context);
  return Boolean(view&&caseRecord&&context&&/^sha256:[0-9a-f]{64}$/.test(String(caseRecord.id))&&text(caseRecord.taskId)
    && text(caseRecord.assignmentId)&&text(caseRecord.deliveryUnitId)&&caseRecord.policyVersion==="platform-dispute-v1"
    && ["soft_lock_pending","frozen","evidence","decided","review_pending","final","orphaned"].includes(String(caseRecord.state))
    && Number.isSafeInteger(caseRecord.aggregateVersion)&&money(caseRecord.frozenAmount)&&text(caseRecord.asset)
    && Array.isArray(caseRecord.claims)&&Array.isArray(caseRecord.evidence)&&Array.isArray(caseRecord.assignments)&&Array.isArray(caseRecord.decisions)&&Array.isArray(caseRecord.leaves)
    && text(context.chainId)&&/^0x[0-9a-f]{40}$/.test(String(context.contractAddress))&&/^0x[0-9a-f]{64}$/.test(String(context.chainTaskId))
    && /^0x[0-9a-f]{40}$/.test(String(context.publisherWallet))&&/^0x[0-9a-f]{40}$/.test(String(context.agentController))&&/^0x[0-9a-f]{40}$/.test(String(context.agentPayout))
    && (context.disputeResolver===""||/^0x[0-9a-f]{40}$/.test(String(context.disputeResolver)))&&money(context.feeCap)
    && Array.isArray(view.accessGrants)&&Array.isArray(view.adminOperations));
}

function validTotals(value: unknown, keys: string[]): boolean {
  const totals = record(value);
  return Boolean(totals && keys.every((key) => money(totals[key])));
}

const submissionStates = new Set(["not_submitted", "submitted"]);
const confirmationStates = new Set(["not_observed", "pending", "confirmed", "failed", "orphaned"]);
const refundStates = new Set(["available", "pending", "confirmed", "unavailable"]);
function record(value: unknown): Record<string, unknown> | null { return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null; }
function text(value: unknown): value is string { return typeof value === "string"; }
function money(value: unknown): value is string { return text(value) && /^(?:0|[1-9]\d*)$/.test(value); }
function validChain(value: unknown): boolean { const chain=record(value); return Boolean(chain && text(chain.submission) && submissionStates.has(chain.submission) && text(chain.confirmation) && confirmationStates.has(chain.confirmation)); }
function validTaskFunds(value: unknown): boolean { const task=record(value); return Boolean(task && text(task.taskId) && text(task.title) && text(task.asset) && text(task.lifecycle) && money(task.discovery) && money(task.formal) && money(task.changeOrders) && money(task.disputeFees) && money(task.refundable) && text(task.refundStatus) && refundStates.has(task.refundStatus) && typeof task.terminal === "boolean" && validChain(task.chain) && (task.transactionHash === undefined || text(task.transactionHash)) && text(task.updatedAt)); }
function validLedgerRecord(value: unknown): boolean { const item=record(value); return Boolean(item && text(item.id) && (item.taskId === undefined || text(item.taskId)) && text(item.type) && money(item.amount) && text(item.asset) && text(item.reasonCode) && (item.transactionHash === undefined || text(item.transactionHash)) && text(item.createdAt)); }
function validEarningPosition(value: unknown): boolean { const item=record(value); return Boolean(item && text(item.agentId) && text(item.agentName) && /^0x[0-9a-f]{40}$/.test(String(item.controller)) && /^0x[0-9a-f]{40}$/.test(String(item.payout)) && text(item.asset) && money(item.overviewReceivable) && money(item.formalClaimable) && money(item.chainClaimable) && validChain(item.chain)); }
function validReconciliationRun(value: unknown): boolean { const run=record(value); return Boolean(run && text(run.id) && text(run.chainId) && /^0x[0-9a-f]{40}$/.test(String(run.contract)) && Number.isSafeInteger(run.safeBlock) && (run.status === "matched" || run.status === "difference_detected") && text(run.startedAt) && text(run.finishedAt) && Array.isArray(run.differences) && run.differences.every((difference)=>{const item=record(difference);return Boolean(item&&text(item.category)&&text(item.resourceId)&&text(item.expected)&&text(item.observed)&&(item.severity==="warning"||item.severity==="critical"));})); }
