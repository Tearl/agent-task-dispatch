import { createHash } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";

export interface ExecutionArtifact {
  id: string;
  bytes: Buffer;
  contentHash: string;
  deliverableRef: string;
}

export interface ExecutionArtifactStore {
  write(logicalExecutionId: string, value: unknown): Promise<ExecutionArtifact>;
  read(id: string): Promise<ExecutionArtifact | undefined>;
}

export class FileExecutionArtifactStore implements ExecutionArtifactStore {
  private readonly directory: string;
  private readonly agentId: string;
  private readonly publicBaseUrl?: string;

  constructor(
    directory: string,
    agentId: string,
    publicBaseUrl?: string,
  ) {
    this.directory = directory;
    this.agentId = agentId;
    this.publicBaseUrl = publicBaseUrl;
  }

  async write(logicalExecutionId: string, value: unknown): Promise<ExecutionArtifact> {
    const id = createHash("sha256").update(`${this.agentId}:artifact:${logicalExecutionId}`).digest("hex");
    const bytes = Buffer.from(JSON.stringify(value), "utf8");
    const contentHash = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
    await mkdir(this.directory, { recursive: true });
    const target = path.join(this.directory, `${id}.json`);
    const temporary = path.join(this.directory, `.${id}.${process.pid}.tmp`);
    await writeFile(temporary, bytes, { mode: 0o600 });
    await rename(temporary, target);
    return { id, bytes, contentHash, deliverableRef: this.ref(id) };
  }

  async read(id: string): Promise<ExecutionArtifact | undefined> {
    if (!/^[a-f0-9]{64}$/u.test(id)) return undefined;
    try {
      const bytes = await readFile(path.join(this.directory, `${id}.json`));
      return {
        id,
        bytes,
        contentHash: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
        deliverableRef: this.ref(id),
      };
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
      throw error;
    }
  }

  private ref(id: string): string {
    return this.publicBaseUrl
      ? `${this.publicBaseUrl.replace(/\/$/u, "")}/v1/artifacts/${id}`
      : `${this.agentId}-artifact://${id}`;
  }
}
