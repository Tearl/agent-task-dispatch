import { z } from "zod";

export const EXECUTION_PROTOCOL_VERSION = "agent-execution-v1" as const;

const digest = z.string().regex(/^sha256:[a-f0-9]{64}$/u);
const money = z.string().regex(/^(0|[1-9][0-9]{0,77})$/u);

const toolPolicySchema = z.object({
  mode: z.enum(["read_only", "scoped"]),
  allowedTools: z.array(z.string().trim().min(1).max(100)).max(50),
}).strict();

const overviewBindingSchema = z.object({
  matchRevision: z.number().int().min(1),
  allocationId: z.string().trim().min(1).max(200),
  quoteHash: digest,
}).strict();

const formalBindingSchema = z.object({
  assignmentId: z.string().trim().min(1).max(200),
  package: z.string().trim().min(1).max(100),
  version: z.number().int().min(1).max(5),
  aggregateVersion: z.number().int().min(1),
  workNonce: z.number().int().min(1),
  scopeSpecHash: digest.optional(),
  changeOrderId: z.string().trim().min(1).max(200).optional(),
  responsibility: z.string().trim().min(1).max(100).optional(),
}).strict();

export const executionEnvelopeSchema = z.object({
  protocolVersion: z.literal(EXECUTION_PROTOCOL_VERSION),
  operation: z.enum(["create", "status", "cancel", "deliverable"]),
  stage: z.enum(["overview", "formal"]),
  logicalExecutionId: z.string().trim().min(1).max(200),
  attemptId: z.string().trim().min(1).max(240),
  agentId: z.string().trim().min(1).max(200),
  taskId: z.string().trim().min(1).max(200),
  taskSpecHash: digest,
  inputRef: z.string().trim().min(1).max(24_000_000),
  inputHash: digest,
  responsibilityCode: z.string().trim().min(1).max(100),
  costCap: money,
  toolPolicy: toolPolicySchema,
  deadline: z.iso.datetime({ offset: true }),
  idempotencyKey: z.string().trim().min(1).max(200),
  callbackUrl: z.url().refine((value) => new URL(value).protocol === "https:", "callback URL must use HTTPS"),
  callbackNonce: z.string().min(1).max(256),
  fencingToken: z.number().int().min(1),
  overview: overviewBindingSchema.optional(),
  formal: formalBindingSchema.optional(),
}).strict().superRefine((value, context) => {
  if (value.stage === "overview") {
    if (!value.overview || value.formal || value.toolPolicy.mode !== "read_only") {
      context.addIssue({ code: "custom", message: "overview execution bindings are invalid" });
    }
  } else if (!value.formal || value.overview) {
    context.addIssue({ code: "custom", message: "formal execution bindings are invalid" });
  }
});

export type ExecutionEnvelope = z.infer<typeof executionEnvelopeSchema>;
export type ExecutionStatus = "running" | "succeeded" | "failed" | "cancelled";

export interface ExecutionCallback {
  protocolVersion: typeof EXECUTION_PROTOCOL_VERSION;
  logicalExecutionId: string;
  attemptId: string;
  agentId: string;
  fencingToken: number;
  status: "succeeded" | "failed";
  usedCost: string;
  contentHash?: string;
  deliverableRef?: string;
  timestamp: string;
  nonce: string;
  keyVersion: string;
}

export const overviewResultSchema = z.object({
  schemaVersion: z.literal("overview-result-v1"),
  understandingSummary: z.string().trim().min(1).max(4_000),
  approach: z.array(z.string().trim().min(1).max(1_000)).min(1).max(10),
  deliverableStructure: z.array(z.string().trim().min(1).max(1_000)).min(1).max(20),
  keyRisks: z.array(z.string().trim().min(1).max(1_000)).min(1).max(10),
  estimatedDurationSeconds: z.number().int().min(1).max(365 * 24 * 60 * 60),
  sample: z.string().max(4_000).optional(),
}).strict();

export type OverviewResult = z.infer<typeof overviewResultSchema>;
