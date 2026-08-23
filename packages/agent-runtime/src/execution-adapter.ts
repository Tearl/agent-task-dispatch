import type { ExecutionArtifactStore } from "./execution-artifact-store.ts";
import type { ExecutionCallbackSender } from "./execution-callback.ts";
import type { ExecutionInputResolver } from "./execution-input.ts";
import {
  EXECUTION_PROTOCOL_VERSION,
  overviewResultSchema,
  type ExecutionCallback,
  type ExecutionEnvelope,
  type ExecutionStatus,
  type OverviewResult,
} from "./execution-protocol.ts";

export class ExecutionAdapterError extends Error {
  readonly code: "not_found" | "conflict" | "not_ready" | "invalid_state";

  constructor(
    code: "not_found" | "conflict" | "not_ready" | "invalid_state",
    message: string,
  ) {
    super(message);
    this.code = code;
  }
}

export interface PlatformExecutionContext {
  envelope: ExecutionEnvelope;
  signal: AbortSignal;
}

export interface PlatformExecutor<TInput, TResult> {
  parseInput(value: unknown): TInput;
  execute(input: TInput, context: PlatformExecutionContext): Promise<TResult>;
  overview(input: TInput, context: PlatformExecutionContext): Promise<OverviewResult> | OverviewResult;
}

interface ExecutionRecord {
  envelope: ExecutionEnvelope;
  status: ExecutionStatus;
  usedCost: string;
  contentHash?: string;
  deliverableRef?: string;
  errorCode?: string;
  controller: AbortController;
}

export interface AgentExecutionAdapterOptions<TInput, TResult> {
  executor: PlatformExecutor<TInput, TResult>;
  inputs: ExecutionInputResolver;
  artifacts: ExecutionArtifactStore;
  callbacks: ExecutionCallbackSender;
  callbackKeyVersion: string;
}

export class AgentExecutionAdapter<TInput, TResult> {
  private readonly records = new Map<string, ExecutionRecord>();
  private readonly options: AgentExecutionAdapterOptions<TInput, TResult>;

  constructor(options: AgentExecutionAdapterOptions<TInput, TResult>) {
    if (!options.callbackKeyVersion.trim()) throw new Error("callback key version is required");
    this.options = options;
  }

  create(envelope: ExecutionEnvelope): { accepted: boolean; status: string; reason?: string } {
    if (new Date(envelope.deadline).getTime() <= Date.now()) {
      return { accepted: false, status: "failed", reason: "deadline_exceeded" };
    }
    const existing = this.records.get(envelope.logicalExecutionId);
    if (existing) {
      if (sameIdentity(existing.envelope, envelope)) {
        return { accepted: existing.status !== "cancelled", status: existing.status };
      }
      const validRetry = envelope.fencingToken > existing.envelope.fencingToken && existing.status !== "succeeded";
      if (!validRetry) throw new ExecutionAdapterError("conflict", "execution identity conflict");
      existing.controller.abort();
    }
    const record: ExecutionRecord = {
      envelope: structuredClone(envelope),
      status: "running",
      usedCost: "0",
      controller: new AbortController(),
    };
    this.records.set(envelope.logicalExecutionId, record);
    setImmediate(() => void this.process(record));
    return { accepted: true, status: "running" };
  }

  status(envelope: ExecutionEnvelope): { status: string; usedCost: string } {
    const record = this.requireRecord(envelope);
    return { status: record.status, usedCost: record.usedCost };
  }

  cancel(envelope: ExecutionEnvelope): { accepted: boolean } {
    const record = this.requireRecord(envelope);
    if (record.status === "running") {
      record.status = "cancelled";
      record.controller.abort();
    }
    return { accepted: record.status === "cancelled" };
  }

  deliverable(envelope: ExecutionEnvelope): { contentHash: string; deliverableRef: string } {
    const record = this.requireRecord(envelope);
    if (record.status !== "succeeded" || !record.contentHash || !record.deliverableRef) {
      throw new ExecutionAdapterError("not_ready", "deliverable is not ready");
    }
    return { contentHash: record.contentHash, deliverableRef: record.deliverableRef };
  }

  private requireRecord(envelope: ExecutionEnvelope): ExecutionRecord {
    const record = this.records.get(envelope.logicalExecutionId);
    if (!record) throw new ExecutionAdapterError("not_found", "execution was not found");
    if (!sameIdentity(record.envelope, envelope)) throw new ExecutionAdapterError("conflict", "execution identity conflict");
    return record;
  }

  private async process(record: ExecutionRecord): Promise<void> {
    const cancelDeadline = scheduleDeadline(record.envelope.deadline, record.controller);
    try {
      const rawInput = await this.options.inputs.resolve(record.envelope.inputRef, record.envelope.inputHash);
      const input = this.options.executor.parseInput(rawInput);
      if (!this.isCurrent(record)) return;
      const context = { envelope: record.envelope, signal: record.controller.signal };
      const result = record.envelope.stage === "overview"
        ? overviewResultSchema.parse(await this.options.executor.overview(input, context))
        : await this.options.executor.execute(input, context);
      if (!this.isCurrent(record)) return;
      if (record.envelope.stage === "overview" && Buffer.byteLength(JSON.stringify(result)) > 64 * 1024) {
        throw new Error("overview artifact is too large");
      }
      const artifact = await this.options.artifacts.write(record.envelope.logicalExecutionId, result);
      if (!this.isCurrent(record)) return;
      record.status = "succeeded";
      record.contentHash = artifact.contentHash;
      record.deliverableRef = artifact.deliverableRef;
      await this.bestEffortCallback(record, "succeeded");
    } catch (error) {
      if (!this.isCurrent(record)) return;
      record.status = "failed";
      record.errorCode = safeErrorCode(error);
      await this.bestEffortCallback(record, "failed");
    } finally {
      cancelDeadline();
      scrubTransientReferences(record.envelope);
    }
  }

  private isCurrent(record: ExecutionRecord): boolean {
    return record.status === "running" && this.records.get(record.envelope.logicalExecutionId) === record;
  }

  private async bestEffortCallback(record: ExecutionRecord, status: "succeeded" | "failed"): Promise<void> {
    const callback: ExecutionCallback = {
      protocolVersion: EXECUTION_PROTOCOL_VERSION,
      logicalExecutionId: record.envelope.logicalExecutionId,
      attemptId: record.envelope.attemptId,
      agentId: record.envelope.agentId,
      fencingToken: record.envelope.fencingToken,
      status,
      usedCost: record.usedCost,
      ...(status === "succeeded" ? { contentHash: record.contentHash, deliverableRef: record.deliverableRef } : {}),
      timestamp: new Date().toISOString(),
      nonce: record.envelope.callbackNonce,
      keyVersion: this.options.callbackKeyVersion,
    };
    try {
      await this.options.callbacks.send(record.envelope.callbackUrl, callback);
    } catch {
      // Engine polling remains authoritative. A callback transport failure must
      // not turn one logical execution into a second billable generation.
    }
  }
}

function scheduleDeadline(deadline: string, controller: AbortController): () => void {
  let timer: NodeJS.Timeout | undefined;
  const deadlineMs = new Date(deadline).getTime();
  const arm = () => {
    const remaining = deadlineMs - Date.now();
    if (remaining <= 0) {
      controller.abort(new Error("deadline exceeded"));
      return;
    }
    timer = setTimeout(arm, Math.min(remaining, 2_147_483_647));
  };
  arm();
  return () => {
    if (timer) clearTimeout(timer);
  };
}

function sameIdentity(left: ExecutionEnvelope, right: ExecutionEnvelope): boolean {
  return left.logicalExecutionId === right.logicalExecutionId &&
    left.attemptId === right.attemptId &&
    left.agentId === right.agentId &&
    left.taskId === right.taskId &&
    left.stage === right.stage &&
    left.taskSpecHash === right.taskSpecHash &&
    left.inputHash === right.inputHash &&
    left.responsibilityCode === right.responsibilityCode &&
    left.costCap === right.costCap &&
    left.deadline === right.deadline &&
    left.idempotencyKey === right.idempotencyKey &&
    JSON.stringify(left.toolPolicy) === JSON.stringify(right.toolPolicy) &&
    JSON.stringify(left.overview) === JSON.stringify(right.overview) &&
    JSON.stringify(left.formal) === JSON.stringify(right.formal) &&
    left.fencingToken === right.fencingToken;
}

function safeErrorCode(error: unknown): string {
  if (!(error instanceof Error)) return "execution_failed";
  if (error.message.includes("hash mismatch")) return "input_hash_mismatch";
  if (error.message.includes("too large")) return "input_too_large";
  if (error.name === "AbortError" || error.message.includes("deadline")) return "deadline_exceeded";
  if ("issues" in error) return "invalid_input";
  return "execution_failed";
}

function scrubTransientReferences(envelope: ExecutionEnvelope): void {
  envelope.callbackNonce = "consumed";
  if (envelope.inputRef.startsWith("data:")) envelope.inputRef = "consumed:inline-input";
}
