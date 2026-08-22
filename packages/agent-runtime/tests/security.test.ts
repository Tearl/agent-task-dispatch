import assert from "node:assert/strict";
import test from "node:test";
import { htmlToText, isPublicAddress } from "../src/index.ts";

test("runtime rejects private and mapped loopback addresses", () => {
  for (const address of ["127.0.0.1", "10.0.0.1", "100.64.0.1", "192.168.0.1", "::1", "fd00::1", "::ffff:7f00:1"]) {
    assert.equal(isPublicAddress(address), false, address);
  }
  assert.equal(isPublicAddress("8.8.8.8"), true);
});

test("runtime strips scripts and markup from evidence text", () => {
  assert.equal(htmlToText("<script>bad()</script><p>A &amp; B</p>"), "A & B");
});
