const defaultEngineRequestTimeoutMs = 5_000;
const maxEngineRequestTimeoutMs = 30_000;
export const maxEngineResponseBytes = 1_048_576;

export class EngineRequestTimeoutError extends Error {}
export class EngineConnectionError extends Error {}
export class EngineResponseTooLargeError extends Error {}

export type EngineRequestOptions = {
  fetch?: typeof fetch;
  timeoutMs?: number;
  environment?: Readonly<Record<string, string | undefined>>;
};

export function resolveEngineRequestTimeoutMs(
  environment: Readonly<Record<string, string | undefined>> = process.env,
): number {
  const raw = environment.ENGINE_REQUEST_TIMEOUT_MS;
  if (raw === undefined || raw === "") return defaultEngineRequestTimeoutMs;
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < 100 || value > maxEngineRequestTimeoutMs) {
    throw new Error("invalid ENGINE_REQUEST_TIMEOUT_MS");
  }
  return value;
}

/**
 * The only transport boundary for BFF -> Engine requests. It bounds both the
 * request and response-read time, buffers at most 1 MiB, and never exposes
 * connection-specific error text to callers.
 */
export async function requestEngine(
  input: string | URL | Request,
  init: RequestInit = {},
  options: EngineRequestOptions = {},
): Promise<Response> {
  const request = options.fetch ?? fetch;
  const timeoutMs = options.timeoutMs ?? resolveEngineRequestTimeoutMs(options.environment);
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > maxEngineRequestTimeoutMs) {
    throw new Error("invalid Engine request timeout");
  }

  const controller = new AbortController();
  let timedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_resolve, reject) => {
    timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
      reject(new EngineRequestTimeoutError("Engine request timed out"));
    }, timeoutMs);
  });

  try {
    const response = await Promise.race([
      request(input, { ...init, signal: controller.signal }),
      timeout,
    ]);
    const body = await Promise.race([readBoundedResponse(response), timeout]);
    const responseBody = body === null
      ? null
      : body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) as ArrayBuffer;
    return new Response(responseBody, {
      status: response.status,
      statusText: response.statusText,
      headers: response.headers,
    });
  } catch (error) {
    if (error instanceof EngineRequestTimeoutError || error instanceof EngineResponseTooLargeError) throw error;
    if (timedOut) throw new EngineRequestTimeoutError("Engine request timed out");
    throw new EngineConnectionError("Engine connection failed");
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}

async function readBoundedResponse(response: Response): Promise<Uint8Array | null> {
  const contentLength = response.headers.get("content-length");
  if (contentLength !== null) {
    const declared = Number(contentLength);
    if (!Number.isSafeInteger(declared) || declared < 0 || declared > maxEngineResponseBytes) {
      throw new EngineResponseTooLargeError("Engine response too large");
    }
  }
  if (!response.body) return null;

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxEngineResponseBytes) {
      await reader.cancel();
      throw new EngineResponseTooLargeError("Engine response too large");
    }
    chunks.push(value);
  }

  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes;
}
