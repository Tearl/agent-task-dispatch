import {
  AgentExecutionAdapter,
  AsyncJobService,
  FileExecutionArtifactStore,
  FileJobRepository,
  HmacExecutionCallbackSender,
  NoopExecutionCallbackSender,
  SafeJsonExecutionInputResolver,
  type OverviewResult,
} from "@agent-platform/agent-runtime";
import { imageRequestSchema, type GeneratedImage, type ImageRequest } from "./domain.ts";
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
  const artifacts = new FileExecutionArtifactStore(config.dataDir, "image-generator", config.publicBaseUrl);
  const callbacks = config.callbackKey
    ? new HmacExecutionCallbackSender(config.callbackKey)
    : new NoopExecutionCallbackSender();
  const executions = new AgentExecutionAdapter({
    executor: {
      parseInput: (value) => imageRequestSchema.parse(value),
      execute: (request, context) => generator.generate(request, context.envelope.logicalExecutionId, context.signal),
      overview: (request): OverviewResult => ({
        schemaVersion: "overview-result-v1",
        understandingSummary: `根据自然语言描述生成一张 ${request.size} 的高质量图片。`,
        approach: ["理解主体、场景、风格与构图要求", "补足必要的视觉细节并生成图片", "转存生成结果并校验内容摘要"],
        deliverableStructure: ["生成图片文件", "图片尺寸、格式与 SHA-256 元数据", "可访问的图片引用"],
        keyRisks: ["生成模型可能对文字、手部或精确品牌元素产生偏差", "含糊描述需要由 Agent 做视觉假设"],
        estimatedDurationSeconds: 120,
        sample: request.prompt.slice(0, 4_000),
      }),
    },
    inputs: new SafeJsonExecutionInputResolver({ maxBytes: 128 * 1024, userAgent: "ImageGeneratorAgent/0.1" }),
    artifacts,
    callbacks,
    callbackKeyVersion: config.callbackKeyVersion,
  });
  return { jobs, images, executions, artifacts };
}
