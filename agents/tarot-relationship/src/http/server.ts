import {
  AgentHttpError,
  bearerAuthorized,
  readBoundedJson,
  sendJson,
} from "@agent-platform/agent-runtime";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { ZodError } from "zod";
import type { TarotExecutionService } from "../application/execution-service.ts";
import { ExecutionServiceError } from "../application/execution-service.ts";
import { executionEnvelopeSchema } from "../protocol/schema.ts";
import type { ExecutionEnvelope } from "../protocol/types.ts";
import { EXECUTION_PROTOCOL_VERSION } from "../protocol/types.ts";
import type { ArtifactStore } from "../storage/artifact-store.ts";

const maxRequestBytes = 128 * 1024;

export function createAgentServer(service: TarotExecutionService, artifacts: ArtifactStore, apiToken?: string) {
  return createServer(async (request, response) => {
    try {
      await route(request, response, service, artifacts, apiToken);
    } catch (error) {
      handleError(response, error);
    }
  });
}

async function route(
  request: IncomingMessage,
  response: ServerResponse,
  service: TarotExecutionService,
  artifacts: ArtifactStore,
  apiToken?: string,
): Promise<void> {
  const url = new URL(request.url ?? "/", "http://agent.local");
  if (request.method === "GET" && url.pathname === "/health") {
    sendJson(response, 200, { status: "healthy", protocolVersion: "1", agent: "tarot-relationship", version: "0.1.0" });
    return;
  }
  if (!bearerAuthorized(request, apiToken)) {
    sendJson(response, 401, { error: "unauthorized" });
    return;
  }
  const artifactMatch = url.pathname.match(/^\/v1\/artifacts\/([a-f0-9]{64})$/u);
  if (request.method === "GET" && artifactMatch?.[1]) {
    const artifact = await artifacts.read(artifactMatch[1]);
    if (!artifact) {
      sendJson(response, 404, { error: "artifact_not_found" });
      return;
    }
    response.writeHead(200, {
      "Content-Type": "application/json; charset=utf-8",
      "Content-Length": artifact.bytes.byteLength,
      ETag: `"${artifact.contentHash}"`,
      "Cache-Control": "private, no-store",
    });
    response.end(artifact.bytes);
    return;
  }

  const operation = protocolOperation(request.method, url.pathname);
  if (!operation) {
    sendJson(response, 404, { error: "not_found" });
    return;
  }
  if (request.headers["x-agent-protocol-version"] !== EXECUTION_PROTOCOL_VERSION) {
    sendJson(response, 400, { error: "protocol_version_mismatch" });
    return;
  }
  const envelope = executionEnvelopeSchema.parse(await readBoundedJson(request, maxRequestBytes, {
    tooLargeCode: "request_too_large",
    invalidJsonCode: "invalid_json",
  })) as ExecutionEnvelope;
  if (envelope.operation !== operation || request.headers["idempotency-key"] !== envelope.idempotencyKey) {
    sendJson(response, 400, { error: "protocol_binding_mismatch" });
    return;
  }
  switch (operation) {
    case "create":
      sendJson(response, 202, service.create(envelope));
      return;
    case "status":
      sendJson(response, 200, service.status(envelope));
      return;
    case "cancel":
      sendJson(response, 200, service.cancel(envelope));
      return;
    case "deliverable":
      sendJson(response, 200, service.deliverable(envelope));
      return;
  }
}

function protocolOperation(method: string | undefined, pathname: string): ExecutionEnvelope["operation"] | undefined {
  if (method !== "POST") return undefined;
  if (pathname === "/v1/executions") return "create";
  if (pathname === "/v1/executions/status") return "status";
  if (pathname === "/v1/executions/cancel") return "cancel";
  if (pathname === "/v1/executions/deliverable") return "deliverable";
  return undefined;
}

function handleError(response: ServerResponse, error: unknown): void {
  if (error instanceof AgentHttpError) {
    sendJson(response, error.status, { error: error.code });
    return;
  }
  if (error instanceof ZodError) {
    sendJson(response, 400, { error: "invalid_request", details: error.issues });
    return;
  }
  if (error instanceof ExecutionServiceError) {
    const status = error.code === "not_found" ? 404 : error.code === "conflict" || error.code === "not_ready" ? 409 : 400;
    sendJson(response, status, { error: error.code });
    return;
  }
  sendJson(response, 500, { error: "request_failed" });
}
