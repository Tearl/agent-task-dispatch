export const EXECUTION_PROTOCOL_VERSION = "agent-execution-v1" as const;

export interface ToolPolicy {
  mode: "read_only" | "scoped";
  allowedTools: string[];
}

export interface OverviewBinding {
  matchRevision: number;
  allocationId: string;
  quoteHash: string;
}

export interface FormalBinding {
  assignmentId: string;
  package: string;
  version: number;
  aggregateVersion: number;
  workNonce: number;
}

export interface ExecutionEnvelope {
  protocolVersion: typeof EXECUTION_PROTOCOL_VERSION;
  operation: "create" | "status" | "cancel" | "deliverable";
  stage: "overview" | "formal";
  logicalExecutionId: string;
  attemptId: string;
  agentId: string;
  taskId: string;
  taskSpecHash: string;
  inputRef: string;
  inputHash: string;
  responsibilityCode: string;
  costCap: string;
  toolPolicy: ToolPolicy;
  deadline: string;
  idempotencyKey: string;
  callbackUrl: string;
  callbackNonce: string;
  fencingToken: number;
  overview?: OverviewBinding;
  formal?: FormalBinding;
}

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

export type ExecutionStatus = "running" | "succeeded" | "failed" | "cancelled";

export interface ExecutionRecord {
  envelope: ExecutionEnvelope;
  status: ExecutionStatus;
  usedCost: string;
  contentHash?: string;
  deliverableRef?: string;
  errorCode?: string;
}

export interface ArtifactRecord {
  id: string;
  contentHash: string;
  deliverableRef: string;
  bytes: Buffer;
}
