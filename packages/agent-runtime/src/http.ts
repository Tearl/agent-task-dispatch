import { createHash, timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";

export class AgentHttpError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string) {
    super(code);
    this.status = status;
    this.code = code;
  }
}

export interface BoundedJsonOptions {
  tooLargeCode?: string;
  invalidJsonCode?: string;
}

export async function readBoundedJson(
  request: IncomingMessage,
  maxBodyBytes: number,
  options: BoundedJsonOptions = {},
): Promise<unknown> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += buffer.byteLength;
    if (size > maxBodyBytes) throw new AgentHttpError(413, options.tooLargeCode ?? "request_body_too_large");
    chunks.push(buffer);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    throw new AgentHttpError(400, options.invalidJsonCode ?? "invalid_json");
  }
}

export function bearerAuthorized(request: IncomingMessage, expected?: string): boolean {
  if (!expected) return true;
  const provided = request.headers.authorization;
  if (!provided?.startsWith("Bearer ")) return false;
  const expectedHash = createHash("sha256").update(expected).digest();
  const providedHash = createHash("sha256").update(provided.slice(7)).digest();
  return timingSafeEqual(expectedHash, providedHash);
}

export function sendJson(
  response: ServerResponse,
  status: number,
  body: unknown,
  extraHeaders: Record<string, string> = {},
): void {
  const value = JSON.stringify(body);
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(value),
    "Cache-Control": "no-store",
    ...extraHeaders,
  });
  response.end(value);
}
