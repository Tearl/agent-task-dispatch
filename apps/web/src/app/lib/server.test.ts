import assert from "node:assert/strict";
import test from "node:test";

import { resolveBffOrigin } from "../../../server.mjs";

test("production BFF target accepts only an HTTP(S) origin", () => {
  assert.equal(resolveBffOrigin("https://bff.example.test").origin, "https://bff.example.test");
  assert.throws(() => resolveBffOrigin("https://user:secret@bff.example.test"), /HTTP\(S\) origin/);
  assert.throws(() => resolveBffOrigin("https://bff.example.test/private"), /HTTP\(S\) origin/);
  assert.throws(() => resolveBffOrigin("file:///tmp/bff"), /HTTP\(S\) origin/);
});
