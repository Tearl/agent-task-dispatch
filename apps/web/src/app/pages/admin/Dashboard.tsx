import { Activity, AlertTriangle, Bell, ServerCog, ShieldAlert, Users } from "lucide-react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from "recharts";

import { Page } from "../../components/AppShell";
import { PageHeader, Panel, Pill, SectionTitle, StatCard } from "../../components/kit/primitives";

const TRAFFIC = [
  { d: "00", v: 120 },
  { d: "04", v: 90 },
  { d: "08", v: 260 },
  { d: "12", v: 420 },
  { d: "16", v: 510 },
  { d: "20", v: 380 },
  { d: "24", v: 210 },
];

const TODO = [
  { t: "Agent「PixForge」协议校验失败，待复核", tone: "red" as const },
  { t: "3 个异常任务等待人工排障", tone: "amber" as const },
  { t: "链上对账存在 1 笔状态差异", tone: "amber" as const },
  { t: "2 项系统配置变更等待第二人审批", tone: "cyan" as const },
];

const EVENTS = [
  { t: "异常登录尝试（已拦截）", ip: "103.21.xx.xx", time: "10:22", tone: "red" as const },
  { t: "管理员权限变更", ip: "admin@ops", time: "09:40", tone: "cyan" as const },
  { t: "系统配置回滚", ip: "admin@sec", time: "昨日 22:10", tone: "amber" as const },
];

export default function AdminDashboard() {
  return (
    <Page>
      <PageHeader
        title="治理概览"
        subtitle="平台状态、待处理事项、风险告警与安全事件"
        actions={
          <Pill tone="green" dot>
            系统运行正常
          </Pill>
        }
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="活跃用户" value="12,480" icon={Users} delta={6} accent="#38bdf8" />
        <StatCard label="进行中任务" value="1,342" icon={Activity} delta={4} accent="#22d3ee" />
        <StatCard label="风险告警" value="4" unit="条" icon={ShieldAlert} accent="#fb7185" />
        <StatCard label="异常任务" value="3" unit="个" icon={AlertTriangle} accent="#fbbf24" />
      </div>

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <Panel className="p-5">
          <SectionTitle right={<span className="text-[12px] text-[var(--ap-muted)]">近 24 小时</span>}>
            平台交易吞吐
          </SectionTitle>
          <div className="h-[240px]">
            <ResponsiveContainer>
              <AreaChart data={TRAFFIC} margin={{ left: -20, right: 8, top: 8 }}>
                <defs>
                  <linearGradient id="admin-traffic" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#38bdf8" stopOpacity={0.5} />
                    <stop offset="100%" stopColor="#38bdf8" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(120,170,220,0.1)" />
                <XAxis dataKey="d" tick={{ fill: "#7286a6", fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip
                  contentStyle={{
                    background: "#0c1730",
                    border: "1px solid rgba(56,189,248,0.3)",
                    borderRadius: 12,
                    color: "#e8f0ff",
                  }}
                />
                <Area type="monotone" dataKey="v" stroke="#38bdf8" strokeWidth={2} fill="url(#admin-traffic)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle right={<ServerCog size={16} className="text-[var(--ap-info)]" />}>待处理事项</SectionTitle>
          <div className="space-y-3">
            {TODO.map((item) => (
              <div key={item.t} className="flex items-center gap-3 rounded-xl border border-[var(--ap-border)] px-4 py-3">
                <Pill tone={item.tone} dot>
                  {item.tone === "red" ? "高" : item.tone === "amber" ? "中" : "低"}
                </Pill>
                <span className="text-[13px] text-[var(--ap-text-2)]">{item.t}</span>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <Panel className="p-5">
        <SectionTitle right={<Bell size={16} className="text-[var(--ap-danger)]" />}>安全事件</SectionTitle>
        <div className="ap-scroll overflow-x-auto">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="text-left text-[var(--ap-muted)]">
                <th className="pb-3 font-normal">事件</th>
                <th className="pb-3 font-normal">来源</th>
                <th className="pb-3 font-normal">级别</th>
                <th className="pb-3 text-right font-normal">时间</th>
              </tr>
            </thead>
            <tbody>
              {EVENTS.map((event) => (
                <tr key={event.t} className="border-t border-[var(--ap-border)]">
                  <td className="py-3 text-[var(--ap-text)]">{event.t}</td>
                  <td className="py-3 font-mono text-[var(--ap-muted)]">{event.ip}</td>
                  <td className="py-3">
                    <Pill tone={event.tone}>
                      {event.tone === "red" ? "严重" : event.tone === "amber" ? "警告" : "信息"}
                    </Pill>
                  </td>
                  <td className="py-3 text-right text-[var(--ap-muted)]">{event.time}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </Page>
  );
}
