import { useNavigate } from 'react-router';
import {
  LineChart, Line, ResponsiveContainer, XAxis, Tooltip, CartesianGrid,
} from 'recharts';
import { Cpu, ClipboardList, AlertTriangle, Coins, Star, ArrowRight, Activity } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, StatCard, Panel, SectionTitle, Pill } from '../../components/kit/primitives';
import { ReputationRadar } from '../../components/kit/ReputationRadar';
import { MY_AGENTS, REVENUE_SERIES } from '../../lib/mock';

const ALERTS = [
  { t: 'LinguaX 响应延迟升高（P95 8.2s）', tone: 'amber' as const, time: '20 分钟前' },
  { t: 'PixForge 协议校验失败，已下线', tone: 'red' as const, time: '1 小时前' },
  { t: 'DataForge 新增 12 个待处理订单', tone: 'cyan' as const, time: '2 小时前' },
];

export default function AgentDashboard() {
  const nav = useNavigate();
  return (
    <Page>
      <PageHeader title="Agent 工作台" subtitle="Agent 状态、待处理订单、运行告警、收入与信誉概览" />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="在线 Agent" value="2 / 3" icon={Cpu} accent="#8b5cf6" hint="1 个离线" />
        <StatCard label="待处理订单" value="18" unit="单" icon={ClipboardList} delta={9} accent="#22d3ee" />
        <StatCard label="运行告警" value="2" unit="条" icon={AlertTriangle} accent="#fbbf24" />
        <StatCard label="本月已结算收入" value="8,142" unit="USDC" icon={Coins} delta={13} accent="#34d399" />
      </div>

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        <Panel className="p-5">
          <SectionTitle right={<Pill tone="violet" dot>不含履约金</Pill>}>结算收入趋势</SectionTitle>
          <div className="h-[240px]">
            <ResponsiveContainer>
              <LineChart data={REVENUE_SERIES} margin={{ left: -18, right: 8, top: 8 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(120,170,220,0.1)" />
                <XAxis dataKey="d" tick={{ fill: '#7286a6', fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip contentStyle={{ background: '#0c1730', border: '1px solid rgba(139,92,246,0.3)', borderRadius: 12, color: '#e8f0ff' }} />
                <Line type="monotone" dataKey="v" stroke="#8b5cf6" strokeWidth={2.5} dot={{ r: 3, fill: '#8b5cf6' }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle>五维信誉概览</SectionTitle>
          <ReputationRadar dims={{ quality: 95, speed: 88, reliability: 96, communication: 90, compliance: 99 }} color="#8b5cf6" height={200} />
          <div className="mt-2 flex items-center justify-center gap-1 text-[13px] text-[var(--ap-text-2)]">
            <Star size={14} className="text-[var(--ap-warning)]" /> 综合信誉 4.8 / 5.0
          </div>
        </Panel>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel className="p-5">
          <SectionTitle right={<button onClick={() => nav('/agent/integration')} className="text-[12px] text-[var(--ap-cyan)]">管理</button>}>Agent 运行状态</SectionTitle>
          <div className="space-y-3">
            {MY_AGENTS.map((a) => (
              <div key={a.id} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3">
                <div className="flex items-center gap-3">
                  <Activity size={16} className={a.status === 'online' ? 'text-[var(--ap-success)]' : a.status === 'degraded' ? 'text-[var(--ap-warning)]' : 'text-[var(--ap-danger)]'} />
                  <div>
                    <div className="text-[14px] text-[var(--ap-text)]">{a.name}</div>
                    <div className="text-[12px] text-[var(--ap-muted)]">{a.category} · 30 天 {a.orders30d} 单</div>
                  </div>
                </div>
                <Pill tone={a.status === 'online' ? 'green' : a.status === 'degraded' ? 'amber' : 'red'} dot>
                  {a.status === 'online' ? '在线' : a.status === 'degraded' ? '降级' : '离线'} {a.health}%
                </Pill>
              </div>
            ))}
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle right={<button onClick={() => nav('/agent/orders')} className="text-[12px] text-[var(--ap-cyan)]">全部订单</button>}>运行告警</SectionTitle>
          <div className="space-y-3">
            {ALERTS.map((al) => (
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
