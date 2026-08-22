import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import type { AsyncJobRecord, JobRepository } from "./job.ts";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export interface FileJobRepositoryOptions {
  legacyResultFields?: string[];
}

export class FileJobRepository<TRequest, TResult> implements JobRepository<TRequest, TResult> {
  private readonly dataDir: string;
  private readonly legacyResultFields: string[];

  constructor(dataDir: string, options: FileJobRepositoryOptions = {}) {
    this.dataDir = dataDir;
    this.legacyResultFields = options.legacyResultFields ?? [];
  }

  async create(job: AsyncJobRecord<TRequest, TResult>): Promise<void> {
    await this.save(job);
  }

  async get(id: string): Promise<AsyncJobRecord<TRequest, TResult> | undefined> {
    if (!uuidPattern.test(id)) return undefined;
    try {
      const parsed = JSON.parse(await readFile(this.jobPath(id), "utf8")) as AsyncJobRecord<TRequest, TResult> & Record<string, unknown>;
      if (parsed.result === undefined) {
        const legacyField = this.legacyResultFields.find((field) => parsed[field] !== undefined);
        if (legacyField) parsed.result = parsed[legacyField] as TResult;
      }
      return parsed;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
      throw error;
    }
  }

  async save(job: AsyncJobRecord<TRequest, TResult>): Promise<void> {
    if (!uuidPattern.test(job.id)) throw new Error("job ID must be a UUID");
    await mkdir(this.dataDir, { recursive: true });
    const target = this.jobPath(job.id);
    const temporary = `${target}.${process.pid}.${randomUUID()}.tmp`;
    await writeFile(temporary, `${JSON.stringify(job, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
    await rename(temporary, target);
  }

  private jobPath(id: string): string {
    return path.join(this.dataDir, `${id}.json`);
  }
}
