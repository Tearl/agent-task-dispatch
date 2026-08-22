import { z } from "zod";
import { EXECUTION_PROTOCOL_VERSION } from "./types.ts";

const digest = z.string().regex(/^sha256:[a-f0-9]{64}$/);
const money = z.string().regex(/^(0|[1-9][0-9]{0,77})$/);

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
  inputRef: z.string().trim().min(1).max(100_000),
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
