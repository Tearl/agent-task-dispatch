import { useNavigate } from 'react-router';
import {
  LineChart, Line, ResponsiveContainer, XAxis, Tooltip, CartesianGrid,
} from 'recharts';
import { Cpu, ClipboardList, AlertTriangle, Coins, ArrowRight, Activity } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, StatCard, Panel, SectionTitle, Pill } from '../../components/kit/primitives';
import { readAgentFinance, readWorkspaceAgents } from '../../lib/platform-api';
import { useFinanceView } from '../../lib/use-finance-view';

export default function AgentDashboard() {
  const nav = useNavigate();
  const agentsView=useFinanceView(readWorkspaceAgents);
  const financeView=useFinanceView(readAgentFinance);
  const agents=agentsView.value?.agents??[];
  const finance=financeView.value;
  const alerts=agents.filter((agent)=>agent.health!=="healthy").map((agent)=>({t:`${agent.name} 当前健康状态：${agent.health}`,tone:agent.health==="unhealthy"?'red' as const:'amber' as const,time:new Date(agent.updatedAt).toLocaleString()}));
  const revenueSeries=monthlySeries(finance?.records??[]);
  return (
    <Page>
      <PageHeader title="Agent 工作台" subtitle="Agent 状态、待处理订单、运行告警、收入与信誉概览" />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="健康 Agent" value={`${agents.filter((agent)=>agent.health==="healthy").length} / ${agents.length}`} icon={Cpu} accent="#8b5cf6" />
        <StatCard label="资金头寸" value={String(finance?.positions.length??0)} unit="项" icon={ClipboardList} accent="#22d3ee" />
        <StatCard label="运行告警" value={String(alerts.length)} unit="条" icon={AlertTriangle} accent="#fbbf24" />
        <StatCard label="当前可用收入" value={finance?.totals.totalAvailable??"0"} unit="最小单位" icon={Coins} accent="#34d399" />
      </div>

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        <Panel className="p-5">
          <SectionTitle right={<Pill tone="violet" dot>不含履约金</Pill>}>结算收入趋势</SectionTitle>
          <div className="h-[240px]">
            <ResponsiveContainer>
              <LineChart data={revenueSeries} margin={{ left: -18, right: 8, top: 8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(120,170,220,0.1)" />
                <XAxis dataKey="d" tick={{ fill: '#7286a6', fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={{ background: '#0c1730', border: '1px solid rgba(139,92,246,0.3)', borderRadius: 12, color: '#e8f0ff' }} />
                <Line type="monotone" dataKey="v" stroke="#8b5cf6" strokeWidth={2.5} dot={{ r: 3, fill: '#8b5cf6' }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle>权威资金状态</SectionTitle>
          <div className="space-y-3">{finance?.positions.map((position)=><div key={position.agentId} className="rounded-xl border border-[var(--ap-border)] p-3"><div className="text-[13px] text-[var(--ap-text)]">{position.agentName}</div><div className="mt-1 text-[11px] text-[var(--ap-muted)]">概览应收 {position.overviewReceivable} · 正式可领 {position.formalClaimable}</div></div>)}</div>
        </Panel>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel className="p-5">
          <SectionTitle right={<button onClick={() => nav('/agent/integration')} className="text-[12px] text-[var(--ap-cyan)]">管理</button>}>Agent 运行状态</SectionTitle>
          <div className="space-y-3">
            {agents.map((a) => (
              <div key={a.id} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3">
                <div className="flex items-center gap-3">
                  <Activity size={16} className={a.health === 'healthy' ? 'text-[var(--ap-success)]' : a.health === 'degraded' ? 'text-[var(--ap-warning)]' : 'text-[var(--ap-danger)]'} />
                  <div>
                    <div className="text-[14px] text-[var(--ap-text)]">{a.name}</div>
                    <div className="text-[12px] text-[var(--ap-muted)]">{a.category} · 容量 {a.activeCapacity}/{a.maxConcurrency}</div>
                  </div>
                </div>
                <Pill tone={a.health === 'healthy' ? 'green' : a.health === 'degraded' ? 'amber' : 'red'} dot>
                  {a.health}
                </Pill>
              </div>
            ))}
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle right={<button onClick={() => nav('/agent/orders')} className="text-[12px] text-[var(--ap-cyan)]">全部订单</button>}>运行告警</SectionTitle>
          <div className="space-y-3">
            {alerts.map((al) => (
              <div key={al.t} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] px-4 py-3">
                <div className="flex items-center gap-3">
                  <Pill tone={al.tone} dot>{al.tone === 'red' ? '严重' : al.tone === 'amber' ? '警告' : '信息'}</Pill>
                  <span className="text-[13px] text-[var(--ap-text-2)]">{al.t}</span>
                </div>
                <span className="flex items-center gap-2 text-[12px] text-[var(--ap-muted)]">{al.time}<ArrowRight size={13} /></span>
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </Page>
  );
}

function monthlySeries(records:Array<{amount:string;createdAt:string}>){const now=new Date();return Array.from({length:6},(_,offset)=>{const date=new Date(now.getFullYear(),now.getMonth()-5+offset,1);const key=`${date.getFullYear()}-${date.getMonth()}`;const value=records.filter((record)=>{const item=new Date(record.createdAt);return `${item.getFullYear()}-${item.getMonth()}`===key}).reduce((sum,record)=>sum+Number(record.amount),0);return{d:`${date.getMonth()+1}月`,v:value};});}
