import { imageToCodeInstructions } from "@agent-platform/image-to-code-core";
import { Agent } from "@mastra/core/agent";

export const glmImageToCodeAgent = new Agent({
  id: "glm_image-to-code",
  name: "glm_image-to-code",
  description: "Uses GLM vision to reconstruct UI screenshots as runnable frontend code.",
  instructions: imageToCodeInstructions,
  model: process.env.GLM_IMAGE_TO_CODE_MODEL?.trim() || "zai/glm-4.6v",
});
