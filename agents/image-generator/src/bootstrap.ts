import { AsyncJobService, FileJobRepository } from "@agent-platform/agent-runtime";
import type { GeneratedImage, ImageRequest } from "./domain.ts";
import type { ImageAgentConfig } from "./config.ts";
import { OneSentenceImageGenerator } from "./generator.ts";
import { ImageStore } from "./image-store.ts";
import { GlmImageClient } from "./glm-image-client.ts";
import { createMastraImageAgent } from "./mastra-agent.ts";

export function createRuntime(config: ImageAgentConfig) {
  const images = new ImageStore(config.dataDir, config.publicBaseUrl);
  const imageClient = new GlmImageClient({ apiKey: config.zaiApiKey, baseUrl: config.zaiBaseUrl });
  const generator = new OneSentenceImageGenerator(
    (request) => createMastraImageAgent(
      config.zaiApiKey,
      config.zaiBaseUrl,
      config.plannerModel,
      imageClient,
      request,
    ),
    images,
  );
  const jobs = new AsyncJobService<ImageRequest, GeneratedImage>(
    { execute: (request, context) => generator.generate(request, context.jobId, context.signal) },
    new FileJobRepository<ImageRequest, GeneratedImage>(config.dataDir),
    { concurrency: config.jobConcurrency },
  );
  return { jobs, images };
}
