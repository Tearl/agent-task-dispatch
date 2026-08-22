import { Coins, Info, Link2, RefreshCw, Wallet } from "lucide-react";
import { useCallback } from "react";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle, StatCard } from "../../components/kit/primitives";
import { readAgentFinance } from "../../lib/platform-api";
import { useFinanceView } from "../../lib/use-finance-view";

export default function AgentEarnings() {
  const loader = useCallback(() => readAgentFinance(), []);
  const { value, error, loading, reload } = useFinanceView(loader);
  return <Page>
    <PageHeader title="Agent 收益中心" subtitle="概览应收与正式可提现收益独立展示" actions={<GhostButton icon={RefreshCw} onClick={reload}>刷新</GhostButton>} />
    <InfoNote tone="green"><span className="inline-flex items-center gap-1.5"><Info size={14}/>正式收益只能由绑定 controller 发起提现，并只能转入绑定 payout；本页不提供修改收款地址。</span></InfoNote>
    {loading && <InfoNote>正在读取权威收益视图…</InfoNote>}
    {error && <InfoNote tone="red"><span role="alert">{error}</span></InfoNote>}
    {value && <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="概览应收" value={amount(value.totals.overviewReceivable)} icon={Coins} accent="#8b5cf6" />
        <StatCard label="正式可提现" value={amount(value.totals.formalClaimable)} icon={Wallet} accent="#34d399" />
        <StatCard label="总可用收益" value={amount(value.totals.totalAvailable)} icon={Coins} accent="#22d3ee" />
      </div>
      <Panel className="p-5"><SectionTitle right={<span className="text-[12px] text-[var(--ap-muted)]">截至 {date(value.asOf)}</span>}>收益仓位</SectionTitle><div className="ap-scroll overflow-x-auto"><table className="w-full min-w-[900px] text-[13px]"><thead><tr className="text-left text-[var(--ap-muted)]"><th className="pb-3 font-normal">Agent</th><th className="pb-3 font-normal">概览应收</th><th className="pb-3 font-normal">正式账本</th><th className="pb-3 font-normal">链上可提</th><th className="pb-3 font-normal">确认</th><th className="pb-3 font-normal">收款绑定</th></tr></thead><tbody>{value.positions.map((position)=><tr key={`${position.agentId}:${position.asset}`} className="border-t border-[var(--ap-border)]"><td className="py-3"><div>{position.agentName}</div><div className="text-[11px] text-[var(--ap-muted)]">{position.agentId}</div></td><td className="py-3">{amount(position.overviewReceivable)} {position.asset}</td><td className="py-3 text-[var(--ap-success)]">{amount(position.formalClaimable)} {position.asset}</td><td className="py-3 text-[var(--ap-cyan)]">{amount(position.chainClaimable)} {position.asset}</td><td className="py-3"><Pill tone={position.chain.confirmation === "confirmed" ? "green" : "amber"} dot>{position.chain.confirmation === "confirmed" ? "已确认" : "待确认"}</Pill></td><td className="py-3 font-mono text-[11px] text-[var(--ap-muted)]">{short(position.controller)} → {short(position.payout)}</td></tr>)}</tbody></table></div></Panel>
      <Panel className="p-5"><SectionTitle right={<GhostButton icon={Link2}>链上与账本明细</GhostButton>}>收益流水</SectionTitle><div className="ap-scroll overflow-x-auto"><table className="w-full min-w-[720px] text-[13px]"><thead><tr className="text-left text-[var(--ap-muted)]"><th className="pb-3 font-normal">流水</th><th className="pb-3 font-normal">类型</th><th className="pb-3 font-normal">金额</th><th className="pb-3 font-normal">时间</th><th className="pb-3 text-right font-normal">交易</th></tr></thead><tbody>{value.records.map((record)=><tr key={record.id} className="border-t border-[var(--ap-border)]"><td className="py-3 text-[var(--ap-text-2)]">{short(record.id)}</td><td className="py-3"><Pill tone={record.type === "earnings_withdrawal" ? "blue" : "green"}>{record.type}</Pill></td><td className="py-3">{amount(record.amount)} {record.asset}</td><td className="py-3 text-[var(--ap-muted)]">{date(record.createdAt)}</td><td className="py-3 text-right font-mono text-[var(--ap-cyan)]">{record.transactionHash ? short(record.transactionHash) : "—"}</td></tr>)}</tbody></table></div></Panel>
    </>}
  </Page>;
}

function amount(value:string){try{return BigInt(value).toLocaleString("zh-CN")}catch{return "—"}}
function date(value:string){const parsed=new Date(value);return Number.isFinite(parsed.getTime())?parsed.toLocaleString("zh-CN"):"—"}
function short(value:string){return value.length>14?`${value.slice(0,8)}…${value.slice(-6)}`:value}
