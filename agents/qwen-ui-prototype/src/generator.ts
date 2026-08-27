import type { GeneratedImage, ImageRequest } from "./domain.ts";
import type { ImageStore } from "./image-store.ts";
import type { QwenImageClient } from "./qwen-image-client.ts";

export class QwenUiPrototypeGenerator {
  private readonly client: QwenImageClient;
  private readonly images: ImageStore;

  constructor(client: QwenImageClient, images: ImageStore) {
    this.client = client;
    this.images = images;
  }

  async generate(request: ImageRequest, jobId: string, signal?: AbortSignal): Promise<GeneratedImage> {
    const bytes = await this.client.generate(request.prompt, request.size, signal);
    const stored = await this.images.write(jobId, bytes);
    return { prompt: request.prompt, mimeType: "image/png", size: request.size, quality: request.quality, bytes: stored.bytes, sha256: stored.sha256, imageUrl: stored.url };
  }
}
