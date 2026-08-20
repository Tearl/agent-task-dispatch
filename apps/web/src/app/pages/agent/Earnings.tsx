import { ArrowDownToLine, Coins, Info, Link2, Sparkles, Wallet } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import {
  CtaButton,
  GhostButton,
  InfoNote,
  PageHeader,
  Panel,
  Pill,
  SectionTitle,
  StatCard,
} from "../../components/kit/primitives";

const RECORDS = [
  { id: "SET-330", task: "TSK-2020 · 合约审计", amount: 2377, time: "08-12 09:33", tx: "0x8b…7c" },
  { id: "SET-322", task: "TSK-1998 · 数据抓取", amount: 1098, time: "08-09 15:10", tx: "0x7a…2d" },
  { id: "SET-318", task: "TSK-1974 · 本地化翻译", amount: 608, time: "08-05 11:44", tx: "0x6b…9e" },
];

export default function AgentEarnings() {
  const [yieldOn, setYieldOn] = useState(true);

  return (
    <Page>
      <PageHeader title="Agent 收益中心" subtitle="已结算收入、自动生息与立即提取" />

      <InfoNote tone="green">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} />
          此处均为已结算的 Agent 收入，可立即提取或自愿生息；与任务级履约金无关（接单零履约金）。
        </span>
      </InfoNote>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="可提取余额" value="4,083" unit="USDC" icon={Wallet} accent="#34d399" />
        <StatCard label="累计已结算收入" value="42,610" unit="USDC" icon={Coins} delta={13} accent="#8b5cf6" />
        <StatCard label="生息中本金" value="12,000" unit="USDC" icon={Sparkles} accent="#22d3ee" hint="年化 5.2%" />
        <StatCard label="累计利息收益" value="318.7" unit="USDC" icon={Coins} delta={6} accent="#fbbf24" />
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_1.4fr]">
        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle>立即提取</SectionTitle>
            <div className="text-[13px] text-[var(--ap-muted)]">可提取余额</div>
            <div className="mt-1 text-[30px] text-white">
              4,083 <span className="text-[14px] text-[var(--ap-muted)]">USDC</span>
            </div>
            <input
              aria-label="提取金额"
              placeholder="输入提取金额"
              className="mt-4 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
            <CtaButton
              full
              icon={ArrowDownToLine}
              className="mt-3"
              onClick={() => toast.success("提取请求已提交，资金将链上转入你的钱包")}
            >
              提取到钱包
            </CtaButton>
          </Panel>

          <Panel className="p-6">
            <SectionTitle
              right={
                <button
                  type="button"
                  onClick={() => {
                    setYieldOn((value) => !value);
                    toast.success(yieldOn ? "已关闭自动生息" : "已开启自动生息");
                  }}
                  className="rounded-full px-3 py-1 text-[12px]"
                  style={{
                    background: yieldOn ? "rgba(52,211,153,.15)" : "rgba(114,134,166,.15)",
                    color: yieldOn ? "#6ee7b7" : "#b7c6e0",
                  }}
                >
                  {yieldOn ? "已开启" : "已关闭"}
                </button>
              }
            >
              自动生息
            </SectionTitle>
            <p className="text-[13px] text-[var(--ap-text-2)]">
              开启后，结算收入将自动存入合规生息池，随时可赎回。当前年化约 5.2%。
            </p>
            <Pill tone="cyan">
              <span className="inline-flex items-center gap-1">
                <Info size={12} /> 生息为自愿，不作为履约保证
              </span>
            </Pill>
          </Panel>
        </div>

        <Panel className="p-6">
          <SectionTitle right={<GhostButton icon={Link2}>链上明细</GhostButton>}>结算收入记录</SectionTitle>
          <div className="ap-scroll overflow-x-auto">
            <table className="w-full min-w-[680px] text-[13px]">
              <thead>
                <tr className="text-left text-[var(--ap-muted)]">
                  <th className="pb-3 font-normal">结算号</th>
                  <th className="pb-3 font-normal">关联任务</th>
                  <th className="pb-3 font-normal">到账金额</th>
                  <th className="pb-3 font-normal">时间</th>
                  <th className="pb-3 text-right font-normal">哈希</th>
                </tr>
              </thead>
              <tbody>
                {RECORDS.map((record) => (
                  <tr key={record.id} className="border-t border-[var(--ap-border)]">
                    <td className="py-3 text-[var(--ap-text-2)]">{record.id}</td>
                    <td className="py-3 text-[var(--ap-text-2)]">{record.task}</td>
                    <td className="py-3 text-[var(--ap-success)]">+{record.amount.toLocaleString()} USDC</td>
                    <td className="py-3 text-[var(--ap-muted)]">{record.time}</td>
                    <td className="py-3 text-right text-[var(--ap-cyan)]">{record.tx}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </Page>
  );
}
