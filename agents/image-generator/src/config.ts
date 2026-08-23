import path from "node:path";

export interface ImageAgentConfig {
  port: number;
  dataDir: string;
  apiToken?: string;
  publicBaseUrl?: string;
  zaiApiKey: string;
  zaiBaseUrl: string;
  plannerModel: string;
  jobConcurrency: number;
  callbackKey?: Buffer;
  callbackKeyVersion: string;
}

export function loadConfig(): ImageAgentConfig {
  const callbackEncoded = process.env.IMAGE_AGENT_CALLBACK_KEY_BASE64?.trim();
  return {
    port: integerFromEnvironment("IMAGE_AGENT_PORT", 8092, 1, 65_535),
    dataDir: path.resolve(process.env.IMAGE_AGENT_DATA_DIR ?? ".data/image-generator"),
    apiToken: process.env.IMAGE_AGENT_API_TOKEN?.trim() || undefined,
    publicBaseUrl: normalizePublicBaseUrl(process.env.IMAGE_AGENT_PUBLIC_BASE_URL),
    zaiApiKey: process.env.ZAI_API_KEY?.trim() ?? "",
    zaiBaseUrl: normalizeZaiBaseUrl(process.env.ZAI_BASE_URL),
    plannerModel: process.env.IMAGE_AGENT_PLANNER_MODEL?.trim() || "glm-4.5-flash",
    jobConcurrency: integerFromEnvironment("IMAGE_AGENT_JOB_CONCURRENCY", 1, 1, 10),
    callbackKey: callbackEncoded ? Buffer.from(callbackEncoded, "base64") : undefined,
    callbackKeyVersion: process.env.IMAGE_AGENT_CALLBACK_KEY_VERSION?.trim() || "image-agent-callback-v1",
  };
}

function normalizeZaiBaseUrl(value: string | undefined): string {
  const url = new URL(value?.trim() || "https://open.bigmodel.cn/api/paas/v4");
  const localHttp = url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  if ((url.protocol !== "https:" && !localHttp) || url.username || url.password || url.search || url.hash) {
    throw new Error("ZAI_BASE_URL must use HTTPS, except for a loopback development gateway");
  }
  return url.toString().replace(/\/$/u, "");
}

export function assertConfig(config: ImageAgentConfig): void {
  if (!config.zaiApiKey) throw new Error("ZAI_API_KEY is required");
  if (process.env.NODE_ENV !== "production") return;
  if (!config.apiToken || config.apiToken.length < 24) {
    throw new Error("IMAGE_AGENT_API_TOKEN must contain at least 24 characters in production");
  }
  if (!config.publicBaseUrl) throw new Error("IMAGE_AGENT_PUBLIC_BASE_URL is required in production");
  if (!config.callbackKey || config.callbackKey.byteLength < 32) {
    throw new Error("IMAGE_AGENT_CALLBACK_KEY_BASE64 must decode to at least 32 bytes in production");
  }
}

function normalizePublicBaseUrl(value: string | undefined): string | undefined {
  if (!value?.trim()) return undefined;
  const url = new URL(value.trim());
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    throw new Error("IMAGE_AGENT_PUBLIC_BASE_URL must be a clean HTTPS origin or base path");
  }
  return url.toString().replace(/\/$/u, "");
}

function integerFromEnvironment(name: string, fallback: number, minimum: number, maximum: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < minimum || value > maximum) throw new Error(`${name} is invalid`);
  return value;
}
