import { AlertTriangle, CheckCircle2, FileText, Link2, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import {
  CtaButton,
  GhostButton,
  PageHeader,
  Panel,
  Pill,
  SectionTitle,
  StatCard,
} from "../../components/kit/primitives";

const DIFFERENCES = [
  { tx: "0x9a…21", task: "TSK-2001", chain: "已托管 2,600", platform: "待托管", status: "diff" },
  { tx: "0x8b…7c", task: "TSK-2020", chain: "已结算 2,377", platform: "已结算 2,377", status: "ok" },
  { tx: "0x6d…4e", task: "TSK-2012", chain: "已冻结 1,500", platform: "已冻结 1,500", status: "ok" },
  { tx: "0x5c…3a", task: "TSK-1990", chain: "已退款 500", platform: "退款处理中", status: "diff" },
];

export default function AdminReconciliation() {
  return (
    <Page>
      <PageHeader
        title="链上对账"
        subtitle="链上与平台状态差异、事件重放与对账报告"
        actions={
          <CtaButton icon={FileText} onClick={() => toast.success("对账报告已生成")}>
            生成对账报告
          </CtaButton>
        }
      />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="今日对账笔数" value="8,214" icon={Link2} accent="#38bdf8" />
        <StatCard label="一致" value="8,212" unit="笔" icon={CheckCircle2} accent="#34d399" />
        <StatCard label="状态差异" value="2" unit="笔" icon={AlertTriangle} accent="#fb7185" />
        <StatCard label="对账覆盖率" value="99.98" unit="%" icon={RefreshCw} accent="#22d3ee" />
      </div>

      <Panel className="p-5">
        <SectionTitle
          right={
            <GhostButton icon={RefreshCw} onClick={() => toast.success("已重新执行对账")}>
              重新对账
            </GhostButton>
          }
        >
          差异明细
        </SectionTitle>
        <div className="ap-scroll overflow-x-auto">
          <table className="w-full min-w-[800px] text-[13px]">
            <thead>
              <tr className="text-left text-[var(--ap-muted)]">
                <th className="pb-3 font-normal">交易哈希</th>
                <th className="pb-3 font-normal">任务</th>
                <th className="pb-3 font-normal">链上状态</th>
                <th className="pb-3 font-normal">平台状态</th>
                <th className="pb-3 font-normal">对账结果</th>
                <th className="pb-3 text-right font-normal">操作</th>
              </tr>
            </thead>
            <tbody>
              {DIFFERENCES.map((item) => (
                <tr key={item.tx} className="border-t border-[var(--ap-border)]">
                  <td className="py-3 font-mono text-[var(--ap-cyan)]">{item.tx}</td>
                  <td className="py-3 text-[var(--ap-text-2)]">{item.task}</td>
                  <td className="py-3 text-[var(--ap-text-2)]">{item.chain}</td>
                  <td className="py-3 text-[var(--ap-text-2)]">{item.platform}</td>
                  <td className="py-3">
                    <Pill tone={item.status === "ok" ? "green" : "red"} dot>
                      {item.status === "ok" ? "一致" : "差异"}
                    </Pill>
                  </td>
                  <td className="py-3 text-right">
                    {item.status === "diff" ? (
                      <GhostButton onClick={() => toast.success("已重放链上事件以修复状态")}>事件重放</GhostButton>
                    ) : (
                      <span className="text-[var(--ap-muted)]">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </Page>
  );
}
