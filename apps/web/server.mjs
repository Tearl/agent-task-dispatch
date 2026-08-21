import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { dirname, extname, join, normalize, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { createRequestListener } from "@react-router/node";

const appRoot = dirname(fileURLToPath(import.meta.url));
const clientRoot = join(appRoot, "build", "client");
const publicRoot = join(appRoot, "public");

const contentTypes = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".ico", "image/x-icon"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".map", "application/json; charset=utf-8"],
  [".png", "image/png"],
  [".svg", "image/svg+xml"],
  [".webp", "image/webp"],
  [".woff", "font/woff"],
  [".woff2", "font/woff2"],
]);

export function resolveBffOrigin(raw = process.env.BFF_URL || "http://localhost:3000") {
  const value = new URL(raw);
  if (
    (value.protocol !== "http:" && value.protocol !== "https:") ||
    value.username ||
    value.password ||
    value.pathname !== "/" ||
    value.search ||
    value.hash
  ) {
    throw new Error("BFF_URL must be an HTTP(S) origin");
  }
  return value;
}

function proxyApi(request, response, bffOrigin) {
  const incoming = new URL(request.url || "/api", "http://web.invalid");
  const forward = bffOrigin.protocol === "https:" ? httpsRequest : httpRequest;
  const headers = { ...request.headers, host: bffOrigin.host };
  delete headers.connection;
  delete headers["proxy-connection"];

  const upstream = forward({
    protocol: bffOrigin.protocol,
    hostname: bffOrigin.hostname,
    port: bffOrigin.port || undefined,
    method: request.method,
    path: `${incoming.pathname}${incoming.search}`,
    headers,
  }, (upstreamResponse) => {
    response.writeHead(upstreamResponse.statusCode || 502, upstreamResponse.headers);
    upstreamResponse.pipe(response);
  });
  upstream.on("error", () => {
    if (!response.headersSent) {
      response.writeHead(502, { "content-type": "application/json; charset=utf-8" });
    }
    response.end(JSON.stringify({ error: "bff service temporarily unavailable" }));
  });
  request.on("aborted", () => upstream.destroy());
  request.pipe(upstream);
}

function safeFile(root, pathname) {
  let decoded;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  const candidate = resolve(root, normalize(decoded).replace(/^[/\\]+/, ""));
  return candidate.startsWith(`${resolve(root)}${sep}`) ? candidate : null;
}

async function serveStatic(request, response, pathname) {
  if (request.method !== "GET" && request.method !== "HEAD") return false;
  for (const root of [clientRoot, publicRoot]) {
    const candidate = safeFile(root, pathname);
    if (!candidate) continue;
    try {
      const metadata = await stat(candidate);
      if (!metadata.isFile()) continue;
      response.writeHead(200, {
        "cache-control": pathname.startsWith("/assets/") ? "public, max-age=31536000, immutable" : "public, max-age=3600",
        "content-length": metadata.size,
        "content-type": contentTypes.get(extname(candidate)) || "application/octet-stream",
      });
      if (request.method === "HEAD") response.end();
      else createReadStream(candidate).pipe(response);
      return true;
    } catch (error) {
      if (error && error.code !== "ENOENT") throw error;
    }
  }
  return false;
}

export async function startServer(environment = process.env) {
  const bffOrigin = resolveBffOrigin(environment.BFF_URL || "http://localhost:3000");
  const port = Number(environment.PORT || 5173);
  if (!Number.isInteger(port) || port < 0 || port > 65_535) throw new Error("PORT must be a valid TCP port");
  const build = await import(pathToFileURL(join(appRoot, "build", "server", "index.js")).href);
  const router = createRequestListener({ build, mode: environment.NODE_ENV || "production" });
  const server = createServer(async (request, response) => {
    try {
      const pathname = new URL(request.url || "/", "http://web.invalid").pathname;
      if (pathname === "/api" || pathname.startsWith("/api/")) return proxyApi(request, response, bffOrigin);
      if (await serveStatic(request, response, pathname)) return;
      router(request, response);
    } catch {
      if (!response.headersSent) response.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
      response.end("Internal Server Error");
    }
  });
  await new Promise((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(port, environment.HOST || undefined, resolveListen);
  });
  const address = server.address();
  const actualPort = typeof address === "object" && address ? address.port : port;
  console.log(`[web] http://localhost:${actualPort} (/api -> ${bffOrigin.origin})`);
  return server;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const server = await startServer();
  for (const signal of ["SIGINT", "SIGTERM"]) process.once(signal, () => server.close());
}
