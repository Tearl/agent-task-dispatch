import assert from "node:assert/strict";
import test from "node:test";
import {
  AsyncJobService,
  type AsyncJobRecord,
  type JobRepository,
} from "../src/index.ts";

class MemoryRepository<TRequest, TResult> implements JobRepository<TRequest, TResult> {
  readonly jobs = new Map<string, AsyncJobRecord<TRequest, TResult>>();

  async create(job: AsyncJobRecord<TRequest, TResult>): Promise<void> {
    await this.save(job);
  }

  async get(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined> {
    const job = this.jobs.get(id);
    return job ? structuredClone(job) : undefined;
  }

  async save(job: AsyncJobRecord<TRequest, TResult>): Promise<void> {
    this.jobs.set(job.id, structuredClone(job));
  }
}

test("async job service completes a queued job", async () => {
  const repository = new MemoryRepository<number, number>();
  const service = new AsyncJobService({ execute: async (value) => value * 2 }, repository);
  const submitted = await service.submit(21);
  const completed = await waitForStatus(service, submitted.id, "completed");
  assert.equal(completed.result, 42);
  assert.equal(completed.error, undefined);
});

test("async job service records scoped errors", async () => {
  const repository = new MemoryRepository<string, string>();
  const service = new AsyncJobService<string, string>({
    execute: async () => { throw new Error("provider unavailable"); },
  }, repository);
  const submitted = await service.submit("request");
  const failed = await waitForStatus(service, submitted.id, "failed");
  assert.equal(failed.error, "provider unavailable");
});

test("cancel aborts a running job and prevents a late result", async () => {
  const repository = new MemoryRepository<string, string>();
  const service = new AsyncJobService<string, string>({
    execute: (_request, context) => new Promise((resolve, reject) => {
      const timer = setTimeout(() => resolve("late result"), 500);
      context.signal.addEventListener("abort", () => {
        clearTimeout(timer);
        reject(new Error("aborted"));
      }, { once: true });
    }),
  }, repository);
  const submitted = await service.submit("request");
  await waitForStatus(service, submitted.id, "running");
  const canceled = await service.cancel(submitted.id);
  assert.equal(canceled?.status, "canceled");
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal((await service.get(submitted.id))?.result, undefined);
});

async function waitForStatus<TRequest, TResult>(
  service: AsyncJobService<TRequest, TResult>,
  id: string,
  status: AsyncJobRecord<TRequest, TResult>["status"],
): Promise<AsyncJobRecord<TRequest, TResult>> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const job = await service.get(id);
    if (job?.status === status) return job;
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error(`job did not reach ${status}`);
}
