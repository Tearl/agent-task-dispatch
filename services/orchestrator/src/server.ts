import { createServer } from "node:http";
import { planTask } from "./graph.ts";

const port = Number(process.env.ORCHESTRATOR_PORT ?? "8090");
const token = process.env.ORCHESTRATOR_INTERNAL_TOKEN?.trim() ?? "";
if (!token && process.env.NODE_ENV === "production") throw new Error("ORCHESTRATOR_INTERNAL_TOKEN is required");

createServer(async (request, response) => {
  response.setHeader("content-type", "application/json");
  if (request.method === "GET" && request.url === "/health") { response.end(JSON.stringify({ status: "healthy", graphVersion: "task-orchestration-langgraph-v1" })); return; }
  if (request.method !== "POST" || request.url !== "/v1/plans") { response.writeHead(404).end(JSON.stringify({ error: "not found" })); return; }
  if (token && request.headers.authorization !== `Bearer ${token}`) { response.writeHead(401).end(JSON.stringify({ error: "unauthorized" })); return; }
  try {
    const chunks: Buffer[] = []; let size = 0;
    for await (const chunk of request) { size += chunk.length; if (size > 262_144) throw new Error("request too large"); chunks.push(chunk); }
    const result = await planTask(JSON.parse(Buffer.concat(chunks).toString("utf8")), { apiKey: process.env.DEEPSEEK_API_KEY, baseUrl: process.env.DEEPSEEK_BASE_URL, model: process.env.DEEPSEEK_MODEL });
    response.end(JSON.stringify(result));
  } catch (error) {
    response.writeHead(422).end(JSON.stringify({ error: error instanceof Error ? error.message : "planning failed" }));
  }
}).listen(port, "127.0.0.1", () => process.stdout.write(`orchestrator listening on 127.0.0.1:${port}\n`));
