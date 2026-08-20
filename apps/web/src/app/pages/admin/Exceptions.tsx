import { Ban, Info, RefreshCw, Repeat, Wrench } from "lucide-react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";

const EXCEPTIONS = [
  { id: "TSK-2044", type: "回调超时", detail: "Webhook 连续 3 次投递失败", stage: "执行中", tone: "amber" as const },
  { id: "TSK-2039", type: "消息丢失", detail: "结算事件未被下游消费", stage: "待结算", tone: "red" as const },
  { id: "TSK-2030", type: "状态卡死", detail: '任务停留在"匹配中"超过 SLA', stage: "匹配中", tone: "amber" as const },
];

export default function AdminExceptions() {
  return (
    <Page>
      <PageHeader title="异常任务" subtitle="重试、消息重放与排障" />

      <InfoNote tone="amber">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} /> 管理员可进行技术层排障（重试、消息重放），但不能代替用户验收或发起退款。
        </span>
      </InfoNote>

      <div className="space-y-4">
        {EXCEPTIONS.map((item) => (
          <Panel key={item.id} className="p-5">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{item.id}</span>
                  <Pill tone={item.tone} dot>
                    {item.type}
                  </Pill>
                  <Pill tone="gray">{item.stage}</Pill>
                </div>
                <p className="mt-1.5 text-[13px] text-[var(--ap-muted)]">{item.detail}</p>
              </div>
              <div className="flex flex-wrap gap-2">
                <GhostButton icon={RefreshCw} onClick={() => toast.success(`${item.id} 已触发重试`)}>
                  重试
                </GhostButton>
                <GhostButton icon={Repeat} onClick={() => toast.success(`${item.id} 消息已重放`)}>
                  消息重放
                </GhostButton>
                <GhostButton icon={Wrench} onClick={() => toast.success("已生成排障工单")}>
                  排障
                </GhostButton>
              </div>
            </div>
            <div className="mt-3 flex items-center gap-2 rounded-lg border border-[rgba(251,113,133,0.2)] bg-[rgba(251,113,133,0.06)] px-3 py-2 text-[12px] text-[var(--ap-danger)]">
              <Ban size={13} className="shrink-0" />
              不可代用户验收 · 不可代发起退款 · 资金操作须由当事方或仲裁裁决触发
            </div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}
