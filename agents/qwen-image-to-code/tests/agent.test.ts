import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { EXECUTION_PROTOCOL_VERSION, type ExecutionEnvelope } from "@agent-platform/agent-runtime";
import { qwenImageToCodeAgent } from "../src/agent.ts";
import { createPlatformRuntime } from "../src/platform.ts";

test("exports the independent Qwen image-to-code agent", () => {
  assert.equal(qwenImageToCodeAgent.id, "qwen_image-to-code");
  assert.equal(qwenImageToCodeAgent.name, "Qwen_image-to-code");
});

test("Qwen platform adapter produces an overview without invoking the vision model", async () => {
  const dataDir = await mkdtemp(path.join(os.tmpdir(), "qwen-platform-"));
  const runtime = createPlatformRuntime({ port: 8094, dataDir, callbackKeyVersion: "test-v1" });
  const envelope = overviewEnvelope("qwen-overview");
  runtime.executions.create(envelope);
  for (let index = 0; index < 40; index += 1) {
    if (runtime.executions.status({ ...envelope, operation: "status" }).status === "succeeded") break;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  const delivered = runtime.executions.deliverable({ ...envelope, operation: "deliverable" });
  const artifact = await runtime.artifacts.read(delivered.deliverableRef.split("://")[1] ?? "");
  assert.equal((JSON.parse(artifact?.bytes.toString("utf8") ?? "{}") as { schemaVersion?: string }).schemaVersion, "overview-result-v1");
});

function overviewEnvelope(id: string): ExecutionEnvelope {
  const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1]);
  const input = Buffer.from(JSON.stringify({ image: { data: png.toString("base64"), filename: "screen.png", mediaType: "image/png" }, target: "React" }));
  return {
    protocolVersion: EXECUTION_PROTOCOL_VERSION, operation: "create", stage: "overview", logicalExecutionId: id,
    attemptId: `${id}:attempt:1`, agentId: "agent-db-id", taskId: "task-1", taskSpecHash: `sha256:${"a".repeat(64)}`,
    inputRef: `data:application/json;base64,${input.toString("base64")}`, inputHash: `sha256:${createHash("sha256").update(input).digest("hex")}`,
    responsibilityCode: "overview", costCap: "0", toolPolicy: { mode: "read_only", allowedTools: [] },
    deadline: new Date(Date.now() + 60_000).toISOString(), idempotencyKey: id, callbackUrl: "https://engine.example/callback",
    callbackNonce: "nonce", fencingToken: 1, overview: { matchRevision: 1, allocationId: "allocation-1", quoteHash: `sha256:${"b".repeat(64)}` },
  };
}
