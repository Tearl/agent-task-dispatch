import assert from "node:assert/strict";
import test from "node:test";
import { ActionBlockedError, authenticateWallet, clientRolesForEngineRoles, createAndPublishTask, onboardAgent, readAgentFinance, readPublisherFinance, readReconciliationFinance, requireAllowed, revokeSession, submitSelectionTransaction, type SelectionIntent } from "./platform-api.ts";

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

test("wallet errors stop before nonce issuance", async () => {
  let requested = false;
  await assert.rejects(() => authenticateWallet({ request: async () => [] }, async () => { requested = true; return Response.json({}); }), /有效账户/);
  assert.equal(requested, false);
});

test("blocked actions preserve Engine reasons", () => {
  assert.throws(() => requireAllowed({ aggregateVersion: 2, actions: [{ action: "publish", allowed: false, reasons: [{ code: "deadline_expired", message: "Set a future deadline." }] }] }, "publish"), (error) => error instanceof ActionBlockedError && error.reasons[0]?.code === "deadline_expired");
});

test("task publishing uses one operation id and Engine publication eligibility", async () => {
  const calls: Array<{ url: string; key: string | null }> = [];
  const request: typeof fetch = async (input, init) => {
    const url = String(input); calls.push({ url, key: new Headers(init?.headers).get("idempotency-key") });
    if (url === "/api/tasks") return Response.json({ id: "task-1", aggregateVersion: 1 }, { status: 201 });
    if (url === "/api/tasks/task-1") return Response.json({ task: { aggregateVersion: 1 }, availableActions: { aggregateVersion: 1, actions: [{ action: "publish", allowed: true, reasons: [] }] } });
    return Response.json({ task: { id: "task-1", status: "pending_escrow" }, spec: { contentHash: "sha256:spec" }, acceptance: { contentHash: "sha256:acceptance" } }, { status: 201 });
  };
  await createAndPublishTask({ operationId: "op-1", title: "Task", description: "Description", category: "research", amount: "10", deadline: "2026-09-01", criteria: ["Correct", "Complete"] }, request);
  assert.deepEqual(calls, [{ url: "/api/tasks", key: "op-1:create" }, { url: "/api/tasks/task-1", key: null }, { url: "/api/tasks/task-1/publish", key: "op-1:publish" }]);
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
  await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, credentialType: "api_key", secret: "secret", overviewPrice: "10", formalPrice: "20" }, request);
  assert.deepEqual(calls.map((call) => call.key), ["agent-op:create", "agent-op:credential", "agent-op:price", null, "agent-op:health:3", null, "agent-op:activate"]);
  assert.equal(calls[0]?.body?.controllerAddress, "0x1111111111111111111111111111111111111111");
  assert.equal(calls[0]?.body?.payoutAddress, "0x1111111111111111111111111111111111111111");
  assert.equal(calls[0]?.body?.endpointUrl, "https://agent.example/health");
  assert.equal(calls[2]?.body?.overviewPrice, "10");
  assert.equal(calls[2]?.body?.formalPackageGrossPrice, "20");
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
  const result = await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, credentialType: "api_key", secret: "secret", overviewPrice: "10", formalPrice: "20" }, request);
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
  await onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, credentialType: "api_key", secret: "secret", overviewPrice: "10", formalPrice: "20" }, request);
  const health = calls.find((call) => call.url.endsWith("health"));
  assert.equal(health?.key, "agent-op:health:4");
  assert.equal(health?.body?.expectedVersion, 4);
  assert.equal(health?.body?.health, undefined);
});

test("invalid correctable inputs fail before any mutation", async () => {
  let calls = 0;
  const request: typeof fetch = async () => { calls += 1; return Response.json({}); };
  await assert.rejects(() => createAndPublishTask({ operationId: "op", title: "Task", description: "Description", category: "research", amount: "01", deadline: "2026-09-01", criteria: ["Correct"] }, request), /前导零/);
  await assert.rejects(() => onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "https://agent.example/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, credentialType: "api_key", secret: "secret", overviewPrice: "30", formalPrice: "20" }, request), /不得高于/);
  await assert.rejects(() => onboardAgent({ operationId: "agent-op", name: "Agent", category: "research", tagline: "Research", endpointUrl: "http://127.0.0.1/health", capabilities: ["analysis"], controllerAddress: "0x1111111111111111111111111111111111111111", maxConcurrency: 2, credentialType: "api_key", secret: "secret", overviewPrice: "10", formalPrice: "20" }, request), /HTTPS URL/);
  assert.equal(calls, 0);
});

test("failed logout remains an error so client state can stay authenticated", async () => {
  await assert.rejects(() => revokeSession(async () => Response.json({ error: "temporarily unavailable" }, { status: 503 })), (error) => error instanceof Error && /temporarily unavailable/.test(error.message));
});
