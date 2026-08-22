import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { FileJobRepository, type AsyncJobRecord } from "../src/index.ts";

test("file repository uses UUID-bound atomic records", async (context) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "agent-runtime-test-"));
  context.after(() => rm(directory, { recursive: true, force: true }));
  const repository = new FileJobRepository<{ input: string }, { output: string }>(directory);
  const job: AsyncJobRecord<{ input: string }, { output: string }> = {
    id: "00000000-0000-4000-8000-000000000001",
    status: "queued",
    createdAt: "2026-08-22T00:00:00.000Z",
    updatedAt: "2026-08-22T00:00:00.000Z",
    request: { input: "test" },
  };
  await repository.create(job);
  assert.deepEqual(await repository.get(job.id), job);
  assert.equal(await repository.get("../../escape"), undefined);
});

test("file repository reads a configured legacy result field", async (context) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "agent-runtime-legacy-test-"));
  context.after(() => rm(directory, { recursive: true, force: true }));
  const id = "00000000-0000-4000-8000-000000000002";
  await writeFile(path.join(directory, `${id}.json`), JSON.stringify({
    id,
    status: "completed",
    createdAt: "2026-08-22T00:00:00.000Z",
    updatedAt: "2026-08-22T00:00:01.000Z",
    request: { input: "legacy" },
    report: { output: "legacy result" },
  }));
  const repository = new FileJobRepository<{ input: string }, { output: string }>(directory, {
    legacyResultFields: ["report"],
  });
  assert.deepEqual((await repository.get(id))?.result, { output: "legacy result" });
});
