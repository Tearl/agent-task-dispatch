import { Lock, RefreshCw, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { PlatformAPIError, prepareTaskFunding, readTaskFunding, recordTaskFundingSubmission, submitTaskFundingTransaction, type TaskFundingIntent, type WalletProvider } from "../../lib/platform-api";

export default function TaskFunding() {
  const { taskId = "" } = useParams();
  const navigate = useNavigate();
  const [intent,setIntent]=useState<TaskFundingIntent|null>(null);const[busy,setBusy]=useState(false);const[error,setError]=useState<string|null>(null);
  const load=async()=>{if(!taskId)return;setError(null);try{setIntent(await readTaskFunding(taskId));}catch(cause){if(cause instanceof PlatformAPIError&&cause.status===404)return;setError(cause instanceof Error?cause.message:"读取托管状态失败");}};
  useEffect(()=>{void load();},[taskId]);
  const act=async()=>{if(!taskId||busy)return;const provider=(window as typeof window&{ethereum?:WalletProvider}).ethereum;if(!provider){setError("未检测到以太坊兼容钱包。");return;}setBusy(true);setError(null);try{let current=intent??await prepareTaskFunding(taskId,crypto.randomUUID());if(current.status==="prepared"||current.status==="orphaned"){const hash=await submitTaskFundingTransaction(provider,current);current=await recordTaskFundingSubmission(taskId,current,hash);}else{current=await readTaskFunding(taskId);}setIntent(current);}catch(cause){setError(cause instanceof Error?cause.message:"托管失败");}finally{setBusy(false);}};
  return <Page><PageHeader title="任务资金托管" subtitle="只有 canonical TaskCreated 事件可以把任务推进到已托管" actions={<GhostButton icon={RefreshCw} onClick={()=>void load()}>刷新</GhostButton>}/>
    <Panel strong className="p-6"><SectionTitle right={<Pill tone={intent?.status==="confirmed"?"green":intent?.status==="submitted"?"amber":"cyan"}>{intent?.status??"尚未创建"}</Pill>}>权威托管意图</SectionTitle>
      {intent?<dl className="grid gap-3 text-[13px] sm:grid-cols-2"><Datum label="链" value={intent.chainId}/><Datum label="总托管" value={intent.totalAmount}/><Datum label="概览池" value={intent.overviewAmount}/><Datum label="正式池" value={intent.formalAmount}/><Datum label="外部成本池" value={intent.externalCostAmount}/><Datum label="合约" value={intent.contractAddress}/>{intent.transactionHash?<Datum label="交易" value={intent.transactionHash}/>:null}</dl>:<InfoNote tone="cyan"><span>创建意图不会移动资金，钱包确认后才会调用托管合约。</span></InfoNote>}
      {error?<div role="alert" className="mt-4 rounded-xl border border-rose-300/30 bg-rose-300/10 p-3 text-rose-100">{error}</div>:null}
      {intent?.status==="confirmed"?<CtaButton className="mt-5" icon={ShieldCheck} onClick={()=>navigate(`/publisher/recommendations?taskId=${encodeURIComponent(taskId)}`)}>开始 Agent 匹配</CtaButton>:<CtaButton className="mt-5" icon={Lock} busy={busy} disabled={busy} onClick={()=>void act()}>{intent?.status==="submitted"?"同步链上确认":"创建并提交托管交易"}</CtaButton>}
    </Panel></Page>;
}
function Datum({label,value}:{label:string;value:string}){return <div className="min-w-0"><dt className="text-[var(--ap-muted)]">{label}</dt><dd className="mt-1 break-all text-[var(--ap-text)]">{value}</dd></div>}
