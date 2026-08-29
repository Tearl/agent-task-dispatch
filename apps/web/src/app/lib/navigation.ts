export function publisherTaskDestination(taskId: string, status: string, actionPath: string): string {
  const encodedTaskId = encodeURIComponent(taskId);
  if (status === "funding_refund_pending") return "/publisher/tasks";
  if (["pending_escrow", "funding_configuration_invalid"].includes(status)) return `/publisher/tasks/${encodedTaskId}/funding`;
  if (status === "formal_review" || status === "formal_generating") return `/publisher/tasks/${encodedTaskId}/delivery`;
  if (actionPath === "/publisher/recommendations") return `${actionPath}?taskId=${encodedTaskId}`;
  return actionPath;
}

export function arbitratorCaseReviewPath(caseId: string): string {
  return `/arbitrator/review/${encodeURIComponent(caseId)}`;
}
