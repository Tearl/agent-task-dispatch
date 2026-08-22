import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { TarotExecutionService } from "../src/application/execution-service.ts";
import { TarotReadingService } from "../src/application/reading-service.ts";
import { TemplateRelationshipInterpreter } from "../src/interpretation/interpreter.ts";
import { NoopCallbackSender } from "../src/protocol/callback.ts";
import { SafeInputResolver } from "../src/protocol/input-resolver.ts";
import { executionEnvelopeSchema } from "../src/protocol/schema.ts";
import { EXECUTION_PROTOCOL_VERSION, type ExecutionEnvelope } from "../src/protocol/types.ts";
import { FileArtifactStore } from "../src/storage/artifact-store.ts";

test("agent-execution-v1 service creates, polls and returns an idempotent deliverable", async () => {
  const { executions, artifacts } = await fixture();
  const envelope = executionEnvelope();
  assert.deepEqual(executions.create(envelope), { accepted: true, status: "running" });

  let status = executions.status({ ...envelope, operation: "status" });
  for (let attempt = 0; attempt < 20 && status.status === "running"; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 5));
    status = executions.status({ ...envelope, operation: "status" });
  }
  assert.equal(status.status, "succeeded");
  assert.deepEqual(executions.create(envelope), { accepted: true, status: "succeeded" });

  const delivered = executions.deliverable({ ...envelope, operation: "deliverable" });
  assert.match(delivered.contentHash, /^sha256:[a-f0-9]{64}$/u);
  const artifactId = delivered.deliverableRef.replace("tarot-artifact://", "");
  const artifact = await artifacts.read(artifactId);
  assert.ok(artifact);
  const reading = JSON.parse(artifact.bytes.toString("utf8")) as { schemaVersion: string; kind: string; cards: unknown[] };
  assert.equal(reading.schemaVersion, "tarot-relationship-reading-v1");
  assert.equal(reading.kind, "reading");
  assert.equal(reading.cards.length, 3);
});

test("input hash mismatch fails without producing a deliverable", async () => {
  const { executions } = await fixture();
  const envelope = { ...executionEnvelope(), logicalExecutionId: "execution-bad", inputHash: `sha256:${"f".repeat(64)}` };
  assert.equal(executions.create(envelope).accepted, true);
  let status = executions.status({ ...envelope, operation: "status" });
  for (let attempt = 0; attempt < 20 && status.status === "running"; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 5));
    status = executions.status({ ...envelope, operation: "status" });
  }
  assert.equal(status.status, "failed");
  assert.throws(() => executions.deliverable({ ...envelope, operation: "deliverable" }), /not ready/u);

  const retry = {
    ...executionEnvelope(),
    logicalExecutionId: "execution-bad",
    attemptId: "execution-bad:attempt:2",
    fencingToken: 2,
  };
  assert.deepEqual(executions.create(retry), { accepted: true, status: "running" });
  let retryStatus = executions.status({ ...retry, operation: "status" });
  for (let attempt = 0; attempt < 20 && retryStatus.status === "running"; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 5));
    retryStatus = executions.status({ ...retry, operation: "status" });
  }
  assert.equal(retryStatus.status, "succeeded");
});

test("protocol schema enforces stage bindings and read-only overviews", () => {
  const formal = executionEnvelope();
  assert.equal(executionEnvelopeSchema.parse(formal).stage, "formal");
  assert.throws(() => executionEnvelopeSchema.parse({
    ...formal,
    stage: "overview",
    formal: undefined,
    overview: { matchRevision: 1, allocationId: "allocation-1", quoteHash: `sha256:${"b".repeat(64)}` },
    toolPolicy: { mode: "scoped", allowedTools: [] },
  }), /overview execution bindings are invalid/u);
});

async function fixture(): Promise<{ executions: TarotExecutionService; artifacts: FileArtifactStore }> {
  const directory = await mkdtemp(path.join(tmpdir(), "tarot-agent-test-"));
  const artifacts = new FileArtifactStore(directory);
  return {
    artifacts,
    executions: new TarotExecutionService(
      new TarotReadingService(new TemplateRelationshipInterpreter()),
      new SafeInputResolver(),
      artifacts,
      new NoopCallbackSender(),
      "test-key-v1",
    ),
  };
}

function executionEnvelope(): ExecutionEnvelope {
  const input = Buffer.from(JSON.stringify({
    relationshipStage: "dating",
    question: "最近沟通变少，我应该主动联系吗？",
    context: "交往半年，最近两周联系频率下降。",
    tone: "gentle",
    drawMode: "platform_random",
    ageConfirmed: true,
  }), "utf8");
  return {
    protocolVersion: EXECUTION_PROTOCOL_VERSION,
    operation: "create",
    stage: "formal",
    logicalExecutionId: "execution-1",
    attemptId: "execution-1:attempt:1",
    agentId: "tarot-relationship",
    taskId: "task-1",
    taskSpecHash: `sha256:${"a".repeat(64)}`,
    inputRef: `data:application/json;base64,${input.toString("base64")}`,
    inputHash: `sha256:${createHash("sha256").update(input).digest("hex")}`,
    responsibilityCode: "formal_delivery",
    costCap: "0",
    toolPolicy: { mode: "scoped", allowedTools: [] },
    deadline: new Date(Date.now() + 60_000).toISOString(),
    idempotencyKey: "execution-1",
    callbackUrl: "https://engine.example/v1/agent-callbacks/execution-1/attempt-1",
    callbackNonce: "callback-nonce-1",
    fencingToken: 1,
    formal: { assignmentId: "assignment-1", package: "relationship-mirror", version: 1, aggregateVersion: 1, workNonce: 1 },
  };
}
