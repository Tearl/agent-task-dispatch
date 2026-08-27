import {
  AgentExecutionAdapter, AsyncJobService, FileExecutionArtifactStore, FileJobRepository,
  HmacExecutionCallbackSender, NoopExecutionCallbackSender, SafeJsonExecutionInputResolver,
  type OverviewResult,
} from "@agent-platform/agent-runtime";
import type { QwenUiPrototypeConfig } from "./config.ts";
import { imageRequestSchema, type GeneratedImage, type ImageRequest } from "./domain.ts";
import { QwenUiPrototypeGenerator } from "./generator.ts";
import { ImageStore } from "./image-store.ts";
import { QwenImageClient } from "./qwen-image-client.ts";

export function createRuntime(config: QwenUiPrototypeConfig) {
  const images = new ImageStore(config.dataDir, config.publicBaseUrl);
  const generator = new QwenUiPrototypeGenerator(new QwenImageClient({ apiKey: config.dashscopeApiKey, baseUrl: config.dashscopeBaseUrl, model: config.model }), images);
  const jobs = new AsyncJobService<ImageRequest, GeneratedImage>(
    { execute: (request, context) => generator.generate(request, context.jobId, context.signal) },
    new FileJobRepository<ImageRequest, GeneratedImage>(config.dataDir),
    { concurrency: config.jobConcurrency },
  );
  const artifacts = new FileExecutionArtifactStore(config.dataDir, "qwen-ui-prototype", config.publicBaseUrl);
  const callbacks = config.callbackKey ? new HmacExecutionCallbackSender(config.callbackKey) : new NoopExecutionCallbackSender();
  const executions = new AgentExecutionAdapter({
    executor: {
      parseInput: (value) => imageRequestSchema.parse(value),
      execute: (request, context) => generator.generate(request, context.envelope.logicalExecutionId, context.signal),
      overview: (request): OverviewResult => ({
        schemaVersion: "overview-result-v1",
        understandingSummary: `使用 Qwen-Image 3.0 Pro 生成 ${request.size} 的可实现 UI 高保真设计稿。`,
        approach: ["梳理信息架构和核心用户路径", "构建一致的组件栅格与视觉层级", "生成并持久化文字清晰的页面原型"],
        deliverableStructure: ["高保真 UI 原型图片", "尺寸、格式与 SHA-256 元数据", "可供前端实现参考的图片引用"],
        keyRisks: ["图片模型输出不等同于精确设计 token 或可编辑 Figma 文件", "复杂小字仍需在代码实现阶段校正"],
        estimatedDurationSeconds: 180,
        sample: request.prompt.slice(0, 4_000),
      }),
    },
    inputs: new SafeJsonExecutionInputResolver({ maxBytes: 128 * 1024, userAgent: "QwenUiPrototypeAgent/0.1", authorization: config.apiToken ? `Bearer ${config.apiToken}` : undefined }),
    artifacts,
    callbacks,
    callbackKeyVersion: config.callbackKeyVersion,
  });
  return { jobs, images, executions, artifacts };
}
