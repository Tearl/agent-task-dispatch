import { createHash } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";

export interface StoredImage {
  id: string;
  bytes: number;
  sha256: string;
  url: string;
}

export class ImageStore {
  private readonly directory: string;
  private readonly publicBaseUrl?: string;

  constructor(
    directory: string,
    publicBaseUrl?: string,
  ) {
    this.directory = directory;
    this.publicBaseUrl = publicBaseUrl;
  }

  async write(jobId: string, bytes: Buffer): Promise<StoredImage> {
    const id = createHash("sha256").update(`generated-image:${jobId}`).digest("hex");
    const sha256 = createHash("sha256").update(bytes).digest("hex");
    await mkdir(this.directory, { recursive: true });
    const target = path.join(this.directory, `${id}.png`);
    const temporary = path.join(this.directory, `.${id}.${process.pid}.tmp`);
    await writeFile(temporary, bytes, { mode: 0o600 });
    await rename(temporary, target);
    return { id, bytes: bytes.byteLength, sha256: `sha256:${sha256}`, url: this.url(id) };
  }

  async read(id: string): Promise<Buffer | undefined> {
    if (!/^[a-f0-9]{64}$/u.test(id)) return undefined;
    try {
      return await readFile(path.join(this.directory, `${id}.png`));
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
      throw error;
    }
  }

  private url(id: string): string {
    const imagePath = `/v1/images/${id}`;
    return this.publicBaseUrl ? `${this.publicBaseUrl}${imagePath}` : imagePath;
  }
}
