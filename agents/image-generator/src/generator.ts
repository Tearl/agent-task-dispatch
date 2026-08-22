import type { GeneratedImage, ImageRequest } from "./domain.ts";
import type { ImageStore } from "./image-store.ts";

export interface ImageAgentStream {
  fullStream: AsyncIterable<unknown>;
  files?: Promise<unknown[]>;
}

export interface MastraImageAgent {
  stream(prompt: string, options?: {
    abortSignal?: AbortSignal;
    maxSteps?: number;
    toolChoice?: { type: "tool"; toolName: "imageGeneration" };
  }): Promise<ImageAgentStream>;
}

export class OneSentenceImageGenerator {
  private readonly createAgent: (request: ImageRequest) => MastraImageAgent;
  private readonly images: ImageStore;

  constructor(
    createAgent: (request: ImageRequest) => MastraImageAgent,
    images: ImageStore,
  ) {
    this.createAgent = createAgent;
    this.images = images;
  }

  async generate(request: ImageRequest, jobId: string, signal?: AbortSignal): Promise<GeneratedImage> {
    const agent = this.createAgent(request);
    const stream = await agent.stream(request.prompt, {
      abortSignal: signal,
      maxSteps: 1,
      toolChoice: { type: "tool", toolName: "imageGeneration" },
    });
    let base64: string | undefined;
    let providerError: string | undefined;
    const observedEvents = new Set<string>();

    for await (const chunk of stream.fullStream) {
      observedEvents.add(describeChunk(chunk));
      const image = imageFromChunk(chunk);
      if (image) base64 = image;
      providerError ??= imageErrorFromChunk(chunk);
    }

    if (!base64 && stream.files) {
      for (const file of await stream.files) {
        observedEvents.add(describeChunk(file));
        const image = imageFromChunk(file);
        if (image) base64 = image;
      }
    }

    if (!base64) {
      const details = [...observedEvents].filter(Boolean).join(", ") || "none";
      throw new Error(
        providerError
          ? `image generation failed: ${providerError}`
          : `image agent completed without an image (observed events: ${details})`,
      );
    }
    const bytes = Buffer.from(base64, "base64");
    if (bytes.byteLength === 0) throw new Error("image agent returned an empty image");
    const stored = await this.images.write(jobId, bytes);
    return {
      prompt: request.prompt,
      mimeType: "image/png",
      size: request.size,
      quality: request.quality,
      bytes: stored.bytes,
      sha256: stored.sha256,
      imageUrl: stored.url,
    };
  }
}

function imageFromChunk(value: unknown): string | undefined {
  if (!isRecord(value)) return undefined;
  if (value.type === "file") return imageFromFilePayload(value.payload);
  if (value.type !== "tool-result" && value.type !== "tool-output-available") return undefined;

  const payload = isRecord(value.payload) ? value.payload : value;
  const toolName = payload.toolName;
  if (typeof toolName === "string" && !isImageToolName(toolName)) return undefined;
  return base64FromToolOutput(payload.result ?? payload.output);
}

function imageFromFilePayload(value: unknown): string | undefined {
  if (!isRecord(value) || typeof value.mimeType !== "string" || !value.mimeType.startsWith("image/")) return undefined;
  if (typeof value.base64 === "string" && value.base64.length > 0) return value.base64;
  return typeof value.data === "string" && value.data.length > 0 ? stripDataUrl(value.data) : undefined;
}

function describeChunk(value: unknown): string {
  if (!isRecord(value)) return typeof value;
  const payload = isRecord(value.payload) ? value.payload : value;
  const type = typeof value.type === "string" ? value.type : "object";
  const toolName = typeof payload.toolName === "string" ? `:${payload.toolName}` : "";
  return `${type}${toolName}`;
}

function base64FromToolOutput(value: unknown): string | undefined {
  if (typeof value === "string" && value.length > 0) return stripDataUrl(value);
  if (!isRecord(value)) return undefined;
  for (const key of ["result", "base64", "data", "output", "value"] as const) {
    const candidate = base64FromToolOutput(value[key]);
    if (candidate) return candidate;
  }
  return undefined;
}

function imageErrorFromChunk(value: unknown): string | undefined {
  if (!isRecord(value) || (value.type !== "tool-error" && value.type !== "error")) return undefined;
  const payload = isRecord(value.payload) ? value.payload : value;
  if (value.type === "tool-error" && typeof payload.toolName === "string" && !isImageToolName(payload.toolName)) {
    return undefined;
  }
  return errorMessage(payload.error) ?? (value.type === "tool-error" ? "provider tool error" : "provider error");
}

function errorMessage(value: unknown): string | undefined {
  if (value instanceof Error) return value.message;
  if (typeof value === "string" && value.length > 0) return value;
  if (!isRecord(value)) return undefined;
  for (const key of ["message", "error", "cause"] as const) {
    const message = errorMessage(value[key]);
    if (message) return message;
  }
  return undefined;
}

function isImageToolName(value: string): boolean {
  return value === "imageGeneration" || value === "image_generation";
}

function stripDataUrl(value: string): string {
  const match = value.match(/^data:image\/[a-z0-9.+-]+;base64,(.+)$/isu);
  return match?.[1] ?? value;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
