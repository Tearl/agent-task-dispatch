import { lookup } from "node:dns/promises";
import { isIP } from "node:net";

const blockedHostnames = new Set(["localhost", "localhost.localdomain"]);

export async function assertPublicHttpsUrl(rawUrl: string): Promise<URL> {
  const url = new URL(rawUrl);
  if (url.protocol !== "https:") throw new Error("only HTTPS sources are allowed");
  if (url.username || url.password) throw new Error("source URL credentials are forbidden");
  if (blockedHostnames.has(url.hostname.toLowerCase())) throw new Error("local source URL is forbidden");
  const addresses = await lookup(url.hostname, { all: true, verbatim: true });
  if (addresses.length === 0 || addresses.some(({ address }) => !isPublicAddress(address))) {
    throw new Error("source URL resolves to a non-public address");
  }
  return url;
}

export function isPublicAddress(address: string): boolean {
  const version = isIP(address);
  if (version === 4) return isPublicV4(address);
  if (version === 6) {
    const normalized = address.toLowerCase();
    if (normalized.startsWith("::ffff:")) {
      const mapped = mappedV4(normalized.slice(7));
      return mapped !== undefined && isPublicV4(mapped);
    }
    return !(
      normalized === "::" || normalized === "::1" ||
      normalized.startsWith("fc") || normalized.startsWith("fd") ||
      normalized.startsWith("fe8") || normalized.startsWith("fe9") ||
      normalized.startsWith("fea") || normalized.startsWith("feb") ||
      normalized.startsWith("fec") || normalized.startsWith("fed") ||
      normalized.startsWith("fee") || normalized.startsWith("fef") ||
      normalized.startsWith("ff") || normalized.startsWith("2001:db8") ||
      normalized.startsWith("2002:") || normalized.startsWith("64:ff9b:")
    );
  }
  return false;
}

function isPublicV4(address: string): boolean {
  const [a = 0, b = 0] = address.split(".").map(Number);
  return !(
    a === 0 || a === 10 || a === 127 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && (b === 0 || b === 168)) ||
    (a === 198 && (b === 18 || b === 19 || b === 51)) ||
    (a === 203 && b === 0) || a >= 224
  );
}

function mappedV4(tail: string): string | undefined {
  if (isIP(tail) === 4) return tail;
  const parts = tail.split(":");
  if (parts.length !== 2) return undefined;
  const high = Number.parseInt(parts[0] ?? "", 16);
  const low = Number.parseInt(parts[1] ?? "", 16);
  if (!Number.isInteger(high) || !Number.isInteger(low)) return undefined;
  return `${high >> 8}.${high & 255}.${low >> 8}.${low & 255}`;
}
