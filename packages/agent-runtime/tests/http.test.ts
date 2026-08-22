import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import test from "node:test";
import {
  createAgentHttpServer,
  type AgentJobApplication,
  type AsyncJobRecord,
} from "../src/index.ts";

interface Input { prompt: string }
interface Output { answer: string }

test("runtime HTTP contract supports auth, status, result and cancel", async (context) => {
  const job: AsyncJobRecord<Input, Output> = {
    id: "00000000-0000-4000-8000-000000000001",
    status: "completed",
    createdAt: "2026-08-22T00:00:00.000Z",
    updatedAt: "2026-08-22T00:00:01.000Z",
    request: { prompt: "question" },
    result: { answer: "response" },
  };
  const service: AgentJobApplication<Input, Output> = {
    async submit() { return job; },
    async get() { return job; },
    async cancel() { return { ...job, status: "canceled", result: undefined }; },
  };
  const server = createAgentHttpServer({
    manifest: { id: "test-agent", version: "1.0.0" },
    service,
    apiToken: "secret",
    basePath: "/v1/tasks",
    resultPath: "answer",
    parseRequest(value) {
      if (typeof value !== "object" || value === null || !("prompt" in value)) {
        throw { issues: [{ path: ["prompt"], message: "required" }] };
      }
      return value as Input;
    },
    renderText: (output) => output.answer,
    textFormat: "text",
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => server.close());
  const port = (server.address() as AddressInfo).port;
  const request = (path: string, init?: RequestInit) => fetch(`http://127.0.0.1:${port}${path}`, init);

  assert.equal((await request("/health")).status, 200);
  assert.equal((await request("/v1/tasks/jobs")).status, 401);
  const headers = { Authorization: "Bearer secret", "Content-Type": "application/json" };
  assert.deepEqual(await (await request(`/v1/tasks/jobs/${job.id}/answer`, { headers })).json(), job.result);
  assert.equal(await (await request(`/v1/tasks/jobs/${job.id}/answer?format=text`, { headers })).text(), "response");
  const canceled = await request(`/v1/tasks/jobs/${job.id}/cancel`, { method: "POST", headers });
  assert.equal((await canceled.json() as { status: string }).status, "canceled");
});
