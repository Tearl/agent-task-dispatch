import { Mastra } from "@mastra/core/mastra";
import { qwenImageToCodeAgent } from "../agent.ts";

export const mastra = new Mastra({ agents: { qwenImageToCodeAgent } });
