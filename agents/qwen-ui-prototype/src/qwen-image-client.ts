import { assertPublicHttpsUrl } from "@agent-platform/agent-runtime";
import { z } from "zod";
import type { ImageRequest } from "./domain.ts";

const responseSchema = z.object({
  output: z.object({ choices: z.array(z.object({
    message: z.object({ content: z.array(z.object({ image: z.url() })).min(1) }),
  })).min(1) }),
});

export interface QwenImageClientOptions {
  apiKey: string;
  baseUrl: string;
  model: string;
  timeoutMs?: number;
  maxImageBytes?: number;
  fetchImpl?: typeof fetch;
  validateImageUrl?: (url: string) => Promise<URL>;
}

export class QwenImageClient {
  private readonly options: Required<Omit<QwenImageClientOptions, "fetchImpl" | "validateImageUrl">> & {
    fetchImpl: typeof fetch;
    validateImageUrl: (url: string) => Promise<URL>;
  };

  constructor(options: QwenImageClientOptions) {
    this.options = {
      ...options,
      baseUrl: options.baseUrl.replace(/\/$/u, ""),
      timeoutMs: options.timeoutMs ?? 180_000,
      maxImageBytes: options.maxImageBytes ?? 30 * 1024 * 1024,
      fetchImpl: options.fetchImpl ?? fetch,
      validateImageUrl: options.validateImageUrl ?? assertPublicHttpsUrl,
    };
  }

  async generate(prompt: string, size: ImageRequest["size"], signal?: AbortSignal): Promise<Buffer> {
    const response = await this.options.fetchImpl(`${this.options.baseUrl}/api/v1/services/aigc/multimodal-generation/generation`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.options.apiKey}`, "Content-Type": "application/json" },
      body: JSON.stringify({
        model: this.options.model,
        input: { messages: [{ role: "user", content: [{ text: uiPrototypePrompt(prompt) }] }] },
        parameters: {
          prompt_extend: true,
          prompt_extend_mode: "agent",
          enable_thinking: true,
          negative_prompt: "模糊文字，乱码，重复组件，错位栅格，不一致间距，无法实现的控件，设备外框，透视变形，低分辨率",
          size: size.replace("x", "*"),
          n: 1,
          watermark: false,
        },
      }),
      signal: requestSignal(signal, this.options.timeoutMs),
    });
    if (!response.ok) throw new Error(`Qwen-Image API returned ${response.status}: ${await safeError(response)}`);
    const parsed = responseSchema.parse(await response.json());
    return this.download(parsed.output.choices[0]!.message.content[0]!.image, signal);
  }

  private async download(rawUrl: string, signal?: AbortSignal): Promise<Buffer> {
    let current = await this.options.validateImageUrl(rawUrl);
    for (let redirects = 0; redirects <= 3; redirects += 1) {
      const response = await this.options.fetchImpl(current, { redirect: "manual", signal: requestSignal(signal, this.options.timeoutMs) });
      if (response.status >= 300 && response.status < 400) {
        const location = response.headers.get("location");
        if (!location || redirects === 3) throw new Error("invalid Qwen image redirect");
        current = await this.options.validateImageUrl(new URL(location, current).toString());
        continue;
      }
      if (!response.ok) throw new Error(`Qwen image download returned ${response.status}`);
      const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
      if (!contentType.startsWith("image/")) throw new Error("Qwen result URL did not return an image");
      const declaredLength = Number(response.headers.get("content-length") ?? "0");
      if (declaredLength > this.options.maxImageBytes) throw new Error("Qwen image is too large");
      const bytes = Buffer.from(await response.arrayBuffer());
      if (bytes.byteLength === 0 || bytes.byteLength > this.options.maxImageBytes) throw new Error("Qwen image has an invalid size");
      return bytes;
    }
    throw new Error("too many Qwen image redirects");
  }
}

export function uiPrototypePrompt(prompt: string): string {
  return [
    "生成一张可供前端工程师实现的软件 UI/UX 高保真设计稿原型。",
    "使用平视正投影视角，只展示完整页面画布，不要展示手机、电脑、桌面或手持设备外框。",
    "布局必须遵循清晰栅格、一致间距、真实可实现的组件层级，并保证关键中英文标题和按钮清晰可读。",
    "明确呈现导航、主要内容、交互状态和视觉层级；避免装饰性伪文字。",
    `产品需求：${prompt}`,
  ].join("\n").slice(0, 12_000);
}

function requestSignal(signal: AbortSignal | undefined, timeoutMs: number): AbortSignal {
  const timeout = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function safeError(response: Response): Promise<string> {
  const text = (await response.text()).slice(0, 1_000);
  try {
    const parsed = JSON.parse(text) as { message?: unknown; code?: unknown };
    if (typeof parsed.message === "string") return `${String(parsed.code ?? "provider_error")}: ${parsed.message}`;
  } catch {}
  return text || "unknown error";
}
