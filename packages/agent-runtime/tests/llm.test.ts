import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import test from "node:test";
import { CompatibleChatProvider, type JsonGenerationMetadata } from "../src/llm.ts";

test("compatible provider sends strict JSON Schema and exposes response metadata", async (context) => {
  let requestBody: Record<string, unknown> | undefined;
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    requestBody = JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({
      id: "response-1",
      model: "routed/model",
      choices: [{ message: { content: '{"status":"ok"}' } }],
    }));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  context.after(() => server.close());
  const port = (server.address() as AddressInfo).port;
  let metadata: JsonGenerationMetadata | undefined;

  const result = await new CompatibleChatProvider({
    baseUrl: `http://127.0.0.1:${port}/v1`,
    model: "requested/model",
    maxTokens: 2048,
  }).generate("system", "user", undefined, {
    jsonSchema: {
      name: "status_response",
      strict: true,
      schema: {
        type: "object",
        properties: { status: { type: "string", enum: ["ok"] } },
        required: ["status"],
        additionalProperties: false,
      },
    },
    onResponse(value) {
      metadata = value;
    },
  });

  assert.deepEqual(result, { status: "ok" });
  assert.deepEqual(metadata, {
    responseId: "response-1",
    model: "routed/model",
    rawContent: '{"status":"ok"}',
  });
  assert.deepEqual(requestBody?.response_format, {
    type: "json_schema",
    json_schema: {
      name: "status_response",
      strict: true,
      schema: {
        type: "object",
        properties: { status: { type: "string", enum: ["ok"] } },
        required: ["status"],
        additionalProperties: false,
      },
    },
  });
  assert.equal(requestBody?.max_tokens, 2048);
});
