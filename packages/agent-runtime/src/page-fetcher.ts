import { assertPublicHttpsUrl } from "./public-url.ts";

export interface PageFetcher {
  fetchText(url: string, signal?: AbortSignal): Promise<string>;
}

export interface SafePageFetcherOptions {
  maxResponseBytes?: number;
  maxTextCharacters?: number;
  timeoutMs?: number;
  maxRedirects?: number;
  userAgent?: string;
}

export class SafePageFetcher implements PageFetcher {
  private readonly options: Required<SafePageFetcherOptions>;

  constructor(options: SafePageFetcherOptions = {}) {
    this.options = {
      maxResponseBytes: options.maxResponseBytes ?? 1_500_000,
      maxTextCharacters: options.maxTextCharacters ?? 6_000,
      timeoutMs: options.timeoutMs ?? 12_000,
      maxRedirects: options.maxRedirects ?? 3,
      userAgent: options.userAgent ?? "AgentPlatformRuntime/0.1",
    };
  }

  async fetchText(rawUrl: string, signal?: AbortSignal): Promise<string> {
    let current = await assertPublicHttpsUrl(rawUrl);
    for (let redirects = 0; redirects <= this.options.maxRedirects; redirects += 1) {
      const response = await fetch(current, {
        redirect: "manual",
        headers: { "User-Agent": this.options.userAgent },
        signal: combinedSignal(signal, this.options.timeoutMs),
      });
      if (response.status >= 300 && response.status < 400) {
        const location = response.headers.get("location");
        if (!location || redirects === this.options.maxRedirects) throw new Error("invalid source redirect");
        current = await assertPublicHttpsUrl(new URL(location, current).toString());
        continue;
      }
      if (!response.ok) throw new Error(`source returned ${response.status}`);
      const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
      if (!contentType.includes("text/html") && !contentType.includes("text/plain")) {
        throw new Error("unsupported source content type");
      }
      const declaredLength = Number(response.headers.get("content-length") ?? "0");
      if (declaredLength > this.options.maxResponseBytes) throw new Error("source response is too large");
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.byteLength > this.options.maxResponseBytes) throw new Error("source response is too large");
      return htmlToText(new TextDecoder().decode(bytes)).slice(0, this.options.maxTextCharacters);
    }
    throw new Error("too many source redirects");
  }
}

function combinedSignal(signal: AbortSignal | undefined, timeoutMs: number): AbortSignal {
  const timeout = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

export function htmlToText(input: string): string {
  return input
    .replace(/<(script|style|noscript|svg)[^>]*>[\s\S]*?<\/\1>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, "\"")
    .replace(/&#39;/gi, "'")
    .replace(/\s+/g, " ")
    .trim();
}
