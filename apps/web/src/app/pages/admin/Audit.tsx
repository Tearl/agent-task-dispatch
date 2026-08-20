import { FileText, Search, ShieldCheck } from "lucide-react";

import { Page } from "../../components/AppShell";
import { GhostButton, PageHeader, Panel, Pill } from "../../components/kit/primitives";

const LOGS = [
  {
    id: "LOG-90231",
    actor: "admin@ops",
    action: "角色授权变更",
    target: "0x21be…9a4f",
    before: "发布方",
    after: "发布方, 开发者",
    time: "2026-08-20 09:40:12",
    hash: "0xa1…f0",
  },
  {
    id: "LOG-90230",
    actor: "admin@sec",
    action: "系统配置回滚",
    target: "match.shuffle.seed",
    before: "v12",
    after: "v11",
    time: "2026-08-19 22:10:03",
    hash: "0xb2…7c",
  },
  {
    id: "LOG-90229",
    actor: "admin@ops",
    action: "Agent 下架",
    target: "AG-15 GrayBot",
    before: "已上架",
    after: "已下架",
    time: "2026-08-19 16:22:47",
    hash: "0xc3…9d",
  },
  {
    id: "LOG-90228",
    actor: "system",
    action: "异常任务重试",
    target: "TSK-2044",
    before: "失败",
    after: "重试中",
    time: "2026-08-19 15:01:20",
    hash: "0xd4…1e",
  },
];

export default function AdminAudit() {
  return (
    <Page>
      <PageHeader
        title="审计日志"
        subtitle="只追加日志、变更前后对比与完整性校验"
        actions={<GhostButton icon={FileText}>导出</GhostButton>}
      />

      <Panel className="flex flex-wrap items-center gap-3 p-4">
        <div className="flex min-w-[220px] flex-1 items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-3 py-2">
          <Search size={16} className="text-[var(--ap-muted)]" />
          <input
            aria-label="搜索审计日志"
            placeholder="按操作人、操作类型或对象搜索…"
            className="w-full bg-transparent text-[14px] text-white outline-none placeholder:text-[var(--ap-muted)]"
          />
        </div>
        <span className="inline-flex items-center gap-1.5 rounded-lg border border-[rgba(52,211,153,0.3)] bg-[rgba(52,211,153,0.08)] px-3 py-2 text-[13px] text-[var(--ap-success)]">
          <ShieldCheck size={15} /> 完整性校验通过
        </span>
      </Panel>

      <div className="space-y-3">
        {LOGS.map((log) => (
          <Panel key={log.id} className="p-5">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[14px] text-[var(--ap-text)]">{log.action}</span>
                  <Pill tone="cyan">{log.actor}</Pill>
                  <Pill tone="gray">只追加</Pill>
                </div>
                <div className="mt-1 text-[12px] text-[var(--ap-muted)]">
                  {log.id} · 对象 {log.target} · {log.time}
                </div>
              </div>
              <span className="inline-flex items-center gap-1 font-mono text-[12px] text-[var(--ap-cyan)]">
                链上锚定 {log.hash}
              </span>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-2.5 text-[13px]">
              <span className="text-[var(--ap-muted)]">变更前</span>
              <span className="rounded bg-[rgba(251,113,133,0.12)] px-2 py-0.5 text-[var(--ap-danger)]">{log.before}</span>
              <span className="text-[var(--ap-muted)]">→</span>
              <span className="rounded bg-[rgba(52,211,153,0.12)] px-2 py-0.5 text-[var(--ap-success)]">{log.after}</span>
            </div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}
