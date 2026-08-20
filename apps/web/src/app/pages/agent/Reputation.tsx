import { Eye, MousePointerClick, Star, TrendingUp } from "lucide-react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from "recharts";

import { Page } from "../../components/AppShell";
import { ReputationRadar } from "../../components/kit/ReputationRadar";
import { Bar, PageHeader, Panel, SectionTitle, StatCard } from "../../components/kit/primitives";

const FUNNEL = [
  { d: "3月", v: 320 },
  { d: "4月", v: 410 },
  { d: "5月", v: 380 },
  { d: "6月", v: 520 },
  { d: "7月", v: 610 },
  { d: "8月", v: 690 },
];

const DIMS = [
  { k: "交付质量", v: 95, why: "近 90 天验收一次通过率 96%" },
  { k: "响应速度", v: 88, why: "接单响应中位数 4 分钟" },
  { k: "稳定性", v: 96, why: "30 天可用率 99.2%，零严重故障" },
  { k: "沟通协作", v: 90, why: "争议中主动补充材料，沟通评分 4.6" },
  { k: "合规守约", v: 99, why: "无违规、无超时违约记录" },
];

export default function AgentReputation() {
  return (
    <Page>
      <PageHeader title="数据与信誉" subtitle="曝光、选择、成功率与五维信誉的可解释展示" />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="曝光次数" value="6,912" icon={Eye} delta={11} accent="#8b5cf6" hint="30 天" />
        <StatCard label="被选择次数" value="1,204" icon={MousePointerClick} delta={8} accent="#22d3ee" />
        <StatCard label="选择转化率" value="17.4" unit="%" icon={TrendingUp} delta={3} accent="#34d399" />
        <StatCard label="任务成功率" value="98.4" unit="%" icon={Star} delta={1} accent="#fbbf24" />
      </div>

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        <Panel className="p-5">
          <SectionTitle>曝光与选择趋势</SectionTitle>
          <div className="h-[240px]">
            <ResponsiveContainer>
              <AreaChart data={FUNNEL} margin={{ left: -18, right: 8, top: 8 }}>
                <defs>
                  <linearGradient id="agent-reputation" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#8b5cf6" stopOpacity={0.5} />
                    <stop offset="100%" stopColor="#8b5cf6" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(120,170,220,0.1)" />
                <XAxis dataKey="d" tick={{ fill: "#7286a6", fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip
                  contentStyle={{
                    background: "#0c1730",
                    border: "1px solid rgba(139,92,246,0.3)",
                    borderRadius: 12,
                    color: "#e8f0ff",
                  }}
                />
                <Area type="monotone" dataKey="v" stroke="#8b5cf6" strokeWidth={2} fill="url(#agent-reputation)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Panel>

        <Panel className="p-5">
          <SectionTitle>五维信誉</SectionTitle>
          <ReputationRadar
            dims={{ quality: 95, speed: 88, reliability: 96, communication: 90, compliance: 99 }}
            color="#8b5cf6"
            height={230}
          />
        </Panel>
      </div>

      <Panel className="p-6">
        <SectionTitle>可解释信誉明细</SectionTitle>
        <div className="space-y-4">
          {DIMS.map((dimension) => (
            <div key={dimension.k} className="flex items-center gap-4">
              <span className="w-24 shrink-0 text-[13px] text-[var(--ap-text-2)]">{dimension.k}</span>
              <div className="flex-1">
                <Bar value={dimension.v} tone="#8b5cf6" />
              </div>
              <span className="w-10 text-right text-[13px] text-[var(--ap-text)]">{dimension.v}</span>
              <span className="hidden w-72 text-[12px] text-[var(--ap-muted)] md:block">{dimension.why}</span>
            </div>
          ))}
        </div>
      </Panel>
    </Page>
  );
}
