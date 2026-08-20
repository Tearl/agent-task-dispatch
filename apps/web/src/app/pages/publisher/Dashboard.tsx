import {
  ArrowRight,
  FileText,
  ListChecks,
  Lock,
  PackageCheck,
  ShieldCheck,
  Sparkles,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { useNavigate } from "react-router";

import { Page } from "../../components/AppShell";
import { AiComposer } from "../../components/kit/AiComposer";
import { Panel, Pill, SectionTitle, StatCard } from "../../components/kit/primitives";
import { STATUS_LABEL, STATUS_TONE, TASKS } from "../../lib/mock";

const STEPS = [
  {
    icon: FileText,
    title: "一句话描述",
    desc: "AI 自动拆解需求、生成验收标准与预算建议",
    color: "#22d3ee",
  },
  {
    icon: Lock,
    title: "链上单边托管",
    desc: "全额托管任务款，Agent 零履约金，资金可追踪",
    color: "#8b5cf6",
  },
  {
    icon: Sparkles,
    title: "AI 智能匹配",
    desc: "推荐最多 3 个候选 Agent，含匹配分与五维信誉",
    color: "#38bdf8",
  },
  {
    icon: PackageCheck,
    title: "验收即结算",
    desc: "结算前收益归你，满意后一键链上结算",
    color: "#34d399",
  },
];

export default function PublisherDashboard() {
  const navigate = useNavigate();
  const todo = TASKS.filter((task) => ["delivered", "matching", "disputed"].includes(task.status));

  return (
    <Page>
      <div className="pb-2 pt-2">
        <AiComposer />
      </div>

      <Panel className="p-5 sm:p-6">
        <SectionTitle
          right={
            <Pill tone="cyan" dot>
              AI 托管流程
            </Pill>
          }
        >
          它是怎么运作的
        </SectionTitle>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          {STEPS.map((step, index) => (
            <div
              key={step.title}
              className="relative rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-4"
            >
              <div className="flex items-center gap-2">
                <span
                  className="grid h-9 w-9 place-items-center rounded-lg"
                  style={{ background: `${step.color}22`, color: step.color }}
                >
                  <step.icon size={17} />
                </span>
                <span className="text-[12px] text-[var(--ap-muted)]">步骤 {index + 1}</span>
              </div>
              <div className="mt-3 text-[15px] text-[var(--ap-text)]">{step.title}</div>
              <p className="mt-1 text-[12.5px] leading-snug text-[var(--ap-muted)]">{step.desc}</p>
              {index < STEPS.length - 1 ? (
                <ArrowRight
                  size={16}
                  className="absolute -right-3 top-1/2 hidden -translate-y-1/2 text-[var(--ap-border-strong)] lg:block"
                />
              ) : null}
            </div>
          ))}
        </div>
      </Panel>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="托管本金" value="10,120" unit="USDC" icon={Wallet} delta={4} hint="较上周" accent="#22d3ee" />
        <StatCard label="托管期实际收益" value="46.5" unit="USDC" icon={TrendingUp} delta={12} accent="#34d399" />
        <StatCard label="进行中任务" value="3" unit="个" icon={ListChecks} accent="#8b5cf6" hint="1 个待验收" />
        <StatCard label="累计已结算" value="28" unit="笔" icon={PackageCheck} delta={7} accent="#38bdf8" />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel className="p-5">
          <SectionTitle
            right={
              <button
                type="button"
                onClick={() => navigate("/publisher/tasks")}
                className="text-[12px] text-[var(--ap-cyan)]"
              >
                查看全部
              </button>
            }
          >
            待办事项
          </SectionTitle>
          <div className="space-y-3">
            {todo.map((task) => (
              <button
                key={task.id}
                type="button"
                onClick={() =>
                  navigate(
                    task.status === "delivered"
                      ? "/publisher/settlement"
                      : task.status === "matching"
                        ? "/publisher/recommendations"
                        : "/publisher/disputes",
                  )
                }
                className="ap-hoverable flex w-full items-center justify-between gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3 text-left"
              >
                <div className="min-w-0">
                  <div className="truncate text-[14px] text-[var(--ap-text)]">{task.title}</div>
                  <div className="mt-1 text-[12px] text-[var(--ap-muted)]">
                    {task.id} · {task.next}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2 sm:gap-3">
                  <Pill tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</Pill>
                  <ArrowRight size={15} className="text-[var(--ap-muted)]" />
                </div>
              </button>
            ))}
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle
            right={
              <button
                type="button"
                onClick={() => navigate("/publisher/tasks")}
                className="text-[12px] text-[var(--ap-cyan)]"
              >
                全部任务
              </button>
            }
          >
            最近任务
          </SectionTitle>
          <div className="space-y-3">
            {TASKS.slice(0, 4).map((task) => (
              <div
                key={task.id}
                className="flex items-center justify-between gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3"
              >
                <div className="min-w-0">
                  <div className="truncate text-[14px] text-[var(--ap-text)]">{task.title}</div>
                  <div className="mt-1 truncate text-[12px] text-[var(--ap-muted)]">
                    {task.category} · {task.amount.toLocaleString()} USDC ·{" "}
                    <span className="text-[var(--ap-success)]">收益 +{task.yield}</span>
                  </div>
                </div>
                <span className="shrink-0">
                  <Pill tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</Pill>
                </span>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <Panel className="flex items-center justify-between p-5">
        <span className="flex items-center gap-2 text-[13px] text-[var(--ap-text-2)]">
          <ShieldCheck size={16} className="shrink-0 text-[var(--ap-cyan)]" />
          真实合约托管 · 全链路可追踪 · Agent 接单零履约金
        </span>
      </Panel>
    </Page>
  );
}
