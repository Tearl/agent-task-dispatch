import assert from "node:assert/strict";
import test from "node:test";
import { ActionBlockedError, analyzePublisherTask, authenticateWallet, clientRolesForEngineRoles, createAndPublishTask, createFormalAcceptance, createOrchestrationPlan, onboardAgent, readAgentFinance, readAgentProfile, readFormalDelivery, readOrchestrationPlan, readPublisherFinance, readReconciliationFinance, recordFormalAcceptanceSubmission, requestTaskDeletion, requireAllowed, revokeSession, startFormalVersion, submitDisputeFreezeTransaction, submitFormalAcceptanceTransaction, submitSelectionTransaction, submitTaskFundingTransaction, submitTaskRefundTransaction, submitWorkNonceTransaction, updateAgentProfile, type AgentProfile, type DisputeView, type FormalAcceptance, type FormalVersion, type SelectionIntent, type TaskFundingIntent } from "./platform-api.ts";

test("orchestration planning stays same-origin and preserves idempotency", async () => {
  const calls: Array<{ url: string; method: string; key: string | null }> = [];
  const plan = { id: `sha256:${"1".repeat(64)}`, taskId: "task-1", taskSpecHash: `sha256:${"2".repeat(64)}`, mode: "multi", summary: "two stages", rationale: ["dependency"], confidence: .9, steps: [{ id: "step-1", title: "Research", objective: "collect", requiredCapabilities: ["research"], dependsOn: [], output: "data" }, { id: "step-2", title: "Analyze", objective: "analyze", requiredCapabilities: ["analysis"], dependsOn: ["step-1"], output: "report" }], model: "local", graphVersion: "langgraph-v1", createdAt: "2026-08-25T00:00:00Z" } as const;
  const request: typeof fetch = async (input, init) => { calls.push({ url: String(input), method: init?.method ?? "GET", key: new Headers(init?.headers).get("idempotency-key") }); return Response.json({ plan, replay: false }, { status: init?.method === "POST" ? 201 : 200 }); };
  assert.equal((await createOrchestrationPlan("task-1", "op-1", request)).mode, "multi");
  assert.equal((await readOrchestrationPlan("task-1", request)).steps[1]?.dependsOn[0], "step-1");
  assert.deepEqual(calls, [{ url: "/api/tasks/task-1/orchestration-plans", method: "POST", key: "op-1" }, { url: "/api/tasks/task-1/orchestration-plan", method: "GET", key: null }]);
});

test("publisher analysis stays same-origin and sends refinement context", async () => {
  let call: { url: string; body: Record<string, unknown> } | undefined;
  const analysis = { title: "Task", summary: "Summary", category: "数据分析", depth: "深度", budget: 100, deliveryDays: 2, tags: ["data"], deliverables: ["table"], acceptanceCriteria: ["complete"], risk: "none" };
  const request: typeof fetch = async (input, init) => {
    call = { url: String(input), body: JSON.parse(String(init?.body)) as Record<string, unknown> };
    return Response.json({ analysis: { ...analysis, budget: 150 }, model: "deepseek-v4-flash" });
  };
  const result = await analyzePublisherTask({ prompt: "Analyze", currentAnalysis: analysis, instruction: "Increase budget" }, request);
  assert.equal(call?.url, "/api/task-analysis");
  assert.deepEqual(call?.body.currentAnalysis, analysis);
  assert.equal(call?.body.instruction, "Increase budget");
  assert.equal(result.analysis.budget, 150);
});

test("wallet authentication requests a nonce, signs it, and creates an HttpOnly-backed session", async () => {
  const walletCalls: unknown[] = [];
  const provider = { request: async (input: { method: string; params?: unknown[] }) => {
    walletCalls.push(input);
    return input.method === "eth_requestAccounts" ? ["0x1111111111111111111111111111111111111111"] : "0xsigned";
  } };
  const calls: Array<{ url: string; body: unknown }> = [];
  const request: typeof fetch = async (input, init) => {
    calls.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) : null });
    return String(input).endsWith("nonce") ? Response.json({ message: "challenge" }) : Response.json({ sessionId: "s", userId: "u", walletAddress: "0x1", roles: ["publisher"], expiresAt: "2026-08-22T00:00:00Z" }, { status: 201 });
  };
  const session = await authenticateWallet(provider, request);
  assert.equal(session.sessionId, "s");
  assert.deepEqual(walletCalls, [{ method: "eth_requestAccounts" }, { method: "personal_sign", params: ["challenge", "0x1111111111111111111111111111111111111111"] }]);
  assert.deepEqual(calls.map((call) => call.url), ["/api/auth/nonce", "/api/auth/verify"]);
});

test("client roles are derived only from authoritative session roles", () => {
  assert.deepEqual(clientRolesForEngineRoles(["publisher", "agent_provider", "admin"]), ["publisher", "agent", "admin"]);
  assert.deepEqual(clientRolesForEngineRoles([]), []);
});

test("finance reads stay same-origin and preserve orthogonal presentation states", async () => {
  const calls:string[]=[];
  const request:typeof fetch=async(input)=>{calls.push(String(input));return Response.json({asOf:"2026-08-22T00:00:00Z",totals:{},tasks:[{chain:{submission:"submitted",confirmation:"pending"},refundStatus:"available",terminal:false}],ledger:[],positions:[],records:[],runs:[]})};
  const publisher=await readPublisherFinance(request);
  await readAgentFinance(request); await readReconciliationFinance(request);
  assert.deepEqual(calls,["/api/finance/publisher","/api/finance/agent","/api/finance/reconciliation"]);
  assert.deepEqual(publisher.tasks[0]?.chain,{submission:"submitted",confirmation:"pending"});
  assert.equal(publisher.tasks[0]?.refundStatus,"available"); assert.equal(publisher.tasks[0]?.terminal,false);
});

test("selection transaction encodes the frozen proof and never asks the wallet to sign new fields", async () => {
  const calls: unknown[] = [];
  const word = `0x${"11".repeat(32)}`;
  const intent: SelectionIntent = { reservation: { id: "sha256:r", publisherWallet: "0x2222222222222222222222222222222222222222", taskId: "task-1", batchId: "sha256:b", slotId: "sha256:s", agentId: "agent-1", chainId: "1", contractAddress: "0x3333333333333333333333333333333333333333", formalPayable: "90", status: "reserved", proof: { taskId: word, assignmentId: word, agentController: "0x4444444444444444444444444444444444444444", payout: "0x5555555555555555555555555555555555555555", overviewId: word, allocationId: word, quoteHash: word, taskSpecHash: word, matchRevision: 1, priceVersion: 1, overviewPrice: "10", formalGrossPrice: "100", overviewCredit: "10", policyHash: word, nonce: word, deadline: 2_000_000_000 } }, platformSignature: `0x${"66".repeat(65)}` };
  const hash = await submitSelectionTransaction({ request: async (input) => { calls.push(input); return input.method === "eth_chainId" ? "0x1" : `0x${"77".repeat(32)}`; } }, intent);
  assert.equal(hash, `0x${"77".repeat(32)}`);
  assert.deepEqual(calls[0], { method: "eth_chainId" });
  const call = calls[1] as { method: string; params: Array<{ data: string; to: string }> };
  assert.equal(call.method, "eth_sendTransaction");
  assert.equal(call.params[0]?.to, intent.reservation.contractAddress);
  assert.match(call.params[0]?.data ?? "", /^0xa2dfc191[0-9a-f]+$/);
  const data = call.params[0]?.data ?? "";
  assert.equal(data.slice(10 + 16 * 64, 10 + 17 * 64), (17 * 32).toString(16).padStart(64, "0"));
  assert.equal(data.slice(10 + 17 * 64, 10 + 18 * 64), (65).toString(16).padStart(64, "0"));
  assert.equal(data.length, 2 + 8 + 21 * 64);
});

test("task funding transaction binds chain, publisher, contract, task id and exact value", async () => {
  const calls: Array<{ method: string; params?: unknown[] }> = [];
  const provider = { request: async (input: { method: string; params?: unknown[] }) => {
    calls.push(input);
    if (input.method === "eth_chainId") return "0x7a69";
    if (input.method === "eth_requestAccounts") return ["0x1111111111111111111111111111111111111111"];
    return `0x${"9".repeat(64)}`;
  } };
  const intent: TaskFundingIntent = { id:`sha256:${"1".repeat(64)}`,taskId:"task-1",publisherWallet:"0x1111111111111111111111111111111111111111",chainId:"31337",contractAddress:"0x2222222222222222222222222222222222222222",chainTaskId:`0x${"3".repeat(64)}`,overviewAmount:"10",formalAmount:"90",externalCostAmount:"5",totalAmount:"105",status:"prepared",aggregateVersion:1,createdAt:"2026-01-01T00:00:00Z",updatedAt:"2026-01-01T00:00:00Z" };
  await submitTaskFundingTransaction(provider,intent);
  const transaction = calls.find((call)=>call.method==="eth_sendTransaction")?.params?.[0] as Record<string,string>;
  assert.equal(transaction.to,intent.contractAddress);
  assert.equal(transaction.from,intent.publisherWallet);
  assert.equal(transaction.value,"0x69");
  assert.equal(transaction.data,`0xb293e81c${intent.chainTaskId.slice(2)}`);
});

test("task deletion stays same-origin and escrowed deletion uses the refund selector", async () => {
  const calls: Array<{ url: string; key: string | null; body: string }> = [];
  const deletion = { taskId: "task-1", status: "escrowed", refundRequired: true, chainId: "31337", contractAddress: "0x3333333333333333333333333333333333333333", chainTaskId: `0x${"4".repeat(64)}`, publisherWallet: "0x2222222222222222222222222222222222222222" };
  const request: typeof fetch = async (input, init) => { calls.push({ url: String(input), key: new Headers(init?.headers).get("idempotency-key"), body: String(init?.body) }); return Response.json(deletion, { status: 201 }); };
  assert.equal((await requestTaskDeletion("task-1", 3, "delete-op", request)).refundRequired, true);
  assert.deepEqual(calls, [{ url: "/api/tasks/task-1/deletion-requests", key: "delete-op", body: JSON.stringify({ expectedVersion: 3 }) }]);
  const walletCalls: Array<{ method: string; params?: unknown[] }> = [];
  const provider = { request: async (input: { method: string; params?: unknown[] }) => { walletCalls.push(input); if (input.method === "eth_chainId") return "0x7a69"; if (input.method === "eth_requestAccounts") return [deletion.publisherWallet]; return `0x${"5".repeat(64)}`; } };
  await submitTaskRefundTransaction(provider, deletion);
  const transaction = walletCalls.find((call) => call.method === "eth_sendTransaction")?.params?.[0] as Record<string, string>;
  assert.equal(transaction.data, `0x7249fbb6${deletion.chainTaskId.slice(2)}`);
  assert.equal(transaction.to, deletion.contractAddress);
});

test("formal acceptance and work nonce calls use only authoritative chain bindings", async () => {
  const calls: unknown[]=[];
  const provider={request:async(input:{method:string;params?:unknown[]})=>{calls.push(input);return input.method==="eth_chainId"?"0x1":`0x${"77".repeat(32)}`;}};
  const intent:FormalAcceptance={id:`sha256:${"1".repeat(64)}`,packageId:`sha256:${"2".repeat(64)}`,taskId:"task-1",formalVersion:1,contentHash:`sha256:${"3".repeat(64)}`,proofDigest:`sha256:${"4".repeat(64)}`,workNonce:1,packageAggregateVersion:2,aggregateVersion:1,state:"intent_recorded",chainId:"1",contractAddress:`0x${"5".repeat(40)}`,publisherWallet:`0x${"6".repeat(40)}`,chainTaskId:`0x${"7".repeat(64)}`,settlementEligibility:{eligible:true},createdAt:"2026-08-23T00:00:00Z",updatedAt:"2026-08-23T00:00:00Z"};
  assert.equal(await submitFormalAcceptanceTransaction(provider,intent),`0x${"77".repeat(32)}`);
  const acceptanceCall=calls[1] as {method:string;params:Array<{data:string;from:string;to:string}>};
  assert.equal(acceptanceCall.method,"eth_sendTransaction"); assert.equal(acceptanceCall.params[0]?.data,`0xe4725ba1${"7".repeat(64)}`); assert.equal(acceptanceCall.params[0]?.from,intent.publisherWallet);
  calls.length=0;
  await submitWorkNonceTransaction(provider,{chainId:"1",contractAddress:intent.contractAddress,publisherWallet:intent.publisherWallet,taskId:intent.chainTaskId,assignmentId:`0x${"8".repeat(64)}`,workNonce:1});
  const nonceCall=calls[1] as {params:Array<{data:string}>}; assert.equal(nonceCall.params[0]?.data,`0x201abd80${"7".repeat(64)}`);
});

test("dispute freeze calldata binds complete stable leaves and resolver fee cap",async()=>{
  const publisher=`0x${"2".repeat(40)}`,controller=`0x${"3".repeat(40)}`,payout=`0x${"4".repeat(40)}`,resolver=`0x${"5".repeat(40)}`,contract=`0x${"6".repeat(40)}`,task=`0x${"7".repeat(64)}`;const calls:unknown[]=[];
  const provider={request:async(input:{method:string;params?:unknown[]})=>{calls.push(input);if(input.method==="eth_chainId")return "0x1";if(input.method==="eth_requestAccounts")return [publisher];return `0x${"8".repeat(64)}`;}};
  const view={case:{state:"soft_lock_pending"},context:{eligible:true,chainId:"1",contractAddress:contract,chainTaskId:task,publisherWallet:publisher,agentController:controller,agentPayout:payout,disputeResolver:resolver,frozenAmount:"100",feeCap:"5"}} as unknown as DisputeView;
  assert.equal(await submitDisputeFreezeTransaction(provider,view),`0x${"8".repeat(64)}`);const sent=calls[2] as {params:Array<{data:string;from:string;to:string}>};const data=sent.params[0]!.data;assert.match(data,/^0xee1e9b21[0-9a-f]+$/);assert.equal(sent.params[0]!.from,publisher);assert.equal(sent.params[0]!.to,contract);assert.equal(data.slice(10+64,10+128),(128).toString(16).padStart(64,"0"));assert.equal(data.slice(10+4*64,10+5*64),(2).toString(16).padStart(64,"0"));assert.equal(data.length,10+13*64);
});

test("formal delivery mutations stay task-bound and preserve caller idempotency", async () => {
  const calls:Array<{url:string;key:string|null;body:Record<string,unknown>|null}>=[];
  const request:typeof fetch=async(input,init)=>{calls.push({url:String(input),key:new Headers(init?.headers).get("idempotency-key"),body:init?.body?JSON.parse(String(init.body)) as Record<string,unknown>:null});return Response.json({id:`sha256:${"1".repeat(64)}`,aggregateVersion:1,state:"intent_recorded"},{status:201});};
  await readFormalDelivery("task-1",request);
  const proofDigest=`sha256:${"4".repeat(64)}`;
  const version={packageId:`sha256:${"2".repeat(64)}`,number:1,aggregateVersion:2,scopeId:`sha256:${"3".repeat(64)}`,scopeHash:`sha256:${"3".repeat(64)}`,workNonce:1,logicalExecutionId:`sha256:${"5".repeat(64)}`,status:"review",contentHash:`sha256:${"6".repeat(64)}`,usedCost:"0",createdAt:"2026-08-23T00:00:00Z",updatedAt:"2026-08-23T00:00:00Z",proof:{proof:{version:"formal-proof-v1",taskId:"task-1",assignmentId:"assignment",deliveryUnit:"default",packageId:`sha256:${"2".repeat(64)}`,scopeHash:`sha256:${"3".repeat(64)}`,formalVersion:1,packageAggregateVersion:2,workNonce:1,agentId:"agent",contentHash:`sha256:${"6".repeat(64)}`,agentResponseHash:proofDigest,changeSummaryHash:proofDigest,policyHash:proofDigest,deadline:1},payloadHash:proofDigest,digest:proofDigest,signature:`0x${"a".repeat(130)}`}} satisfies FormalVersion;
  const intent=await createFormalAcceptance("task-1","accept:create",version,2,request);
  await recordFormalAcceptanceSubmission("task-1",{...intent,id:`sha256:${"1".repeat(64)}`,aggregateVersion:1},`0x${"7".repeat(64)}`,"accept:submit",request);
  await startFormalVersion("task-1","revision:start",{expectedPackageVersion:3,workNonce:2},request);
  assert.deepEqual(calls.map((call)=>[call.url,call.key]),[["/api/tasks/task-1/formal-package",null],["/api/tasks/task-1/formal-acceptance-intents","accept:create"],[`/api/tasks/task-1/formal-acceptance-intents/${encodeURIComponent(`sha256:${"1".repeat(64)}`)}/submit`,"accept:submit"],["/api/tasks/task-1/formal-package/start","revision:start"]]);
});

test("wallet errors stop before nonce issuance", async () => {
  let requested = false;
  await assert.rejects(() => authenticateWallet({ request: async () => [] }, async () => { requested = true; return Response.json({}); }), /有效账户/);
  assert.equal(requested, false);
});

test("blocked actions preserve Engine reasons", () => {
  assert.throws(() => requireAllowed({ aggregateVersion: 2, actions: [{ action: "publish", allowed: false, reasons: [{ code: "deadline_expired", message: "Set a future deadline." }] }] }, "publish"), (error) => error instanceof ActionBlockedError && error.reasons[0]?.code === "deadline_expired");
});

test("task publishing uses one operation id and Engine publication eligibility", async () => {
  const calls: Array<{ url: string; key: string | null; body?: Record<string, unknown> }> = [];
  const request: typeof fetch = async (input, init) => {
    const url = String(input); calls.push({ url, key: new Headers(init?.headers).get("idempotency-key"), ...(init?.body ? { body: JSON.parse(String(init.body)) as Record<string, unknown> } : {}) });
    if (url === "/api/tasks") return Response.json({ id: "task-1", aggregateVersion: 1 }, { status: 201 });
    if (url === "/api/tasks/task-1") return Response.json({ task: { aggregateVersion: 1 }, availableActions: { aggregateVersion: 1, actions: [{ action: "publish", allowed: true, reasons: [] }] } });
    return Response.json({ task: { id: "task-1", status: "pending_escrow" }, spec: { contentHash: "sha256:spec" }, acceptance: { contentHash: "sha256:acceptance" } }, { status: 201 });
  };
  await createAndPublishTask({ operationId: "op-1", title: "Task", description: "Description", category: "research", tags: ["analysis", "report"], amount: "10", deadline: "2026-09-01", criteria: ["Correct", "Complete"] }, request);
  assert.deepEqual(calls.map(({ url, key }) => ({ url, key })), [{ url: "/api/tasks", key: "op-1:create" }, { url: "/api/tasks/task-1", key: null }, { url: "/api/tasks/task-1/publish", key: "op-1:publish" }]);
  assert.deepEqual(calls[0]?.body?.tags, ["analysis", "report"]);
});

test("Agent onboarding uses stable stage keys and follows Engine activation decision", async () => {
  const calls: Array<{ url: string; key: string | null; body: Record<string, unknown> | null }> = [];
  const request: typeof fetch = async (input, init) => {
    const url = String(input); calls.push({ url, key: new Headers(init?.headers).get("idempotency-key"), body: init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : null });
    if (url === "/api/agents") return Response.json({ id: "agent-1", aggregateVersion: 1 }, { status: 201 });
    if (url.endsWith("credentials")) return Response.json({ agentAggregateVersion: 2 }, { status: 201 });
    if (url.endsWith("prices")) return Response.json({ agentAggregateVersion: 3 }, { status: 201 });
    if (url.endsWith("health")) return Response.json({ aggregateVersion: 4 }, { status: 200 });
    if (url === "/api/agents/agent-1" && calls.filter((call) => call.url === url).length === 1) return Response.json({ agent: { id: "agent-1", aggregateVersion: 3, status: "draft" }, availableActions: { aggregateVersion: 3, actions: [{ action: "activate", allowed: false, reasons: [{ code: "healthy_status_required", message: "Health required." }, { code: "health_check_expired", message: "Health expired." }] }] } });
    if (url === "/api/agents/agent-1") return Response.json({ agent: { id: "agent-1", aggregateVersion: 4, status: "draft" }, availableActions: { aggregateVersion: 4, actions: [{ action: "activate", allowed: true, reasons: [] }] } });
    return Response.json({ id: "agent-1", status: "active", aggregateVersion: 5 });
  };
  await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "10", formalPrice: "20" }, request);
  assert.deepEqual(calls.map((call) => call.key), ["agent-op:create", "agent-op:credential", "agent-op:price", null, "agent-op:health:3", null, "agent-op:activate"]);
  assert.equal(calls[0]?.body?.controllerAddress, "0x1111111111111111111111111111111111111111");
  assert.equal(calls[0]?.body?.payoutAddress, "0x1111111111111111111111111111111111111111");
  assert.equal(calls[0]?.body?.endpointUrl, "https://agent.example");
  assert.equal(calls[2]?.body?.overviewPrice, "10");
  assert.equal(calls[2]?.body?.formalPackageGrossPrice, "20");
});

test("Agent endpoint updates preserve the complete profile and use a version-bound PUT", async () => {
  const profile = { id: "agent-1", name: "Agent", category: "image", tags: ["image"], capabilities: "Generate images", authorBio: "Image Agent", endpointUrl: "https://old.example", status: "active", health: "unhealthy", maxConcurrency: 1, activeCapacity: 0, aggregateVersion: 4, estimatedDurationSeconds: 3600, updatedAt: "2026-08-27T00:00:00Z", languages: ["zh-CN"], controllerAddress: `0x${"1".repeat(40)}`, payoutAddress: `0x${"2".repeat(40)}` } satisfies AgentProfile;
  const calls: Array<{ url: string; method: string; key: string | null; body?: Record<string, unknown> }> = [];
  const request: typeof fetch = async (input, init) => { calls.push({ url: String(input), method: init?.method ?? "GET", key: new Headers(init?.headers).get("idempotency-key"), ...(init?.body ? { body: JSON.parse(String(init.body)) as Record<string, unknown> } : {}) }); return String(input).endsWith("/profile") ? Response.json({ ...profile, endpointUrl: "https://new.example", aggregateVersion: 5 }) : Response.json({ agent: profile, availableActions: { aggregateVersion: 4, actions: [] } }); };
  const view = await readAgentProfile(profile.id, request);
  const updated = await updateAgentProfile(view.agent, "https://new.example", "profile-op", request);
  assert.equal(updated.endpointUrl, "https://new.example");
  assert.deepEqual(calls.map(({ url, method, key }) => ({ url, method, key })), [{ url: "/api/agents/agent-1", method: "GET", key: null }, { url: "/api/agents/agent-1/profile", method: "PUT", key: "profile-op" }]);
  assert.equal(calls[1]?.body?.expectedVersion, 4);
  assert.equal(calls[1]?.body?.controllerAddress, profile.controllerAddress);
});

test("task flow does not publish when Engine blocks the action", async () => {
  const calls: string[] = [];
  const request: typeof fetch = async (input) => {
    const url = String(input); calls.push(url);
    if (url === "/api/tasks") return Response.json({ id: "task-1", aggregateVersion: 1 }, { status: 201 });
    return Response.json({ task: { aggregateVersion: 1 }, availableActions: { aggregateVersion: 1, actions: [{ action: "publish", allowed: false, reasons: [{ code: "deadline_expired", message: "Deadline expired." }] }] } });
  };
  await assert.rejects(() => createAndPublishTask({ operationId: "op", title: "Task", description: "Description", category: "research", amount: "10", deadline: "2026-09-01", criteria: ["Correct"] }, request), (error) => error instanceof ActionBlockedError && error.reasons[0]?.code === "deadline_expired");
  assert.deepEqual(calls, ["/api/tasks", "/api/tasks/task-1"]);
});

test("task flow replays a lost successful publication response", async () => {
  const calls: string[] = [];
  const request: typeof fetch = async (input) => {
    const url = String(input); calls.push(url);
    if (url === "/api/tasks") return Response.json({ id: "task-1", aggregateVersion: 1 }, { status: 201 });
    if (url === "/api/tasks/task-1") return Response.json({ task: { aggregateVersion: 2, status: "pending_escrow" }, availableActions: { aggregateVersion: 2, actions: [{ action: "publish", allowed: false, reasons: [{ code: "task_not_draft", message: "Already published." }] }] } });
    return Response.json({ task: { id: "task-1", status: "pending_escrow" }, spec: { contentHash: "sha256:spec" }, acceptance: { contentHash: "sha256:acceptance" } }, { status: 201 });
  };
  const result = await createAndPublishTask({ operationId: "op", title: "Task", description: "Description", category: "research", amount: "10", deadline: "2026-09-01", criteria: ["Correct"] }, request);
  assert.equal(result.spec.contentHash, "sha256:spec");
  assert.deepEqual(calls, ["/api/tasks", "/api/tasks/task-1", "/api/tasks/task-1/publish"]);
});

test("Agent flow recovers a lost successful activation response from the authoritative view", async () => {
  const calls: string[] = [];
  const request: typeof fetch = async (input) => {
    const url = String(input); calls.push(url);
    if (url === "/api/agents") return Response.json({ id: "agent-1", aggregateVersion: 1 }, { status: 201 });
    if (url.endsWith("credentials")) return Response.json({ agentAggregateVersion: 2 }, { status: 201 });
    if (url.endsWith("prices")) return Response.json({ agentAggregateVersion: 3 }, { status: 201 });
    if (url === "/api/agents/agent-1") return Response.json({ agent: { id: "agent-1", aggregateVersion: 5, status: "active" }, availableActions: { aggregateVersion: 5, actions: [{ action: "activate", allowed: false, reasons: [{ code: "activation_transition_not_allowed", message: "Already active." }] }] } });
    throw new Error(`unexpected request: ${url}`);
  };
  const result = await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "10", formalPrice: "20" }, request);
  assert.equal(result.status, "active");
  assert.equal(calls.at(-1), "/api/agents/agent-1");
});

test("Agent flow refreshes an expired health check with a version-bound idempotency key", async () => {
  const calls: Array<{ url: string; key: string | null; body: Record<string, unknown> | null }> = [];
  let views = 0;
  const request: typeof fetch = async (input, init) => {
    const url = String(input);
    calls.push({ url, key: new Headers(init?.headers).get("idempotency-key"), body: init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : null });
    if (url === "/api/agents") return Response.json({ id: "agent-1", aggregateVersion: 1 }, { status: 201 });
    if (url.endsWith("credentials")) return Response.json({ agentAggregateVersion: 2 }, { status: 201 });
    if (url.endsWith("prices")) return Response.json({ agentAggregateVersion: 3 }, { status: 201 });
    if (url === "/api/agents/agent-1") {
      views += 1;
      const version = views === 1 ? 4 : 5;
      return Response.json({ agent: { id: "agent-1", aggregateVersion: version, status: "draft" }, availableActions: { aggregateVersion: version, actions: [{ action: "activate", allowed: views > 1, reasons: views === 1 ? [{ code: "health_check_expired", message: "Health expired." }] : [] }] } });
    }
    if (url.endsWith("health")) return Response.json({ aggregateVersion: 5 });
    return Response.json({ id: "agent-1", status: "active", aggregateVersion: 6 });
  };
  await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "10", formalPrice: "20" }, request);
  const health = calls.find((call) => call.url.endsWith("health"));
  assert.equal(health?.key, "agent-op:health:4");
  assert.equal(health?.body?.expectedVersion, 4);
  assert.equal(health?.body?.health, undefined);
});

test("invalid correctable inputs fail before any mutation", async () => {
  let calls = 0;
  const request: typeof fetch = async () => { calls += 1; return Response.json({}); };
  await assert.rejects(() => createAndPublishTask({ operationId: "op", title: "Task", description: "Description", category: "research", amount: "01", deadline: "2026-09-01", criteria: ["Correct"] }, request), /前导零/);
  await assert.rejects(() => onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "30", formalPrice: "20" }, request), /不得高于/);
  await assert.rejects(() => onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "http://127.0.0.1/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "10", formalPrice: "20" }, request), /HTTPS URL/);
  await assert.rejects(() => onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, bearerToken: "secret", callbackKeyBase64: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", callbackKeyVersion: "callback-v1", overviewPrice: "10", formalPrice: "20" }, request), /路径/);
  assert.equal(calls, 0);
});

test("failed logout remains an error so client state can stay authenticated", async () => {
  await assert.rejects(() => revokeSession(async () => Response.json({ error: "temporarily unavailable" }, { status: 503 })), (error) => error instanceof Error && /temporarily unavailable/.test(error.message));
});
