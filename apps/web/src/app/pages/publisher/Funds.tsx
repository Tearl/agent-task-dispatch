import { Link2, RefreshCw, RotateCcw, Snowflake, Wallet } from "lucide-react";
import { useCallback } from "react";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle, StatCard } from "../../components/kit/primitives";
import { readPublisherFinance, type ChainPresentation } from "../../lib/platform-api";
import { useFinanceView } from "../../lib/use-finance-view";

const chainLabels: Record<ChainPresentation["confirmation"], string> = { not_observed: "未观察", pending: "待确认", confirmed: "已确认", failed: "失败", orphaned: "重组隔离" };
const refundLabels = { available: "可退款", pending: "退款待确认", confirmed: "已退款", unavailable: "不可退款" } as const;

export default function PublisherFunds() {
  const loader = useCallback(() => readPublisherFinance(), []);
  const { value, error, loading, reload } = useFinanceView(loader);
  return <Page>
    <PageHeader title="资金与退款" subtitle="托管子账、交易提交、链上确认、可退金额与终态资金分离展示" actions={<GhostButton icon={RefreshCw} onClick={reload}>刷新</GhostButton>} />
    {loading && <InfoNote>正在读取权威资金视图…</InfoNote>}
    {error && <InfoNote tone="red"><span role="alert">{error}</span></InfoNote>}
    {value && <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="概览池余额" value={amount(value.totals.discovery)} icon={Wallet} accent="#22d3ee" />
        <StatCard label="正式托管余额" value={amount(value.totals.formal)} icon={Snowflake} accent="#8b5cf6" />
        <StatCard label="当前可退" value={amount(value.totals.refundable)} icon={RotateCcw} accent="#38bdf8" />
        <StatCard label="累计已退" value={amount(value.totals.refunded)} icon={RotateCcw} accent="#34d399" />
      </div>
      <Panel className="p-5">
        <SectionTitle right={<span className="text-[12px] text-[var(--ap-muted)]">截至 {date(value.asOf)}</span>}>任务资金状态</SectionTitle>
        <div className="ap-scroll overflow-x-auto"><table className="w-full min-w-[980px] text-[13px]">
          <thead><tr className="text-left text-[var(--ap-muted)]"><th className="pb-3 font-normal">任务</th><th className="pb-3 font-normal">正式托管</th><th className="pb-3 font-normal">提交</th><th className="pb-3 font-normal">链上确认</th><th className="pb-3 font-normal">退款</th><th className="pb-3 font-normal">终态</th><th className="pb-3 text-right font-normal">交易</th></tr></thead>
          <tbody>{value.tasks.map((task) => <tr key={`${task.taskId}:${task.asset}`} className="border-t border-[var(--ap-border)]">
            <td className="py-3"><div className="text-[var(--ap-text)]">{task.title}</div><div className="text-[11px] text-[var(--ap-muted)]">{task.taskId} · {task.lifecycle}</div></td>
            <td className="py-3 text-[var(--ap-text-2)]">{amount(task.formal)} {task.asset}</td>
            <td className="py-3"><Pill tone={task.chain.submission === "submitted" ? "cyan" : "gray"}>{task.chain.submission === "submitted" ? "已提交" : "未提交"}</Pill></td>
            <td className="py-3"><Pill tone={confirmationTone(task.chain.confirmation)} dot>{chainLabels[task.chain.confirmation]}</Pill></td>
            <td className="py-3"><Pill tone={refundTone(task.refundStatus)}>{refundLabels[task.refundStatus]} · {amount(task.refundable)}</Pill></td>
            <td className="py-3">{task.terminal ? <Pill tone="green">终态</Pill> : <Pill tone="amber">进行中</Pill>}</td>
            <td className="py-3 text-right font-mono text-[var(--ap-cyan)]">{task.transactionHash ? <span className="inline-flex items-center gap-1"><Link2 size={12}/>{shortHash(task.transactionHash)}</span> : "—"}</td>
          </tr>)}</tbody>
        </table></div>
      </Panel>
      <Panel className="p-5"><SectionTitle>不可变资金流水</SectionTitle><div className="ap-scroll overflow-x-auto"><table className="w-full min-w-[760px] text-[13px]"><thead><tr className="text-left text-[var(--ap-muted)]"><th className="pb-3 font-normal">流水</th><th className="pb-3 font-normal">类型</th><th className="pb-3 font-normal">任务</th><th className="pb-3 font-normal">金额</th><th className="pb-3 font-normal">时间</th><th className="pb-3 text-right font-normal">交易</th></tr></thead><tbody>{value.ledger.map((record)=><tr key={record.id} className="border-t border-[var(--ap-border)]"><td className="py-3 text-[var(--ap-text-2)]">{shortId(record.id)}</td><td className="py-3"><Pill tone="cyan">{record.type}</Pill></td><td className="py-3 text-[var(--ap-text-2)]">{record.taskId || "—"}</td><td className="py-3">{amount(record.amount)} {record.asset}</td><td className="py-3 text-[var(--ap-muted)]">{date(record.createdAt)}</td><td className="py-3 text-right font-mono text-[var(--ap-cyan)]">{record.transactionHash ? shortHash(record.transactionHash) : "—"}</td></tr>)}</tbody></table></div></Panel>
    </>}
  </Page>;
}

function amount(value: string) { try { return BigInt(value).toLocaleString("zh-CN"); } catch { return "—"; } }
function date(value: string) { const parsed=new Date(value); return Number.isFinite(parsed.getTime())?parsed.toLocaleString("zh-CN"):"—"; }
function shortHash(value:string){return value.length>14?`${value.slice(0,8)}…${value.slice(-6)}`:value}
function shortId(value:string){return value.length>18?`${value.slice(0,10)}…${value.slice(-6)}`:value}
function confirmationTone(value:ChainPresentation["confirmation"]){return ({confirmed:"green",pending:"amber",failed:"red",orphaned:"red",not_observed:"gray"} as const)[value]}
function refundTone(value:keyof typeof refundLabels){return ({available:"blue",pending:"amber",confirmed:"green",unavailable:"gray"} as const)[value]}
