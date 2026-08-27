import {
  AgentExecutionAdapter, AsyncJobService, FileExecutionArtifactStore, FileJobRepository,
  HmacExecutionCallbackSender, NoopExecutionCallbackSender, SafeJsonExecutionInputResolver,
  type OverviewResult,
} from "@agent-platform/agent-runtime";
import type { SeedreamVisualDesignConfig } from "./config.ts";
import { imageRequestSchema, type GeneratedImage, type ImageRequest } from "./domain.ts";
import { SeedreamVisualDesignGenerator } from "./generator.ts";
import { ImageStore } from "./image-store.ts";
import { SeedreamClient } from "./seedream-client.ts";

export function createRuntime(config: SeedreamVisualDesignConfig) {
  const images = new ImageStore(config.dataDir, config.publicBaseUrl);
  const generator = new SeedreamVisualDesignGenerator(new SeedreamClient({ apiKey: config.arkApiKey, baseUrl: config.arkBaseUrl, model: config.model }), images);
  const jobs = new AsyncJobService<ImageRequest, GeneratedImage>(
    { execute: (request, context) => generator.generate(request, context.jobId, context.signal) },
    new FileJobRepository<ImageRequest, GeneratedImage>(config.dataDir),
    { concurrency: config.jobConcurrency },
  );
  const artifacts = new FileExecutionArtifactStore(config.dataDir, "seedream-visual-design", config.publicBaseUrl);
  const callbacks = config.callbackKey ? new HmacExecutionCallbackSender(config.callbackKey) : new NoopExecutionCallbackSender();
  const executions = new AgentExecutionAdapter({
    executor: {
      parseInput: (value) => imageRequestSchema.parse(value),
      execute: (request, context) => generator.generate(request, context.envelope.logicalExecutionId, context.signal),
      overview: (request): OverviewResult => ({
        schemaVersion: "overview-result-v1",
        understandingSummary: `使用 Seedream 生成 ${request.size} 的软件品牌视觉与高保真页面设计稿。`,
        approach: ["提炼产品定位与品牌气质", "建立差异化配色、字体和组件视觉语言", "生成并持久化可供前端参考的完整页面画布"],
        deliverableStructure: ["2K 高保真视觉设计稿", "尺寸、格式与 SHA-256 元数据", "可供前端实现参考的图片引用"],
        keyRisks: ["视觉探索稿仍需在代码阶段校准响应式布局", "生成文字可能需要工程实现时复核"],
        estimatedDurationSeconds: 180,
        sample: request.prompt.slice(0, 4_000),
      }),
    },
    inputs: new SafeJsonExecutionInputResolver({ maxBytes: 128 * 1024, userAgent: "SeedreamVisualDesignAgent/0.1", authorization: config.apiToken ? `Bearer ${config.apiToken}` : undefined }),
    artifacts,
    callbacks,
    callbackKeyVersion: config.callbackKeyVersion,
  });
  return { jobs, images, executions, artifacts };
}
