import assert from "node:assert/strict";
import test from "node:test";
import { BodyTooLargeError, readBoundedBody } from "./body.ts";

test("bounded body accepts a payload at the limit", async () => {
  assert.equal(await readBoundedBody(new Request("http://test", { method: "POST", body: "1234" }), 4), "1234");
});

test("bounded body rejects an oversized streamed payload", async () => {
  await assert.rejects(() => readBoundedBody(new Request("http://test", { method: "POST", body: "12345" }), 4), BodyTooLargeError);
});
