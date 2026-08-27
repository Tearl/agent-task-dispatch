import { z } from "zod";

const responseSchema = z.object({
  data: z.array(z.object({ b64_json: z.string().min(1) })).min(1),
});

export interface SeedreamClientOptions {
  apiKey: string;
  baseUrl: string;
  model: string;
  timeoutMs?: number;
  maxImageBytes?: number;
  fetchImpl?: typeof fetch;
}

export class SeedreamClient {
  private readonly apiKey: string;
  private readonly baseUrl: string;
  private readonly model: string;
  private readonly timeoutMs: number;
  private readonly maxImageBytes: number;
  private readonly fetchImpl: typeof fetch;

  constructor(options: SeedreamClientOptions) {
    this.apiKey = options.apiKey;
    this.baseUrl = options.baseUrl.replace(/\/$/u, "");
    this.model = options.model;
    this.timeoutMs = options.timeoutMs ?? 180_000;
    this.maxImageBytes = options.maxImageBytes ?? 30 * 1024 * 1024;
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  async generate(prompt: string, signal?: AbortSignal): Promise<Buffer> {
    const response = await this.fetchImpl(`${this.baseUrl}/images/generations`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.apiKey}`, "Content-Type": "application/json" },
      body: JSON.stringify({
        model: this.model,
        prompt: visualDesignPrompt(prompt),
        size: "2K",
        response_format: "b64_json",
        output_format: "png",
        sequential_image_generation: "disabled",
        watermark: false,
        optimize_prompt_options: { mode: "standard" },
      }),
      signal: requestSignal(signal, this.timeoutMs),
    });
    if (!response.ok) throw new Error(`Seedream API returned ${response.status}: ${await safeError(response)}`);
    const parsed = responseSchema.parse(await response.json());
    const bytes = Buffer.from(parsed.data[0]!.b64_json, "base64");
    if (bytes.byteLength === 0 || bytes.byteLength > this.maxImageBytes) throw new Error("Seedream image has an invalid size");
    return bytes;
  }
}

export function visualDesignPrompt(prompt: string): string {
  return [
    "为真实软件产品生成一张高保真 UI/UX 视觉设计稿，用于前端开发原型参考。",
    "重点探索鲜明但可实现的品牌视觉、配色、字体层级、图标、插画与组件风格。",
    "平视正投影展示完整页面画布，不出现设备模型、手、桌面、透视角度或环境背景。",
    "保持清晰栅格、一致间距、完整边界和可读的关键中英文；不要使用乱码或无意义伪文字。",
    `产品需求：${prompt}`,
  ].join("\n").slice(0, 8_000);
}

function requestSignal(signal: AbortSignal | undefined, timeoutMs: number): AbortSignal {
  const timeout = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}

async function safeError(response: Response): Promise<string> {
  const text = (await response.text()).slice(0, 1_000);
  try {
    const parsed = JSON.parse(text) as { error?: { message?: unknown; code?: unknown } };
    if (typeof parsed.error?.message === "string") return `${String(parsed.error.code ?? "provider_error")}: ${parsed.error.message}`;
  } catch {}
  return text || "unknown error";
}
