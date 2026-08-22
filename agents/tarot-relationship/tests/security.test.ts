import { isPublicAddress } from "@agent-platform/agent-runtime";
import assert from "node:assert/strict";
import test from "node:test";

test("input resolver URL guard rejects private and special addresses", () => {
  for (const address of ["127.0.0.1", "10.1.2.3", "100.64.0.1", "172.16.0.1", "192.168.1.1", "169.254.1.1", "::1", "fd00::1", "::ffff:127.0.0.1"]) {
    assert.equal(isPublicAddress(address), false, address);
  }
  assert.equal(isPublicAddress("8.8.8.8"), true);
  assert.equal(isPublicAddress("2606:4700:4700::1111"), true);
});
