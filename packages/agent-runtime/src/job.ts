import { randomUUID } from "node:crypto";

export type AsyncJobStatus = "queued" | "running" | "completed" | "failed" | "canceled";

export interface AsyncJobRecord<TRequest, TResult> {
  recordVersion?: 1;
  id: string;
  status: AsyncJobStatus;
  createdAt: string;
  updatedAt: string;
  request: TRequest;
  result?: TResult;
  error?: string;
}

export interface JobRepository<TRequest, TResult> {
  create(job: AsyncJobRecord<TRequest, TResult>): Promise<void>;
  get(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined>;
  save(job: AsyncJobRecord<TRequest, TResult>): Promise<void>;
}

export interface JobExecutionContext {
  jobId: string;
  signal: AbortSignal;
}

export interface JobExecutor<TRequest, TResult> {
  execute(request: TRequest, context: JobExecutionContext): Promise<TResult>;
}

export interface AsyncJobServiceOptions {
  concurrency?: number;
  errorMessage?: (error: unknown) => string;
}

export class AsyncJobService<TRequest, TResult> {
  private readonly executor: JobExecutor<TRequest, TResult>;
  private readonly jobs: JobRepository<TRequest, TResult>;
  private readonly concurrency: number;
  private readonly errorMessage: (error: unknown) => string;
  private readonly pending: string[] = [];
  private readonly controllers = new Map<string, AbortController>();
  private activeCount = 0;

  constructor(
    executor: JobExecutor<TRequest, TResult>,
    jobs: JobRepository<TRequest, TResult>,
    options: AsyncJobServiceOptions = {},
  ) {
    this.executor = executor;
    this.jobs = jobs;
    const concurrency = options.concurrency ?? 2;
    if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 100) {
      throw new Error("job concurrency must be an integer between 1 and 100");
    }
    this.concurrency = concurrency;
    this.errorMessage = options.errorMessage ?? defaultErrorMessage;
  }

  async submit(request: TRequest): Promise<AsyncJobRecord<TRequest, TResult>> {
    const now = new Date().toISOString();
    const job: AsyncJobRecord<TRequest, TResult> = {
      recordVersion: 1,
      id: randomUUID(),
      status: "queued",
      createdAt: now,
      updatedAt: now,
      request,
    };
    await this.jobs.create(job);
    this.pending.push(job.id);
    setImmediate(() => void this.drain());
    return job;
  }

  get(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined> {
    return this.jobs.get(id);
  }

  async cancel(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined> {
    const job = await this.jobs.get(id);
    if (!job) return undefined;
    if (job.status === "completed" || job.status === "failed" || job.status === "canceled") return job;
    job.status = "canceled";
    job.updatedAt = new Date().toISOString();
    await this.jobs.save(job);
    this.controllers.get(id)?.abort();
    return job;
  }

  private async drain(): Promise<void> {
    while (this.activeCount < this.concurrency) {
      const id = this.pending.shift();
      if (!id) return;
      this.activeCount += 1;
      void this.execute(id).finally(() => {
        this.activeCount -= 1;
        void this.drain();
      });
    }
  }

  private async execute(id: string): Promise<void> {
    const job = await this.jobs.get(id);
    if (!job || job.status !== "queued") return;
    job.status = "running";
    job.updatedAt = new Date().toISOString();
    await this.jobs.save(job);
    const controller = new AbortController();
    this.controllers.set(id, controller);

    try {
      const result = await this.executor.execute(job.request, { jobId: id, signal: controller.signal });
      const latest = await this.jobs.get(id);
      if (!latest || latest.status === "canceled") return;
      latest.result = result;
      latest.status = "completed";
      latest.updatedAt = new Date().toISOString();
      await this.jobs.save(latest);
    } catch (error) {
      const latest = await this.jobs.get(id);
      if (!latest || latest.status === "canceled") return;
      latest.status = "failed";
      latest.error = this.errorMessage(error);
      latest.updatedAt = new Date().toISOString();
      await this.jobs.save(latest);
    } finally {
      this.controllers.delete(id);
    }
  }
}

function defaultErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "unknown job execution error";
}
