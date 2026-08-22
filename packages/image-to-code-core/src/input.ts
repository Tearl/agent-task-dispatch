import { readFile } from "node:fs/promises";
import path from "node:path";

export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

export type SupportedImageMediaType = "image/png" | "image/jpeg" | "image/webp" | "image/gif";

export interface ImageInput {
  data: string;
  filename: string;
  mediaType: SupportedImageMediaType;
}

const MEDIA_TYPES = new Map<string, SupportedImageMediaType>([
  [".png", "image/png"],
  [".jpg", "image/jpeg"],
  [".jpeg", "image/jpeg"],
  [".webp", "image/webp"],
  [".gif", "image/gif"],
]);

export async function loadImageInput(filePath: string): Promise<ImageInput> {
  const resolvedPath = path.resolve(filePath);
  const mediaType = MEDIA_TYPES.get(path.extname(resolvedPath).toLowerCase());
  if (!mediaType) throw new Error("unsupported image type; use PNG, JPEG, WebP, or GIF");

  const contents = await readFile(resolvedPath);
  if (contents.byteLength === 0) throw new Error("image is empty");
  if (contents.byteLength > MAX_IMAGE_BYTES) {
    throw new Error(`image exceeds the ${MAX_IMAGE_BYTES / 1024 / 1024} MiB limit`);
  }
  assertImageSignature(contents, mediaType);

  return {
    data: contents.toString("base64"),
    filename: path.basename(resolvedPath),
    mediaType,
  };
}

function assertImageSignature(contents: Buffer, mediaType: SupportedImageMediaType): void {
  const valid = mediaType === "image/png"
    ? contents.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))
    : mediaType === "image/jpeg"
      ? contents[0] === 0xff && contents[1] === 0xd8 && contents[contents.length - 2] === 0xff && contents[contents.length - 1] === 0xd9
      : mediaType === "image/webp"
        ? contents.subarray(0, 4).toString("ascii") === "RIFF" && contents.subarray(8, 12).toString("ascii") === "WEBP"
        : ["GIF87a", "GIF89a"].includes(contents.subarray(0, 6).toString("ascii"));

  if (!valid) throw new Error(`file contents do not match ${mediaType}`);
}
