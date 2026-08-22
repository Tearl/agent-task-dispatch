import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { AgentHttpError, bearerAuthorized, readBoundedJson, sendJson } from "./http.ts";
import type { AsyncJobRecord } from "./job.ts";

export interface AgentManifest {
  id: string;
  version: string;
}

export interface AgentJobApplication<TRequest, TResult> {
  submit(request: TRequest): Promise<AsyncJobRecord<TRequest, TResult>>;
  get(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined>;
  cancel?(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined>;
}

export interface AgentHttpServerOptions<TRequest, TResult> {
  manifest: AgentManifest;
  service: AgentJobApplication<TRequest, TResult>;
  parseRequest(value: unknown): TRequest;
  apiToken?: string;
  basePath?: string;
  resultPath?: string;
  resultUrlProperty?: string;
  resultNotReadyError?: string;
  renderText?: (result: TResult) => string;
  textFormat?: string;
  textContentType?: string;
  maxBodyBytes?: number;
}

export function createAgentHttpServer<TRequest, TResult>(options: AgentHttpServerOptions<TRequest, TResult>) {
  const routeConfig = normalizeRouteConfig(options);
  return createServer(async (request, response) => {
    try {
      await route(request, response, options, routeConfig);
    } catch (error) {
      if (error instanceof AgentHttpError) {
        sendJson(response, error.status, { error: error.code });
        return;
      }
      const issues = validationIssues(error);
      if (issues) {
        sendJson(response, 400, { error: "invalid_request", details: issues });
        return;
      }
      sendJson(response, 500, {
        error: "request_failed",
        message: error instanceof Error ? error.message : "unexpected error",
      });
    }
  });
}

interface RouteConfig {
  basePath: string;
  resultPath: string;
  jobPattern: RegExp;
  resultPattern: RegExp;
  cancelPattern: RegExp;
}

async function route<TRequest, TResult>(
  request: IncomingMessage,
  response: ServerResponse,
  options: AgentHttpServerOptions<TRequest, TResult>,
  config: RouteConfig,
): Promise<void> {
  const url = new URL(request.url ?? "/", "http://agent.local");
  if (request.method === "GET" && url.pathname === "/health") {
    sendJson(response, 200, { status: "healthy", agent: options.manifest.id, version: options.manifest.version });
    return;
  }
  if (!bearerAuthorized(request, options.apiToken)) {
    sendJson(response, 401, { error: "unauthorized" });
    return;
  }
  if (request.method === "POST" && url.pathname === `${config.basePath}/jobs`) {
    const body = options.parseRequest(await readBoundedJson(request, options.maxBodyBytes ?? 128 * 1024));
    const job = await options.service.submit(body);
    sendJson(response, 202, { id: job.id, status: job.status, statusUrl: `${config.basePath}/jobs/${job.id}` });
    return;
  }

  const cancelMatch = url.pathname.match(config.cancelPattern);
  if (request.method === "POST" && cancelMatch?.[1] && options.service.cancel) {
    const job = await options.service.cancel(cancelMatch[1]);
    sendJson(response, job ? 200 : 404, job ? { id: job.id, status: job.status } : { error: "job_not_found" });
    return;
  }

  const resultMatch = url.pathname.match(config.resultPattern);
  if (request.method === "GET" && resultMatch?.[1]) {
    const job = await options.service.get(resultMatch[1]);
    if (!job) return sendJson(response, 404, { error: "job_not_found" });
    if (job.result === undefined) {
      return sendJson(response, 409, { error: options.resultNotReadyError ?? "result_not_ready", status: job.status });
    }
    if (options.renderText && url.searchParams.get("format") === (options.textFormat ?? "text")) {
      return sendText(response, options.renderText(job.result), options.textContentType ?? "text/plain; charset=utf-8");
    }
    sendJson(response, 200, job.result);
    return;
  }

  const jobMatch = url.pathname.match(config.jobPattern);
  if (request.method === "GET" && jobMatch?.[1]) {
    const job = await options.service.get(jobMatch[1]);
    if (!job) return sendJson(response, 404, { error: "job_not_found" });
    const resultUrl = job.result === undefined ? undefined : `${config.basePath}/jobs/${job.id}/${config.resultPath}`;
    sendJson(response, 200, {
      id: job.id,
      status: job.status,
      createdAt: job.createdAt,
      updatedAt: job.updatedAt,
      error: job.error,
      [options.resultUrlProperty ?? "resultUrl"]: resultUrl,
    });
    return;
  }
  sendJson(response, 404, { error: "not_found" });
}

function normalizeRouteConfig<TRequest, TResult>(options: AgentHttpServerOptions<TRequest, TResult>): RouteConfig {
  const basePath = `/${(options.basePath ?? "/v1/agent").split("/").filter(Boolean).join("/")}`;
  const resultPath = (options.resultPath ?? "result").replaceAll("/", "");
  const escapedBase = escapeRegExp(basePath);
  const escapedResult = escapeRegExp(resultPath);
  const uuid = "([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})";
  return {
    basePath,
    resultPath,
    jobPattern: new RegExp(`^${escapedBase}/jobs/${uuid}$`, "i"),
    resultPattern: new RegExp(`^${escapedBase}/jobs/${uuid}/${escapedResult}$`, "i"),
    cancelPattern: new RegExp(`^${escapedBase}/jobs/${uuid}/cancel$`, "i"),
  };
}

function validationIssues(error: unknown): unknown[] | undefined {
  if (typeof error !== "object" || error === null || !("issues" in error)) return undefined;
  const issues = (error as { issues?: unknown }).issues;
  return Array.isArray(issues) ? issues : undefined;
}

function sendText(response: ServerResponse, body: string, contentType: string): void {
  response.writeHead(200, { "Content-Type": contentType, "Content-Length": Buffer.byteLength(body) });
  response.end(body);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
