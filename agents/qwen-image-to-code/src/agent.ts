import { generatedProjectSchema, imageToCodeInstructions } from "@agent-platform/image-to-code-core";
import { Agent } from "@mastra/core/agent";

export const qwenImageToCodeAgent = new Agent({
  id: "qwen_image-to-code",
  name: "Qwen_image-to-code",
  description: "Uses Qwen vision to reconstruct UI screenshots as runnable frontend code.",
  instructions: imageToCodeInstructions,
  model: process.env.QWEN_IMAGE_TO_CODE_MODEL?.trim() || "alibaba-cn/qwen3.7-plus",
  defaultOptions: {
    structuredOutput: { schema: generatedProjectSchema },
    providerOptions: {
      alibaba: { enableThinking: false },
    },
  },
});
