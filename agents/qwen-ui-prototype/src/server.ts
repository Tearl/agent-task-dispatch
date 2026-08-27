import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { bearerAuthorized, createAgentHttpServer, createExecutionHttpHandler, sendJson, type AgentExecutionAdapter, type AgentJobApplication, type ExecutionArtifactStore } from "@agent-platform/agent-runtime";
import { imageRequestSchema, type GeneratedImage, type ImageRequest } from "./domain.ts";
import type { ImageStore } from "./image-store.ts";

const manifest = { id: "qwen-ui-prototype", version: "0.1.0" };

export function createQwenUiPrototypeServer(service: AgentJobApplication<ImageRequest, GeneratedImage>, images: ImageStore, apiToken?: string, executions?: AgentExecutionAdapter<ImageRequest, GeneratedImage>, artifacts?: ExecutionArtifactStore) {
  const jobsServer = createAgentHttpServer({ manifest, service, apiToken, basePath: "/v1/ui-prototypes", resultPath: "result", parseRequest: (value) => imageRequestSchema.parse(value) });
  const executionHandler = executions && artifacts ? createExecutionHttpHandler({ manifest, executions, artifacts, apiToken }) : undefined;
  return createServer(async (request, response) => {
    try {
      if (executionHandler && await executionHandler(request, response)) return;
      if (await serveImage(request, response, images, apiToken)) return;
      jobsServer.emit("request", request, response);
    } catch (error) {
      sendJson(response, 500, { error: "request_failed", message: error instanceof Error ? error.message : "unexpected error" });
    }
  });
}

async function serveImage(request: IncomingMessage, response: ServerResponse, images: ImageStore, apiToken?: string): Promise<boolean> {
  const match = new URL(request.url ?? "/", "http://agent.local").pathname.match(/^\/v1\/images\/([a-f0-9]{64})$/u);
  if (request.method !== "GET" || !match?.[1]) return false;
  if (!bearerAuthorized(request, apiToken)) { sendJson(response, 401, { error: "unauthorized" }); return true; }
  const bytes = await images.read(match[1]);
  if (!bytes) { sendJson(response, 404, { error: "image_not_found" }); return true; }
  response.writeHead(200, { "Content-Type": "image/png", "Content-Length": bytes.byteLength, "Cache-Control": "private, max-age=31536000, immutable", "X-Content-Type-Options": "nosniff" });
  response.end(bytes);
  return true;
}
