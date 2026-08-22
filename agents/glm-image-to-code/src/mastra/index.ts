import { Mastra } from "@mastra/core/mastra";
import { glmImageToCodeAgent } from "../agent.ts";

export const mastra = new Mastra({ agents: { glmImageToCodeAgent } });
