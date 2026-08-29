import { AlertTriangle, Check, Clock, GitBranch, Layers3, Play, RefreshCw, ShieldCheck, Sparkles } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { createOrchestrationPlan, finalizeOverviewSlot, PlatformAPIError, readMatchingView, readOrchestrationPlan, readSelection, readTaskExecutions, reconcileSelection, reserveSelection, startMatching, startOverview, submitSelectionTransaction, type ExecutionView, type MatchingView, type OrchestrationPlan, type SelectionIntent, type WalletProvider } from "../../lib/platform-api";
import type { PublisherFlowState } from "../../lib/publisher-flow";

export default function AgentRecommendations() {
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const flowState = (location.state ?? {}) as PublisherFlowState;
  const taskID = params.get("taskId") ?? flowState.taskId ?? "";
  const [view, setView] = useState<MatchingView | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [intent, setIntent] = useState<SelectionIntent | null>(null);
  const [executions, setExecutions] = useState<ExecutionView[]>([]);
  const [plan, setPlan] = useState<OrchestrationPlan | null>(null);
  const [localTx, setLocalTx] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const operationID = useRef<string | undefined>(undefined);

  const load = async () => {
    if (!taskID) return;
    setError(null);
    try {
      const [value, executionResult, orchestrationPlan] = await Promise.all([readMatchingView(taskID), readTaskExecutions(taskID), readOrchestrationPlan(taskID).catch((cause) => cause instanceof PlatformAPIError && cause.status === 404 ? null : Promise.reject(cause))]);
      setView(value);
      setExecutions(executionResult.executions);
      setPlan(orchestrationPlan);
      if (value.reservation) setSelected(value.reservation.agentId);
      if (value.reservation?.id) setIntent(await readSelection(taskID, value.reservation.id));
    } catch (cause) {
      setError(message(cause));
    }
  };

  useEffect(() => { void load(); }, [taskID]);

  const advance = async () => {
    if (!taskID || busy || view?.task.deletionPending) return;
    setBusy(true); setError(null);
    try {
      if (!plan) {
        setPlan(await createOrchestrationPlan(taskID, crypto.randomUUID()));
      } else if (view?.batch?.status === "completed" && batchNeedsRematch(view.batch, view.snapshot?.candidates)) {
        await startMatching(taskID, crypto.randomUUID());
      } else if (!view?.snapshot || (!view.batch && view.snapshot.candidates.length === 0)) {
        await startMatching(taskID, crypto.randomUUID());
      } else if (!view.batch) {
        await startOverview(taskID, crypto.randomUUID());
      } else {
        const terminal = new Set(["succeeded", "failed", "cancelled", "cost_stopped"]);
        const byID = new Map(executions.map((item) => [item.logicalExecutionId, item]));
        const pendingSlots = view.snapshot.candidates.flatMap((candidate) => candidate.overview?.status === "dispatched" ? [candidate.overview] : []);
        let finalized = 0;
        for (const slot of pendingSlots) {
          const execution = byID.get(slot.logicalExecutionId);
          if (!execution || (!terminal.has(execution.status) && execution.status !== "running")) continue;
          await finalizeOverviewSlot(taskID, view.batch.id, slot.slotId, `${slot.slotId}:finalize`);
          finalized += 1;
        }
        if (finalized === 0 && pendingSlots.length > 0 && [...byID.values()].some((item) => item.stage === "overview" && !terminal.has(item.status))) {
          setError("概览执行仍在进行中；状态来自 Engine 权威执行记录，请稍后再次同步。");
        }
      }
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally { setBusy(false); }
  };

  const submit = async () => {
    if (!taskID || !view?.batch || !selected || busy || view.task.deletionPending || view.task.status !== "awaiting_selection") return;
    setBusy(true); setError(null);
    try {
      const currentView = await readMatchingView(taskID);
      setView(currentView);
      if (currentView.task.deletionPending || currentView.task.status !== "awaiting_selection") {
        setError("该任务已进入删除/退款流程，不能再选择 Agent。");
        return;
      }
      const candidate = currentView.snapshot?.candidates.find((item) => item.agentId === selected);
      if (!currentView.batch || !candidate?.overview || candidate.overview.status !== "valid" || candidate.overview.billingStatus !== "captured") {
        setError("只有已通过客观校验且完成概览计费的候选可以选择。");
        return;
      }
      const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
      if (!ethereum) { setError("未检测到以太坊兼容钱包。"); return; }
      const current = intent ? await readSelection(taskID, intent.reservation.id) : await reserveSelection(taskID, currentView.batch.id, candidate.overview.slotId, operationID.current ??= crypto.randomUUID());
      setIntent(current);
      const txHash = current.reservation.transactionHash ?? (localTx || await submitSelectionTransaction(ethereum, current));
      setLocalTx(txHash);
      try {
        const result = await reconcileSelection(taskID, current.reservation.id, txHash);
        setIntent({ ...current, reservation: result.reservation, platformSignature: result.reservation.status === "reserved" ? current.platformSignature : "" });
        await load();
      } catch (cause) {
        if (!(cause instanceof PlatformAPIError) || cause.status !== 425) throw cause;
        setError("交易已提交，权威链投影尚未达到确认深度。稍后点击“检查链上确认”。");
      }
    } catch (cause) {
      setError(message(cause));
    } finally { setBusy(false); }
  };

  if (!taskID) return <Page><PageHeader title="Agent 推荐" subtitle="从已发布任务进入稳定匹配快照" /><InfoNote tone="amber"><span role="alert">缺少 taskId。请先发布任务，再从发布结果进入匹配。</span></InfoNote></Page>;
  if (!view && !error) return <Page><div role="status" className="py-24 text-center text-[var(--ap-muted)]">正在读取权威匹配快照…</div></Page>;
  if (!view) return <Page><div role="alert" className="rounded-xl border border-rose-300/30 bg-rose-300/10 p-4 text-rose-100">{error}<div className="mt-3"><GhostButton icon={RefreshCw} onClick={() => void load()}>重试</GhostButton></div></div></Page>;

  const candidates = view.snapshot?.candidates ?? [];
  const txHash = intent?.reservation.transactionHash ?? localTx;
  const reservationStatus = intent?.reservation.status ?? view.reservation?.status;
  const deletionBlocked = view.task.deletionPending;
  const overviewFundingBlocked = Boolean(view.snapshot && !view.batch && !view.overviewFundingReady);
  const selectionBlocked = deletionBlocked || view.task.status !== "awaiting_selection";
  return <Page>
    <PageHeader title="Agent 推荐与执行编排" subtitle={`${view.task.title} · ${plan ? `${plan.mode === "multi" ? "多 Agent DAG" : "单 Agent"}方案` : "等待 AI 编排分析"}`} actions={<div className="flex gap-2"><GhostButton icon={RefreshCw} onClick={() => void load()}>刷新状态</GhostButton><button type="button" disabled={busy || deletionBlocked || overviewFundingBlocked || Boolean(view.reservation) || plan?.mode === "multi"} onClick={() => void advance()} className="ap-cta inline-flex items-center gap-2 rounded-xl px-4 py-2 text-[13px] disabled:opacity-40"><Play size={15} />{busy ? "处理中…" : workflowAction(view, plan)}</button></div>} />

    {deletionBlocked ? <InfoNote tone="amber"><span role="alert">该任务已进入删除/退款流程，Agent 匹配、预留和链上选择均已关闭。</span></InfoNote> : null}
    {!plan ? <InfoNote tone="cyan"><span role="status">托管已经确认。下一步由 LangGraph 先判断任务需要单 Agent 还是多 Agent DAG，生成方案后才允许进入权威匹配。</span></InfoNote> : null}
    {plan ? <OrchestrationPlanCard plan={plan} /> : null}
    {plan && !view.snapshot ? <InfoNote tone="cyan"><span role="status">编排方案已冻结，但权威匹配快照尚未生成。刷新只读取 Latest，不会触发重新规划或增加修订。</span></InfoNote> : null}
    {overviewFundingBlocked ? <InfoNote tone="amber"><span role="alert">当前 testnet-only V3 部署只托管正式预算，尚无独立 DiscoveryPool 充值通道；候选匹配快照可测试，但付费概览入口已关闭，不会推进任务状态。</span></InfoNote> : null}
    {batchNeedsRematch(view.batch, view.snapshot?.candidates) ? <InfoNote tone="amber"><span role="alert">{view.batch?.status === "completed" ? "概览批次全部失败，请重新匹配。旧批次和未结算授权将由 Engine 作废并释放。" : "概览批次已到期，请先校验并恢复 Agent 终态；Engine 会释放失败执行的未结算授权。"}</span></InfoNote> : null}
    {view.snapshot?.degradations.map((item) => <div key={`${item.dependency}:${item.code}`} role="status" className="flex gap-2 rounded-xl border border-amber-300/30 bg-amber-300/10 p-3 text-[13px] text-amber-100"><AlertTriangle size={16} className="shrink-0" /><span><b>{item.dependency}</b> · {item.code}：{item.message}</span></div>)}

    {view.snapshot ? <Panel strong className="overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b border-[var(--ap-border)] px-4 py-3 sm:px-5">
        <div><h2 className="text-[15px] text-white">为你匹配了 {candidates.length} 个 Agent</h2><p className="mt-1 text-[11px] text-[var(--ap-muted)]">选择卡片或节点，即可切换执行 Agent</p></div>
        <Pill tone="cyan">智能匹配 · R{view.snapshot.revision}</Pill>
      </div>
      {candidates.length === 0 ? <div role="status" className="p-8 text-center text-[var(--ap-muted)]">当前没有达到质量门槛的候选，系统不会用低分 Agent 补位。</div> : <>
        <AgentMatchConstellation taskTitle={view.task.title} candidates={candidates} selected={selected} selectionBlocked={selectionBlocked} reservationLocked={Boolean(view.reservation)} onSelect={setSelected} />
        <section className="grid gap-2.5 border-t border-[var(--ap-border)] p-3 sm:grid-cols-2 sm:p-4 xl:grid-cols-3" aria-label="Agent 候选比较">
          {candidates.map((candidate) => {
            const active = selected === candidate.agentId;
            const selectable = candidateSelectable(candidate, selectionBlocked) && !view.reservation;
            return <button key={candidate.agentId} type="button" aria-pressed={active} disabled={!selectable} onClick={() => setSelected(candidate.agentId)} className={`group rounded-xl border p-3.5 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60 ${active ? "border-cyan-300/60 bg-cyan-300/10 shadow-[0_0_28px_rgba(34,211,238,.12)]" : "border-[var(--ap-border)] bg-[rgba(5,9,20,.38)] hover:border-[var(--ap-border-strong)] hover:bg-white/[.035]"}`}>
              <div className="flex items-start justify-between gap-3"><div className="min-w-0"><div className="flex items-center gap-2"><span className="truncate text-[14px] text-white">{candidate.name || candidate.agentId}</span>{candidate.exploration ? <Pill tone="violet">探索</Pill> : null}</div><div className="mt-1 truncate text-[10px] text-[var(--ap-muted)]">{candidate.category}{candidate.tags.slice(0, 2).map((tag) => ` · ${tag}`)}</div></div><span className={`grid h-6 w-6 shrink-0 place-items-center rounded-full border ${active ? "border-cyan-300 bg-cyan-300 text-[#04121c]" : "border-[var(--ap-border-strong)] text-transparent"}`}><Check size={13} /></span></div>
              <div className="mt-3 grid grid-cols-3 gap-2 border-t border-[var(--ap-border)] pt-3"><CompactMetric label="匹配分" value={String(candidate.score.ranking)} accent /><CompactMetric label="正式价" value={candidate.formalPrice} /><CompactMetric label="预计耗时" value={duration(candidate.estimatedDurationSeconds)} /></div>
              <div className="mt-3 flex items-center justify-between gap-2"><span className="text-[10px] text-[var(--ap-muted)]">第 {candidate.position} 位推荐</span><Pill tone={candidateSelectable(candidate, selectionBlocked) ? "green" : "amber"}>{overviewLabel(candidate.overview)}</Pill></div>
            </button>;
          })}
        </section>
      </>}
    </Panel> : null}

    <Panel strong className="p-5">
      <div className="flex flex-wrap items-center justify-between gap-4"><div><div className="text-[11px] text-[var(--ap-muted)]">选择状态</div><div role="status" className="mt-1 text-[14px] text-[var(--ap-text)]">{deletionBlocked ? "任务已删除，Agent 选择已关闭" : reservationStatus ? `${reservationStatus}${txHash ? ` · ${short(txHash)}` : ""}` : selected ? `已选择 ${candidates.find((item)=>item.agentId===selected)?.name}` : "请选择一个有效概览"}</div></div><button type="button" disabled={selectionBlocked || !selected || busy || ["confirmed","failed","expired","orphaned"].includes(reservationStatus ?? "")} onClick={() => void submit()} className="ap-cta inline-flex items-center gap-2 rounded-xl px-5 py-2.5 text-[14px] disabled:opacity-40"><ShieldCheck size={16} />{busy ? "处理中…" : txHash ? "检查链上确认" : intent ? "提交链上选择" : "预留并选择 Agent"}</button></div>
      {error ? <div role="alert" className="mt-4 rounded-xl border border-amber-300/30 bg-amber-300/10 p-3 text-[12px] text-amber-100">{error}</div> : null}
      <div className="mt-4 flex items-start gap-2 text-[11px] text-[var(--ap-muted)]"><Sparkles size={14} className="mt-0.5" />重复点击复用同一操作 ID、预留和交易哈希；只有权威 canonical 事件确认后才创建 assignment。</div>
    </Panel>
    {(view.snapshot || executions.length > 0) ? <details className="group rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.3)]">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-[12px] text-[var(--ap-muted)] hover:text-[var(--ap-text-2)]"><span>系统与审计详情</span><span className="text-[10px] group-open:hidden">展开</span><span className="hidden text-[10px] group-open:inline">收起</span></summary>
      <div className="space-y-5 border-t border-[var(--ap-border)] p-4">
        {view.snapshot ? <div><SectionTitle right={<Pill tone="cyan">{view.snapshot.algorithmVersion}</Pill>}>快照审计</SectionTitle><div className="grid gap-3 text-[12px] sm:grid-cols-2 lg:grid-cols-4"><Audit label="修订" value={`R${view.snapshot.revision}`} /><Audit label="候选数量" value={String(candidates.length)} /><Audit label="探索位" value={view.snapshot.explorationTriggered ? "已触发，仅第三位" : "未触发"} /><Audit label="Seed 摘要" value={short(view.snapshot.seedDigest)} /></div></div> : null}
        {executions.length > 0 ? <div><SectionTitle right={<Pill tone="gray">{executions.length} 条</Pill>}>权威执行状态</SectionTitle><div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">{executions.map((execution) => <div key={execution.logicalExecutionId} className="rounded-xl border border-[var(--ap-border)] p-3 text-[11px]"><div className="flex items-center justify-between gap-2"><span className="truncate text-[var(--ap-text)]">{execution.agentId}</span><Pill tone={execution.status === "succeeded" ? "green" : execution.status === "failed" ? "red" : "amber"}>{execution.status}</Pill></div><div className="mt-2 text-[var(--ap-muted)]">{execution.stage} · 尝试 {execution.currentAttempt} · 成本 {execution.usedCost}/{execution.costCap}</div><div className="mt-1 break-all text-[var(--ap-muted)]">{short(execution.logicalExecutionId)}</div></div>)}</div></div> : null}
      </div>
    </details> : null}
    <GhostButton icon={Clock} onClick={() => navigate("/publisher/tasks")}>返回任务列表</GhostButton>
  </Page>;
}

function message(cause: unknown) { return cause instanceof Error ? cause.message : "读取匹配流程失败，请重试。"; }
function OrchestrationPlanCard({ plan }: { plan: OrchestrationPlan }) {
  return <Panel strong className="overflow-hidden">
    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--ap-border)] bg-[linear-gradient(120deg,rgba(139,92,246,.13),rgba(34,211,238,.08))] px-5 py-4">
      <div className="flex items-center gap-2"><GitBranch size={17} className="text-violet-300" /><span className="text-[15px] text-white">AI 执行方案</span><Pill tone={plan.mode === "multi" ? "violet" : "cyan"}>{plan.mode === "multi" ? `${plan.steps.length} 节点多 Agent DAG` : "单 Agent 闭环"}</Pill></div>
      <div className="text-[11px] text-[var(--ap-muted)]">置信度 {Math.round(plan.confidence * 100)}% · {plan.graphVersion}</div>
    </div>
    <div className="space-y-5 p-5">
      <div><p className="text-[13px] leading-relaxed text-[var(--ap-text-2)]">{plan.summary}</p><div className="mt-3 flex flex-wrap gap-2">{plan.rationale.map((item) => <Pill key={item} tone="gray">{item}</Pill>)}</div></div>
      <div className={`grid gap-3 ${plan.steps.length > 1 ? "lg:grid-cols-2 xl:grid-cols-3" : ""}`}>
        {plan.steps.map((step, index) => <article key={step.id} className="relative rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.42)] p-4">
          <div className="flex items-center justify-between gap-3"><span className="grid h-7 w-7 place-items-center rounded-full border border-violet-300/35 bg-violet-300/10 text-[11px] text-violet-200">{index + 1}</span><span className="text-[10px] text-[var(--ap-muted)]">{step.dependsOn.length ? `依赖 ${step.dependsOn.join("、")}` : "起始节点"}</span></div>
          <h3 className="mt-3 text-[14px] text-white">{step.title}</h3><p className="mt-1.5 text-[11px] leading-relaxed text-[var(--ap-text-2)]">{step.objective}</p>
          <div className="mt-3 flex flex-wrap gap-1.5">{step.requiredCapabilities.map((capability) => <Pill key={capability} tone="cyan">{capability}</Pill>)}</div>
          <div className="mt-3 flex items-start gap-2 border-t border-[var(--ap-border)] pt-3 text-[10px] text-[var(--ap-muted)]"><Layers3 size={12} className="mt-0.5 shrink-0" />输出：{step.output}</div>
        </article>)}
      </div>
      {plan.mode === "multi" ? <InfoNote tone="amber"><span>该方案已经识别多 Agent 能力依赖。当前匹配阶段会先建立候选池；逐节点链上分账与结算需要使用多受益人 Escrow 扩展后才能正式派单。</span></InfoNote> : null}
    </div>
  </Panel>;
}
type MatchCandidate = NonNullable<MatchingView["snapshot"]>["candidates"][number];
type NetworkPoint = { x: number; y: number };

function AgentMatchConstellation({ taskTitle, candidates, selected, selectionBlocked, reservationLocked, onSelect }: { taskTitle: string; candidates: MatchCandidate[]; selected: string | null; selectionBlocked: boolean; reservationLocked: boolean; onSelect: (agentID: string) => void }) {
  return <div className="agent-match-constellation" aria-label="任务与候选 Agent 匹配关系">
    <div className="sm:hidden"><AgentNetworkSvg variant="mobile" taskTitle={taskTitle} candidates={candidates} selected={selected} selectionBlocked={selectionBlocked} reservationLocked={reservationLocked} onSelect={onSelect} /></div>
    <div className="hidden sm:block"><AgentNetworkSvg variant="desktop" taskTitle={taskTitle} candidates={candidates} selected={selected} selectionBlocked={selectionBlocked} reservationLocked={reservationLocked} onSelect={onSelect} /></div>
  </div>;
}

function AgentNetworkSvg({ variant, taskTitle, candidates, selected, selectionBlocked, reservationLocked, onSelect }: { variant: "mobile" | "desktop"; taskTitle: string; candidates: MatchCandidate[]; selected: string | null; selectionBlocked: boolean; reservationLocked: boolean; onSelect: (agentID: string) => void }) {
  const mobile = variant === "mobile";
  const source = mobile ? { x: 180, y: 70 } : { x: 150, y: 112 };
  const points = networkPoints(variant, candidates.length);
  const gradientPrefix = `agent-network-${variant}`;
  return <svg viewBox={mobile ? "0 0 360 278" : "0 0 780 224"} role="img" className="w-full" aria-labelledby={`${gradientPrefix}-title`}>
    <title id={`${gradientPrefix}-title`}>任务与 {candidates.length} 个候选 Agent 的动态匹配关系</title>
    <defs>
      <radialGradient id={`${gradientPrefix}-task`} cx="35%" cy="28%"><stop offset="0%" stopColor="#a5f3fc" /><stop offset="36%" stopColor="#22d3ee" /><stop offset="100%" stopColor="#0e7490" /></radialGradient>
      <radialGradient id={`${gradientPrefix}-agent`} cx="35%" cy="28%"><stop offset="0%" stopColor="#ddd6fe" /><stop offset="42%" stopColor="#8b5cf6" /><stop offset="100%" stopColor="#4c1d95" /></radialGradient>
      <radialGradient id={`${gradientPrefix}-selected`} cx="35%" cy="28%"><stop offset="0%" stopColor="#ecfeff" /><stop offset="38%" stopColor="#22d3ee" /><stop offset="100%" stopColor="#0891b2" /></radialGradient>
      <filter id={`${gradientPrefix}-glow`} x="-100%" y="-100%" width="300%" height="300%"><feGaussianBlur stdDeviation="6" result="blur" /><feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge></filter>
    </defs>
    {candidates.map((candidate, index) => {
      const point = points[index];
      if (!point) return null;
      const active = selected === candidate.agentId;
      return <path key={`link-${candidate.agentId}`} d={connectionPath(variant, source, point)} className={`ap-match-link ${active ? "ap-match-link--active" : ""}`} />;
    })}
    <g className="ap-match-task-node" aria-hidden="true">
      <circle cx={source.x} cy={source.y} r={mobile ? 51 : 63} fill="rgba(34,211,238,.08)" className="ap-match-halo" />
      <circle cx={source.x} cy={source.y} r={mobile ? 43 : 54} fill={`url(#${gradientPrefix}-task)`} filter={`url(#${gradientPrefix}-glow)`} />
      <circle cx={source.x - (mobile ? 12 : 15)} cy={source.y - (mobile ? 13 : 16)} r={mobile ? 9 : 12} fill="rgba(255,255,255,.28)" />
      <text x={source.x} y={source.y - 4} textAnchor="middle" className="ap-match-task-kicker">当前任务</text>
      <text x={source.x} y={source.y + 13} textAnchor="middle" className="ap-match-task-title">{compactLabel(taskTitle, mobile ? 8 : 10)}</text>
    </g>
    {candidates.map((candidate, index) => {
      const point = points[index];
      if (!point) return null;
      const active = selected === candidate.agentId;
      const selectable = candidateSelectable(candidate, selectionBlocked) && !reservationLocked;
      const label = candidate.name || candidate.agentId;
      return <g key={candidate.agentId} role="button" aria-label={`选择 ${label}，匹配分 ${candidate.score.ranking}`} aria-pressed={active} aria-disabled={!selectable} tabIndex={selectable ? 0 : -1} onClick={() => selectable && onSelect(candidate.agentId)} onKeyDown={(event) => { if (selectable && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); onSelect(candidate.agentId); } }} className={`ap-match-agent-node ${active ? "ap-match-agent-node--active" : ""} ${selectable ? "" : "ap-match-agent-node--disabled"}`} style={{ animationDelay: `${index * -0.65}s` }}>
        {active ? <circle cx={point.x} cy={point.y} r={mobile ? 34 : 37} className="ap-match-selection-ring" /> : null}
        <circle cx={point.x} cy={point.y} r={mobile ? 27 : 29} fill={`url(#${gradientPrefix}-${active ? "selected" : "agent"})`} filter={active ? `url(#${gradientPrefix}-glow)` : undefined} />
        <circle cx={point.x - 7} cy={point.y - 8} r="5" fill="rgba(255,255,255,.25)" />
        <text x={point.x} y={point.y + 4} textAnchor="middle" className="ap-match-agent-score">{candidate.score.ranking}</text>
        <text x={mobile ? point.x : point.x + 43} y={mobile ? point.y + 46 : point.y + 1} textAnchor={mobile ? "middle" : "start"} className={`ap-match-agent-label ${active ? "ap-match-agent-label--active" : ""}`}>{compactLabel(label, mobile ? 8 : 12)}</text>
        {!mobile ? <text x={point.x + 43} y={point.y + 17} className="ap-match-agent-meta">第 {candidate.position} 位 · {duration(candidate.estimatedDurationSeconds)}</text> : null}
      </g>;
    })}
  </svg>;
}

function networkPoints(variant: "mobile" | "desktop", count: number): NetworkPoint[] {
  if (variant === "mobile") {
    if (count === 1) return [{ x: 180, y: 218 }];
    if (count === 2) return [{ x: 108, y: 218 }, { x: 252, y: 218 }];
    return [{ x: 65, y: 218 }, { x: 180, y: 218 }, { x: 295, y: 218 }];
  }
  if (count === 1) return [{ x: 566, y: 112 }];
  if (count === 2) return [{ x: 566, y: 72 }, { x: 566, y: 152 }];
  return [{ x: 566, y: 42 }, { x: 566, y: 112 }, { x: 566, y: 182 }];
}

function connectionPath(variant: "mobile" | "desktop", source: NetworkPoint, target: NetworkPoint) {
  return variant === "mobile" ? `M ${source.x} ${source.y + 44} C ${source.x} 152, ${target.x} 158, ${target.x} ${target.y - 28}` : `M ${source.x + 55} ${source.y} C 330 ${source.y}, 415 ${target.y}, ${target.x - 30} ${target.y}`;
}

function candidateSelectable(candidate: MatchCandidate, selectionBlocked: boolean) { return !selectionBlocked && candidate.overview?.status === "valid" && candidate.overview.billingStatus === "captured"; }
function compactLabel(value: string, max: number) { return value.length > max ? `${value.slice(0, max)}…` : value; }
function CompactMetric({ label, value, accent = false }: { label: string; value: string; accent?: boolean }) { return <div className="min-w-0"><div className="text-[9px] text-[var(--ap-muted)]">{label}</div><div className={`mt-1 truncate text-[12px] ${accent ? "text-[var(--ap-cyan)]" : "text-[var(--ap-text)]"}`} title={value}>{value}</div></div>; }
function short(value: string) { return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value; }
function duration(seconds: number) { const hours=Math.ceil(seconds/3600); return hours<24?`${hours} 小时`:`${Math.ceil(hours/24)} 天`; }
type CandidateOverview = NonNullable<NonNullable<MatchingView["snapshot"]>["candidates"][number]["overview"]>;
function overviewLabel(value: CandidateOverview | undefined) { if (!value) return "概览待创建"; if(value.status==="valid"&&value.billingStatus==="captured")return "概览有效"; return `${value.status} / ${value.billingStatus}`; }
function batchNeedsRematch(batch: MatchingView["batch"], candidates: NonNullable<MatchingView["snapshot"]>["candidates"] = []) { if (!batch) return false; if (batch.status === "completed") return candidates.length > 0 && candidates.every((candidate) => candidate.overview?.status === "invalid"); const deadline = Date.parse(batch.deadline); return Number.isFinite(deadline) && deadline <= Date.now(); }
function workflowAction(view: MatchingView, plan?: OrchestrationPlan | null) { if (!plan) return "AI 分析执行编排"; if (plan.mode === "multi") return "等待多 Agent 执行器"; if (!view.snapshot) return "按方案开始匹配"; if (view.batch?.status === "completed" && batchNeedsRematch(view.batch, view.snapshot.candidates)) return "重新匹配"; if (!view.batch && view.snapshot.candidates.length === 0) return "重新匹配"; if (!view.batch) return "生成候选概览"; return view.batch.status === "completed" ? "同步执行状态" : "校验概览结果"; }
function Audit({label,value}:{label:string;value:string}) { return <div className="rounded-xl border border-[var(--ap-border)] p-3"><dt className="text-[10px] text-[var(--ap-muted)]">{label}</dt><dd className="mt-1 text-[13px] text-[var(--ap-text)]">{value}</dd></div>; }
