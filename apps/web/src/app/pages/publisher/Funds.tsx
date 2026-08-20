import {
  BarChart, Bar as RBar, ResponsiveContainer, XAxis, Tooltip, CartesianGrid, Cell,
} from 'recharts';
import { Wallet, TrendingUp, RotateCcw, Snowflake, Link2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, StatCard, Panel, SectionTitle, Pill } from '../../components/kit/primitives';
import { REVENUE_SERIES } from '../../lib/mock';

const LEDGER = [
  { id: 'LG-91', type: '托管', task: 'TSK-2001', amount: -2600, tone: 'cyan', time: '08-19 14:02', tx: '0x9a…21' },
  { id: 'LG-90', type: '结算分账', task: 'TSK-2020', amount: -3177, tone: 'gray', time: '08-12 09:33', tx: '0x8b…7c' },
  { id: 'LG-89', type: '收益入账', task: 'TSK-2020', amount: 22.6, tone: 'green', time: '08-12 09:33', tx: '0x8b…7c' },
  { id: 'LG-88', type: '退款', task: 'TSK-1990', amount: 500, tone: 'blue', time: '08-08 18:20', tx: '0x7c…9d' },
  { id: 'LG-87', type: '冻结', task: 'TSK-2012', amount: -1500, tone: 'red', time: '08-15 11:10', tx: '0x6d…4e' },
] as const;

export default function PublisherFunds() {
  return (
    <Page>
      <PageHeader title="资金与收益" subtitle="托管本金、实际净收益、退款、冻结与链上记录分账" />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="托管中本金" value="4,100" unit="USDC" icon={Wallet} accent="#22d3ee" />
        <StatCard label="累计实际净收益" value="46.5" unit="USDC" icon={TrendingUp} delta={12} accent="#34d399" />
        <StatCard label="累计退款" value="1,240" unit="USDC" icon={RotateCcw} accent="#38bdf8" />
        <StatCard label="争议冻结" value="1,500" unit="USDC" icon={Snowflake} accent="#fb7185" />
      </div>

      <Panel className="p-5">
        <SectionTitle right={<Pill tone="green" dot>结算前收益归发布方</Pill>}>月度资金规模</SectionTitle>
        <div className="h-[220px]">
          <ResponsiveContainer>
            <BarChart data={REVENUE_SERIES} margin={{ left: -18, right: 8 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(120,170,220,0.1)" />
              <XAxis dataKey="d" tick={{ fill: '#7286a6', fontSize: 12 }} axisLine={false} tickLine={false} />
              <Tooltip cursor={{ fill: 'rgba(34,211,238,0.06)' }} contentStyle={{ background: '#0c1730', border: '1px solid rgba(96,165,214,0.3)', borderRadius: 12, color: '#e8f0ff' }} />
              <RBar dataKey="v" radius={[6, 6, 0, 0]}>
                {REVENUE_SERIES.map((_, i) => <Cell key={i} fill="#22d3ee" fillOpacity={0.35 + i * 0.1} />)}
              </RBar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      </Panel>

      <Panel className="p-5">
        <SectionTitle right={<span className="text-[12px] text-[var(--ap-muted)]">全部记录链上可查</span>}>链上记录分账</SectionTitle>
        <div className="overflow-x-auto ap-scroll">
          <table className="w-full min-w-[760px] text-[13px]">
            <thead>
              <tr className="text-left text-[var(--ap-muted)]">
                <th className="pb-3 font-normal">流水号</th>
                <th className="pb-3 font-normal">类型</th>
                <th className="pb-3 font-normal">关联任务</th>
                <th className="pb-3 font-normal">金额</th>
                <th className="pb-3 font-normal">时间</th>
                <th className="pb-3 font-normal text-right">交易哈希</th>
              </tr>
            </thead>
            <tbody>
              {LEDGER.map((l) => (
                <tr key={l.id} className="border-t border-[var(--ap-border)]">
                  <td className="py-3 text-[var(--ap-text-2)]">{l.id}</td>
                  <td className="py-3"><Pill tone={l.tone as never}>{l.type}</Pill></td>
                  <td className="py-3 text-[var(--ap-text-2)]">{l.task}</td>
                  <td className="py-3" style={{ color: l.amount >= 0 ? 'var(--ap-success)' : 'var(--ap-text)' }}>
                    {l.amount >= 0 ? '+' : ''}{l.amount.toLocaleString()} USDC
                  </td>
                  <td className="py-3 text-[var(--ap-muted)]">{l.time}</td>
                  <td className="py-3 text-right"><span className="inline-flex items-center gap-1 text-[var(--ap-cyan)]"><Link2 size={12} />{l.tx}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </Page>
  );
}
