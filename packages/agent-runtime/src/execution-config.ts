import path from "node:path";

export interface ExecutionAdapterConfig {
  port: number;
  dataDir: string;
  apiToken?: string;
  publicBaseUrl?: string;
  callbackKey?: Buffer;
  callbackKeyVersion: string;
}

export function loadExecutionAdapterConfig(options: {
  environmentPrefix: string;
  defaultPort: number;
  defaultDataDir: string;
}): ExecutionAdapterConfig {
  const prefix = options.environmentPrefix;
  const callbackEncoded = process.env[`${prefix}_CALLBACK_KEY_BASE64`]?.trim();
  return {
    port: integerEnvironment(`${prefix}_PORT`, options.defaultPort, 1, 65_535),
    dataDir: path.resolve(process.env[`${prefix}_DATA_DIR`] ?? options.defaultDataDir),
    apiToken: process.env[`${prefix}_API_TOKEN`]?.trim() || undefined,
    publicBaseUrl: cleanPublicBaseUrl(process.env[`${prefix}_PUBLIC_BASE_URL`], `${prefix}_PUBLIC_BASE_URL`),
    callbackKey: callbackEncoded ? Buffer.from(callbackEncoded, "base64") : undefined,
    callbackKeyVersion: process.env[`${prefix}_CALLBACK_KEY_VERSION`]?.trim() || `${prefix.toLowerCase()}-callback-v1`,
  };
}

export function assertExecutionAdapterProductionConfig(config: ExecutionAdapterConfig, prefix: string): void {
  if (process.env.NODE_ENV !== "production") return;
  if (!config.apiToken || config.apiToken.length < 24) throw new Error(`${prefix}_API_TOKEN must contain at least 24 characters in production`);
  if (!config.publicBaseUrl) throw new Error(`${prefix}_PUBLIC_BASE_URL is required in production`);
  if (!config.callbackKey || config.callbackKey.byteLength < 32) throw new Error(`${prefix}_CALLBACK_KEY_BASE64 must decode to at least 32 bytes in production`);
}

function cleanPublicBaseUrl(value: string | undefined, name: string): string | undefined {
  if (!value?.trim()) return undefined;
  const url = new URL(value.trim());
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    throw new Error(`${name} must be a clean HTTPS origin or base path`);
  }
  return url.toString().replace(/\/$/u, "");
}

function integerEnvironment(name: string, fallback: number, minimum: number, maximum: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < minimum || value > maximum) throw new Error(`${name} is invalid`);
  return value;
}
