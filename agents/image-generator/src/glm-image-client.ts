import { assertPublicHttpsUrl } from "@agent-platform/agent-runtime";
import { z } from "zod";
import type { ImageRequest } from "./domain.ts";

const responseSchema = z.object({
  data: z.array(z.object({ url: z.url() })).min(1),
});

export interface GlmImageClientOptions {
  apiKey: string;
  baseUrl: string;
  timeoutMs?: number;
  maxImageBytes?: number;
  fetchImpl?: typeof fetch;
  validateImageUrl?: (url: string) => Promise<URL>;
}

export class GlmImageClient {
  private readonly apiKey: string;
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxImageBytes: number;
  private readonly fetchImpl: typeof fetch;
  private readonly validateImageUrl: (url: string) => Promise<URL>;

  constructor(options: GlmImageClientOptions) {
    this.apiKey = options.apiKey;
    this.baseUrl = options.baseUrl.replace(/\/$/u, "");
    this.timeoutMs = options.timeoutMs ?? 120_000;
    this.maxImageBytes = options.maxImageBytes ?? 30 * 1024 * 1024;
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.validateImageUrl = options.validateImageUrl ?? assertPublicHttpsUrl;
  }

  async generate(prompt: string, size: ImageRequest["size"], signal?: AbortSignal): Promise<Buffer> {
    const response = await this.fetchImpl(`${this.baseUrl}/images/generations`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: "glm-image",
        prompt,
        quality: "hd",
        size,
        watermark_enabled: true,
      }),
      signal: requestSignal(signal, this.timeoutMs),
    });
    if (!response.ok) throw new Error(`GLM-Image API returned ${response.status}: ${await safeError(response)}`);
    const parsed = responseSchema.parse(await response.json());
    return this.download(parsed.data[0]!.url, signal);
  }

  private async download(rawUrl: string, signal?: AbortSignal): Promise<Buffer> {
    let current = await this.validateImageUrl(rawUrl);
    for (let redirects = 0; redirects <= 3; redirects += 1) {
      const response = await this.fetchImpl(current, {
        redirect: "manual",
        signal: requestSignal(signal, this.timeoutMs),
      });
      if (response.status >= 300 && response.status < 400) {
        const location = response.headers.get("location");
        if (!location || redirects === 3) throw new Error("invalid GLM image redirect");
        current = await this.validateImageUrl(new URL(location, current).toString());
        continue;
      }
      if (!response.ok) throw new Error(`GLM image download returned ${response.status}`);
      const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
      if (!contentType.startsWith("image/")) throw new Error("GLM result URL did not return an image");
      const declaredLength = Number(response.headers.get("content-length") ?? "0");
      if (declaredLength > this.maxImageBytes) throw new Error("GLM image is too large");
      const bytes = Buffer.from(await response.arrayBuffer());
      if (bytes.byteLength === 0 || bytes.byteLength > this.maxImageBytes) throw new Error("GLM image has an invalid size");
      return bytes;
    }
    throw new Error("too many GLM image redirects");
  }
}

function requestSignal(signal: AbortSignal | undefined, timeoutMs: number): AbortSignal {
  const timeout = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function safeError(response: Response): Promise<string> {
  const text = (await response.text()).slice(0, 1_000);
  try {
    const parsed = JSON.parse(text) as unknown;
    if (typeof parsed === "object" && parsed && "error" in parsed) {
      const error = (parsed as { error?: unknown }).error;
      if (typeof error === "object" && error && "message" in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === "string") return message;
      }
    }
  } catch {}
  return text || "unknown error";
}
