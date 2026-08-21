export type EngineResourceKind = "agents" | "tasks";

export type EngineAggregateResult = {
  status: number;
  body: Record<string, unknown>;
};

export class InvalidResourceIdError extends Error {}
export class InvalidEngineResponseError extends Error {}

const maxEngineResponseBytes = 1_048_576;
const sensitiveKeys = new Set([
  "authorization",
  "accesstoken",
  "apikey",
  "ciphertext",
  "credential",
  "credentialvalue",
  "plaintext",
  "privatekey",
  "password",
  "refreshtoken",
  "secret",
  "sessiontoken",
  "signature",
  "token",
  "wrappeddatakey",
]);

export function resolveEngineBaseUrl(environment: Readonly<Record<string, string | undefined>> = process.env): string {
  const raw = environment.ENGINE_BASE_URL ?? "http://localhost:8080";
  const value = new URL(raw);
  if ((value.protocol !== "http:" && value.protocol !== "https:") || value.username || value.password || value.search || value.hash) {
    throw new Error("invalid ENGINE_BASE_URL");
  }
  return value.toString().replace(/\/$/, "");
}

export async function aggregateEngineResource(
  kind: EngineResourceKind,
  id: string,
  sessionToken: string,
  options: { fetch?: typeof fetch; engineBaseUrl?: string } = {},
): Promise<EngineAggregateResult> {
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(id)) throw new InvalidResourceIdError("invalid resource id");
  if (!sessionToken) return { status: 401, body: { error: "unauthorized" } };
  const request = options.fetch ?? fetch;
  const baseUrl = options.engineBaseUrl ?? resolveEngineBaseUrl();
  const encodedID = encodeURIComponent(id);
  const headers = { authorization: `Bearer ${sessionToken}`, accept: "application/json" };
  const singular = kind === "agents" ? "agent" : "task";
  const response = await request(`${baseUrl}/v1/${kind}/${encodedID}/view`, { headers, cache: "no-store" });
  if (!response.ok) return engineError(response);
  const view = sanitizePayload(await readEngineJSON(response));
  if (!view || typeof view !== "object" || Array.isArray(view)) throw new InvalidEngineResponseError("invalid engine view response");
  const resource = (view as Record<string, unknown>)[singular];
  const availableActions = (view as Record<string, unknown>).availableActions;
  if (!validResource(resource, id) || !validActions(availableActions, singular, id) || resource.aggregateVersion !== availableActions.aggregateVersion) {
    throw new InvalidEngineResponseError("invalid engine view snapshot");
  }
  return { status: 200, body: { [singular]: resource, availableActions } };
}

async function engineError(response: Response): Promise<EngineAggregateResult> {
  try {
    const value = await readEngineJSON(response);
    if (value && typeof value === "object" && !Array.isArray(value) && typeof (value as Record<string, unknown>).error === "string") {
      return { status: response.status, body: { error: (value as Record<string, string>).error } };
    }
  } catch {
    // Replace malformed upstream errors with a stable BFF error contract.
  }
  return { status: response.status >= 400 && response.status <= 599 ? response.status : 502, body: { error: "engine request failed" } };
}

async function readEngineJSON(response: Response): Promise<unknown> {
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > maxEngineResponseBytes) throw new InvalidEngineResponseError("engine response too large");
  if (!response.body) throw new InvalidEngineResponseError("empty engine response");
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxEngineResponseBytes) {
      await reader.cancel();
      throw new InvalidEngineResponseError("engine response too large");
    }
    chunks.push(value);
  }
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    throw new InvalidEngineResponseError("invalid engine encoding");
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new InvalidEngineResponseError("invalid engine JSON");
  }
}

function sanitizePayload(value: unknown, depth = 0): unknown {
  if (depth > 20) throw new InvalidEngineResponseError("engine response too deeply nested");
  if (Array.isArray(value)) return value.map((item) => sanitizePayload(item, depth + 1));
  if (!value || typeof value !== "object") return value;
  const result: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    if (!sensitiveKeys.has(key.toLowerCase())) result[key] = sanitizePayload(item, depth + 1);
  }
  return result;
}

function validResource(value: unknown, id: string): value is Record<string, unknown> & { aggregateVersion: number } {
  return Boolean(value && typeof value === "object" && !Array.isArray(value) && (value as Record<string, unknown>).id === id && Number.isSafeInteger((value as Record<string, unknown>).aggregateVersion));
}

function validActions(value: unknown, resourceType: string, id: string): value is Record<string, unknown> & { aggregateVersion: number } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const response = value as Record<string, unknown>;
  if (response.resourceType !== resourceType || response.resourceId !== id || !Number.isSafeInteger(response.aggregateVersion) || !Array.isArray(response.actions)) return false;
  return response.actions.every((item) => {
    if (!item || typeof item !== "object" || Array.isArray(item)) return false;
    const decision = item as Record<string, unknown>;
    return typeof decision.action === "string" && typeof decision.allowed === "boolean" && Array.isArray(decision.reasons) && decision.reasons.every((reason) => Boolean(reason && typeof reason === "object" && !Array.isArray(reason) && typeof (reason as Record<string, unknown>).code === "string" && typeof (reason as Record<string, unknown>).message === "string"));
  });
}
