export const STATUS_LABEL: Record<string, string> = {
  draft: "草稿", pending_escrow: "待托管", funding_configuration_invalid: "托管配置无效", funding_refund_pending: "待退款", chain_reorg_pending: "链重组隔离", escrowed: "已托管", matching: "匹配中", overview_generating: "概览生成中", awaiting_selection: "待选择", assigned: "已分配", formal_generating: "执行中", formal_review: "待验收", revision_requested: "修订中", change_order_pending: "变更待处理", accepted: "已验收", settlement_pending: "结算中", settled: "已结算", cancelled: "已取消", refund_pending: "退款中", refunded: "已退款", dispute_requested: "争议申请中", disputed: "争议中", partially_settled: "部分结算", failed: "失败",
};
export const statusLabel = (status: string) => STATUS_LABEL[status] ?? status;
export const statusTone = (status: string): "gray" | "cyan" | "violet" | "blue" | "amber" | "green" | "red" => {
  if (["settled", "accepted"].includes(status)) return "green";
  if (["disputed", "dispute_requested", "failed"].includes(status)) return "red";
  if (["formal_review", "change_order_pending", "refund_pending", "funding_refund_pending", "funding_configuration_invalid", "chain_reorg_pending"].includes(status)) return "amber";
  if (["formal_generating", "assigned", "revision_requested"].includes(status)) return "blue";
  if (["matching", "overview_generating", "awaiting_selection"].includes(status)) return "violet";
  if (["escrowed", "pending_escrow", "settlement_pending"].includes(status)) return "cyan";
  return "gray";
};
export const taskAmount = (task: { overviewBudget: string; formalBudget: string }) => (BigInt(task.overviewBudget || "0") + BigInt(task.formalBudget || "0")).toString();
