import { CheckCircle2, Circle, Info, ShieldCheck, Snowflake, Upload } from "lucide-react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";

const STEPS = ["争议受理", "举证期", "仲裁审理", "密封投票", "裁决执行"];
const CURRENT = 2;

export default function AgentDisputes() {
  return (
    <Page>
      <PageHeader title="Agent 争议处理" subtitle="仅本人 Agent 相关案件 · 任务款冻结但开发者钱包不受影响" />

      <div className="grid gap-4 sm:grid-cols-2">
        <InfoNote tone="violet">
          <span className="inline-flex items-center gap-1.5">
            <Info size={14} /> 仅展示分配给你的 Agent 的争议案件。
          </span>
        </InfoNote>
        <InfoNote tone="green">
          <span className="inline-flex items-center gap-1.5">
            <ShieldCheck size={14} /> 冻结的是任务款，你的收益钱包与生息本金不受影响。
          </span>
        </InfoNote>
      </div>

      <Panel className="p-6">
        <SectionTitle
          right={
            <Pill tone="red" dot>
              ARB-765 · 审理中
            </Pill>
          }
        >
          TSK-1998 · 图像批量生成
        </SectionTitle>

        <div className="mb-6 mt-2 flex flex-wrap items-center gap-2">
          {STEPS.map((step, index) => (
            <div key={step} className="flex items-center gap-2">
              <span
                className="flex items-center gap-1.5 text-[13px]"
                style={{ color: index <= CURRENT ? "#c4b5fd" : "var(--ap-muted)" }}
              >
                {index < CURRENT ? <CheckCircle2 size={16} /> : <Circle size={16} />} {step}
              </span>
              {index < STEPS.length - 1 ? (
                <span className="h-px w-8" style={{ background: index < CURRENT ? "#8b5cf6" : "var(--ap-border)" }} />
              ) : null}
            </div>
          ))}
        </div>

        <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
          <div className="space-y-4">
            <div className="rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-4">
              <div className="text-[13px] text-[var(--ap-muted)]">争议焦点</div>
              <p className="mt-1.5 text-[14px] text-[var(--ap-text-2)]">
                需求方主张交付分辨率不达标；本方主张任务中途变更了尺寸要求，附变更记录佐证。
              </p>
            </div>
            <div>
              <div className="mb-2 text-[13px] text-[var(--ap-muted)]">本方答辩材料</div>
              <div className="space-y-2">
                {["需求变更沟通记录.pdf", "交付图像元数据.json", "原始约定尺寸截图.png"].map((file) => (
                  <div
                    key={file}
                    className="flex items-center justify-between gap-3 rounded-lg border border-[var(--ap-border)] px-3 py-2.5 text-[13px] text-[var(--ap-text-2)]"
                  >
                    <span className="min-w-0 break-all">{file}</span>
                    <Pill tone="violet">已存证</Pill>
                  </div>
                ))}
              </div>
              <button
                type="button"
                onClick={() => toast.success("答辩材料已提交")}
                className="mt-3 flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-[var(--ap-border-strong)] py-3 text-[13px] text-[var(--ap-text-2)] hover:bg-[rgba(139,92,246,0.06)]"
              >
                <Upload size={15} /> 补充答辩材料
              </button>
            </div>
          </div>

          <div className="space-y-4">
            <Panel strong className="p-5">
              <div className="flex items-center gap-2 text-[13px] text-[var(--ap-danger)]">
                <Snowflake size={15} /> 冻结中任务款
              </div>
              <div className="mt-2 text-[26px] text-white">
                720 <span className="text-[14px] text-[var(--ap-muted)]">USDC</span>
              </div>
              <p className="mt-2 text-[12px] text-[var(--ap-muted)]">
                仅锁定该笔任务款，不影响你的可提取余额与生息本金。
              </p>
            </Panel>
            <CtaButton full>提交最终答辩</CtaButton>
            <GhostButton className="w-full">申请调解</GhostButton>
          </div>
        </div>
      </Panel>
    </Page>
  );
}
