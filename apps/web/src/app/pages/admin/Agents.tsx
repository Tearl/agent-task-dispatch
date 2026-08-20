import { Cpu, EyeOff, Info, ShieldCheck } from "lucide-react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";

const AGENTS = [
  { id: "AG-01", name: "DataForge", dev: "0x8f2a…c21d", status: "listed", risk: "低", compliance: "通过" },
  { id: "AG-07", name: "LinguaX", dev: "0x21be…9a4f", status: "review", risk: "中", compliance: "待复核" },
  { id: "AG-11", name: "PixForge", dev: "0x7cd0…1e88", status: "rectify", risk: "高", compliance: "协议校验失败" },
  { id: "AG-15", name: "GrayBot", dev: "0x6d3f…4e02", status: "delisted", risk: "高", compliance: "违规下架" },
];

const TONE = { listed: "green", review: "amber", rectify: "amber", delisted: "red" } as const;
const LABEL = { listed: "已上架", review: "审核中", rectify: "整改中", delisted: "已下架" };

export default function AdminAgents() {
  return (
    <Page>
      <PageHeader title="Agent 治理" subtitle="审核、整改、下架管理" />

      <InfoNote tone="blue">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} /> 管理员可审核、要求整改或下架 Agent，但不能修改其业务内容或查看接入凭证明文。
        </span>
      </InfoNote>

      <Panel className="p-5">
        <div className="ap-scroll overflow-x-auto">
          <table className="w-full min-w-[860px] text-[13px]">
            <thead>
              <tr className="text-left text-[var(--ap-muted)]">
                <th className="pb-3 font-normal">Agent</th>
                <th className="pb-3 font-normal">开发者</th>
                <th className="pb-3 font-normal">合规状态</th>
                <th className="pb-3 font-normal">风险</th>
                <th className="pb-3 font-normal">上架状态</th>
                <th className="pb-3 text-right font-normal">治理操作</th>
              </tr>
            </thead>
            <tbody>
              {AGENTS.map((agent) => (
                <tr key={agent.id} className="border-t border-[var(--ap-border)]">
                  <td className="py-3">
                    <div className="flex items-center gap-2">
                      <Cpu size={15} className="text-[var(--ap-info)]" />
                      <span className="text-[var(--ap-text)]">{agent.name}</span>
                    </div>
                    <div className="text-[12px] text-[var(--ap-muted)]">{agent.id}</div>
                  </td>
                  <td className="py-3 font-mono text-[var(--ap-muted)]">{agent.dev}</td>
                  <td className="py-3 text-[var(--ap-text-2)]">{agent.compliance}</td>
                  <td className="py-3">
                    <Pill tone={agent.risk === "低" ? "green" : agent.risk === "中" ? "amber" : "red"}>{agent.risk}</Pill>
                  </td>
                  <td className="py-3">
                    <Pill tone={TONE[agent.status as keyof typeof TONE]}>{LABEL[agent.status as keyof typeof LABEL]}</Pill>
                  </td>
                  <td className="py-3 text-right">
                    <div className="flex justify-end gap-2">
                      <GhostButton onClick={() => toast.success("已通过审核")}>审核</GhostButton>
                      <GhostButton onClick={() => toast.success("已下发整改要求")}>整改</GhostButton>
                      <GhostButton onClick={() => toast.success("已下架")}>下架</GhostButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-4 flex flex-wrap gap-4 text-[12px] text-[var(--ap-muted)]">
          <span className="inline-flex items-center gap-1.5">
            <ShieldCheck size={13} /> 治理操作全程审计
          </span>
          <span className="inline-flex items-center gap-1.5">
            <EyeOff size={13} /> 凭证与业务逻辑对管理员不可见
          </span>
        </div>
      </Panel>
    </Page>
  );
}
