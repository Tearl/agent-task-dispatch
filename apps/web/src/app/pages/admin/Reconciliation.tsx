import { AlertTriangle, CheckCircle2, Link2, RefreshCw } from "lucide-react";
import { useCallback } from "react";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle, StatCard } from "../../components/kit/primitives";
import { readReconciliationFinance } from "../../lib/platform-api";
import { useFinanceView } from "../../lib/use-finance-view";

export default function AdminReconciliation() {
  const loader=useCallback(()=>readReconciliationFinance(),[]);
  const {value,error,loading,reload}=useFinanceView(loader);
  const latest=value?.runs[0];
  const differences=value?.runs.reduce((total,run)=>total+run.differences.length,0)??0;
  return <Page>
    <PageHeader title="链上对账" subtitle="只读展示安全区块、账本预期、合约观测与不可变差异证据" actions={<GhostButton icon={RefreshCw} onClick={reload}>刷新</GhostButton>}/>
    {loading&&<InfoNote>正在读取对账记录…</InfoNote>}{error&&<InfoNote tone="red"><span role="alert">{error}</span></InfoNote>}
    {value&&<>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4"><StatCard label="最近安全区块" value={latest?String(latest.safeBlock):"—"} icon={Link2} accent="#38bdf8"/><StatCard label="最近运行" value={latest?.status==="matched"?"一致":"有差异"} icon={latest?.status==="matched"?CheckCircle2:AlertTriangle} accent={latest?.status==="matched"?"#34d399":"#fb7185"}/><StatCard label="保留运行" value={String(value.runs.length)} icon={RefreshCw} accent="#22d3ee"/><StatCard label="差异证据" value={String(differences)} icon={AlertTriangle} accent="#fbbf24"/></div>
      <InfoNote>对账页面不直接重放事件或修改账本；处置操作属于受审计的管理员流程。</InfoNote>
      {value.runs.map((run)=><Panel key={run.id} className="p-5"><SectionTitle right={<Pill tone={run.status==="matched"?"green":"red"} dot>{run.status==="matched"?"一致":"检测到差异"}</Pill>}>{run.chainId} · 区块 {run.safeBlock}</SectionTitle><div className="mb-3 text-[12px] text-[var(--ap-muted)]">{short(run.contract)} · {date(run.finishedAt)}</div><div className="ap-scroll overflow-x-auto"><table className="w-full min-w-[760px] text-[13px]"><thead><tr className="text-left text-[var(--ap-muted)]"><th className="pb-3 font-normal">类别</th><th className="pb-3 font-normal">资源</th><th className="pb-3 font-normal">账本预期</th><th className="pb-3 font-normal">链上观测</th><th className="pb-3 font-normal">级别</th></tr></thead><tbody>{run.differences.length===0?<tr className="border-t border-[var(--ap-border)]"><td colSpan={5} className="py-4 text-center text-[var(--ap-success)]">未发现差异</td></tr>:run.differences.map((difference,index)=><tr key={`${difference.category}:${difference.resourceId}:${index}`} className="border-t border-[var(--ap-border)]"><td className="py-3">{difference.category}</td><td className="py-3 font-mono text-[var(--ap-text-2)]">{difference.resourceId}</td><td className="py-3 text-[var(--ap-text-2)]">{difference.expected}</td><td className="py-3 text-[var(--ap-text-2)]">{difference.observed}</td><td className="py-3"><Pill tone={difference.severity==="critical"?"red":"amber"}>{difference.severity}</Pill></td></tr>)}</tbody></table></div></Panel>)}
    </>}
  </Page>;
}
function date(value:string){const parsed=new Date(value);return Number.isFinite(parsed.getTime())?parsed.toLocaleString("zh-CN"):"—"}
function short(value:string){return value.length>18?`${value.slice(0,10)}…${value.slice(-6)}`:value}
