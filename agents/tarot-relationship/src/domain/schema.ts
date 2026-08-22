import { z } from "zod";

export const readingRequestSchema = z.object({
  relationshipStage: z.enum(["single", "crush", "dating", "committed", "separated", "post_breakup", "self_reflection"]),
  question: z.string().trim().min(2).max(300),
  context: z.string().trim().max(1_500).optional(),
  feedback: z.string().trim().max(1_000).optional(),
  tone: z.enum(["gentle", "direct", "neutral"]).default("gentle"),
  drawMode: z.literal("platform_random").default("platform_random"),
  ageConfirmed: z.literal(true),
}).strict();

export const interpretationSchema = z.object({
  cardInterpretations: z.array(z.string().trim().min(1).max(1_200)).length(3),
  synthesis: z.string().trim().min(1).max(3_000),
  relationshipDynamics: z.array(z.string().trim().min(1).max(600)).min(1).max(5),
  controllableFactors: z.array(z.string().trim().min(1).max(600)).min(1).max(5),
  uncontrollableFactors: z.array(z.string().trim().min(1).max(600)).min(1).max(5),
  actionSuggestions: z.array(z.string().trim().min(1).max(600)).min(1).max(5),
  reflectionQuestions: z.array(z.string().trim().min(1).max(600)).min(1).max(5),
  uncertainty: z.string().trim().min(1).max(1_000),
}).strict();

export const overviewArtifactSchema = z.object({
  schemaVersion: z.literal("overview-result-v1"),
  understandingSummary: z.string().trim().min(1).max(4_000),
  approach: z.array(z.string().trim().min(1).max(1_000)).min(1).max(10),
  deliverableStructure: z.array(z.string().trim().min(1).max(1_000)).min(1).max(20),
  keyRisks: z.array(z.string().trim().min(1).max(1_000)).min(1).max(10),
  estimatedDurationSeconds: z.number().int().min(1).max(31_536_000),
  sample: z.string().max(4_000).optional(),
}).strict();
