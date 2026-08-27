import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { EXECUTION_PROTOCOL_VERSION, type ExecutionEnvelope } from "@agent-platform/agent-runtime";
import { createRuntime } from "../src/bootstrap.ts";
import { ImageStore } from "../src/image-store.ts";
import { createSeedreamVisualDesignServer } from "../src/server.ts";

test("Seedream Agent exposes its independent manifest and protects images", async (context) => {
  const dataDir = await mkdtemp(path.join(os.tmpdir(), "seedream-http-"));
  const images = new ImageStore(dataDir);
  const stored = await images.write("job", Buffer.from("png"));
  const server = createSeedreamVisualDesignServer({ async submit() { throw new Error("not used"); }, async get() { return undefined; } }, images, "token");
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => new Promise<void>((resolve) => server.close(() => resolve())));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const health = await fetch(`${origin}/health`);
  assert.equal((await health.json() as { agent: string }).agent, "seedream-visual-design");
  assert.equal((await fetch(`${origin}${stored.url}`)).status, 401);
});

test("Seedream overview is bounded and does not call the billable image API", async () => {
  const dataDir = await mkdtemp(path.join(os.tmpdir(), "seedream-overview-"));
  const runtime = createRuntime({ port: 8096, dataDir, arkApiKey: "test", arkBaseUrl: "https://example.com/api/v3", model: "doubao-seedream-5-0-lite-260128", jobConcurrency: 1, callbackKeyVersion: "test-v1" });
  const input = Buffer.from(JSON.stringify({ prompt: "开发者工具品牌首页", size: "1280x1280", quality: "hd" }));
  const envelope: ExecutionEnvelope = {
    protocolVersion: EXECUTION_PROTOCOL_VERSION, operation: "create", stage: "overview", logicalExecutionId: "seedream-overview", attemptId: "seedream-overview:1", agentId: "agent-id", taskId: "task-1", taskSpecHash: `sha256:${"a".repeat(64)}`,
    inputRef: `data:application/json;base64,${input.toString("base64")}`, inputHash: `sha256:${createHash("sha256").update(input).digest("hex")}`, responsibilityCode: "overview", costCap: "0", toolPolicy: { mode: "read_only", allowedTools: [] }, deadline: new Date(Date.now() + 60_000).toISOString(), idempotencyKey: "seedream-overview", callbackUrl: "https://engine.example/callback", callbackNonce: "nonce", fencingToken: 1, overview: { matchRevision: 1, allocationId: "allocation", quoteHash: `sha256:${"b".repeat(64)}` },
  };
  runtime.executions.create(envelope);
  for (let index = 0; index < 40 && runtime.executions.status({ ...envelope, operation: "status" }).status !== "succeeded"; index += 1) await new Promise((resolve) => setTimeout(resolve, 5));
  const delivered = runtime.executions.deliverable({ ...envelope, operation: "deliverable" });
  const artifact = await runtime.artifacts.read(delivered.deliverableRef.split("://")[1] ?? "");
  assert.equal((JSON.parse(artifact?.bytes.toString("utf8") ?? "{}") as { schemaVersion?: string }).schemaVersion, "overview-result-v1");
});
