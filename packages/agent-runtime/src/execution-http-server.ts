import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { ZodError } from "zod";
import { AgentExecutionAdapter, ExecutionAdapterError } from "./execution-adapter.ts";
import type { ExecutionArtifactStore } from "./execution-artifact-store.ts";
import { executionEnvelopeSchema, EXECUTION_PROTOCOL_VERSION, type ExecutionEnvelope } from "./execution-protocol.ts";
import { AgentHttpError, bearerAuthorized, readBoundedJson, sendJson } from "./http.ts";

export interface ExecutionHttpHandlerOptions<TInput, TResult> {
  manifest: { id: string; version: string };
  executions: AgentExecutionAdapter<TInput, TResult>;
  artifacts: ExecutionArtifactStore;
  apiToken?: string;
  maxRequestBytes?: number;
}

export function createAgentExecutionServer<TInput, TResult>(options: ExecutionHttpHandlerOptions<TInput, TResult>) {
  const handle = createExecutionHttpHandler(options);
  return createServer(async (request, response) => {
    if (!await handle(request, response)) sendJson(response, 404, { error: "not_found" });
  });
}

export function createExecutionHttpHandler<TInput, TResult>(options: ExecutionHttpHandlerOptions<TInput, TResult>) {
  return async (request: IncomingMessage, response: ServerResponse): Promise<boolean> => {
    const url = new URL(request.url ?? "/", "http://agent.local");
    if (request.method === "GET" && url.pathname === "/health") {
      sendJson(response, 200, {
        status: "healthy",
        protocolVersion: "1",
        agent: options.manifest.id,
        version: options.manifest.version,
      });
      return true;
    }
    const artifactMatch = url.pathname.match(/^\/v1\/artifacts\/([a-f0-9]{64})$/u);
    const operation = protocolOperation(request.method, url.pathname);
    if (!artifactMatch?.[1] && !operation) return false;
    if (!bearerAuthorized(request, options.apiToken)) {
      sendJson(response, 401, { error: "unauthorized" });
      return true;
    }
    try {
      if (artifactMatch?.[1]) {
        const artifact = await options.artifacts.read(artifactMatch[1]);
        if (!artifact) {
          sendJson(response, 404, { error: "artifact_not_found" });
          return true;
        }
        response.writeHead(200, {
          "Content-Type": "application/json; charset=utf-8",
          "Content-Length": artifact.bytes.byteLength,
          ETag: `"${artifact.contentHash}"`,
          "Cache-Control": "private, no-store",
        });
        response.end(artifact.bytes);
        return true;
      }
      if (request.headers["x-agent-protocol-version"] !== EXECUTION_PROTOCOL_VERSION) {
        sendJson(response, 400, { error: "protocol_version_mismatch" });
        return true;
      }
      const envelope = executionEnvelopeSchema.parse(await readBoundedJson(
        request,
        options.maxRequestBytes ?? 24 * 1024 * 1024,
        { tooLargeCode: "request_too_large", invalidJsonCode: "invalid_json" },
      ));
      if (envelope.operation !== operation || request.headers["idempotency-key"] !== envelope.idempotencyKey) {
        sendJson(response, 400, { error: "protocol_binding_mismatch" });
        return true;
      }
      sendOperationResponse(response, operation, options.executions, envelope);
    } catch (error) {
      handleError(response, error);
    }
    return true;
  };
}

function protocolOperation(method: string | undefined, pathname: string): ExecutionEnvelope["operation"] | undefined {
  if (method !== "POST") return undefined;
  if (pathname === "/v1/executions") return "create";
  if (pathname === "/v1/executions/status") return "status";
  if (pathname === "/v1/executions/cancel") return "cancel";
  if (pathname === "/v1/executions/deliverable") return "deliverable";
  return undefined;
}

function sendOperationResponse<TInput, TResult>(
  response: ServerResponse,
  operation: ExecutionEnvelope["operation"],
  executions: AgentExecutionAdapter<TInput, TResult>,
  envelope: ExecutionEnvelope,
): void {
  if (operation === "create") return sendJson(response, 202, executions.create(envelope));
  if (operation === "status") return sendJson(response, 200, executions.status(envelope));
  if (operation === "cancel") return sendJson(response, 200, executions.cancel(envelope));
  sendJson(response, 200, executions.deliverable(envelope));
}

function handleError(response: ServerResponse, error: unknown): void {
  if (error instanceof AgentHttpError) return sendJson(response, error.status, { error: error.code });
  if (error instanceof ZodError) return sendJson(response, 400, { error: "invalid_request", details: error.issues });
  if (error instanceof ExecutionAdapterError) {
    const status = error.code === "not_found" ? 404 : error.code === "conflict" || error.code === "not_ready" ? 409 : 400;
    return sendJson(response, status, { error: error.code });
  }
  sendJson(response, 500, { error: "request_failed" });
}
