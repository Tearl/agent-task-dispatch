import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { aggregateEngineResource, forwardEngineMutation, InvalidEngineResponseError, InvalidResourceIdError, resolveEngineBaseUrl } from "./engine.ts";

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
