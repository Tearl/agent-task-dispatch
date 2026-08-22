import { assertPublicHttpsUrl } from "@agent-platform/agent-runtime";
import { createHash } from "node:crypto";

const maxInputBytes = 128 * 1024;

export interface InputResolver {
  resolve(inputRef: string, expectedHash: string): Promise<unknown>;
}

export class SafeInputResolver implements InputResolver {
  async resolve(inputRef: string, expectedHash: string): Promise<unknown> {
    const bytes = inputRef.startsWith("data:application/json")
      ? decodeDataUrl(inputRef)
      : await fetchHttpsInput(inputRef);
    if (bytes.byteLength > maxInputBytes) throw new Error("execution input is too large");
    const actualHash = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
    if (actualHash !== expectedHash) throw new Error("execution input hash mismatch");
    try {
      return JSON.parse(bytes.toString("utf8"));
    } catch {
      throw new Error("execution input must be valid JSON");
    }
  }
}

async function fetchHttpsInput(rawUrl: string): Promise<Buffer> {
  const url = await assertPublicHttpsUrl(rawUrl);
  const response = await fetch(url, {
    redirect: "error",
    headers: { Accept: "application/json", "User-Agent": "TarotRelationshipAgent/0.1" },
    signal: AbortSignal.timeout(10_000),
  });
  if (!response.ok) throw new Error(`input reference returned ${response.status}`);
  const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
  if (!contentType.includes("application/json")) throw new Error("input reference is not JSON");
  const declaredLength = Number(response.headers.get("content-length") ?? "0");
  if (declaredLength > maxInputBytes) throw new Error("execution input is too large");
  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.byteLength > maxInputBytes) throw new Error("execution input is too large");
  return bytes;
}

function decodeDataUrl(value: string): Buffer {
  const match = value.match(/^data:application\/json(?:(;charset=utf-8)?)(;base64)?,(.*)$/su);
  if (!match) throw new Error("unsupported JSON data URL");
  const payload = match[3] ?? "";
  try {
    return match[2] ? Buffer.from(payload, "base64") : Buffer.from(decodeURIComponent(payload), "utf8");
  } catch {
    throw new Error("invalid JSON data URL");
  }
}
