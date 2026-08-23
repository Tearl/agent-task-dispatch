import {
  AgentExecutionAdapter,
  FileExecutionArtifactStore,
  HmacExecutionCallbackSender,
  NoopExecutionCallbackSender,
  SafeJsonExecutionInputResolver,
  type ExecutionAdapterConfig,
} from "@agent-platform/agent-runtime";
import { createImageToCodePlatformExecutor } from "@agent-platform/image-to-code-core";
import { generateCodeFromImage } from "./generate.ts";

export function createPlatformRuntime(config: ExecutionAdapterConfig) {
  const artifacts = new FileExecutionArtifactStore(config.dataDir, "qwen-image-to-code", config.publicBaseUrl);
  const callbacks = config.callbackKey
    ? new HmacExecutionCallbackSender(config.callbackKey)
    : new NoopExecutionCallbackSender();
  return {
    artifacts,
    executions: new AgentExecutionAdapter({
      executor: createImageToCodePlatformExecutor(generateCodeFromImage),
      inputs: new SafeJsonExecutionInputResolver({ userAgent: "QwenImageToCodeAgent/0.1" }),
      artifacts,
      callbacks,
      callbackKeyVersion: config.callbackKeyVersion,
    }),
  };
}
