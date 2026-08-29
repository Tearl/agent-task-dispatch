import assert from "node:assert/strict";
import test from "node:test";

import {
  EngineConnectionError,
  EngineRequestTimeoutError,
  EngineResponseTooLargeError,
  requestEngine,
  resolveEngineRequestTimeoutMs,
} from "./engine-http.ts";
import { InvalidEngineResponseError, mapEngineFailure } from "./engine.ts";

test("Engine transport bounds fetch and response-read time", async () => {
  const neverFetch: typeof fetch = async () => new Promise<Response>(() => undefined);
  await assert.rejects(
    requestEngine("http://engine/slow-fetch", {}, { fetch: neverFetch, timeoutMs: 10 }),
    EngineRequestTimeoutError,
  );

  const neverEndingBody = new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(new TextEncoder().encode("{")); },
  });
  await assert.rejects(
    requestEngine("http://engine/slow-body", {}, {
      fetch: async () => new Response(neverEndingBody),
      timeoutMs: 10,
    }),
    EngineRequestTimeoutError,
  );
});

test("Engine transport distinguishes connection failures and both oversized response forms", async () => {
  await assert.rejects(
    requestEngine("http://engine/disconnected", {}, {
      fetch: async () => { throw new TypeError("socket details must not escape"); },
    }),
    EngineConnectionError,
  );

  await assert.rejects(
    requestEngine("http://engine/declared-large", {}, {
      fetch: async () => new Response("{}", { headers: { "content-length": "1048577" } }),
    }),
    EngineResponseTooLargeError,
  );

  const streamed = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array(700_000));
      controller.enqueue(new Uint8Array(400_000));
      controller.close();
    },
  });
  await assert.rejects(
    requestEngine("http://engine/streamed-large", {}, {
      fetch: async () => new Response(streamed),
    }),
    EngineResponseTooLargeError,
  );
});

test("Engine failures have stable public status and error mappings", () => {
  assert.deepEqual(mapEngineFailure(new EngineRequestTimeoutError()), { status: 504, body: { error: "engine request timed out" } });
  assert.deepEqual(mapEngineFailure(new EngineConnectionError()), { status: 503, body: { error: "engine service temporarily unavailable" } });
  assert.deepEqual(mapEngineFailure(new EngineResponseTooLargeError()), { status: 502, body: { error: "invalid engine response" } });
  assert.deepEqual(mapEngineFailure(new InvalidEngineResponseError()), { status: 502, body: { error: "invalid engine response" } });
});

test("Engine timeout configuration is bounded", () => {
  assert.equal(resolveEngineRequestTimeoutMs({}), 5_000);
  assert.equal(resolveEngineRequestTimeoutMs({ ENGINE_REQUEST_TIMEOUT_MS: "7500" }), 7_500);
  assert.throws(() => resolveEngineRequestTimeoutMs({ ENGINE_REQUEST_TIMEOUT_MS: "0" }));
  assert.throws(() => resolveEngineRequestTimeoutMs({ ENGINE_REQUEST_TIMEOUT_MS: "not-a-number" }));
});

test("orchestration planning receives the Engine server latency budget", async () => {
  const route = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../app/api/tasks/[id]/orchestration-plans/route.ts", import.meta.url), "utf8"));
  assert.match(route, /mutationRoute\([^\n]*15_000\)/);
});
