import assert from "node:assert/strict";
import test from "node:test";
import { sessionCookie, shouldClearSessionAfterLogout, splitSession } from "./session.ts";

test("session token is separated from browser metadata", () => {
  const { token, publicSession } = splitSession({ token: "a".repeat(64), sessionId: "s", userId: "u", walletAddress: "0x1", roles: ["publisher"], expiresAt: "2026-08-22T00:00:00Z", credential: "must-not-pass-through" });
  assert.equal(token, "a".repeat(64)); assert.equal("token" in publicSession, false); assert.equal("credential" in publicSession, false);
});

test("logout preserves the cookie for retryable Engine failures", () => {
  assert.equal(shouldClearSessionAfterLogout(503), false); assert.equal(shouldClearSessionAfterLogout(204), true); assert.equal(shouldClearSessionAfterLogout(401), true);
});

test("session cookie is httpOnly, strict, and secure in production", () => {
  const cookie = sessionCookie("secret", "2026-08-22T00:00:00Z", true);
  assert.equal(cookie.httpOnly, true); assert.equal(cookie.sameSite, "strict"); assert.equal(cookie.secure, true); assert.equal(cookie.path, "/");
});
