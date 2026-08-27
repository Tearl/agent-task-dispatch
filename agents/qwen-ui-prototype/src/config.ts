import path from "node:path";

export interface QwenUiPrototypeConfig {
  port: number;
  dataDir: string;
  apiToken?: string;
  publicBaseUrl?: string;
  dashscopeApiKey: string;
  dashscopeBaseUrl: string;
  model: string;
  jobConcurrency: number;
  callbackKey?: Buffer;
  callbackKeyVersion: string;
}

export function loadConfig(): QwenUiPrototypeConfig {
  const callbackEncoded = process.env.QWEN_UI_AGENT_CALLBACK_KEY_BASE64?.trim();
  return {
    port: integerFromEnvironment("QWEN_UI_AGENT_PORT", 8095, 1, 65_535),
    dataDir: path.resolve(process.env.QWEN_UI_AGENT_DATA_DIR ?? ".data/qwen-ui-prototype"),
    apiToken: process.env.QWEN_UI_AGENT_API_TOKEN?.trim() || undefined,
    publicBaseUrl: cleanHttpsUrl(process.env.QWEN_UI_AGENT_PUBLIC_BASE_URL, "QWEN_UI_AGENT_PUBLIC_BASE_URL"),
    dashscopeApiKey: process.env.QWEN_UI_DASHSCOPE_API_KEY?.trim() ?? "",
    dashscopeBaseUrl: cleanHttpsUrl(process.env.DASHSCOPE_IMAGE_BASE_URL, "DASHSCOPE_IMAGE_BASE_URL") ?? "https://dashscope.aliyuncs.com",
    model: process.env.QWEN_UI_AGENT_MODEL?.trim() || "qwen-image-3.0-pro",
    jobConcurrency: integerFromEnvironment("QWEN_UI_AGENT_JOB_CONCURRENCY", 1, 1, 10),
    callbackKey: callbackEncoded ? Buffer.from(callbackEncoded, "base64") : undefined,
    callbackKeyVersion: process.env.QWEN_UI_AGENT_CALLBACK_KEY_VERSION?.trim() || "qwen-ui-prototype-callback-v1",
  };
}

export function assertConfig(config: QwenUiPrototypeConfig): void {
  if (!config.dashscopeApiKey) throw new Error("QWEN_UI_DASHSCOPE_API_KEY is required");
  if (!/^qwen-image-3\.0(?:-pro)?$/u.test(config.model)) throw new Error("QWEN_UI_AGENT_MODEL must be qwen-image-3.0 or qwen-image-3.0-pro");
  if (process.env.NODE_ENV !== "production") return;
  if (!config.apiToken || config.apiToken.length < 24) throw new Error("QWEN_UI_AGENT_API_TOKEN must contain at least 24 characters in production");
  if (!config.publicBaseUrl) throw new Error("QWEN_UI_AGENT_PUBLIC_BASE_URL is required in production");
  if (!config.callbackKey || config.callbackKey.byteLength !== 32) throw new Error("QWEN_UI_AGENT_CALLBACK_KEY_BASE64 must decode to exactly 32 bytes in production");
}

function cleanHttpsUrl(value: string | undefined, name: string): string | undefined {
  if (!value?.trim()) return undefined;
  const url = new URL(value.trim());
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) throw new Error(`${name} must be a clean HTTPS URL`);
  return url.toString().replace(/\/$/u, "");
}

function integerFromEnvironment(name: string, fallback: number, minimum: number, maximum: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < minimum || value > maximum) throw new Error(`${name} is invalid`);
  return value;
}
