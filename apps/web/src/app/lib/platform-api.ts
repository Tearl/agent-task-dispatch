import type { TaskAnalysis } from "./publisher-flow";

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
  task: { id: string; title: string; status: string; specHash: string; deletionPending: boolean };
  snapshot?: { id: string; revision: number; algorithmVersion: string; ruleVersion: string; modelVersion: string; seedDigest: string; explorationTriggered: boolean; createdAt: string; degradations: Array<{ dependency: string; code: string; message: string }>; candidates: Array<{ agentId: string; name: string; category: string; tags: string[]; estimatedDurationSeconds: number; position: number; exploration: boolean; overviewPrice: string; formalPrice: string; externalCostCap: string; score: { taskMatch: number; reputation: number; priceTime: number; availability: number; rule: number; modelDelta: number; ranking: number }; overview?: { slotId: string; logicalExecutionId: string; status: string; billingStatus: string; validationCodes: string[]; contentHash?: string; replacement: boolean } }> };
  batch?: { id: string; status: string; deadline: string; replacementUsed: boolean; replacementExhausted: boolean };
  reservation?: { id: string; agentId: string; slotId: string; status: string; transactionHash?: string };
};
export type OrchestrationPlan = { id: string; taskId: string; taskSpecHash: string; mode: "single" | "multi"; summary: string; rationale: string[]; confidence: number; steps: Array<{ id: string; title: string; objective: string; requiredCapabilities: string[]; dependsOn: string[]; output: string }>; model: string; graphVersion: string; createdAt: string };
export type ExecutionView = { logicalExecutionId: string; stage: string; agentId: string; status: string; currentAttempt: number; usedCost: string; costCap: string; contentHash?: string; deliverableRef?: string; deadline: string; createdAt: string; updatedAt: string };
export type WorkspaceTask = { id: string; title: string; category: string; status: string; overviewBudget: string; formalBudget: string; externalCostCap: string; aggregateVersion: number; deletionPending: boolean; deadline: string; createdAt: string; updatedAt: string };
export type TaskDeletion = { taskId: string; status: string; refundRequired: boolean; chainId?: string; contractAddress?: string; chainTaskId?: string; publisherWallet?: string };
export type WorkspaceAgent = { id: string; name: string; category: string; tags: string[]; capabilities: string; authorBio: string; endpointUrl?: string; status: string; health: string; healthCheckedAt?: string; healthValidUntil?: string; maxConcurrency: number; activeCapacity: number; aggregateVersion: number; estimatedDurationSeconds: number; currentPriceVersion?: number; currentCredentialVersion?: number; overviewPrice?: string; formalPrice?: string; updatedAt: string };
export type AgentProfile = WorkspaceAgent & { endpointUrl: string; languages: string[]; controllerAddress: string; payoutAddress: string };
export type WorkspaceNotification = { id: string; action: string; resourceType: string; resourceId: string; occurredAt: string };
export type OverviewBatch = { id: string; snapshotId: string; taskId: string; taskSpecHash: string; matchRevision: number; algorithmVersion: string; deadline: string; status: string; replacementUsed: boolean; replacementExhausted: boolean; slots: Array<{ id: string; logicalExecutionId: string; agentId: string; status: string; billingStatus: string; validation: { valid: boolean; codes: string[] } }> };
export type SelectionProof = { taskId: string; assignmentId: string; agentController: string; payout: string; overviewId: string; allocationId: string; quoteHash: string; taskSpecHash: string; matchRevision: number; priceVersion: number; overviewPrice: string; formalGrossPrice: string; overviewCredit: string; policyHash: string; nonce: string; deadline: number };
export type SelectionIntent = { reservation: { id: string; publisherWallet: string; taskId: string; batchId: string; slotId: string; agentId: string; chainId: string; contractAddress: string; proof: SelectionProof; formalPayable: string; status: string; transactionHash?: string }; platformSignature: string };
export type TaskFundingIntent = { id: string; taskId: string; publisherWallet: string; chainId: string; contractAddress: string; chainTaskId: string; overviewAmount: string; formalAmount: string; externalCostAmount: string; totalAmount: string; status: "prepared" | "submitted" | "confirmed" | "orphaned" | "failed"; transactionHash?: string; failureReasonCode?: string; aggregateVersion: number; createdAt: string; updatedAt: string };

export type FormalProofRecord = {
  proof: { version: string; taskId: string; assignmentId: string; deliveryUnit: string; packageId: string; scopeHash: string; formalVersion: number; packageAggregateVersion: number; workNonce: number; agentId: string; contentHash: string; parentContentHash?: string; feedbackDigest?: string; changeOrderId?: string; agentResponseHash: string; changeSummaryHash: string; policyHash: string; deadline: number };
  payloadHash: string;
  digest: string;
  signature: string;
};
export type FormalVersion = { packageId: string; number: number; aggregateVersion: number; scopeId: string; scopeHash: string; workNonce: number; revision?: { parentVersion: number; parentContentHash: string; feedbackSetId: string; feedbackDigest: string; feedbackAggregateVersion: number }; changeOrderId?: string; logicalExecutionId: string; status: "allocated" | "generating" | "review" | "failed"; contentHash?: string; deliverableRef?: string; usedCost: string; failureReasonCode?: string; feedbackResponses?: Array<{ feedbackItemId: string; disposition: string; summary: string }>; changes?: Array<{ path: string; kind: "added" | "modified" | "deleted"; beforeHash?: string; afterHash?: string }>; proof?: FormalProofRecord; createdAt: string; updatedAt: string };
export type FormalFeedback = { id: string; packageId: string; parentVersion: number; parentContentHash: string; scopeId: string; scopeHash: string; digest: string; packageAggregateVersion: number; items: Array<{ id: string; ordinal: number; criterionId: string; category: string; priority: string; target: string; description: string; expectedOutcome: string; scopeClaim: string }>; createdAt: string };
export type FormalChangeOrder = { id: string; packageId: string; taskId: string; targetVersion: number; triggerVersion: number; triggerContentHash: string; feedbackSetId: string; feedbackDigest: string; baseScopeId: string; baseScopeHash: string; newScopeId?: string; newScopeHash?: string; newSpecHash: string; differenceDigest: string; differences: Array<{ path: string; kind: "added" | "modified" | "deleted"; beforeHash?: string; afterHash?: string; description: string; workloadDeltaPercent: number }>; requestedPrice: string; authorizedPrice: string; responsibility?: "publisher" | "agent" | "platform"; responsibilityReasonCode?: string; fundingSource?: string; fundAccountId?: string; principalOwnerId?: string; residualRecipientId?: string; publisherCompensationIrrevocable: boolean; packageAggregateVersion: number; aggregateVersion: number; status: string; deadline: string; acceptedAt?: string; effectiveAt?: string; consumedAt?: string; createdAt: string; updatedAt: string };
export type FormalAcceptance = { id: string; packageId: string; taskId: string; formalVersion: number; contentHash: string; proofDigest: string; workNonce: number; packageAggregateVersion: number; aggregateVersion: number; state: "intent_recorded" | "pending_confirmation" | "confirmed" | "orphaned"; transactionHash?: string; chainEventId?: string; chainId: string; contractAddress: string; publisherWallet: string; chainTaskId: string; settlementEligibility: { eligible: boolean; reasonCode?: string }; createdAt: string; updatedAt: string };
export type FormalDeliveryView = {
  package: { id: string; taskId: string; assignmentId: string; deliveryUnit: string; kind: string; scopeId: string; scopeRevision: number; agentId: string; providerId: string; publisherId: string; includedVersions: number; maximumVersions: number; allocatedVersion: number; aggregateVersion: number; status: string; createdAt: string; updatedAt: string };
  scope: { id: string; packageId: string; revision: number; contentHash: string; taskSpecHash: string; selectedOverviewId: string; overviewHash: string; overviewRef: string; inputs: string[]; acceptanceHash: string; acceptanceCriteria: Array<Record<string, unknown>>; outputConstraints: Record<string, unknown>; allowedTools: string[]; externalCostCap: string; exclusions: string[]; changeOrderId?: string; differences?: FormalChangeOrder["differences"]; createdAt: string };
  versions: FormalVersion[];
  feedback: FormalFeedback[];
  changeOrders: FormalChangeOrder[];
  acceptances: FormalAcceptance[];
  chain: { chainId: string; contractAddress: string; publisherWallet: string; taskId: string; assignmentId: string; workNonce: number };
};
export type DisputeView = {
  case: { id: string; taskId: string; assignmentId: string; deliveryUnitId: string; policyVersion: string; publisherId: string; agentProviderId: string; state: "soft_lock_pending"|"frozen"|"evidence"|"decided"|"review_pending"|"final"|"orphaned"; aggregateVersion: number; softLockedAt?: string; freezeSubmittedAt?: string; frozenAt?: string; freezeTransactionHash?: string; freezeEventId?: string; freezeRoot?: string; frozenAmount: string; asset: string; evidenceDeadline?: string; decisionDeadline?: string; reviewDeadline?: string; claims: Array<{id:string;side:"publisher"|"agent";kind:string;reasonCode:string;statementHash:string;createdAt:string}>; evidence: Array<{id:string;claimId:string;category:string;objectKey:string;ciphertextDigest:string;envelopeKeyReference:string;objectVersionId:string;retentionMode:string;retainUntil:string;submittedBy:string;createdAt:string}>; assignments:Array<{id:string;stage:"initial"|"review";assigneeId:string;conflictCheckedAt:string;assignedAt:string}>; decisions:Array<{id:string;kind:"initial"|"review"|"settlement";decidedBy:string;reasonCode:string;evidenceRoot:string;publisherBps:number;createdAt:string}>; leaves:Array<{index:number;owner:string;account:string;cap:string;kind:string}>; reputationPending:boolean; finalizedAt?:string; createdAt:string; updatedAt:string };
  context: { taskId:string;assignmentId:string;deliveryUnitId:string;publisherId:string;agentProviderId:string;chainId:string;contractAddress:string;chainTaskId:string;publisherWallet:string;agentController:string;agentPayout:string;disputeResolver:string;frozenAmount:string;asset:string;feeCap:string;eligible:boolean;reasonCode?:string;disputeDeadline:string };
  accessGrants:Array<{id:string;evidenceId:string;principalId:string;purpose:string;expiresAt:string;createdAt:string}>;
  adminOperations:Array<{id:string;kind:string;resourceType:string;resourceId:string;reasonCode:string;payloadHash:string;actorId:string;status:string;createdAt:string}>;
};

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
export async function readOrchestrationPlan(taskID: string, request: typeof fetch = fetch) { const value = await apiRequest<{ plan: OrchestrationPlan }>(`/api/tasks/${encodeURIComponent(taskID)}/orchestration-plan`, {}, request); return value.plan; }
export async function createOrchestrationPlan(taskID: string, operationID: string, request: typeof fetch = fetch) { const value = await mutation<{ plan: OrchestrationPlan; replay: boolean }>(`/api/tasks/${encodeURIComponent(taskID)}/orchestration-plans`, operationID, {}, request); return value.plan; }
export function startMatching(taskID: string, operationID: string, request: typeof fetch = fetch) { return mutation<{ snapshotId: string; matchRevision: number; qualified: number; selected: number; replay: boolean }>(`/api/tasks/${encodeURIComponent(taskID)}/matching-runs`, operationID, {}, request); }
export function startOverview(taskID: string, operationID: string, request: typeof fetch = fetch) { return mutation<{ batch: OverviewBatch; replay: boolean }>(`/api/tasks/${encodeURIComponent(taskID)}/overview-batches`, operationID, {}, request); }
export function finalizeOverviewSlot(taskID: string, batchID: string, slotID: string, operationID: string, request: typeof fetch = fetch) { return mutation<OverviewBatch>(`/api/tasks/${encodeURIComponent(taskID)}/overview-batches/${encodeURIComponent(batchID)}/slots/${encodeURIComponent(slotID)}/finalize`, operationID, {}, request); }
export function readTaskExecutions(taskID: string, request: typeof fetch = fetch) { return apiRequest<{ executions: ExecutionView[] }>(`/api/tasks/${encodeURIComponent(taskID)}/executions`, {}, request); }
export function readWorkspaceTasks(request: typeof fetch = fetch) { return apiRequest<{ tasks: WorkspaceTask[] }>("/api/workspace/tasks", {}, request); }
export function requestTaskDeletion(taskID: string, expectedVersion: number, operationID: string, request: typeof fetch = fetch) { return mutation<TaskDeletion>(`/api/tasks/${encodeURIComponent(taskID)}/deletion-requests`, operationID, { expectedVersion }, request); }
export function readWorkspaceAgents(request: typeof fetch = fetch) { return apiRequest<{ agents: WorkspaceAgent[] }>("/api/workspace/agents", {}, request); }
export function readAgentProfile(agentID: string, request: typeof fetch = fetch) { return apiRequest<{ agent: AgentProfile; availableActions: AvailableActions }>(`/api/agents/${encodeURIComponent(agentID)}`, {}, request); }
export function updateAgentProfile(agent: AgentProfile, endpointUrl: string, operationID: string, request: typeof fetch = fetch) {
  return apiRequest<AgentProfile>(`/api/agents/${encodeURIComponent(agent.id)}/profile`, { method: "PUT", idempotencyKey: operationID, body: { name: agent.name, category: agent.category, tags: agent.tags, capabilities: agent.capabilities, languages: agent.languages, estimatedDurationSeconds: agent.estimatedDurationSeconds, authorBio: agent.authorBio, endpointUrl, controllerAddress: agent.controllerAddress, payoutAddress: agent.payoutAddress, maxConcurrency: agent.maxConcurrency, expectedVersion: agent.aggregateVersion } }, request);
}
export function readMarketplaceAgents(request: typeof fetch = fetch) { return apiRequest<{ marketplace: WorkspaceAgent[] }>("/api/workspace/marketplace", {}, request); }
export function readWorkspaceNotifications(request: typeof fetch = fetch) { return apiRequest<{ notifications: WorkspaceNotification[] }>("/api/workspace/notifications", {}, request); }
export function checkAgentHealth(agentID: string, expectedVersion: number, request: typeof fetch = fetch) { return mutation<{ aggregateVersion: number; health: string }>(`/api/agents/${encodeURIComponent(agentID)}/health`, `${agentID}:health:${crypto.randomUUID()}`, { expectedVersion }, request); }
export function prepareTaskFunding(taskID: string, operationID: string, request: typeof fetch = fetch) { return mutation<TaskFundingIntent>(`/api/tasks/${encodeURIComponent(taskID)}/funding-intents`, operationID, {}, request); }
export function readTaskFunding(taskID: string, request: typeof fetch = fetch) { return apiRequest<TaskFundingIntent>(`/api/tasks/${encodeURIComponent(taskID)}/funding-intent`, {}, request); }
export function recordTaskFundingSubmission(taskID: string, intent: TaskFundingIntent, transactionHash: string, request: typeof fetch = fetch) { return mutation<TaskFundingIntent>(`/api/tasks/${encodeURIComponent(taskID)}/funding-intents/${encodeURIComponent(intent.id)}/submit`, `${intent.id}:submit:${transactionHash.toLowerCase()}`, { transactionHash, expectedVersion: intent.aggregateVersion }, request); }
export function reserveSelection(taskID: string, batchID: string, slotID: string, operationID: string, request: typeof fetch = fetch) { return mutation<SelectionIntent>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations`, operationID, { batchId: batchID, slotId: slotID }, request); }
export function readSelection(taskID: string, reservationID: string, request: typeof fetch = fetch) { return apiRequest<SelectionIntent>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations/${encodeURIComponent(reservationID)}`, {}, request); }
export function reconcileSelection(taskID: string, reservationID: string, transactionHash: string, request: typeof fetch = fetch) { return mutation<{ reservation: SelectionIntent["reservation"]; assignment: { id: string; workNonce: number } | null }>(`/api/tasks/${encodeURIComponent(taskID)}/selection-reservations/${encodeURIComponent(reservationID)}/reconcile`, `${reservationID}:reconcile:${transactionHash.toLowerCase()}`, { transactionHash }, request); }
export function readFormalDelivery(taskID: string, request: typeof fetch = fetch) { return apiRequest<FormalDeliveryView>(`/api/tasks/${encodeURIComponent(taskID)}/formal-package`, {}, request); }
export function submitFormalFeedback(taskID: string, operationID: string, input: { packageId: string; expectedPackageVersion: number; parentVersion: number; parentContentHash: string; items: Array<{ criterionId: string; category: string; priority: string; target: string; description: string; expectedOutcome: string; scopeClaim: string }> }, request: typeof fetch = fetch) { return mutation<FormalFeedback>(`/api/tasks/${encodeURIComponent(taskID)}/formal-feedback`, operationID, input, request); }
export function proposeFormalChangeOrder(taskID: string, operationID: string, input: { packageId: string; expectedPackageVersion: number; triggerVersion: number; triggerContentHash: string; feedbackSetId: string; feedbackDigest: string; newSpecHash: string; differences: FormalChangeOrder["differences"]; requestedPrice: string; deadline: string }, request: typeof fetch = fetch) { return mutation<FormalChangeOrder>(`/api/tasks/${encodeURIComponent(taskID)}/formal-change-orders`, operationID, input, request); }
export function acceptFormalChangeOrder(taskID: string, orderID: string, expectedVersion: number, operationID: string, request: typeof fetch = fetch) { return mutation<FormalChangeOrder>(`/api/tasks/${encodeURIComponent(taskID)}/formal-change-orders/${encodeURIComponent(orderID)}/accept`, operationID, { expectedVersion }, request); }
export function activateFormalChangeOrder(taskID: string, orderID: string, expectedVersion: number, operationID: string, request: typeof fetch = fetch) { return mutation<FormalChangeOrder>(`/api/tasks/${encodeURIComponent(taskID)}/formal-change-orders/${encodeURIComponent(orderID)}/activate`, operationID, { expectedVersion }, request); }
export function startFormalVersion(taskID: string, operationID: string, input: { expectedPackageVersion: number; workNonce: number; revision?: { parentVersion: number; parentContentHash: string; feedbackSetId: string; feedbackDigest: string; feedbackAggregateVersion: number }; changeOrderId?: string }, request: typeof fetch = fetch) { return mutation<{ package: FormalDeliveryView["package"]; scope: FormalDeliveryView["scope"]; version: FormalVersion }>(`/api/tasks/${encodeURIComponent(taskID)}/formal-package/start`, operationID, input, request); }
export function createFormalAcceptance(taskID: string, operationID: string, version: FormalVersion, packageAggregateVersion: number, request: typeof fetch = fetch) {
  if (!version.contentHash || !version.proof) throw new Error("当前版本没有可验收的权威证明。");
  return mutation<FormalAcceptance>(`/api/tasks/${encodeURIComponent(taskID)}/formal-acceptance-intents`, operationID, { packageId: version.packageId, expectedPackageVersion: packageAggregateVersion, formalVersion: version.number, contentHash: version.contentHash, proofDigest: version.proof.digest, workNonce: version.workNonce }, request);
}
export function recordFormalAcceptanceSubmission(taskID: string, intent: FormalAcceptance, transactionHash: string, operationID: string, request: typeof fetch = fetch) { return mutation<FormalAcceptance>(`/api/tasks/${encodeURIComponent(taskID)}/formal-acceptance-intents/${encodeURIComponent(intent.id)}/submit`, operationID, { expectedVersion: intent.aggregateVersion, transactionHash }, request); }
export function reconcileFormalAcceptance(taskID: string, intent: FormalAcceptance, operationID: string, request: typeof fetch = fetch) { return mutation<FormalAcceptance>(`/api/tasks/${encodeURIComponent(taskID)}/formal-acceptance-intents/${encodeURIComponent(intent.id)}/reconcile`, operationID, { expectedVersion: intent.aggregateVersion }, request); }
export function readDisputes(request:typeof fetch=fetch){return apiRequest<{cases:DisputeView[]}>("/api/disputes",{},request);}
export function readDispute(caseID:string,request:typeof fetch=fetch){return apiRequest<DisputeView>(`/api/disputes/${encodeURIComponent(caseID)}`,{},request);}
export function openDisputeCase(taskID:string,operationID:string,input:{deliveryUnitId:string;kind:string;reasonCode:string;statementHash:string},request:typeof fetch=fetch){return mutation<DisputeView>(`/api/tasks/${encodeURIComponent(taskID)}/disputes`,operationID,input,request);}
export function addDisputeClaim(caseID:string,operationID:string,input:{kind:string;reasonCode:string;statementHash:string},request:typeof fetch=fetch){return disputeMutation(caseID,"claims",operationID,input,request);}
export function recordDisputeFreeze(caseID:string,operationID:string,transactionHash:string,request:typeof fetch=fetch){return disputeMutation(caseID,"freeze-submission",operationID,{transactionHash},request);}
export function reconcileDisputeFreeze(caseID:string,transactionHash:string,request:typeof fetch=fetch){return disputeMutation(caseID,"freeze-reconcile",`${caseID}:freeze:${transactionHash}`,{transactionHash},request);}
export function appendDisputeEvidence(caseID:string,operationID:string,input:{claimId:string;category:string;objectKey:string;ciphertextDigest:string;envelopeKeyReference:string;objectVersionId:string;retentionMode:"COMPLIANCE";retainUntil:string},request:typeof fetch=fetch){return disputeMutation(caseID,"evidence",operationID,input,request);}
export function requestEvidenceAccess(caseID:string,evidenceID:string,purpose:string,request:typeof fetch=fetch){return disputeMutation(caseID,"evidence-access",`${caseID}:access:${evidenceID}`,{evidenceId:evidenceID,purpose,ttlSeconds:300},request);}
export function assignDispute(caseID:string,operationID:string,input:{assigneeId:string;stage:"initial"|"review"},request:typeof fetch=fetch){return disputeMutation(caseID,"assignments",operationID,input,request);}
export function decideDispute(caseID:string,operationID:string,input:{publisherBps:number;reasonCode:string;evidenceRoot:string},request:typeof fetch=fetch){return disputeMutation(caseID,"decisions",operationID,input,request);}
export function settleDispute(caseID:string,operationID:string,input:{publisherBps:number;reasonCode:string;evidenceRoot:string;agreementHash:string;publisherSignature:string;agentSignature:string},request:typeof fetch=fetch){return disputeMutation(caseID,"settlements",operationID,input,request);}
export function reviewDispute(caseID:string,operationID:string,input:{assigneeId:string;publisherBps:number;reasonCode:string;evidenceRoot:string},request:typeof fetch=fetch){return disputeMutation(caseID,"reviews",operationID,input,request);}
export function runAdminOperation(operationID:string,input:{kind:"dlq_replay"|"ledger_reversal"|"reconciliation_repair"|"state_migration";resourceType:string;resourceId:string;reasonCode:string;payload:Record<string,unknown>},request:typeof fetch=fetch){return mutation<DisputeView>("/api/admin/operations",operationID,input,request);}

export async function submitSelectionTransaction(provider: WalletProvider, intent: SelectionIntent): Promise<string> {
  if (!intent.platformSignature || intent.reservation.status !== "reserved") throw new Error("选择证明当前不可提交。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || !/^0x[0-9a-fA-F]+$/.test(walletChain) || BigInt(walletChain).toString(10) !== intent.reservation.chainId) throw new Error(`请将钱包切换到链 ${intent.reservation.chainId}。`);
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from: intent.reservation.publisherWallet, to: intent.reservation.contractAddress, data: encodeSelectionCall(intent.reservation.proof, intent.platformSignature), value: "0x0" }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效交易哈希。");
  return transactionHash.toLowerCase();
}

export async function submitTaskFundingTransaction(provider: WalletProvider, intent: TaskFundingIntent): Promise<string> {
  if (intent.status !== "prepared" && intent.status !== "orphaned") throw new Error("当前托管意图不可提交。");
  if (!/^0x[0-9a-f]{64}$/.test(intent.chainTaskId) || !/^0x[0-9a-f]{40}$/.test(intent.contractAddress) || !/^0x[0-9a-f]{40}$/.test(intent.publisherWallet) || !/^[1-9][0-9]*$/.test(intent.totalAmount)) throw new Error("托管链上绑定无效。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || !/^0x[0-9a-fA-F]+$/.test(walletChain) || BigInt(walletChain).toString(10) !== intent.chainId) throw new Error(`请将钱包切换到链 ${intent.chainId}。`);
  const accounts = await provider.request({ method: "eth_requestAccounts" });
  const from = Array.isArray(accounts) && typeof accounts[0] === "string" ? accounts[0].toLowerCase() : "";
  if (from !== intent.publisherWallet) throw new Error("当前钱包不是任务发布钱包。");
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from, to: intent.contractAddress, data: `0xb293e81c${intent.chainTaskId.slice(2)}`, value: `0x${BigInt(intent.totalAmount).toString(16)}` }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效交易哈希。");
  return transactionHash.toLowerCase();
}

export async function submitTaskRefundTransaction(provider: WalletProvider, deletion: TaskDeletion): Promise<string> {
  if (!deletion.refundRequired || !deletion.chainId || !deletion.contractAddress || !deletion.chainTaskId || !deletion.publisherWallet) throw new Error("退款链上绑定无效。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || BigInt(walletChain).toString(10) !== deletion.chainId) throw new Error(`请将钱包切换到链 ${deletion.chainId}。`);
  const accounts = await provider.request({ method: "eth_requestAccounts" });
  const from = Array.isArray(accounts) && typeof accounts[0] === "string" ? accounts[0].toLowerCase() : "";
  if (from !== deletion.publisherWallet) throw new Error("当前钱包不是任务发布钱包。");
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from, to: deletion.contractAddress, data: `0x7249fbb6${deletion.chainTaskId.slice(2)}`, value: "0x0" }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效退款交易哈希。");
  return transactionHash.toLowerCase();
}

export async function submitFormalAcceptanceTransaction(provider: WalletProvider, intent: FormalAcceptance): Promise<string> {
  if (!intent.settlementEligibility.eligible || !["intent_recorded", "orphaned"].includes(intent.state)) throw new Error("当前验收意图没有链上结算资格。");
  if (!/^0x[0-9a-f]{64}$/.test(intent.chainTaskId) || !/^0x[0-9a-f]{40}$/.test(intent.contractAddress) || !/^0x[0-9a-f]{40}$/.test(intent.publisherWallet)) throw new Error("验收链上绑定无效。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || !/^0x[0-9a-fA-F]+$/.test(walletChain) || BigInt(walletChain).toString(10) !== intent.chainId) throw new Error(`请将钱包切换到链 ${intent.chainId}。`);
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from: intent.publisherWallet, to: intent.contractAddress, data: `0xe4725ba1${intent.chainTaskId.slice(2)}`, value: "0x0" }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效交易哈希。");
  return transactionHash.toLowerCase();
}

export async function submitWorkNonceTransaction(provider: WalletProvider, chain: FormalDeliveryView["chain"]): Promise<string> {
  if (!/^0x[0-9a-f]{64}$/.test(chain.taskId) || !/^0x[0-9a-f]{40}$/.test(chain.contractAddress) || !/^0x[0-9a-f]{40}$/.test(chain.publisherWallet)) throw new Error("工作 nonce 链上绑定无效。");
  const walletChain = await provider.request({ method: "eth_chainId" });
  if (typeof walletChain !== "string" || !/^0x[0-9a-fA-F]+$/.test(walletChain) || BigInt(walletChain).toString(10) !== chain.chainId) throw new Error(`请将钱包切换到链 ${chain.chainId}。`);
  const transactionHash = await provider.request({ method: "eth_sendTransaction", params: [{ from: chain.publisherWallet, to: chain.contractAddress, data: `0x201abd80${chain.taskId.slice(2)}`, value: "0x0" }] });
  if (typeof transactionHash !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(transactionHash)) throw new Error("钱包未返回有效交易哈希。");
  return transactionHash.toLowerCase();
}

export async function submitDisputeFreezeTransaction(provider:WalletProvider,view:DisputeView):Promise<string>{
  const chain=view.context;if(!chain.eligible||!/^0x[0-9a-f]{64}$/.test(chain.chainTaskId)||!/^0x[0-9a-f]{40}$/.test(chain.contractAddress)||!/^0x[0-9a-f]{40}$/.test(chain.publisherWallet)||!/^0x[0-9a-f]{40}$/.test(chain.agentPayout)||!/^0x[0-9a-f]{40}$/.test(chain.disputeResolver))throw new Error("争议冻结链绑定无效。");
  const walletChain=await provider.request({method:"eth_chainId"});if(typeof walletChain!=="string"||BigInt(walletChain).toString(10)!==chain.chainId)throw new Error(`请将钱包切换到链 ${chain.chainId}。`);
  const accounts=await provider.request({method:"eth_requestAccounts"});const from=Array.isArray(accounts)&&typeof accounts[0]==="string"?accounts[0].toLowerCase():"";if(from!==chain.publisherWallet&&from!==chain.agentController)throw new Error("当前钱包不是争议当事方。");
  const head=[normalizeWord(chain.chainTaskId),uintWord(128),addressWord(chain.disputeResolver),uintWord(chain.feeCap)];const leaves=[uintWord(2),uintWord(0),addressWord(chain.publisherWallet),uintWord(chain.frozenAmount),uintWord(0),uintWord(1),addressWord(chain.agentPayout),uintWord(chain.frozenAmount),uintWord(1)];
  const transactionHash=await provider.request({method:"eth_sendTransaction",params:[{from,to:chain.contractAddress,data:`0xee1e9b21${head.join("")}${leaves.join("")}`,value:"0x0"}]});if(typeof transactionHash!=="string"||!/^0x[0-9a-fA-F]{64}$/.test(transactionHash))throw new Error("钱包未返回有效交易哈希。");return transactionHash.toLowerCase();
}

export async function sha256Digest(value: unknown): Promise<string> {
  const encoded = new TextEncoder().encode(typeof value === "string" ? value : JSON.stringify(value));
  const digest = await crypto.subtle.digest("SHA-256", encoded);
  return `sha256:${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
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
  bearerToken: string;
  callbackKeyBase64: string;
  callbackKeyVersion: string;
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
    credentialType: "protocol_bundle",
    label: "agent-execution-v1 transport bundle",
    secret: JSON.stringify({ bearerToken: input.bearerToken, callbackKeyBase64: input.callbackKeyBase64, callbackKeyVersion: input.callbackKeyVersion }),
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
  tags?: string[];
  amount: string;
  deadline: string;
  criteria: string[];
};

export type TaskAnalysisRequest = {
  prompt: string;
  category?: string | null;
  depth?: string;
  currentAnalysis?: TaskAnalysis;
  instruction?: string;
};

export function analyzePublisherTask(input: TaskAnalysisRequest, request: typeof fetch = fetch) {
  return apiRequest<{ analysis: TaskAnalysis; model: string }>("/api/task-analysis", {
    method: "POST",
    body: {
      prompt: input.prompt,
      ...(input.category ? { category: input.category } : {}),
      ...(input.depth ? { depth: input.depth } : {}),
      ...(input.currentAnalysis ? { currentAnalysis: input.currentAnalysis } : {}),
      ...(input.instruction ? { instruction: input.instruction } : {}),
    },
  }, request);
}

export async function createAndPublishTask(input: TaskPublishInput, request: typeof fetch = fetch) {
  validateTaskPublishInput(input);
  const criteria = input.criteria.filter((item) => item.trim());
  const weightBase = Math.floor(100 / criteria.length);
  const draft = await mutation<{ id: string; aggregateVersion: number }>("/api/tasks", `${input.operationId}:create`, {
    title: input.title,
    description: input.description,
    expertType: input.category,
    tags: input.tags ?? [],
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
  try { endpoint = new URL(input.endpointUrl); } catch { throw new Error("协议基础地址必须是有效的 HTTPS URL。"); }
  if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || (endpoint.pathname !== "/" && endpoint.pathname !== "") || input.endpointUrl.length > 2_048) throw new Error("协议基础地址必须是不含凭证、路径、查询或片段的 HTTPS URL。");
  if (!/^0x[0-9a-fA-F]{40}$/.test(input.controllerAddress)) throw new Error("当前会话钱包地址无效。");
  if (!Number.isInteger(input.maxConcurrency) || input.maxConcurrency < 1 || input.maxConcurrency > 10_000) throw new Error("并发上限必须为 1–10000。");
  if (input.capabilities.length === 0 || input.capabilities.length > 50 || input.capabilities.some((item) => !item.trim() || item.length > 2_000)) throw new Error("请提供有效的能力标签。");
  if (!input.bearerToken || input.bearerToken.length > 4_096 || /[\r\n]/.test(input.bearerToken)) throw new Error("Bearer Token 无效。");
  let callbackKey: Uint8Array;
  try { callbackKey = Uint8Array.from(atob(input.callbackKeyBase64), (character) => character.charCodeAt(0)); } catch { throw new Error("回调密钥必须是有效 Base64。"); }
  if (callbackKey.length !== 32 || !input.callbackKeyVersion.trim() || input.callbackKeyVersion.length > 128) throw new Error("回调密钥必须是 Base64 编码的 32 字节值，并提供版本。");
  if (!canonicalAmount(input.overviewPrice) || !canonicalAmount(input.formalPrice) || BigInt(input.overviewPrice) > BigInt(input.formalPrice)) throw new Error("概览价格必须是非负整数且不得高于正式套餐总价。");
}

export function validateTaskPublishInput(input: TaskPublishInput): void {
  if (!input.operationId || !input.title.trim() || input.title.length > 200 || !input.description.trim() || input.description.length > 50_000) throw new Error("任务标题或描述不完整或过长。");
  if (!canonicalAmount(input.amount)) throw new Error("预算必须是不含前导零的非负整数。");
  if ((input.tags?.length ?? 0) > 50 || input.tags?.some((tag) => !tag.trim() || tag.length > 100)) throw new Error("任务标签必须为不超过 50 个有效短标签。");
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
function disputeMutation(caseID:string,action:string,operationID:string,body:unknown,request:typeof fetch){return mutation<DisputeView>(`/api/disputes/${encodeURIComponent(caseID)}/${action}`,operationID,body,request);}

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
