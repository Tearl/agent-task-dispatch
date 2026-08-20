import { GitCommitHorizontal, History, Info, RotateCcw, SlidersHorizontal, Users2 } from "lucide-react";
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
} from "../../components/kit/primitives";

const VERSIONS = [
  { v: "v12", by: "admin@ops", time: "08-20 09:40", status: "pending", note: "匹配权重调整 + 洗牌种子更新" },
  { v: "v11", by: "admin@sec", time: "08-18 14:02", status: "active", note: "当前生效版本" },
  { v: "v10", by: "admin@ops", time: "08-12 10:30", status: "archived", note: "争议举证期参数" },
];

export default function AdminConfig() {
  const [match, setMatch] = useState(70);
  const [shuffle, setShuffle] = useState(30);

  return (
    <Page>
      <PageHeader title="系统配置" subtitle="匹配与洗牌参数、版本化配置、双人审批与回滚" />

      <InfoNote tone="blue">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} /> 关键参数变更采用版本化管理与双人审批（four-eyes），可一键回滚到历史版本。
        </span>
      </InfoNote>

      <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
        <Panel className="space-y-6 p-6">
          <SectionTitle
            right={
              <Pill tone="amber" dot>
                草稿 v12
              </Pill>
            }
          >
            匹配与洗牌参数
          </SectionTitle>

          <div>
            <div className="flex justify-between text-[13px]">
              <span className="text-[var(--ap-text-2)]">匹配分权重（信誉 vs 价格）</span>
              <span className="text-[var(--ap-cyan)]">{match}%</span>
            </div>
            <input
              aria-label="匹配分权重"
              type="range"
              min={0}
              max={100}
              value={match}
              onChange={(event) => setMatch(Number(event.target.value))}
              className="mt-3 w-full accent-[var(--ap-cyan)]"
            />
          </div>
          <div>
            <div className="flex justify-between text-[13px]">
              <span className="text-[var(--ap-text-2)]">推荐洗牌随机度</span>
              <span className="text-[var(--ap-cyan)]">{shuffle}%</span>
            </div>
            <input
              aria-label="推荐洗牌随机度"
              type="range"
              min={0}
              max={100}
              value={shuffle}
              onChange={(event) => setShuffle(Number(event.target.value))}
              className="mt-3 w-full accent-[var(--ap-cyan)]"
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="候选 Agent 上限" value="3" />
            <Field label="匹配超时 (分钟)" value="30" />
            <Field label="争议举证期 (小时)" value="48" />
            <Field label="最低仲裁质押 (YD)" value="3000" />
          </div>

          <div className="flex items-center gap-2 rounded-xl border border-[rgba(56,189,248,0.25)] bg-[rgba(56,189,248,0.08)] px-4 py-3 text-[13px] text-[var(--ap-info)]">
            <Users2 size={16} className="shrink-0" /> 提交后需第二位管理员审批方可生效
          </div>
          <CtaButton icon={GitCommitHorizontal} onClick={() => toast.success("已提交变更，等待第二人审批")}>
            提交变更（发起审批）
          </CtaButton>
        </Panel>

        <Panel className="p-6">
          <SectionTitle right={<SlidersHorizontal size={16} className="text-[var(--ap-info)]" />}>版本历史</SectionTitle>
          <div className="space-y-3">
            {VERSIONS.map((version) => (
              <div key={version.v} className="rounded-xl border border-[var(--ap-border)] p-4">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <History size={15} className="text-[var(--ap-muted)]" />
                    <span className="text-[14px] text-[var(--ap-text)]">{version.v}</span>
                    <Pill tone={version.status === "active" ? "green" : version.status === "pending" ? "amber" : "gray"}>
                      {version.status === "active" ? "生效中" : version.status === "pending" ? "待审批" : "已归档"}
                    </Pill>
                  </div>
                  {version.status === "archived" ? (
                    <GhostButton icon={RotateCcw} onClick={() => toast.success(`已回滚到 ${version.v}`)}>
                      回滚
                    </GhostButton>
                  ) : null}
                </div>
                <div className="mt-2 text-[12px] text-[var(--ap-muted)]">
                  {version.by} · {version.time}
                </div>
                <div className="mt-1 text-[13px] text-[var(--ap-text-2)]">{version.note}</div>
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </Page>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  const id = `admin-config-${label}`;
  return (
    <div>
      <label htmlFor={id} className="text-[13px] text-[var(--ap-muted)]">
        {label}
      </label>
      <input
        id={id}
        defaultValue={value}
        className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-2.5 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
      />
    </div>
  );
}
