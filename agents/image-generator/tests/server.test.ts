import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { EXECUTION_PROTOCOL_VERSION, type AsyncJobRecord, type ExecutionEnvelope } from "@agent-platform/agent-runtime";
import { createRuntime } from "../src/bootstrap.ts";
import type { GeneratedImage, ImageRequest } from "../src/domain.ts";
import { ImageStore } from "../src/image-store.ts";
import { createImageAgentServer } from "../src/server.ts";

test("HTTP API validates jobs and protects generated image bytes", async (context) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-agent-http-"));
  const images = new ImageStore(directory);
  const stored = await images.write("job-http", Buffer.from("png"));
  const now = new Date().toISOString();
  const job: AsyncJobRecord<ImageRequest, GeneratedImage> = {
    id: "11111111-1111-4111-8111-111111111111",
    status: "queued",
    createdAt: now,
    updatedAt: now,
    request: { prompt: "test", size: "1280x1280", quality: "hd" },
  };
  const server = createImageAgentServer({
    async submit(request) {
      return { ...job, request };
    },
    async get() {
      return job;
    },
  }, images, "secret-token");
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => new Promise<void>((resolve) => server.close(() => resolve())));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

  const unauthorized = await fetch(`${origin}${stored.url}`);
  assert.equal(unauthorized.status, 401);

  const image = await fetch(`${origin}${stored.url}`, {
    headers: { Authorization: "Bearer secret-token" },
  });
  assert.equal(image.status, 200);
  assert.equal(image.headers.get("content-type"), "image/png");
  assert.equal(Buffer.from(await image.arrayBuffer()).toString(), "png");

  const submitted = await fetch(`${origin}/v1/image-generation/jobs`, {
    method: "POST",
    headers: {
      Authorization: "Bearer secret-token",
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ prompt: "一只橘猫" }),
  });
  assert.equal(submitted.status, 202);
  assert.equal((await submitted.json() as { status: string }).status, "queued");
});

test("image generator platform adapter returns a bounded overview without calling GLM-Image", async () => {
  const dataDir = await mkdtemp(path.join(os.tmpdir(), "image-agent-platform-"));
  const runtime = createRuntime({
    port: 8092,
    dataDir,
    zaiApiKey: "test-key",
    zaiBaseUrl: "https://example.com/v4",
    plannerModel: "test-model",
    jobConcurrency: 1,
    callbackKeyVersion: "test-v1",
  });
  const input = Buffer.from(JSON.stringify({ prompt: "一只橘猫", size: "1280x1280", quality: "hd" }));
  const envelope: ExecutionEnvelope = {
    protocolVersion: EXECUTION_PROTOCOL_VERSION, operation: "create", stage: "overview", logicalExecutionId: "image-overview",
    attemptId: "image-overview:attempt:1", agentId: "agent-db-id", taskId: "task-1", taskSpecHash: `sha256:${"a".repeat(64)}`,
    inputRef: `data:application/json;base64,${input.toString("base64")}`, inputHash: `sha256:${createHash("sha256").update(input).digest("hex")}`,
    responsibilityCode: "overview", costCap: "0", toolPolicy: { mode: "read_only", allowedTools: [] },
    deadline: new Date(Date.now() + 60_000).toISOString(), idempotencyKey: "image-overview", callbackUrl: "https://engine.example/callback",
    callbackNonce: "nonce", fencingToken: 1, overview: { matchRevision: 1, allocationId: "allocation-1", quoteHash: `sha256:${"b".repeat(64)}` },
  };
  runtime.executions.create(envelope);
  for (let index = 0; index < 40; index += 1) {
    if (runtime.executions.status({ ...envelope, operation: "status" }).status === "succeeded") break;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  const delivered = runtime.executions.deliverable({ ...envelope, operation: "deliverable" });
  const artifact = await runtime.artifacts.read(delivered.deliverableRef.split("://")[1] ?? "");
  assert.equal((JSON.parse(artifact?.bytes.toString("utf8") ?? "{}") as { schemaVersion?: string }).schemaVersion, "overview-result-v1");
});
