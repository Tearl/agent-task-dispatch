import { createOpenAI } from "@ai-sdk/openai";
import { Agent } from "@mastra/core/agent";
import { createTool } from "@mastra/core/tools";
import { z } from "zod";
import type { ImageRequest } from "./domain.ts";
import type { MastraImageAgent } from "./generator.ts";
import type { GlmImageClient } from "./glm-image-client.ts";

export function createMastraImageAgent(
  apiKey: string,
  baseUrl: string,
  plannerModel: string,
  imageClient: GlmImageClient,
  request: ImageRequest,
): MastraImageAgent {
  const zai = createOpenAI({ apiKey, baseURL: baseUrl, name: "zhipu" });
  const imageGeneration = createTool({
    id: "image-generation",
    description: "使用 GLM-Image 根据整理后的视觉提示词生成一张图片。",
    inputSchema: z.object({ prompt: z.string().trim().min(1).max(1_000) }),
    outputSchema: z.object({ result: z.string() }),
    execute: async ({ prompt }, options) => ({
      result: (await imageClient.generate(prompt, request.size, options?.abortSignal)).toString("base64"),
    }),
  });
  return new Agent({
    id: "one-sentence-image-agent",
    name: "一句话图片生成 Agent",
    instructions: [
      "你是图片创意总监。用户会用一句自然语言描述想要的图片。",
      "保留主体、场景、风格、构图、文字和色彩等明确要求；只补充必要的视觉细节。",
      "必须调用 imageGeneration 工具生成一张图片，不要只回复提示词。工具提示词不得超过1000字符。",
    ].join("\n"),
    model: zai.chat(plannerModel),
    tools: { imageGeneration },
  });
}
