import { AlertTriangle, CheckCircle2, Clock3, FileDiff, RefreshCw, Send, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useParams, useSearchParams } from "react-router";

import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle, type Tone } from "../../components/kit/primitives";
import { PlatformAPIError, acceptFormalChangeOrder, activateFormalChangeOrder, createFormalAcceptance, proposeFormalChangeOrder, readFormalDelivery, reconcileFormalAcceptance, recordFormalAcceptanceSubmission, sha256Digest, startFormalVersion, submitFormalAcceptanceTransaction, submitFormalFeedback, submitWorkNonceTransaction, type FormalAcceptance, type FormalChangeOrder, type FormalDeliveryView, type FormalVersion, type WalletProvider } from "../../lib/platform-api";

const stateLabels: Record<string, string> = { intent_recorded: "验收意图已记录", pending_confirmation: "等待链上确认", confirmed: "验收已确认", orphaned: "链重组，需重新提交" };
const eligibilityLabels: Record<string, string> = { package_advanced: "套餐已发生变化", newer_version: "已有更新版本", version_not_reviewable: "版本尚未进入评审", proof_mismatch: "证明与当前版本不一致", chain_projection_pending: "等待权威链投影", work_nonce_advanced: "工作 nonce 已前进", change_order_not_funded: "变更单尚未完成资金授权" };

export default function Settlement() {
  const route = useParams<{ taskId?: string }>();
  const [search] = useSearchParams();
  const taskID = route.taskId ?? search.get("taskId") ?? "";
  const [view, setView] = useState<FormalDeliveryView | null>(null);
  const [selectedVersion, setSelectedVersion] = useState(0);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [localAcceptanceTx, setLocalAcceptanceTx] = useState(() => readPendingTransaction("acceptance", taskID));
  const [revisionTx, setRevisionTx] = useState(() => readPendingTransaction("revision", taskID));
  const feedbackOperation = useRef<string>(undefined);
  const changeOperation = useRef<string>(undefined);

  const load = async () => {
    if (!taskID) return;
    setError(null);
    try {
      const value = await readFormalDelivery(taskID);
      setView(value);
      setSelectedVersion((current) => current && value.versions.some((version) => version.number === current) ? current : value.package.allocatedVersion);
    } catch (cause) { setError(message(cause)); }
  };
  useEffect(() => {
    setLocalAcceptanceTx(readPendingTransaction("acceptance", taskID));
    setRevisionTx(readPendingTransaction("revision", taskID));
    void load();
  }, [taskID]);

  const version = useMemo(() => view?.versions.find((item) => item.number === selectedVersion) ?? view?.versions.at(-1), [view, selectedVersion]);
  const acceptance = useMemo(() => view?.acceptances.filter((item) => item.formalVersion === version?.number && item.proofDigest === version?.proof?.digest).at(-1), [view, version]);

  if (!taskID) return <Page><PageHeader title="正式交付" subtitle="从任务列表进入权威交付记录" /><InfoNote tone="amber"><span role="alert">缺少 taskId，无法读取正式交付聚合。</span></InfoNote></Page>;
  if (!view && !error) return <Page><div role="status" className="py-24 text-center text-[var(--ap-muted)]">正在读取正式交付时间线…</div></Page>;
  if (!view) return <Page><div role="alert" className="rounded-xl border border-rose-300/30 bg-rose-300/10 p-4 text-rose-100">{error}<div className="mt-3"><GhostButton icon={RefreshCw} onClick={() => void load()}>重试</GhostButton></div></div></Page>;

  const runAcceptance = async () => {
    if (!version?.proof || busy) return;
    setBusy("acceptance"); setError(null); setNotice(null);
    try {
      let current = acceptance;
      if (!current) current = await createFormalAcceptance(taskID, `acceptance:create:${version.proof.digest}`, version, view.package.aggregateVersion);
      if (current.state === "confirmed") { setNotice("该版本已经由 canonical 链上事件确认验收。"); return; }
      if (current.state === "intent_recorded" || current.state === "orphaned") {
        const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
        if (!ethereum) throw new Error("未检测到以太坊兼容钱包。");
        const txHash = localAcceptanceTx || await submitFormalAcceptanceTransaction(ethereum, current);
        setLocalAcceptanceTx(txHash);
        rememberPendingTransaction("acceptance", taskID, txHash);
        current = await recordFormalAcceptanceSubmission(taskID, current, txHash, `${current.id}:submit:${txHash}`);
        forgetPendingTransaction("acceptance", taskID);
      }
      try {
        current = await reconcileFormalAcceptance(taskID, current, `${current.id}:reconcile:${current.transactionHash ?? "pending"}`);
        setLocalAcceptanceTx("");
        setNotice("验收已由 canonical FundsReleased 事件确认，结算资格已生效。");
      } catch (cause) {
        if (!(cause instanceof PlatformAPIError) || cause.status !== 425) throw cause;
        setNotice("交易已提交，权威链投影尚未达到确认深度。稍后可再次检查，不会重复发起交易。");
      }
      await load();
    } catch (cause) { setError(message(cause)); }
    finally { setBusy(""); }
  };

  return <Page>
    <PageHeader title="正式交付、修订与验收" subtitle={`${taskID} · 套餐版本 ${view.package.allocatedVersion}/${view.package.maximumVersions} · 聚合 R${view.package.aggregateVersion}`} actions={<GhostButton icon={RefreshCw} disabled={Boolean(busy)} onClick={() => void load()}>刷新权威状态</GhostButton>} />
    {error ? <div role="alert" className="rounded-xl border border-rose-300/30 bg-rose-300/10 p-3 text-[13px] text-rose-100">{error}</div> : null}
    {notice ? <div role="status" className="rounded-xl border border-cyan-300/30 bg-cyan-300/10 p-3 text-[13px] text-cyan-100">{notice}</div> : null}
    <div className="grid gap-6 xl:grid-cols-[1.4fr_.8fr]">
      <div className="space-y-6">
        <VersionTimeline versions={view.versions} selected={version?.number ?? 0} onSelect={setSelectedVersion} />
        {version ? <VersionDetail version={version} feedback={view.feedback.find((item) => item.id === version.revision?.feedbackSetId)} /> : null}
        <FeedbackComposer view={view} busy={busy === "feedback"} onSubmit={async (input) => {
          if (busy) return; setBusy("feedback"); setError(null); setNotice(null);
          try { feedbackOperation.current ??= crypto.randomUUID(); await submitFormalFeedback(taskID, feedbackOperation.current, input); feedbackOperation.current = undefined; setNotice("结构化反馈已追加，旧验收证明已失去结算资格。"); await load(); }
          catch (cause) { setError(message(cause)); } finally { setBusy(""); }
        }} />
        <RevisionStarter view={view} transactionHash={revisionTx} busy={busy === "revision"} onAction={async () => {
          if (busy) return;
          const latest=view.versions.at(-1), feedback=view.feedback.at(-1); if(!latest?.contentHash||!feedback||feedback.parentVersion!==latest.number)return;
          setBusy("revision"); setError(null); setNotice(null);
          try {
            if (view.chain.workNonce <= latest.workNonce) {
              const ethereum=(window as typeof window&{ethereum?:WalletProvider}).ethereum; if(!ethereum)throw new Error("未检测到以太坊兼容钱包。");
              const tx=revisionTx||await submitWorkNonceTransaction(ethereum,view.chain); setRevisionTx(tx); rememberPendingTransaction("revision",taskID,tx); setNotice(`work nonce 交易 ${short(tx)} 已提交，等待 canonical 投影后再启动版本。`); await load();
            } else {
              const nextNonce=latest.workNonce+1; if(view.chain.workNonce!==nextNonce)throw new Error("canonical work nonce 与下一版本不连续，当前证明已过时。");
              const needsOrder=latest.number>=3||feedback.items.some((item)=>item.scopeClaim!=="in_scope");
              const order=view.changeOrders.find((item)=>item.targetVersion===latest.number+1&&item.status==="effective");
              if(needsOrder&&!order)throw new Error("范围外修订必须先完成责任判定、接受和资金授权。");
              await startFormalVersion(taskID,`${feedback.id}:start:${nextNonce}`,{expectedPackageVersion:view.package.aggregateVersion,workNonce:nextNonce,revision:{parentVersion:latest.number,parentContentHash:latest.contentHash,feedbackSetId:feedback.id,feedbackDigest:feedback.digest,feedbackAggregateVersion:feedback.packageAggregateVersion},changeOrderId:order?.id});
              setRevisionTx(""); forgetPendingTransaction("revision",taskID); setNotice(`V${latest.number+1} 已分配，执行命令使用 canonical work nonce ${nextNonce}。`); await load();
            }
          } catch(cause){setError(message(cause));} finally{setBusy("");}
        }} />
        <ChangeOrderComposer view={view} busy={busy === "change"} onSubmit={async (input) => {
          if (busy) return; setBusy("change"); setError(null); setNotice(null);
          try { changeOperation.current ??= crypto.randomUUID(); await proposeFormalChangeOrder(taskID, changeOperation.current, input); changeOperation.current = undefined; setNotice("变更单已创建，等待责任判定。"); await load(); }
          catch (cause) { setError(message(cause)); } finally { setBusy(""); }
        }} />
      </div>
      <div className="space-y-6">
        <AcceptancePanel version={version} acceptance={acceptance} busy={busy === "acceptance"} onAction={() => void runAcceptance()} />
        <ChangeOrders orders={view.changeOrders} busy={busy} onTransition={async (order, action) => {
          if (busy) return; setBusy(`change:${order.id}`); setError(null); setNotice(null);
          try {
            const key = `${order.id}:${action}:${order.aggregateVersion}`;
            if (action === "accept") await acceptFormalChangeOrder(taskID, order.id, order.aggregateVersion, key);
            else await activateFormalChangeOrder(taskID, order.id, order.aggregateVersion, key);
            setNotice(action === "accept" ? "责任结果已接受。" : "变更单已生效，可以在新 work nonce 确认后启动下一版本。"); await load();
          } catch (cause) { setError(message(cause)); } finally { setBusy(""); }
        }} />
        <ScopeSummary view={view} />
      </div>
    </div>
  </Page>;
}

function RevisionStarter({view,transactionHash,busy,onAction}:{view:FormalDeliveryView;transactionHash:string;busy:boolean;onAction():Promise<void>}) {
  const latest=view.versions.at(-1),feedback=view.feedback.at(-1); const ready=Boolean(latest?.status==="review"&&latest.contentHash&&feedback?.parentVersion===latest.number&&latest.number<view.package.maximumVersions); const projected=Boolean(latest&&view.chain.workNonce===latest.workNonce+1); const needsOrder=Boolean(latest&&(latest.number>=3||feedback?.items.some((item)=>item.scopeClaim!=="in_scope"))); const orderReady=Boolean(!needsOrder||view.changeOrders.some((item)=>item.targetVersion===(latest?.number??0)+1&&item.status==="effective"));
  return <Panel className="p-5"><SectionTitle right={<Pill tone={projected?"green":transactionHash?"amber":"cyan"}>{projected?"nonce 已确认":transactionHash?"链上待确认":"等待授权"}</Pill>}>启动下一正式版本</SectionTitle><p className="text-[12px] text-[var(--ap-muted)]">先由发布者钱包调用 <code>advanceWorkNonce</code>，只有 canonical 事件到达后 Engine 才会消费反馈并分配下一版本。</p>{needsOrder&&!orderReady?<Blocked>变更单未生效，V4/V5 不能启动。</Blocked>:null}<CtaButton className="mt-4" icon={projected?Send:ShieldCheck} disabled={!ready||!orderReady||busy} busy={busy} onClick={()=>void onAction()}>{busy?"处理中…":projected?`启动 V${(latest?.number??0)+1}`:transactionHash?"检查 nonce 确认":"链上授权下一版本"}</CtaButton></Panel>;
}

function VersionTimeline({ versions, selected, onSelect }: { versions: FormalVersion[]; selected: number; onSelect(value: number): void }) {
  return <Panel className="p-5"><SectionTitle right={<Pill tone="cyan">只追加时间线</Pill>}>正式版本</SectionTitle><ol aria-label="正式交付版本时间线" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{versions.map((version) => <li key={version.number}><button type="button" aria-pressed={selected === version.number} onClick={() => onSelect(version.number)} className={`w-full rounded-xl border p-4 text-left transition-colors ${selected === version.number ? "border-cyan-300/50 bg-cyan-300/10" : "border-[var(--ap-border)] bg-white/[.02]"}`}><div className="flex items-center justify-between gap-2"><b className="text-[15px] text-white">V{version.number}</b><Pill tone={versionTone(version.status)}>{versionLabel(version.status)}</Pill></div><div className="mt-3 text-[11px] text-[var(--ap-muted)]">work nonce {version.workNonce} · R{version.aggregateVersion}</div><div className="mt-1 truncate text-[11px] text-[var(--ap-text-2)]">{version.failureReasonCode || version.contentHash || version.logicalExecutionId}</div></button></li>)}</ol></Panel>;
}

function VersionDetail({ version, feedback }: { version: FormalVersion; feedback?: FormalDeliveryView["feedback"][number] }) {
  return <Panel className="p-5"><SectionTitle right={version.changeOrderId ? <Pill tone="violet">变更单版本</Pill> : <Pill tone="gray">套餐内版本</Pill>}>V{version.number} 交付详情</SectionTitle>
    {version.failureReasonCode ? <div role="alert" className="mb-4 rounded-xl border border-rose-300/30 bg-rose-300/10 p-3 text-[12px] text-rose-100">失败原因：{version.failureReasonCode}</div> : null}
    <dl className="grid gap-3 text-[12px] sm:grid-cols-2"><Datum label="交付引用" value={version.deliverableRef || "尚未生成"} /><Datum label="内容哈希" value={version.contentHash || "—"} /><Datum label="范围哈希" value={version.scopeHash} /><Datum label="已用成本" value={version.usedCost} /></dl>
    {feedback ? <div className="mt-5"><h4 className="text-[13px] text-white">本轮反馈响应</h4><div className="mt-2 space-y-2">{feedback.items.map((item) => { const response=version.feedbackResponses?.find((value)=>value.feedbackItemId===item.id); return <div key={item.id} className="rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"><div className="flex flex-wrap justify-between gap-2"><span>{item.target} · {item.description}</span><Pill tone={response?.disposition === "resolved" ? "green" : "amber"}>{response?.disposition || "待响应"}</Pill></div>{response ? <p className="mt-2 text-[var(--ap-muted)]">{response.summary}</p> : null}</div>; })}</div></div> : null}
    <div className="mt-5"><h4 className="text-[13px] text-white">结构化差异</h4>{version.changes?.length ? <ul className="mt-2 space-y-2">{version.changes.map((change,index)=><li key={`${change.path}:${index}`} className="flex gap-3 rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"><FileDiff size={15} className="mt-0.5 shrink-0 text-cyan-300" /><span><b>{change.kind}</b> · {change.path}<span className="mt-1 block break-all text-[10px] text-[var(--ap-muted)]">{change.beforeHash || "∅"} → {change.afterHash || "∅"}</span></span></li>)}</ul> : <p className="mt-2 text-[12px] text-[var(--ap-muted)]">该版本尚无差异记录。</p>}</div>
    {version.proof ? <details className="mt-5 rounded-xl border border-[var(--ap-border)] p-3 text-[11px]"><summary className="cursor-pointer text-[13px] text-cyan-200">查看签名交付证明</summary><dl className="mt-3 grid gap-2 sm:grid-cols-2"><Datum label="证明摘要" value={version.proof.digest} /><Datum label="Payload" value={version.proof.payloadHash} /><Datum label="父内容" value={version.proof.proof.parentContentHash || "V1 无父版本"} /><Datum label="反馈摘要" value={version.proof.proof.feedbackDigest || "V1 无反馈"} /></dl></details> : null}
  </Panel>;
}

function FeedbackComposer({ view, busy, onSubmit }: { view: FormalDeliveryView; busy: boolean; onSubmit(input: Parameters<typeof submitFormalFeedback>[2]): Promise<void> }) {
  const latest = view.versions.at(-1);
  const [criterionId,setCriterionId]=useState("AC-1"), [target,setTarget]=useState("交付结果"), [description,setDescription]=useState(""), [expected,setExpected]=useState(""), [scopeClaim,setScopeClaim]=useState("in_scope");
  const allowed = latest?.status === "review" && Boolean(latest.contentHash) && view.package.allocatedVersion < view.package.maximumVersions;
  return <Panel className="p-5"><SectionTitle right={<Pill tone={allowed ? "cyan" : "gray"}>{allowed ? "可提交" : "当前不可修订"}</Pill>}>结构化反馈</SectionTitle><form onSubmit={(event)=>{event.preventDefault();if(!allowed||!latest?.contentHash)return;void onSubmit({packageId:view.package.id,expectedPackageVersion:view.package.aggregateVersion,parentVersion:latest.number,parentContentHash:latest.contentHash,items:[{criterionId,category:"defect",priority:"high",target,description,expectedOutcome:expected,scopeClaim}]});}} className="grid gap-3 sm:grid-cols-2">
    <Field id="feedback-criterion" label="验收标准"><input id="feedback-criterion" required disabled={busy} value={criterionId} onChange={(e)=>setCriterionId(e.target.value)} className="form-input" /></Field><Field id="feedback-target" label="修改目标"><input id="feedback-target" required disabled={busy} value={target} onChange={(e)=>setTarget(e.target.value)} className="form-input" /></Field>
    <Field id="feedback-description" label="问题描述" wide><textarea id="feedback-description" required disabled={busy} value={description} onChange={(e)=>setDescription(e.target.value)} className="form-input min-h-24" /></Field><Field id="feedback-expected" label="期望结果" wide><textarea id="feedback-expected" required disabled={busy} value={expected} onChange={(e)=>setExpected(e.target.value)} className="form-input min-h-20" /></Field>
    <Field id="feedback-scope" label="范围判断"><select id="feedback-scope" disabled={busy} value={scopeClaim} onChange={(e)=>setScopeClaim(e.target.value)} className="form-input"><option value="in_scope">原范围内</option><option value="out_of_scope">新增范围</option><option value="uncertain">待责任判定</option></select></Field><div className="flex items-end"><CtaButton type="submit" icon={Send} disabled={!allowed||busy||!description.trim()||!expected.trim()} busy={busy}>追加反馈</CtaButton></div>
  </form><p className="mt-3 text-[11px] text-[var(--ap-muted)]">提交后套餐聚合版本前进，当前验收证明立即失效；重复提交复用同一操作 ID。</p></Panel>;
}

function ChangeOrderComposer({ view, busy, onSubmit }: { view: FormalDeliveryView; busy: boolean; onSubmit(input: Parameters<typeof proposeFormalChangeOrder>[2]): Promise<void> }) {
  const latest=view.versions.at(-1), feedback=view.feedback.at(-1); const [path,setPath]=useState("scope.additional-output"),[description,setDescription]=useState(""),[price,setPrice]=useState(""),[deadline,setDeadline]=useState("");
  const allowed=Boolean(latest?.status==="review"&&latest.number>=3&&latest.number<5&&latest.contentHash&&feedback?.parentVersion===latest.number&&!view.changeOrders.some((order)=>order.targetVersion===latest.number+1));
  return <Panel className="p-5"><SectionTitle right={<Pill tone={allowed?"violet":"gray"}>{allowed?`目标 V${(latest?.number??0)+1}`:"无需或不可创建"}</Pill>}>范围外变更单</SectionTitle><form onSubmit={async(event)=>{event.preventDefault();if(!allowed||!latest?.contentHash||!feedback)return;const afterHash=await sha256Digest({path,description});const newSpecHash=await sha256Digest({base:latest.scopeHash,path,description});await onSubmit({packageId:view.package.id,expectedPackageVersion:view.package.aggregateVersion,triggerVersion:latest.number,triggerContentHash:latest.contentHash,feedbackSetId:feedback.id,feedbackDigest:feedback.digest,newSpecHash,differences:[{path,kind:"added",afterHash,description,workloadDeltaPercent:100}],requestedPrice:price,deadline:new Date(`${deadline}T23:59:59Z`).toISOString()});}} className="grid gap-3 sm:grid-cols-2"><Field id="change-path" label="范围路径"><input id="change-path" required disabled={busy} value={path} onChange={(e)=>setPath(e.target.value)} className="form-input" /></Field><Field id="change-price" label="追加价格"><input id="change-price" required inputMode="numeric" pattern="(?:0|[1-9][0-9]*)" disabled={busy} value={price} onChange={(e)=>setPrice(e.target.value.replace(/\D/g,""))} className="form-input" /></Field><Field id="change-description" label="范围差异" wide><textarea id="change-description" required disabled={busy} value={description} onChange={(e)=>setDescription(e.target.value)} className="form-input min-h-20" /></Field><Field id="change-deadline" label="追加截止日"><input id="change-deadline" required type="date" disabled={busy} value={deadline} onChange={(e)=>setDeadline(e.target.value)} className="form-input" /></Field><div className="flex items-end"><CtaButton type="submit" icon={FileDiff} disabled={!allowed||busy||!description.trim()||!price||!deadline} busy={busy}>提交责任判定</CtaButton></div></form><p className="mt-3 text-[11px] text-[var(--ap-muted)]">V4/V5 必须先完成责任判定、发布者接受及对应资金授权；V6 永久不可创建。</p></Panel>;
}

function AcceptancePanel({ version, acceptance, busy, onAction }: { version?: FormalVersion; acceptance?: FormalAcceptance; busy: boolean; onAction(): void }) {
  const eligible=Boolean(version?.status==="review"&&version.proof&&(acceptance?.settlementEligibility.eligible??true)); const state=acceptance?.state; const confirmed=state==="confirmed"; const reason=acceptance?.settlementEligibility.reasonCode;
  return <Panel strong className="p-5"><SectionTitle right={<Pill dot tone={confirmed?"green":state==="pending_confirmation"?"amber":state==="orphaned"?"red":"cyan"}>{stateLabels[state??"intent_recorded"]}</Pill>}>验收与结算资格</SectionTitle><div aria-live="polite" className="space-y-3 text-[12px]"><Datum label="目标版本" value={version?`V${version.number} · nonce ${version.workNonce}`:"—"} /><Datum label="证明摘要" value={version?.proof?.digest||"尚无证明"} /><Datum label="链上交易" value={acceptance?.transactionHash||"尚未提交"} />{reason?<InfoNote tone="amber"><span role="status">不可结算：{eligibilityLabels[reason]||reason}</span></InfoNote>:null}</div><CtaButton full className="mt-5" icon={confirmed?CheckCircle2:state==="pending_confirmation"?Clock3:ShieldCheck} disabled={!eligible||confirmed||busy} busy={busy} onClick={onAction}>{busy?"处理中…":confirmed?"已确认验收":state==="pending_confirmation"?"检查链上确认":state==="orphaned"?"重新提交验收":"创建意图并链上验收"}</CtaButton><p className="mt-3 text-center text-[11px] text-[var(--ap-muted)]">创建意图不会结算；只有 canonical FundsReleased 才会显示“已确认”。</p></Panel>;
}

function ChangeOrders({ orders, busy, onTransition }: { orders: FormalChangeOrder[]; busy: string; onTransition(order: FormalChangeOrder, action: "accept"|"activate"): Promise<void> }) {
  return <Panel className="p-5"><SectionTitle right={<Pill tone="violet">{orders.length} 项</Pill>}>变更单与资金边界</SectionTitle>{orders.length?<div className="space-y-3">{orders.map((order)=>{const action=order.status==="awaiting_acceptance"?"accept":order.status==="ready_to_activate"?"activate":null;return <article key={order.id} className="rounded-xl border border-[var(--ap-border)] p-4"><div className="flex flex-wrap items-start justify-between gap-2"><div><b className="text-[14px] text-white">V{order.targetVersion} · {order.requestedPrice}</b><div className="mt-1 text-[11px] text-[var(--ap-muted)]">{short(order.id)}</div></div><Pill tone={orderTone(order.status)}>{order.status}</Pill></div><dl className="mt-3 grid gap-2 text-[11px] sm:grid-cols-2"><Datum label="责任" value={order.responsibility||"等待判定"} /><Datum label="资金来源" value={order.fundingSource||"尚未确定"} /><Datum label="授权价格" value={order.authorizedPrice} /><Datum label="余额接收者" value={order.residualRecipientId||"尚未确定"} /></dl>{order.responsibilityReasonCode?<div className="mt-3 text-[11px] text-amber-200">判定原因：{order.responsibilityReasonCode}</div>:null}{order.status==="responsibility_pending"?<Blocked>责任待决，不能执行。</Blocked>:order.status==="awaiting_funding"?<Blocked>资金尚未达到授权价格，不能生效或执行。</Blocked>:null}{action?<GhostButton className="mt-3" disabled={Boolean(busy)} onClick={()=>void onTransition(order,action)}>{action==="accept"?"接受责任结果":"激活已注资变更单"}</GhostButton>:null}</article>;})}</div>:<p className="text-[12px] text-[var(--ap-muted)]">当前没有范围外变更单。</p>}</Panel>;
}

function ScopeSummary({ view }: { view: FormalDeliveryView }) { return <Panel className="p-5"><SectionTitle>冻结范围</SectionTitle><dl className="space-y-3 text-[11px]"><Datum label="规格哈希" value={view.scope.taskSpecHash} /><Datum label="验收哈希" value={view.scope.acceptanceHash} /><Datum label="外部成本上限" value={view.scope.externalCostCap} /><Datum label="允许工具" value={view.scope.allowedTools.join("、")||"无"} /><Datum label="排除项" value={view.scope.exclusions.join("、")||"无"} /></dl></Panel>; }
function Field({id,label,wide=false,children}:{id:string;label:string;wide?:boolean;children:ReactNode}) { return <div className={wide?"sm:col-span-2":""}><label htmlFor={id} className="mb-1.5 block text-[12px] text-[var(--ap-muted)]">{label}</label>{children}</div>; }
function Datum({label,value}:{label:string;value:string}) { return <div className="min-w-0"><dt className="text-[10px] text-[var(--ap-muted)]">{label}</dt><dd className="mt-1 break-all text-[12px] text-[var(--ap-text-2)]">{value}</dd></div>; }
function Blocked({children}:{children:ReactNode}) { return <div role="status" className="mt-3 flex gap-2 rounded-lg bg-amber-300/10 p-2 text-[11px] text-amber-100"><AlertTriangle size={14} className="shrink-0" />{children}</div>; }
function versionLabel(value:string){return ({allocated:"已分配",generating:"生成中",review:"待评审",failed:"失败"} as Record<string,string>)[value]||value;}
function versionTone(value:string):Tone{return value==="review"?"green":value==="failed"?"red":value==="generating"?"cyan":"gray";}
function orderTone(value:string):Tone{return value==="consumed"?"green":value==="effective"?"cyan":value==="responsibility_pending"||value==="awaiting_funding"?"amber":"violet";}
function short(value:string){return value.length>22?`${value.slice(0,12)}…${value.slice(-7)}`:value;}
function message(cause:unknown){return cause instanceof Error?cause.message:"正式交付操作失败，请重试。";}
function pendingTransactionKey(kind:"acceptance"|"revision",taskID:string){return `formal-delivery:${kind}:${taskID}`;}
function readPendingTransaction(kind:"acceptance"|"revision",taskID:string){if(!taskID||typeof window==="undefined")return "";try{return window.sessionStorage.getItem(pendingTransactionKey(kind,taskID))??"";}catch{return "";}}
function rememberPendingTransaction(kind:"acceptance"|"revision",taskID:string,transactionHash:string){if(!taskID||typeof window==="undefined")return;try{window.sessionStorage.setItem(pendingTransactionKey(kind,taskID),transactionHash);}catch{/* The server projection remains authoritative when storage is unavailable. */}}
function forgetPendingTransaction(kind:"acceptance"|"revision",taskID:string){if(!taskID||typeof window==="undefined")return;try{window.sessionStorage.removeItem(pendingTransactionKey(kind,taskID));}catch{/* The value expires with the browser session. */}}
