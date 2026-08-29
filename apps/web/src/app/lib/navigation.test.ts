import assert from "node:assert/strict";
import test from "node:test";

import { arbitratorCaseReviewPath, publisherTaskDestination } from "./navigation.ts";

test("publisher delivery and funding destinations keep the selected task ID", () => {
  assert.equal(publisherTaskDestination("task-A", "formal_review", "/publisher/settlement"), "/publisher/tasks/task-A/delivery");
  assert.equal(publisherTaskDestination("task-B", "formal_generating", "/publisher/settlement"), "/publisher/tasks/task-B/delivery");
  assert.equal(publisherTaskDestination("task/A", "pending_escrow", "/publisher/funding"), "/publisher/tasks/task%2FA/funding");
	assert.equal(publisherTaskDestination("task-refund", "funding_refund_pending", "/publisher/funding"), "/publisher/tasks");
  assert.equal(publisherTaskDestination("task-B", "matching", "/publisher/recommendations"), "/publisher/recommendations?taskId=task-B");
});

test("case review destinations keep the selected case ID", () => {
  const caseA = `sha256:${"a".repeat(64)}`;
  const caseB = `sha256:${"b".repeat(64)}`;
  assert.equal(arbitratorCaseReviewPath(caseA), `/arbitrator/review/${encodeURIComponent(caseA)}`);
  assert.equal(arbitratorCaseReviewPath(caseB), `/arbitrator/review/${encodeURIComponent(caseB)}`);
  assert.notEqual(arbitratorCaseReviewPath(caseA), arbitratorCaseReviewPath(caseB));
});
