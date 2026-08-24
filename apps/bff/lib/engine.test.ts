import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { aggregateEngineDisputes, aggregateEngineExecutions, aggregateEngineFinance, aggregateEngineFormalDelivery, aggregateEngineMatching, aggregateEngineResource, aggregateEngineWorkspace, forwardEngineMutation, forwardEngineRead, InvalidEngineResponseError, InvalidResourceIdError, resolveEngineBaseUrl } from "./engine.ts";

test("BFF aggregation calls only internal Engine endpoints and strips sensitive fields", async () => {
  const calls: Array<{ url: string; authorization: string | null }> = [];
  const fetchMock: typeof fetch = async (input, init) => {
    const url = String(input);
    const headers = new Headers(init?.headers);
    calls.push({ url, authorization: headers.get("authorization") });
    return Response.json({
      task: { id: "task-1", status: "draft", aggregateVersion: 2, token: "must-strip", nested: { apiKey: "must-strip", currentCredentialVersion: 3 } },
      availableActions: { resourceType: "task", resourceId: "task-1", aggregateVersion: 2, actions: [{ action: "publish", allowed: false, reasons: [{ code: "deadline_expired", message: "Expired" }] }], Token: "must-strip" },
    });
  };
  const result = await aggregateEngineResource("tasks", "task-1", "session-secret", { fetch: fetchMock, engineBaseUrl: "http://engine.internal:8080" });
  assert.equal(result.status, 200);
  assert.deepEqual(calls, [
    { url: "http://engine.internal:8080/v1/tasks/task-1/view", authorization: "Bearer session-secret" },
  ]);
  assert.deepEqual(result.body, {
    task: { id: "task-1", status: "draft", aggregateVersion: 2, nested: { currentCredentialVersion: 3 } },
    availableActions: { resourceType: "task", resourceId: "task-1", aggregateVersion: 2, actions: [{ action: "publish", allowed: false, reasons: [{ code: "deadline_expired", message: "Expired" }] }] },
  });
  assert.equal(JSON.stringify(result.body).includes("session-secret"), false);
  assert.equal(JSON.stringify(result.body).includes("engine.internal"), false);
});

test("BFF preserves safe Engine errors and rejects invalid resources or responses", async () => {
  const denied = await aggregateEngineResource("agents", "agent-1", "session", { engineBaseUrl: "http://engine", fetch: async () => Response.json({ error: "agent not found", internal: "hidden" }, { status: 404 }) });
  assert.deepEqual(denied, { status: 404, body: { error: "agent not found" } });
  await assert.rejects(() => aggregateEngineResource("tasks", "../escape", "session", { fetch: async () => Response.json({}) }), InvalidResourceIdError);
  await assert.rejects(() => aggregateEngineResource("tasks", "task-1", "session", { engineBaseUrl: "http://engine", fetch: async () => new Response("not-json") }), InvalidEngineResponseError);
  await assert.rejects(() => aggregateEngineResource("tasks", "task-1", "session", { engineBaseUrl: "http://engine", fetch: async () => new Response("{}", { headers: { "content-length": "1048577" } }) }), InvalidEngineResponseError);
});

test("BFF rejects an internally inconsistent Engine view snapshot", async () => {
  let calls = 0;
  await assert.rejects(() => aggregateEngineResource("tasks", "task-1", "session", {
    engineBaseUrl: "http://engine",
    fetch: async () => {
      calls += 1;
      return Response.json({ task: { id: "task-1", aggregateVersion: 1 }, availableActions: { resourceType: "task", resourceId: "task-1", aggregateVersion: 2, actions: [] } });
    },
  }), InvalidEngineResponseError);
  assert.equal(calls, 1);
});

test("finance aggregation keeps submitted and confirmation states separate and strips secrets", async () => {
  let target = "";
  const result = await aggregateEngineFinance("publisher", "session-secret", { engineBaseUrl: "http://engine.internal:8080", fetch: async (input) => {
    target = String(input);
    return Response.json({ asOf: "2026-08-22T00:00:00Z", totals: { discovery: "0", formal: "90", changeOrders: "0", disputeFees: "0", refundable: "90", refunded: "0" }, tasks: [{ taskId: "task-1", title: "Task", asset: "evm:1/native", lifecycle: "refund_pending", discovery: "0", formal: "90", changeOrders: "0", disputeFees: "0", refundable: "90", refundStatus: "pending", terminal: false, updatedAt: "2026-08-22T00:00:00Z", chain: { submission: "submitted", confirmation: "pending" }, privateKey: "strip" }], ledger: [] });
  }});
  assert.equal(target, "http://engine.internal:8080/v1/finance/publisher");
  assert.equal(JSON.stringify((result.body.tasks as Array<Record<string, unknown>>)[0]).includes("privateKey"), false);
  await assert.rejects(() => aggregateEngineFinance("agent", "session", { engineBaseUrl: "http://engine", fetch: async () => Response.json({ asOf: "x", totals: { available: 1 }, positions: [], records: [] }) }), InvalidEngineResponseError);
});

test("matching aggregation reads one sealed view and preserves degradation evidence", async () => {
  let target = "";
  const result = await aggregateEngineMatching("task-1", "session", { engineBaseUrl: "http://engine", fetch: async (input) => {
    target = String(input);
    return Response.json({ asOf: "2026-08-22T00:00:00Z", task: { id: "task-1", title: "Task", status: "awaiting_selection", specHash: "sha256:x" }, snapshot: { id: "sha256:s", revision: 2, algorithmVersion: "fair-shuffle-v1", ruleVersion: "v1", modelVersion: "fallback", seedDigest: "sha256:d", degradations: [{ dependency: "dense", code: "recall_unavailable", message: "fallback" }], candidates: [{ agentId: "agent-1", name: "Agent", category: "research", tags: ["web"], position: 1, exploration: false, overviewPrice: "10", formalPrice: "100", externalCostCap: "0", score: { taskMatch: 50, reputation: 20, priceTime: 10, availability: 5, rule: 85, modelDelta: 0, ranking: 85 } }] } });
  }});
  assert.equal(target, "http://engine/v1/tasks/task-1/matching-view");
  assert.equal(((result.body.snapshot as { degradations: unknown[] }).degradations).length, 1);
  await assert.rejects(() => aggregateEngineMatching("../admin", "session"), InvalidResourceIdError);
});

test("workflow APIs stay task-bound and workspace reads strip secrets", async () => {
  const digest=`sha256:${"a".repeat(64)}`;
  for (const path of ["/v1/tasks/task-1/matching-runs","/v1/tasks/task-1/overview-batches",`/v1/tasks/task-1/overview-batches/${encodeURIComponent(digest)}/slots/${encodeURIComponent(digest)}/finalize`]) {
    const result=await forwardEngineMutation({path,body:"{}",idempotencyKey:"workflow-op",sessionToken:"session"},{engineBaseUrl:"http://engine",fetch:async()=>Response.json({id:digest},{status:201})});
    assert.equal(result.status,201);
  }
  await assert.rejects(()=>forwardEngineMutation({path:"/v1/tasks/task-1/overview-batches/not-a-digest/slots/x/finalize",body:"{}",idempotencyKey:"x",sessionToken:"session"}),InvalidResourceIdError);
  const executions=await aggregateEngineExecutions("task-1","session",{engineBaseUrl:"http://engine",fetch:async()=>Response.json([{logicalExecutionId:digest,stage:"overview",agentId:"agent-1",status:"running",currentAttempt:1,usedCost:"0",costCap:"10",deadline:"2026-08-24T00:00:00Z",createdAt:"2026-08-23T00:00:00Z",updatedAt:"2026-08-23T00:00:00Z",inputRef:"must-strip",secret:"must-strip"}])});
  assert.equal(JSON.stringify(executions.body).includes("must-strip"),false);
  const workspace=await aggregateEngineWorkspace("agents","session",{engineBaseUrl:"http://engine",fetch:async()=>Response.json({agents:[{id:"agent-1",credential:"strip"}]})});
  assert.deepEqual(workspace.body,{agents:[{id:"agent-1"}]});
});

test("selection routes accept only task-bound reservation identities", async () => {
  const reservation = `sha256:${"a".repeat(64)}`;
  const encoded = encodeURIComponent(reservation);
  const read = await forwardEngineRead(`/v1/tasks/task-1/selection-reservations/${encoded}`, "session", { engineBaseUrl: "http://engine", fetch: async () => Response.json({ reservation: { id: reservation }, platformSignature: "0xsafe" }) });
  assert.equal((read.body.reservation as { id: string }).id, reservation);
  const mutation = await forwardEngineMutation({ path: `/v1/tasks/task-1/selection-reservations/${encoded}/reconcile`, body: `{}`, idempotencyKey: "reconcile", sessionToken: "session" }, { engineBaseUrl: "http://engine", fetch: async () => Response.json({ reservation: { id: reservation }, assignment: null }) });
  assert.equal((mutation.body.reservation as { id: string }).id, reservation);
  await assert.rejects(() => forwardEngineRead(`/v1/tasks/other/selection-reservations/../${encoded}`, "session"), InvalidResourceIdError);
});

test("formal delivery read validates frozen scope and version identity", async () => {
  let target = "";
  const packageID = `sha256:${"1".repeat(64)}`;
  const digest = `sha256:${"2".repeat(64)}`;
  const result = await aggregateEngineFormalDelivery("task-1", "session", { engineBaseUrl: "http://engine", fetch: async (input) => {
    target = String(input);
    return Response.json({
      package: { id: packageID, taskId: "task-1", assignmentId: `0x${"3".repeat(64)}`, aggregateVersion: 2, allocatedVersion: 1, includedVersions: 3, maximumVersions: 5 },
      scope: { id: digest, contentHash: digest, taskSpecHash: digest },
      chain: { chainId: "1", contractAddress: `0x${"4".repeat(40)}`, publisherWallet: `0x${"5".repeat(40)}`, taskId: `0x${"6".repeat(64)}`, assignmentId: `0x${"7".repeat(64)}`, workNonce: 1 },
      versions: [{ packageId: packageID, number: 1, aggregateVersion: 2, scopeHash: digest, workNonce: 1, logicalExecutionId: digest, status: "allocated", usedCost: "0" }],
      feedback: [],
      changeOrders: [],
      acceptances: [],
    });
  }});
  assert.equal(target, "http://engine/v1/tasks/task-1/formal-package");
  assert.equal((result.body.versions as unknown[]).length, 1);
  await assert.rejects(() => aggregateEngineFormalDelivery("../admin", "session"), InvalidResourceIdError);
  const started = await forwardEngineMutation({ path: "/v1/tasks/task-1/formal-packages/start", body: `{}`, idempotencyKey: "formal-start", sessionToken: "session" }, { engineBaseUrl: "http://engine", fetch: async () => Response.json({ version: { number: 1 } }, { status: 201 }) });
  assert.equal((started.body.version as { number: number }).number, 1);
  const feedback = await forwardEngineMutation({ path: "/v1/tasks/task-1/formal-feedback", body: `{}`, idempotencyKey: "feedback", sessionToken: "session" }, { engineBaseUrl: "http://engine", fetch: async () => Response.json({ id: digest }, { status: 201 }) });
  assert.equal(feedback.body.id, digest);
  const changeOrder = await forwardEngineMutation({ path: `/v1/tasks/task-1/formal-change-orders/${encodeURIComponent(digest)}/activate`, body: `{}`, idempotencyKey: "activate", sessionToken: "session" }, { engineBaseUrl: "http://engine", fetch: async () => Response.json({ id: digest }, { status: 201 }) });
  assert.equal(changeOrder.body.id, digest);
  const acceptance = await forwardEngineMutation({ path: `/v1/tasks/task-1/formal-acceptance-intents/${encodeURIComponent(digest)}/reconcile`, body: `{}`, idempotencyKey: "reconcile", sessionToken: "session" }, { engineBaseUrl: "http://engine", fetch: async () => Response.json({ id: digest }, { status: 201 }) });
  assert.equal(acceptance.body.id, digest);
});

test("dispute aggregation validates chain binding and limits mutation paths", async()=>{
  const caseID=`sha256:${"a".repeat(64)}`,address=`0x${"b".repeat(40)}`,chainTask=`0x${"c".repeat(64)}`;let target="";
  const view={case:{id:caseID,taskId:"task-1",assignmentId:"assignment-1",deliveryUnitId:"package-1",policyVersion:"platform-dispute-v1",publisherId:"publisher-1",agentProviderId:"provider-1",state:"soft_lock_pending",aggregateVersion:1,frozenAmount:"100",asset:"evm:1/native",claims:[],evidence:[],assignments:[],decisions:[],leaves:[]},context:{taskId:"task-1",assignmentId:"assignment-1",deliveryUnitId:"package-1",publisherId:"publisher-1",agentProviderId:"provider-1",chainId:"1",contractAddress:address,chainTaskId:chainTask,publisherWallet:address,agentController:address,agentPayout:address,disputeResolver:address,frozenAmount:"100",asset:"evm:1/native",feeCap:"0",eligible:true,disputeDeadline:"2026-08-30T00:00:00Z"},accessGrants:[],adminOperations:[]};
  const result=await aggregateEngineDisputes(caseID,"session",{engineBaseUrl:"http://engine",fetch:async(input)=>{target=String(input);return Response.json(view)}});assert.equal(target,`http://engine/v1/disputes/${encodeURIComponent(caseID)}`);assert.equal((result.body.case as {id:string}).id,caseID);
  await assert.rejects(()=>aggregateEngineDisputes("../admin","session"),InvalidResourceIdError);
  const mutation=await forwardEngineMutation({path:`/v1/disputes/${encodeURIComponent(caseID)}/evidence`,body:"{}",idempotencyKey:"evidence",sessionToken:"session"},{engineBaseUrl:"http://engine",fetch:async()=>Response.json(view,{status:201})});assert.equal(mutation.status,201);
  await assert.rejects(()=>forwardEngineMutation({path:`/v1/disputes/${encodeURIComponent(caseID)}/credentials`,body:"{}",idempotencyKey:"x",sessionToken:"session"}),InvalidResourceIdError);
});

test("Agent capacity and retire decision come from one Engine view request", async () => {
  let calls = 0;
  const result = await aggregateEngineResource("agents", "agent-1", "session", {
    engineBaseUrl: "http://engine",
    fetch: async () => {
      calls += 1;
      return Response.json({
        agent: { id: "agent-1", aggregateVersion: 8, activeCapacity: 1 },
        availableActions: { resourceType: "agent", resourceId: "agent-1", aggregateVersion: 8, actions: [{ action: "retire", allowed: false, reasons: [{ code: "active_capacity_nonzero", message: "Release capacity" }] }] },
      });
    },
  });
  assert.equal(calls, 1);
  assert.equal((result.body.agent as { activeCapacity: number }).activeCapacity, 1);
  assert.deepEqual((result.body.availableActions as { actions: unknown[] }).actions, [{ action: "retire", allowed: false, reasons: [{ code: "active_capacity_nonzero", message: "Release capacity" }] }]);
});

test("protected mutations forward session and idempotency without exposing secrets", async () => {
  const calls: Array<{ url: string; authorization: string | null; idempotencyKey: string | null; body: string }> = [];
  const result = await forwardEngineMutation({ path: "/v1/agents/agent-1/credentials", body: JSON.stringify({ secret: "never-log" }), idempotencyKey: "operation-1", sessionToken: "session-secret" }, {
    engineBaseUrl: "http://engine.internal:8080",
    fetch: async (input, init) => {
      const headers = new Headers(init?.headers);
      calls.push({ url: String(input), authorization: headers.get("authorization"), idempotencyKey: headers.get("idempotency-key"), body: String(init?.body) });
      return Response.json({ agentId: "agent-1", version: 1, fingerprint: "safe", secret: "must-strip" }, { status: 201 });
    },
  });
  assert.deepEqual(calls, [{ url: "http://engine.internal:8080/v1/agents/agent-1/credentials", authorization: "Bearer session-secret", idempotencyKey: "operation-1", body: '{"secret":"never-log"}' }]);
  assert.deepEqual(result, { status: 201, body: { agentId: "agent-1", version: 1, fingerprint: "safe" } });
  await assert.rejects(() => forwardEngineMutation({ path: "/v1/agents/../admin", body: "{}", idempotencyKey: "x", sessionToken: "session" }), InvalidResourceIdError);
});

test("public environment and browser source cannot select or call the internal Engine", async () => {
  assert.equal(resolveEngineBaseUrl({ NEXT_PUBLIC_ENGINE_BASE_URL: "https://attacker.example", ENGINE_BASE_URL: "http://engine.internal:8080" }), "http://engine.internal:8080");
  const sourceFiles = await sourceTree(path.resolve(process.cwd(), "../web/src"));
  for (const file of sourceFiles) {
    const source = await readFile(file, "utf8");
    assert.equal(source.includes("ENGINE_BASE_URL"), false, `${file} reads the internal Engine environment`);
    assert.equal(source.includes("localhost:8080"), false, `${file} calls Engine directly`);
  }
});

async function sourceTree(directory: string): Promise<string[]> {
  const result: string[] = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) result.push(...await sourceTree(target));
    else if (entry.name.endsWith(".ts") || entry.name.endsWith(".tsx")) result.push(target);
  }
  return result;
}
