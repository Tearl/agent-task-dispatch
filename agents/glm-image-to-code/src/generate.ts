import {
  buildImageToCodeMessages,
  type GeneratedProject,
  type ImageToCodeRequest,
  parseGeneratedProjectText,
} from "@agent-platform/image-to-code-core";
import { glmImageToCodeAgent } from "./agent.ts";

export async function generateCodeFromImage(input: ImageToCodeRequest, signal?: AbortSignal): Promise<GeneratedProject> {
  const result = await glmImageToCodeAgent.generate(buildImageToCodeMessages(input), { abortSignal: signal });
  return parseGeneratedProjectText(result.text);
}
