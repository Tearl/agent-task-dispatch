import { createHash } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import type { ArtifactRecord } from "../protocol/types.ts";

export interface ArtifactStore {
  write(logicalExecutionId: string, artifact: unknown): Promise<ArtifactRecord>;
  read(artifactId: string): Promise<ArtifactRecord | undefined>;
}

export class FileArtifactStore implements ArtifactStore {
  private readonly directory: string;
  private readonly publicBaseUrl?: string;

  constructor(
    directory: string,
    publicBaseUrl?: string,
  ) {
    this.directory = directory;
    this.publicBaseUrl = publicBaseUrl;
  }

  async write(logicalExecutionId: string, artifact: unknown): Promise<ArtifactRecord> {
    const id = createHash("sha256").update(`tarot-artifact:${logicalExecutionId}`).digest("hex");
    const bytes = Buffer.from(JSON.stringify(artifact), "utf8");
    const contentHash = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
    await mkdir(this.directory, { recursive: true });
    const target = path.join(this.directory, `${id}.json`);
    const temporary = path.join(this.directory, `.${id}.${process.pid}.tmp`);
    await writeFile(temporary, bytes, { mode: 0o600 });
    await rename(temporary, target);
    return { id, bytes, contentHash, deliverableRef: this.ref(id) };
  }

  async read(artifactId: string): Promise<ArtifactRecord | undefined> {
    if (!/^[a-f0-9]{64}$/u.test(artifactId)) return undefined;
    try {
      const bytes = await readFile(path.join(this.directory, `${artifactId}.json`));
      return {
        id: artifactId,
        bytes,
        contentHash: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
        deliverableRef: this.ref(artifactId),
      };
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
      throw error;
    }
  }

  private ref(id: string): string {
    return this.publicBaseUrl
      ? `${this.publicBaseUrl.replace(/\/$/, "")}/v1/artifacts/${id}`
      : `tarot-artifact://${id}`;
  }
}
