import { RefreshCw, ShieldAlert, UserCheck, Wrench } from "lucide-react";
import { useEffect, useState } from "react";
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
import {
  assignDispute,
  readDisputes,
  runAdminOperation,
  type DisputeView,
} from "../../lib/platform-api";

export default function AdminExceptions() {
  const [cases, setCases] = useState<DisputeView[]>([]),
    [assignee, setAssignee] = useState(""),
    [resource, setResource] = useState(""),
    [reason, setReason] = useState("operational_recovery"),
    [kind, setKind] = useState<
      | "dlq_replay"
      | "ledger_reversal"
      | "reconciliation_repair"
      | "state_migration"
    >("dlq_replay"),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const load = async () => {
    setError("");
    try {
      setCases((await readDisputes()).cases);
    } catch (cause) {
      setError(message(cause));
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const run = async (
    name: string,
    action: () => Promise<unknown>,
    success: string,
  ) => {
    if (busy) return;
    setBusy(name);
    setError("");
    setNotice("");
    try {
      await action();
      setNotice(success);
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy("");
    }
  };
  return (
    <Page>
      <PageHeader
        title="异常、DLQ 与案件运营"
        subtitle="仅追加、幂等且完整审计的受控操作"
        actions={
          <GhostButton icon={RefreshCw} onClick={() => void load()}>
            刷新
          </GhostButton>
        }
      />
      {error ? (
        <div
          role="alert"
          className="rounded-xl bg-rose-300/10 p-3 text-rose-100"
        >
          {error}
        </div>
      ) : null}
      {notice ? (
        <div
          role="status"
          className="rounded-xl bg-cyan-300/10 p-3 text-cyan-100"
        >
          {notice}
        </div>
      ) : null}
      <InfoNote tone="amber">
        <span className="inline-flex gap-2">
          <ShieldAlert size={14} />
          管理员不能代签客户交易、读取 Agent 凭证或覆盖证据、账务及案件历史。
        </span>
      </InfoNote>
      <div className="grid gap-6 xl:grid-cols-[1.2fr_.8fr]">
        <Panel className="p-5">
          <SectionTitle right={<Pill tone="violet">{cases.length} 案件</Pill>}>
            人员分配与冲突隔离
          </SectionTitle>
          <div className="space-y-3">
            {cases.map((view) => (
              <article
                key={view.case.id}
                className="rounded-xl border border-[var(--ap-border)] p-4"
              >
                <div className="flex flex-wrap justify-between gap-2">
                  <b className="text-[13px]">{view.case.id}</b>
                  <Pill tone="amber">{view.case.state}</Pill>
                </div>
                <div className="mt-2 text-[11px] text-[var(--ap-muted)]">
                  初裁：
                  {view.case.assignments.find(
                    (item) => item.stage === "initial",
                  )?.assigneeId ?? "未分配"}{" "}
                  · 复核：
                  {view.case.assignments.find((item) => item.stage === "review")
                    ?.assigneeId ?? "未分配"}
                </div>
                <div className="mt-3 flex gap-2">
                  <label
                    className="sr-only"
                    htmlFor={`assignee-${view.case.id}`}
                  >
                    裁决人员 ID
                  </label>
                  <input
                    id={`assignee-${view.case.id}`}
                    className="form-input"
                    value={assignee}
                    onChange={(e) => setAssignee(e.target.value)}
                    placeholder="裁决人员 ID"
                  />
                  <CtaButton
                    icon={UserCheck}
                    disabled={!assignee || Boolean(busy)}
                    onClick={() =>
                      void run(
                        `assign:${view.case.id}`,
                        () =>
                          assignDispute(view.case.id, crypto.randomUUID(), {
                            assigneeId: assignee,
                            stage: view.case.assignments.some(
                              (item) => item.stage === "initial",
                            )
                              ? "review"
                              : "initial",
                          }),
                        "分配已记录并完成服务端可信冲突校验。",
                      )
                    }
                  >
                    分配
                  </CtaButton>
                </div>
              </article>
            ))}
          </div>
        </Panel>
        <Panel strong className="p-5">
          <SectionTitle>受控修复操作</SectionTitle>
          <form
            className="space-y-3"
            onSubmit={(event) => {
              event.preventDefault();
              void run(
                "admin",
                () =>
                  runAdminOperation(crypto.randomUUID(), {
                    kind,
                    resourceType: "platform_resource",
                    resourceId: resource,
                    reasonCode: reason,
                    payload: { requestedAction: kind },
                  }),
                "操作意图已追加审计；执行器将按领域边界幂等处理。",
              );
            }}
          >
            <label
              htmlFor="admin-operation-kind"
              className="block text-[12px] text-[var(--ap-muted)]"
            >
              操作类型
            </label>
            <select
              id="admin-operation-kind"
              className="form-input"
              value={kind}
              onChange={(e) => setKind(e.target.value as typeof kind)}
            >
              <option value="dlq_replay">DLQ 安全重放</option>
              <option value="ledger_reversal">账本冲正</option>
              <option value="reconciliation_repair">对账修复</option>
              <option value="state_migration">受控状态迁移</option>
            </select>
            <label
              htmlFor="admin-resource"
              className="block text-[12px] text-[var(--ap-muted)]"
            >
              资源 ID
            </label>
            <input
              id="admin-resource"
              className="form-input"
              required
              value={resource}
              onChange={(e) => setResource(e.target.value)}
            />
            <label
              htmlFor="admin-reason"
              className="block text-[12px] text-[var(--ap-muted)]"
            >
              原因代码
            </label>
            <input
              id="admin-reason"
              className="form-input"
              required
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <CtaButton
              type="submit"
              full
              icon={Wrench}
              busy={busy === "admin"}
              disabled={!resource || Boolean(busy)}
            >
              记录受控操作
            </CtaButton>
          </form>
        </Panel>
      </div>
    </Page>
  );
}
function message(value: unknown) {
  return value instanceof Error ? value.message : "管理操作失败。";
}
