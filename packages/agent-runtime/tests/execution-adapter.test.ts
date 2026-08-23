import assert from "node:assert/strict";
import { createHash, createHmac } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  AgentExecutionAdapter,
  createAgentExecutionServer,
  EXECUTION_PROTOCOL_VERSION,
  FileExecutionArtifactStore,
  HmacExecutionCallbackSender,
  NoopExecutionCallbackSender,
  SafeJsonExecutionInputResolver,
  type ExecutionEnvelope,
  type OverviewResult,
} from "../src/index.ts";

interface Input { prompt: string }
interface Output { answer: string }

test("agent-execution-v1 runs overview and returns an idempotent artifact", async () => {
  const { adapter, artifacts } = await fixture();
  const envelope = makeEnvelope("overview");
  assert.deepEqual(adapter.create(envelope), { accepted: true, status: "running" });
  await waitForStatus(adapter, envelope, "succeeded");
  assert.deepEqual(adapter.create(envelope), { accepted: true, status: "succeeded" });

  const delivered = adapter.deliverable({ ...envelope, operation: "deliverable" });
  assert.match(delivered.contentHash, /^sha256:[a-f0-9]{64}$/u);
  const artifact = await artifacts.read(delivered.deliverableRef.split("://")[1] ?? "");
  assert.ok(artifact);
  assert.equal((JSON.parse(artifact.bytes.toString("utf8")) as OverviewResult).schemaVersion, "overview-result-v1");
});

test("agent-execution-v1 verifies hashes, cancels work and accepts only newer fenced retries", async () => {
  const { adapter } = await fixture();
  const invalid = { ...makeEnvelope("formal"), inputHash: `sha256:${"f".repeat(64)}` };
  adapter.create(invalid);
  await waitForStatus(adapter, invalid, "failed");
  assert.throws(() => adapter.create({ ...invalid, attemptId: "attempt-0", fencingToken: 0 }), /conflict/u);

  const retry = { ...makeEnvelope("formal"), logicalExecutionId: invalid.logicalExecutionId, attemptId: "attempt-2", fencingToken: 2 };
  assert.equal(adapter.create(retry).accepted, true);
  assert.equal(adapter.cancel({ ...retry, operation: "cancel" }).accepted, true);
  assert.deepEqual(adapter.status({ ...retry, operation: "status" }), { status: "cancelled", usedCost: "0" });
});

test("execution HTTP server exposes protocol health and enforces bearer and header bindings", async (context) => {
  const { adapter, artifacts } = await fixture();
  const server = createAgentExecutionServer({
    manifest: { id: "test-agent", version: "0.1.0" },
    executions: adapter,
    artifacts,
    apiToken: "test-secret",
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => new Promise<void>((resolve) => server.close(() => resolve())));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const health = await (await fetch(`${origin}/health`)).json() as { protocolVersion: string };
  assert.equal(health.protocolVersion, "1");

  const envelope = makeEnvelope("formal");
  assert.equal((await fetch(`${origin}/v1/executions`, { method: "POST", body: JSON.stringify(envelope) })).status, 401);
  const response = await fetch(`${origin}/v1/executions`, {
    method: "POST",
    headers: {
      Authorization: "Bearer test-secret",
      "Content-Type": "application/json",
      "Idempotency-Key": envelope.idempotencyKey,
      "X-Agent-Protocol-Version": EXECUTION_PROTOCOL_VERSION,
    },
    body: JSON.stringify(envelope),
  });
  assert.equal(response.status, 202);
  assert.equal((await response.json() as { accepted: boolean }).accepted, true);
});

test("callback sender signs the exact JSON body with the configured HMAC key", async (context) => {
  const originalFetch = globalThis.fetch;
  const key = Buffer.alloc(32, 7);
  context.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async (_input, init) => {
    const body = String(init?.body);
    const signature = createHmac("sha256", key).update(body).digest("hex");
    assert.equal((init?.headers as Record<string, string>)["X-Agent-Signature"], `hmac-sha256=${signature}`);
    return new Response(null, { status: 204 });
  };
  await new HmacExecutionCallbackSender(key).send("https://engine.example/callback", {
    protocolVersion: EXECUTION_PROTOCOL_VERSION,
    logicalExecutionId: "execution-1",
    attemptId: "attempt-1",
    agentId: "agent-1",
    fencingToken: 1,
    status: "failed",
    usedCost: "0",
    timestamp: new Date().toISOString(),
    nonce: "nonce",
    keyVersion: "key-v1",
  });
});

async function fixture() {
  const directory = await mkdtemp(path.join(os.tmpdir(), "execution-adapter-"));
  const artifacts = new FileExecutionArtifactStore(directory, "test-agent");
  const adapter = new AgentExecutionAdapter<Input, Output>({
    executor: {
      parseInput(value) {
        if (typeof value !== "object" || value === null || !("prompt" in value) || typeof value.prompt !== "string") {
          throw new Error("invalid input");
        }
        return { prompt: value.prompt };
      },
      async execute(input, context) {
        if (context.signal.aborted) throw context.signal.reason;
        return { answer: input.prompt.toUpperCase() };
      },
      overview(input) {
        return {
          schemaVersion: "overview-result-v1",
          understandingSummary: input.prompt,
          approach: ["execute"],
          deliverableStructure: ["answer"],
          keyRisks: ["none"],
          estimatedDurationSeconds: 1,
        };
      },
    },
    inputs: new SafeJsonExecutionInputResolver(),
    artifacts,
    callbacks: new NoopExecutionCallbackSender(),
    callbackKeyVersion: "test-v1",
  });
  return { adapter, artifacts };
}

async function waitForStatus(adapter: AgentExecutionAdapter<Input, Output>, envelope: ExecutionEnvelope, expected: string): Promise<void> {
  for (let index = 0; index < 40; index += 1) {
    if (adapter.status({ ...envelope, operation: "status" }).status === expected) return;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  assert.fail(`execution did not reach ${expected}`);
}

function makeEnvelope(stage: "overview" | "formal"): ExecutionEnvelope {
  const input = Buffer.from(JSON.stringify({ prompt: "hello" }), "utf8");
  const common = {
    protocolVersion: EXECUTION_PROTOCOL_VERSION,
    operation: "create" as const,
    stage,
    logicalExecutionId: `execution-${stage}`,
    attemptId: "attempt-1",
    agentId: "agent-database-id",
    taskId: "task-1",
    taskSpecHash: `sha256:${"a".repeat(64)}`,
    inputRef: `data:application/json;base64,${input.toString("base64")}`,
    inputHash: `sha256:${createHash("sha256").update(input).digest("hex")}`,
    responsibilityCode: "delivery",
    costCap: "0",
    deadline: new Date(Date.now() + 60_000).toISOString(),
    idempotencyKey: `execution-${stage}`,
    callbackUrl: "https://engine.example/v1/agent-callbacks/execution/attempt",
    callbackNonce: "nonce-1",
    fencingToken: 1,
  };
  return stage === "overview"
    ? { ...common, toolPolicy: { mode: "read_only", allowedTools: [] }, overview: { matchRevision: 1, allocationId: "allocation-1", quoteHash: `sha256:${"b".repeat(64)}` } }
    : { ...common, toolPolicy: { mode: "scoped", allowedTools: [] }, formal: { assignmentId: "assignment-1", package: "default", version: 1, aggregateVersion: 1, workNonce: 1 } };
}
