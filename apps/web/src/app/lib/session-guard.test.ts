import assert from "node:assert/strict";
import test from "node:test";

import { roleAccessDecision } from "./session-guard.ts";

test("arbitrator role guard blocks anonymous and wrong-role sessions", () => {
  assert.equal(roleAccessDecision({ loading: false, restoreError: null, connected: false, authorizedRoles: [] }, "arbitrator"), "login");
  assert.equal(roleAccessDecision({ loading: false, restoreError: null, connected: true, authorizedRoles: ["publisher"] }, "arbitrator"), "login");
  assert.equal(roleAccessDecision({ loading: false, restoreError: null, connected: true, authorizedRoles: ["arbitrator"] }, "arbitrator"), "allow");
});

test("arbitrator role guard preserves loading and recovery states", () => {
  assert.equal(roleAccessDecision({ loading: true, restoreError: null, connected: false, authorizedRoles: [] }, "arbitrator"), "loading");
  assert.equal(roleAccessDecision({ loading: false, restoreError: new Error("restore failed"), connected: false, authorizedRoles: [] }, "arbitrator"), "recovery");
});
