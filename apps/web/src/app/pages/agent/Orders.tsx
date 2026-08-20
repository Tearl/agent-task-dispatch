import { Info } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";

type OrderState = "pending" | "running" | "delivered" | "settled";

const LABEL: Record<OrderState, string> = {
  pending: "待接单",
  running: "执行中",
  delivered: "已交付",
  settled: "已结算",
};

const TONE: Record<OrderState, "amber" | "blue" | "cyan" | "green"> = {
  pending: "amber",
  running: "blue",
  delivered: "cyan",
  settled: "green",
};

interface Order {
  id: string;
  task: string;
  agent: string;
  amount: number;
  state: OrderState;
  deadline: string;
}

const ORDERS: Order[] = [
  { id: "ORD-5521", task: "竞品数据抓取与结构化", agent: "DataForge", amount: 1120, state: "running", deadline: "2026-08-24" },
  { id: "ORD-5518", task: "8 语种本地化翻译", agent: "LinguaX", amount: 620, state: "delivered", deadline: "2026-08-21" },
  { id: "ORD-5509", task: "数据建模与预测", agent: "DataForge", amount: 990, state: "pending", deadline: "2026-08-28" },
  { id: "ORD-5490", task: "合约安全审计", agent: "AuditNode", amount: 2400, state: "settled", deadline: "2026-08-12" },
];

const FILTERS: (OrderState | "all")[] = ["all", "pending", "running", "delivered", "settled"];

export default function AgentOrders() {
  const [filter, setFilter] = useState<OrderState | "all">("all");
  const list = filter === "all" ? ORDERS : ORDERS.filter((order) => order.state === filter);

  return (
    <Page>
      <PageHeader title="任务订单" subtitle="仅展示分配给本人 Agent 的订单与执行状态" />
      <InfoNote tone="violet">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} /> 订单由平台匹配分配，无需抢单；接单不缴纳任务级履约金。
        </span>
      </InfoNote>

      <div className="flex flex-wrap gap-2">
        {FILTERS.map((state) => (
          <button
            key={state}
            type="button"
            onClick={() => setFilter(state)}
            className="rounded-full border px-3.5 py-1.5 text-[13px] transition-colors"
            style={{
              borderColor: filter === state ? "var(--ap-border-strong)" : "var(--ap-border)",
              background: filter === state ? "var(--ap-violet-soft)" : "transparent",
              color: filter === state ? "#c4b5fd" : "var(--ap-text-2)",
            }}
          >
            {state === "all" ? "全部" : LABEL[state]}
          </button>
        ))}
      </div>

      <div className="space-y-3">
        {list.map((order) => (
          <Panel key={order.id} hover className="flex flex-wrap items-center justify-between gap-4 p-5">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[15px] text-[var(--ap-text)]">{order.task}</span>
                <Pill tone={TONE[order.state]}>{LABEL[order.state]}</Pill>
              </div>
              <div className="mt-1 text-[12px] text-[var(--ap-muted)]">
                {order.id} · 执行 Agent {order.agent} · 截止 {order.deadline}
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-5">
              <span className="text-[14px] text-[var(--ap-text)]">{order.amount.toLocaleString()} USDC</span>
              {order.state === "pending" ? (
                <CtaButton onClick={() => toast.success("已接单，开始执行")}>接单执行</CtaButton>
              ) : null}
              {order.state === "running" ? (
                <GhostButton onClick={() => toast.success("交付物已提交待验收")}>提交交付</GhostButton>
              ) : null}
              {order.state !== "pending" && order.state !== "running" ? <GhostButton>查看详情</GhostButton> : null}
            </div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}
