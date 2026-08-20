import {
  ArrowLeft,
  ArrowRight,
  Bot,
  Check,
  CheckCircle2,
  Clock,
  Eye,
  FileCheck2,
  Info,
  Route,
  ShieldCheck,
  Sparkles,
  Target,
  TrendingUp,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { Bar, GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";
import { AGENTS, type AgentCandidate } from "../../lib/mock";
import { buildTaskAnalysis, type PublisherFlowState, type TaskAnalysis } from "../../lib/publisher-flow";

const FALLBACK_PROMPT = "抓取 3 个竞品官网的价格并整理成结构化表格";
type RecommendedAgent = AgentCandidate & { match: number; reasons: string[] };

export default function AgentRecommendations() {
  const navigate = useNavigate();
  const location = useLocation();
  const flowState = (location.state ?? {}) as PublisherFlowState;
  const prompt = flowState.prompt?.trim() || FALLBACK_PROMPT;
  const analysis = flowState.analysis ?? buildTaskAnalysis(prompt, flowState.category, flowState.depth);
  const revision = flowState.analysisRevision ?? 1;
  const recommendations = useMemo(() => rankAgents(analysis), [analysis]);
  const [selected, setSelected] = useState<string | null>(flowState.selectedAgentId ?? null);
  const [overviewAgentId, setOverviewAgentId] = useState<string | null>(null);
  const overviewAgent = recommendations.find((agent) => agent.id === overviewAgentId);

  const backToAnalysis = () => {
    navigate("/publisher/matching", { state: { ...flowState, prompt, analysis, analysisRevision: revision, selectedAgentId: undefined } satisfies PublisherFlowState });
  };

  const continueToEscrow = () => {
    if (!selected) {
      toast.error("请先选择一个 Agent");
      return;
    }
    navigate("/publisher/publish", {
      state: { ...flowState, prompt, analysis, analysisRevision: revision, selectedAgentId: selected } satisfies PublisherFlowState,
    });
  };

  return (
    <Page>
      <PageHeader
        title="Agent 推荐"
        subtitle={`基于已确认的任务分析 R${revision} 生成候选 Agent，可先查看各自的执行概览再做选择`}
        actions={<GhostButton icon={ArrowLeft} onClick={backToAnalysis}>返回修改任务分析</GhostButton>}
      />

      <div className="flex items-center gap-3 rounded-2xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.45)] px-4 py-3">
        <FlowStep active done icon={Sparkles} label="AI 任务分析" />
        <span className="h-px flex-1 bg-[var(--ap-border-strong)]" />
        <FlowStep active icon={Bot} label="Agent 推荐" />
        <span className="h-px flex-1 bg-[var(--ap-border)]" />
        <FlowStep icon={ShieldCheck} label="确认并托管" />
      </div>

      <Panel className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2"><Pill tone="green" dot>任务分析已确认</Pill><span className="text-[11px] text-[var(--ap-muted)]">版本 R{revision}</span></div>
          <h2 className="mt-3 truncate text-[17px] text-[var(--ap-text)]">{analysis.title}</h2>
          <p className="mt-1 line-clamp-2 max-w-4xl text-[12px] leading-relaxed text-[var(--ap-muted)]">{analysis.summary}</p>
        </div>
        <div className="flex shrink-0 gap-2"><Pill tone="gray">{analysis.category}</Pill><Pill tone="cyan">预算 {analysis.budget} USDC</Pill><Pill tone="violet">{analysis.deliveryDays} 天</Pill></div>
      </Panel>

      <section className="space-y-4">
        <div className="flex items-end justify-between gap-4">
          <div><h2 className="text-[20px] text-[var(--ap-text)]">推荐候选</h2><p className="mt-1 text-[12px] text-[var(--ap-muted)]">查看概览版可比较不同 Agent 对同一任务的执行思路</p></div>
          <Pill tone="cyan" dot>{recommendations.length} 个候选</Pill>
        </div>
        <div className="grid gap-4 xl:grid-cols-3">
          {recommendations.map((agent, index) => (
            <AgentCard
              key={agent.id}
              agent={agent}
              best={index === 0}
              active={selected === agent.id}
              overviewActive={overviewAgentId === agent.id}
              budget={analysis.budget}
              onSelect={() => setSelected(agent.id)}
              onOverview={() => setOverviewAgentId(agent.id)}
            />
          ))}
        </div>
      </section>

      {overviewAgent ? <AgentOverview agent={overviewAgent} analysis={analysis} revision={revision} onSelect={() => setSelected(overviewAgent.id)} selected={selected === overviewAgent.id} /> : (
        <Panel className="grid min-h-40 place-items-center border-dashed p-6 text-center">
          <div><Eye size={22} className="mx-auto text-[var(--ap-cyan)]" /><div className="mt-3 text-[14px] text-[var(--ap-text)]">选择任一候选的“查看概览版”</div><p className="mt-1 text-[12px] text-[var(--ap-muted)]">这里会展示该 Agent 针对当前任务的执行方案预览</p></div>
        </Panel>
      )}

      <Panel strong className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
        <div><div className="text-[11px] text-[var(--ap-muted)]">当前选择</div><div className="mt-1 text-[14px] text-[var(--ap-text)]">{selected ? recommendations.find((agent) => agent.id === selected)?.name : "尚未选择 Agent"}</div></div>
        <button type="button" disabled={!selected} onClick={continueToEscrow} className="ap-cta inline-flex items-center justify-center gap-2 rounded-xl px-5 py-2.5 text-[14px] disabled:cursor-not-allowed disabled:opacity-40">确认 Agent，进入资金托管 <ArrowRight size={16} /></button>
      </Panel>
    </Page>
  );
}

function AgentCard({ agent, best, active, overviewActive, budget, onSelect, onOverview }: { agent: RecommendedAgent; best: boolean; active: boolean; overviewActive: boolean; budget: number; onSelect: () => void; onOverview: () => void }) {
  return (
    <article className={`ap-hoverable flex h-full flex-col rounded-2xl border p-5 transition-all ${active ? "border-[var(--ap-border-strong)] bg-[rgba(34,211,238,.075)] shadow-[0_0_32px_rgba(34,211,238,.08)]" : "border-[var(--ap-border)] bg-[rgba(10,18,38,.58)]"}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3"><span className="grid h-12 w-12 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-[#67e8f9] to-[#0891b2] text-[16px] font-semibold text-[#04121c]">{agent.name[0]}</span><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><span className="text-[16px] text-white">{agent.name}</span>{best ? <Pill tone="cyan">最佳匹配</Pill> : null}</div><p className="mt-1 truncate text-[11px] text-[var(--ap-muted)]">{agent.category} · {agent.tagline}</p></div></div>
        <span className={`grid h-6 w-6 shrink-0 place-items-center rounded-full border ${active ? "border-cyan-300 bg-cyan-300 text-[#06131b]" : "border-[var(--ap-border-strong)]"}`}>{active ? <Check size={14} /> : null}</span>
      </div>
      <div className="mt-5 flex items-end justify-between"><div><div className="text-[10px] text-[var(--ap-muted)]">综合匹配分</div><div className="mt-1 text-[28px] leading-none text-[var(--ap-cyan)]">{agent.match}<span className="ml-1 text-[11px] text-[var(--ap-muted)]">/ 100</span></div></div><Pill tone={agent.price <= budget ? "green" : "amber"}>{agent.price} USDC</Pill></div>
      <div className="mt-3"><Bar value={agent.match} /></div>
      <div className="mt-4 grid grid-cols-2 gap-2"><Metric icon={Clock} label="预计交付" value={agent.eta} /><Metric icon={TrendingUp} label="成功率" value={`${agent.success}%`} /></div>
      <ul className="mt-4 flex-1 space-y-2">{agent.reasons.slice(0, 3).map((reason) => <li key={reason} className="flex items-start gap-2 text-[11.5px] leading-relaxed text-[var(--ap-text-2)]"><CheckCircle2 size={13} className="mt-0.5 shrink-0 text-[var(--ap-success)]" />{reason}</li>)}</ul>
      <div className="mt-5 grid grid-cols-2 gap-2 border-t border-[var(--ap-border)] pt-4"><button type="button" onClick={onOverview} className={`inline-flex items-center justify-center gap-1.5 rounded-xl border px-3 py-2.5 text-[12px] ${overviewActive ? "border-cyan-300/50 bg-cyan-300/10 text-[var(--ap-cyan)]" : "border-[var(--ap-border)] text-[var(--ap-text-2)]"}`}><Eye size={14} />查看概览版</button><button type="button" onClick={onSelect} className={`inline-flex items-center justify-center gap-1.5 rounded-xl px-3 py-2.5 text-[12px] ${active ? "bg-cyan-300 text-[#06131b]" : "ap-cta"}`}>{active ? <Check size={14} /> : null}{active ? "已选择" : "选择 Agent"}</button></div>
    </article>
  );
}

function AgentOverview({ agent, analysis, revision, onSelect, selected }: { agent: RecommendedAgent; analysis: TaskAnalysis; revision: number; onSelect: () => void; selected: boolean }) {
  const milestones = buildMilestones(agent, analysis);
  return (
    <Panel strong className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--ap-border)] bg-[linear-gradient(120deg,rgba(34,211,238,.12),rgba(139,92,246,.08))] px-6 py-4"><div><div className="flex items-center gap-2"><Pill tone="cyan" dot>Agent 执行概览版</Pill><span className="text-[11px] text-[var(--ap-muted)]">基于任务分析 R{revision}</span></div><h2 className="mt-2 text-[19px] text-white">{agent.name} · {analysis.title}</h2></div><button type="button" onClick={onSelect} className={`inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-[13px] ${selected ? "border border-cyan-300/40 bg-cyan-300/10 text-[var(--ap-cyan)]" : "ap-cta"}`}>{selected ? <Check size={15} /> : null}{selected ? "已选择该 Agent" : "选择该 Agent"}</button></div>
      <div className="grid gap-6 p-6 xl:grid-cols-[1.05fr_1.35fr_.8fr]">
        <OverviewSection icon={Target} title="任务理解"><p>{agent.name} 将围绕“{analysis.summary.replace(/\n/g, " ")}”执行，并以已确认的交付物和验收标准作为范围边界。</p><div className="mt-3 flex flex-wrap gap-2">{analysis.tags.map((tag) => <Pill key={tag} tone="violet">{tag}</Pill>)}</div></OverviewSection>
        <OverviewSection icon={Route} title="执行步骤"><ol className="space-y-3">{milestones.map((item, index) => <li key={item} className="flex gap-3"><span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-cyan-300/10 text-[11px] text-[var(--ap-cyan)]">{index + 1}</span><span>{item}</span></li>)}</ol></OverviewSection>
        <OverviewSection icon={FileCheck2} title="产出与报价"><ul className="space-y-2">{analysis.deliverables.slice(0, 4).map((item) => <li key={item} className="flex items-start gap-2"><CheckCircle2 size={13} className="mt-0.5 shrink-0 text-[var(--ap-success)]" />{item}</li>)}</ul><div className="mt-4 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.4)] p-3"><div className="flex justify-between"><span className="text-[var(--ap-muted)]">报价</span><span className="text-[var(--ap-cyan)]">{agent.price} USDC</span></div><div className="mt-2 flex justify-between"><span className="text-[var(--ap-muted)]">周期</span><span>{agent.eta}</span></div></div></OverviewSection>
      </div>
      <div className="border-t border-[var(--ap-border)] px-6 py-4"><InfoNote tone="amber"><span className="inline-flex items-start gap-2"><Info size={14} className="mt-0.5 shrink-0" />该概览用于候选比较，最终执行范围以托管前确认的任务、Agent 报价和验收标准为准。</span></InfoNote></div>
    </Panel>
  );
}

function rankAgents(analysis: TaskAnalysis): RecommendedAgent[] {
  return AGENTS.map((agent) => {
    let score = agent.match;
    const reasons = [...agent.reasons];
    if (agent.category === analysis.category) { score += 3; reasons.unshift("任务类型与核心能力直接匹配"); } else score -= 4;
    if (agent.price <= analysis.budget) { score += 1; reasons.unshift("套餐报价符合当前建议预算"); } else score -= Math.min(12, Math.ceil((agent.price - analysis.budget) / 100));
    const etaDays = Number(agent.eta.match(/[\d.]+/)?.[0] ?? 0);
    if (etaDays && etaDays <= analysis.deliveryDays) { score += 1; reasons.unshift("预计交付周期满足当前要求"); } else score -= 3;
    return { ...agent, match: Math.max(60, Math.min(100, score)), reasons: [...new Set(reasons)] };
  }).sort((left, right) => right.match - left.match);
}

function buildMilestones(agent: RecommendedAgent, analysis: TaskAnalysis) {
  return [
    `确认输入资料、数据范围和 ${analysis.acceptanceCriteria.length} 项验收标准`,
    `由 ${agent.name} 按“${analysis.category}”能力链执行并进行中间质量校验`,
    `在 ${agent.eta} 内提交 ${analysis.deliverables.length} 类交付物与可复验说明`,
  ];
}

function OverviewSection({ icon: Icon, title, children }: { icon: typeof Target; title: string; children: React.ReactNode }) { return <section><div className="mb-3 flex items-center gap-2 text-[13px] text-[var(--ap-text)]"><Icon size={15} className="text-[var(--ap-cyan)]" />{title}</div><div className="text-[12px] leading-relaxed text-[var(--ap-text-2)]">{children}</div></section>; }
function Metric({ icon: Icon, label, value }: { icon: typeof Clock; label: string; value: string }) { return <div className="rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.36)] px-3 py-2.5"><span className="flex items-center gap-1.5 text-[10px] text-[var(--ap-muted)]"><Icon size={12} className="text-[var(--ap-cyan)]" />{label}</span><span className="mt-1 block text-[12px] text-[var(--ap-text)]">{value}</span></div>; }
function FlowStep({ icon: Icon, label, active = false, done = false }: { icon: typeof Sparkles; label: string; active?: boolean; done?: boolean }) { return <div className={`flex shrink-0 items-center gap-2 text-[12px] ${active ? "text-[var(--ap-cyan)]" : "text-[var(--ap-muted)]"}`}><span className={`grid h-7 w-7 place-items-center rounded-full border ${active ? "border-cyan-300/40 bg-cyan-300/10" : "border-[var(--ap-border)]"}`}>{done ? <Check size={13} /> : <Icon size={13} />}</span><span className="hidden sm:inline">{label}</span></div>; }
