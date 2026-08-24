import { createHash } from "node:crypto";
import { assertPublicHttpsUrl } from "./public-url.ts";

export interface ExecutionInputResolver {
  resolve(inputRef: string, expectedHash: string): Promise<unknown>;
}

export class SafeJsonExecutionInputResolver implements ExecutionInputResolver {
  private readonly maxBytes: number;
  private readonly userAgent: string;
  private readonly authorization?: string;

  constructor(options: { maxBytes?: number; userAgent?: string; authorization?: string } = {}) {
    this.maxBytes = options.maxBytes ?? 16 * 1024 * 1024;
    this.userAgent = options.userAgent ?? "AgentPlatformExecutionAdapter/1";
    this.authorization = options.authorization;
  }

  async resolve(inputRef: string, expectedHash: string): Promise<unknown> {
    const bytes = inputRef.startsWith("data:application/json")
      ? decodeJsonDataUrl(inputRef)
      : await this.fetchJson(inputRef);
    if (bytes.byteLength > this.maxBytes) throw new Error("execution input is too large");
    const actualHash = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
    if (actualHash !== expectedHash) throw new Error("execution input hash mismatch");
    try {
      return JSON.parse(bytes.toString("utf8"));
    } catch {
      throw new Error("execution input must be valid JSON");
    }
  }

  private async fetchJson(rawUrl: string): Promise<Buffer> {
    const url = await assertPublicHttpsUrl(rawUrl);
    const response = await fetch(url, {
      redirect: "error",
      headers: {
        Accept: "application/json",
        "User-Agent": this.userAgent,
        ...(this.authorization ? { Authorization: this.authorization } : {}),
      },
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) throw new Error(`input reference returned ${response.status}`);
    if (!(response.headers.get("content-type")?.toLowerCase() ?? "").includes("application/json")) {
      throw new Error("input reference is not JSON");
    }
    const declaredLength = Number(response.headers.get("content-length") ?? "0");
    if (declaredLength > this.maxBytes) throw new Error("execution input is too large");
    const bytes = Buffer.from(await response.arrayBuffer());
    if (bytes.byteLength > this.maxBytes) throw new Error("execution input is too large");
    return bytes;
  }
}

function decodeJsonDataUrl(value: string): Buffer {
  const match = value.match(/^data:application\/json(?:(;charset=utf-8)?)(;base64)?,(.*)$/su);
  if (!match) throw new Error("unsupported JSON data URL");
  try {
    return match[2] ? Buffer.from(match[3] ?? "", "base64") : Buffer.from(decodeURIComponent(match[3] ?? ""), "utf8");
  } catch {
    throw new Error("invalid JSON data URL");
  }
}
