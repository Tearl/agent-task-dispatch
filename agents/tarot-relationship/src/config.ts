import path from "node:path";

export interface AgentConfig {
  port: number;
  dataDir: string;
  apiToken?: string;
  publicBaseUrl?: string;
  callbackKey?: Buffer;
  callbackKeyVersion: string;
  llmBaseUrl: string;
  llmModel?: string;
  llmApiKey?: string;
  llmTimeoutMs: number;
}

export function loadConfig(): AgentConfig {
  const callbackEncoded = process.env.TAROT_AGENT_CALLBACK_KEY_BASE64?.trim();
  return {
    port: numberFromEnvironment("TAROT_AGENT_PORT", 8091, 1, 65_535),
    dataDir: path.resolve(process.env.TAROT_AGENT_DATA_DIR ?? ".data/tarot-relationship"),
    apiToken: process.env.TAROT_AGENT_API_TOKEN?.trim() || undefined,
    publicBaseUrl: normalizePublicBaseUrl(process.env.TAROT_AGENT_PUBLIC_BASE_URL),
    callbackKey: callbackEncoded ? Buffer.from(callbackEncoded, "base64") : undefined,
    callbackKeyVersion: process.env.TAROT_AGENT_CALLBACK_KEY_VERSION?.trim() || "tarot-callback-v1",
    llmBaseUrl: process.env.LLM_BASE_URL?.trim() || "http://localhost:11434/v1",
    llmModel: process.env.LLM_MODEL?.trim() || undefined,
    llmApiKey: process.env.LLM_API_KEY?.trim() || undefined,
    llmTimeoutMs: numberFromEnvironment("LLM_TIMEOUT_MS", 60_000, 1_000, 300_000),
  };
}

export function assertProductionConfig(config: AgentConfig): void {
  if (process.env.NODE_ENV !== "production") return;
  if (!config.apiToken || config.apiToken.length < 24) throw new Error("TAROT_AGENT_API_TOKEN must contain at least 24 characters in production");
  if (!config.publicBaseUrl) throw new Error("TAROT_AGENT_PUBLIC_BASE_URL is required in production");
  if (!config.callbackKey || config.callbackKey.byteLength < 32) throw new Error("a 32-byte callback key is required in production");
}

function normalizePublicBaseUrl(value: string | undefined): string | undefined {
  if (!value?.trim()) return undefined;
  const url = new URL(value.trim());
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    throw new Error("TAROT_AGENT_PUBLIC_BASE_URL must be a clean HTTPS origin or base path");
  }
  return url.toString().replace(/\/$/, "");
}

function numberFromEnvironment(name: string, fallback: number, minimum: number, maximum: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < minimum || value > maximum) throw new Error(`${name} is invalid`);
  return value;
}
