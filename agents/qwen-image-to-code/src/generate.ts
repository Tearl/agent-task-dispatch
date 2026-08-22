import {
  buildImageToCodeMessages,
  generatedProjectSchema,
  type GeneratedProject,
  type ImageToCodeRequest,
} from "@agent-platform/image-to-code-core";
import { qwenImageToCodeAgent } from "./agent.ts";

export async function generateCodeFromImage(input: ImageToCodeRequest): Promise<GeneratedProject> {
  const result = await qwenImageToCodeAgent.generate(buildImageToCodeMessages(input), {
    structuredOutput: { schema: generatedProjectSchema },
    providerOptions: {
      alibaba: { enableThinking: false },
    },
  });
  return generatedProjectSchema.parse(result.object);
}
