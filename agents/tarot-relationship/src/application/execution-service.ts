import { overviewArtifactSchema } from "../domain/schema.ts";
import type { TarotReadingService } from "./reading-service.ts";
import type { CallbackSender } from "../protocol/callback.ts";
import type { InputResolver } from "../protocol/input-resolver.ts";
import { EXECUTION_PROTOCOL_VERSION, type ExecutionCallback, type ExecutionEnvelope, type ExecutionRecord } from "../protocol/types.ts";
import type { ArtifactStore } from "../storage/artifact-store.ts";

export class ExecutionServiceError extends Error {
  readonly code: "not_found" | "conflict" | "not_ready" | "invalid_state";

  constructor(
    code: "not_found" | "conflict" | "not_ready" | "invalid_state",
    message: string,
  ) {
    super(message);
    this.code = code;
  }
}

export class TarotExecutionService {
  private readonly records = new Map<string, ExecutionRecord>();
  private readonly readings: TarotReadingService;
  private readonly inputs: InputResolver;
  private readonly artifacts: ArtifactStore;
  private readonly callbacks: CallbackSender;
  private readonly callbackKeyVersion: string;

  constructor(
    readings: TarotReadingService,
    inputs: InputResolver,
    artifacts: ArtifactStore,
    callbacks: CallbackSender,
    callbackKeyVersion: string,
  ) {
    this.readings = readings;
    this.inputs = inputs;
    this.artifacts = artifacts;
    this.callbacks = callbacks;
    this.callbackKeyVersion = callbackKeyVersion;
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
      if (!validRetry) throw new ExecutionServiceError("conflict", "execution identity conflict");
    }
    this.records.set(envelope.logicalExecutionId, { envelope, status: "running", usedCost: "0" });
    setImmediate(() => void this.process(envelope.logicalExecutionId));
    return { accepted: true, status: "running" };
  }

  status(envelope: ExecutionEnvelope): { status: string; usedCost: string } {
    const record = this.requireRecord(envelope);
    return { status: record.status, usedCost: record.usedCost };
  }

  cancel(envelope: ExecutionEnvelope): { accepted: boolean } {
    const record = this.requireRecord(envelope);
    if (record.status === "running") record.status = "cancelled";
    return { accepted: record.status === "cancelled" };
  }

  deliverable(envelope: ExecutionEnvelope): { contentHash: string; deliverableRef: string } {
    const record = this.requireRecord(envelope);
    if (record.status !== "succeeded" || !record.contentHash || !record.deliverableRef) {
      throw new ExecutionServiceError("not_ready", "deliverable is not ready");
    }
    return { contentHash: record.contentHash, deliverableRef: record.deliverableRef };
  }

  private requireRecord(envelope: ExecutionEnvelope): ExecutionRecord {
    const record = this.records.get(envelope.logicalExecutionId);
    if (!record) throw new ExecutionServiceError("not_found", "execution was not found");
    if (!sameIdentity(record.envelope, envelope)) throw new ExecutionServiceError("conflict", "execution identity conflict");
    return record;
  }

  private async process(logicalExecutionId: string): Promise<void> {
    const record = this.records.get(logicalExecutionId);
    if (!record || record.status !== "running") return;
    try {
      const body = await this.inputs.resolve(record.envelope.inputRef, record.envelope.inputHash);
      if (!this.isCurrent(record)) return;
      const artifact = await this.readings.execute({
        taskSpecHash: record.envelope.taskSpecHash,
        stage: record.envelope.stage,
        scopeId: record.envelope.formal?.assignmentId ?? `${record.envelope.taskId}:${record.envelope.agentId}`,
        formalVersion: record.envelope.formal?.version,
        body,
      });
      if (record.envelope.stage === "overview") overviewArtifactSchema.parse(artifact);
      if (!this.isCurrent(record)) return;
      const stored = await this.artifacts.write(logicalExecutionId, artifact);
      if (!this.isCurrent(record)) return;
      record.status = "succeeded";
      record.contentHash = stored.contentHash;
      record.deliverableRef = stored.deliverableRef;
      await this.bestEffortCallback(record, "succeeded");
    } catch (error) {
      if (!this.isCurrent(record)) return;
      record.status = "failed";
      record.errorCode = safeErrorCode(error);
      await this.bestEffortCallback(record, "failed");
    } finally {
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
      keyVersion: this.callbackKeyVersion,
    };
    try {
      await this.callbacks.send(record.envelope.callbackUrl, callback);
    } catch {
      // Polling remains available; callback failure must not turn a completed
      // reading into a second billable execution.
    }
  }
}

function sameIdentity(left: ExecutionEnvelope, right: ExecutionEnvelope): boolean {
  return left.logicalExecutionId === right.logicalExecutionId &&
    left.attemptId === right.attemptId &&
    left.agentId === right.agentId &&
    left.taskId === right.taskId &&
    left.taskSpecHash === right.taskSpecHash &&
    left.inputHash === right.inputHash &&
    left.fencingToken === right.fencingToken;
}

function safeErrorCode(error: unknown): string {
  if (!(error instanceof Error)) return "execution_failed";
  if (error.message.includes("hash mismatch")) return "input_hash_mismatch";
  if (error.message.includes("too large")) return "input_too_large";
  if (error.message.includes("ageConfirmed") || error.message.includes("validation")) return "invalid_input";
  return "execution_failed";
}

function scrubTransientReferences(envelope: ExecutionEnvelope): void {
  envelope.callbackNonce = "consumed";
  if (envelope.inputRef.startsWith("data:")) envelope.inputRef = "consumed:inline-input";
}
