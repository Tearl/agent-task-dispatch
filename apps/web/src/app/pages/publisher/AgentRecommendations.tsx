import { AlertTriangle, Check, Clock, Eye, Play, RefreshCw, ShieldCheck, Sparkles } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

import { Page } from "../../components/AppShell";
import { Bar, GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { finalizeOverviewSlot, PlatformAPIError, readMatchingView, readSelection, readTaskExecutions, reconcileSelection, reserveSelection, startMatching, startOverview, submitSelectionTransaction, type ExecutionView, type MatchingView, type SelectionIntent, type WalletProvider } from "../../lib/platform-api";
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
  const [localTx, setLocalTx] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const operationID = useRef<string | undefined>(undefined);

  const load = async () => {
    if (!taskID) return;
    setError(null);
    try {
      const [value, executionResult] = await Promise.all([readMatchingView(taskID), readTaskExecutions(taskID)]);
      setView(value);
      setExecutions(executionResult.executions);
      if (value.reservation) setSelected(value.reservation.agentId);
      if (value.reservation?.id) setIntent(await readSelection(taskID, value.reservation.id));
    } catch (cause) {
      setError(message(cause));
    }
  };

  useEffect(() => { void load(); }, [taskID]);

  const advance = async () => {
    if (!taskID || busy) return;
    setBusy(true); setError(null);
    try {
      if (!view?.snapshot) {
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
          if (!execution || !terminal.has(execution.status)) continue;
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
    if (!taskID || !view?.batch || !selected || busy) return;
    const candidate = view.snapshot?.candidates.find((item) => item.agentId === selected);
    if (!candidate?.overview || candidate.overview.status !== "valid" || candidate.overview.billingStatus !== "captured") {
      setError("只有已通过客观校验且完成概览计费的候选可以选择。");
      return;
    }
    const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
    if (!ethereum) { setError("未检测到以太坊兼容钱包。"); return; }
    setBusy(true); setError(null);
    try {
      const current = intent ?? await reserveSelection(taskID, view.batch.id, candidate.overview.slotId, operationID.current ??= crypto.randomUUID());
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
  return <Page>
    <PageHeader title="Agent 推荐与概览比较" subtitle={`${view.task.title} · ${view.snapshot ? `不可变快照 R${view.snapshot.revision}` : "等待匹配快照"}`} actions={<div className="flex gap-2"><GhostButton icon={RefreshCw} onClick={() => void load()}>刷新状态</GhostButton><button type="button" disabled={busy || Boolean(view.reservation)} onClick={() => void advance()} className="ap-cta inline-flex items-center gap-2 rounded-xl px-4 py-2 text-[13px] disabled:opacity-40"><Play size={15} />{busy ? "处理中…" : workflowAction(view)}</button></div>} />

    {!view.snapshot ? <InfoNote tone="cyan"><span role="status">任务已经发布，但权威匹配快照尚未生成。刷新只读取 Latest，不会触发重新排序或增加修订。</span></InfoNote> : null}
    {view.snapshot?.degradations.map((item) => <div key={`${item.dependency}:${item.code}`} role="status" className="flex gap-2 rounded-xl border border-amber-300/30 bg-amber-300/10 p-3 text-[13px] text-amber-100"><AlertTriangle size={16} className="shrink-0" /><span><b>{item.dependency}</b> · {item.code}：{item.message}</span></div>)}

    {view.snapshot ? <Panel className="p-5">
      <SectionTitle right={<Pill tone="cyan">{view.snapshot.algorithmVersion}</Pill>}>快照审计</SectionTitle>
      <div className="grid gap-3 text-[12px] sm:grid-cols-2 lg:grid-cols-4"><Audit label="修订" value={`R${view.snapshot.revision}`} /><Audit label="候选数量" value={String(candidates.length)} /><Audit label="探索位" value={view.snapshot.explorationTriggered ? "已触发，仅第三位" : "未触发"} /><Audit label="Seed 摘要" value={short(view.snapshot.seedDigest)} /></div>
    </Panel> : null}

    {executions.length > 0 ? <Panel className="p-5">
      <SectionTitle right={<Pill tone="gray">{executions.length} 条</Pill>}>权威执行状态</SectionTitle>
      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">{executions.map((execution) => <div key={execution.logicalExecutionId} className="rounded-xl border border-[var(--ap-border)] p-3 text-[11px]"><div className="flex items-center justify-between gap-2"><span className="truncate text-[var(--ap-text)]">{execution.agentId}</span><Pill tone={execution.status === "succeeded" ? "green" : execution.status === "failed" ? "red" : "amber"}>{execution.status}</Pill></div><div className="mt-2 text-[var(--ap-muted)]">{execution.stage} · 尝试 {execution.currentAttempt} · 成本 {execution.usedCost}/{execution.costCap}</div><div className="mt-1 break-all text-[var(--ap-muted)]">{short(execution.logicalExecutionId)}</div></div>)}</div>
    </Panel> : null}

    <section className="grid gap-4 xl:grid-cols-3" aria-label="Agent 候选比较">
      {view.snapshot && candidates.length === 0 ? <div role="status" className="xl:col-span-3 rounded-xl border border-[var(--ap-border)] p-6 text-center text-[var(--ap-muted)]">当前快照没有达到质量门槛的候选，系统不会用低分 Agent 补位。</div> : null}
      {candidates.map((candidate) => {
        const active = selected === candidate.agentId;
        const selectable = candidate.overview?.status === "valid" && candidate.overview.billingStatus === "captured";
        return <article key={candidate.agentId} className={`rounded-2xl border p-5 ${active ? "border-cyan-300/50 bg-cyan-300/5" : "border-[var(--ap-border)] bg-[rgba(10,18,38,.58)]"}`}>
          <div className="flex items-start justify-between gap-3"><div><div className="flex flex-wrap items-center gap-2 text-[16px] text-white">{candidate.name || candidate.agentId}{candidate.exploration ? <Pill tone="violet">探索位</Pill> : null}</div><div className="mt-1 text-[11px] text-[var(--ap-muted)]">第 {candidate.position} 位 · {candidate.category}</div></div><button type="button" aria-pressed={active} disabled={!selectable || Boolean(view.reservation)} onClick={() => setSelected(candidate.agentId)} className="grid h-8 w-8 place-items-center rounded-full border border-[var(--ap-border-strong)] disabled:opacity-40">{active ? <Check size={15} /> : null}</button></div>
          <div className="mt-5 flex items-end justify-between"><div><div className="text-[10px] text-[var(--ap-muted)]">权威排名分</div><div className="text-[28px] text-[var(--ap-cyan)]">{candidate.score.ranking}<span className="text-[11px] text-[var(--ap-muted)]"> / 100</span></div></div><Pill tone={selectable ? "green" : "amber"}>{overviewLabel(candidate.overview)}</Pill></div>
          <Bar value={candidate.score.ranking} />
          <dl className="mt-4 grid grid-cols-2 gap-2 text-[11px]"><Metric label="任务匹配" value={candidate.score.taskMatch} /><Metric label="信誉" value={candidate.score.reputation} /><Metric label="价格/时间" value={candidate.score.priceTime} /><Metric label="可用性" value={candidate.score.availability} /></dl>
          <div className="mt-4 flex flex-wrap gap-2">{candidate.tags.map((tag) => <Pill key={tag} tone="gray">{tag}</Pill>)}</div>
          <div className="mt-4 space-y-2 border-t border-[var(--ap-border)] pt-4 text-[12px]"><Line label="概览价" value={candidate.overviewPrice} /><Line label="正式毛价" value={candidate.formalPrice} /><Line label="预计耗时" value={duration(candidate.estimatedDurationSeconds)} /></div>
          {candidate.overview?.validationCodes.length ? <div role="status" className="mt-3 text-[11px] text-amber-200">校验：{candidate.overview.validationCodes.join("、")}</div> : null}
          {candidate.overview?.contentHash ? <div className="mt-2 break-all text-[10px] text-[var(--ap-muted)]"><Eye size={12} className="mr-1 inline" />内容哈希 {candidate.overview.contentHash}</div> : null}
        </article>;
      })}
    </section>

    <Panel strong className="p-5">
      <div className="flex flex-wrap items-center justify-between gap-4"><div><div className="text-[11px] text-[var(--ap-muted)]">选择状态</div><div role="status" className="mt-1 text-[14px] text-[var(--ap-text)]">{reservationStatus ? `${reservationStatus}${txHash ? ` · ${short(txHash)}` : ""}` : selected ? `已选择 ${candidates.find((item)=>item.agentId===selected)?.name}` : "请选择一个有效概览"}</div></div><button type="button" disabled={!selected || busy || ["confirmed","failed","expired","orphaned"].includes(reservationStatus ?? "")} onClick={() => void submit()} className="ap-cta inline-flex items-center gap-2 rounded-xl px-5 py-2.5 text-[14px] disabled:opacity-40"><ShieldCheck size={16} />{busy ? "处理中…" : txHash ? "检查链上确认" : intent ? "提交链上选择" : "预留并选择 Agent"}</button></div>
      {error ? <div role="alert" className="mt-4 rounded-xl border border-amber-300/30 bg-amber-300/10 p-3 text-[12px] text-amber-100">{error}</div> : null}
      <div className="mt-4 flex items-start gap-2 text-[11px] text-[var(--ap-muted)]"><Sparkles size={14} className="mt-0.5" />重复点击复用同一操作 ID、预留和交易哈希；只有权威 canonical 事件确认后才创建 assignment。</div>
    </Panel>
    <GhostButton icon={Clock} onClick={() => navigate("/publisher/tasks")}>返回任务列表</GhostButton>
  </Page>;
}

function message(cause: unknown) { return cause instanceof Error ? cause.message : "读取匹配流程失败，请重试。"; }
function short(value: string) { return value.length > 18 ? `${value.slice(0, 10)}…${value.slice(-6)}` : value; }
function duration(seconds: number) { const hours=Math.ceil(seconds/3600); return hours<24?`${hours} 小时`:`${Math.ceil(hours/24)} 天`; }
type CandidateOverview = NonNullable<NonNullable<MatchingView["snapshot"]>["candidates"][number]["overview"]>;
function overviewLabel(value: CandidateOverview | undefined) { if (!value) return "概览待创建"; if(value.status==="valid"&&value.billingStatus==="captured")return "概览有效"; return `${value.status} / ${value.billingStatus}`; }
function workflowAction(view: MatchingView) { if (!view.snapshot) return "开始权威匹配"; if (!view.batch) return "生成候选概览"; return view.batch.status === "completed" ? "同步执行状态" : "校验概览结果"; }
function Audit({label,value}:{label:string;value:string}) { return <div className="rounded-xl border border-[var(--ap-border)] p-3"><dt className="text-[10px] text-[var(--ap-muted)]">{label}</dt><dd className="mt-1 text-[13px] text-[var(--ap-text)]">{value}</dd></div>; }
function Metric({label,value}:{label:string;value:number}) { return <div className="rounded-lg bg-white/5 p-2"><dt className="text-[var(--ap-muted)]">{label}</dt><dd className="mt-1 text-[var(--ap-text)]">{value}</dd></div>; }
function Line({label,value}:{label:string;value:string}) { return <div className="flex justify-between"><span className="text-[var(--ap-muted)]">{label}</span><span>{value}</span></div>; }
