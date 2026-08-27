import path from "node:path";

export interface SeedreamVisualDesignConfig {
  port: number;
  dataDir: string;
  apiToken?: string;
  publicBaseUrl?: string;
  arkApiKey: string;
  arkBaseUrl: string;
  model: string;
  jobConcurrency: number;
  callbackKey?: Buffer;
  callbackKeyVersion: string;
}

export function loadConfig(): SeedreamVisualDesignConfig {
  const callbackEncoded = process.env.SEEDREAM_AGENT_CALLBACK_KEY_BASE64?.trim();
  return {
    port: integerFromEnvironment("SEEDREAM_AGENT_PORT", 8096, 1, 65_535),
    dataDir: path.resolve(process.env.SEEDREAM_AGENT_DATA_DIR ?? ".data/seedream-visual-design"),
    apiToken: process.env.SEEDREAM_AGENT_API_TOKEN?.trim() || undefined,
    publicBaseUrl: cleanHttpsUrl(process.env.SEEDREAM_AGENT_PUBLIC_BASE_URL, "SEEDREAM_AGENT_PUBLIC_BASE_URL"),
    arkApiKey: process.env.ARK_API_KEY?.trim() ?? "",
    arkBaseUrl: cleanHttpsUrl(process.env.ARK_BASE_URL, "ARK_BASE_URL") ?? "https://ark.cn-beijing.volces.com/api/v3",
    model: process.env.SEEDREAM_AGENT_MODEL?.trim() || "doubao-seedream-5-0-lite-260128",
    jobConcurrency: integerFromEnvironment("SEEDREAM_AGENT_JOB_CONCURRENCY", 1, 1, 10),
    callbackKey: callbackEncoded ? Buffer.from(callbackEncoded, "base64") : undefined,
    callbackKeyVersion: process.env.SEEDREAM_AGENT_CALLBACK_KEY_VERSION?.trim() || "seedream-visual-design-callback-v1",
  };
}

export function assertConfig(config: SeedreamVisualDesignConfig): void {
  if (!config.arkApiKey) throw new Error("ARK_API_KEY is required");
  if (!/^doubao-seedream-5-0-lite-[0-9]{6}$/u.test(config.model)) throw new Error("SEEDREAM_AGENT_MODEL must be a versioned Seedream 5.0 Lite model ID");
  if (process.env.NODE_ENV !== "production") return;
  if (!config.apiToken || config.apiToken.length < 24) throw new Error("SEEDREAM_AGENT_API_TOKEN must contain at least 24 characters in production");
  if (!config.publicBaseUrl) throw new Error("SEEDREAM_AGENT_PUBLIC_BASE_URL is required in production");
  if (!config.callbackKey || config.callbackKey.byteLength !== 32) throw new Error("SEEDREAM_AGENT_CALLBACK_KEY_BASE64 must decode to exactly 32 bytes in production");
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
