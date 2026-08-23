import { FileLock2, RefreshCw, Scale, ShieldAlert } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
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
  decideDispute,
  readDisputes,
  reviewDispute,
  sha256Digest,
  type DisputeView,
} from "../../lib/platform-api";

export default function CaseReview() {
  const [cases, setCases] = useState<DisputeView[]>([]),
    [selected, setSelected] = useState(""),
    [award, setAward] = useState(50),
    [reason, setReason] = useState("evidence_weight"),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const operation = useRef("");
  const load = async () => {
    try {
      const result = await readDisputes();
      setCases(result.cases);
      setSelected((current) =>
        result.cases.some((item) => item.case.id === current)
          ? current
          : (result.cases[0]?.case.id ?? ""),
      );
    } catch (cause) {
      setError(message(cause));
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const view = useMemo(
    () => cases.find((item) => item.case.id === selected) ?? cases[0],
    [cases, selected],
  );
  const submit = async () => {
    if (!view || busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      operation.current ||= crypto.randomUUID();
      const evidenceRoot = await sha256Digest(
        view.case.evidence.map((item) => item.ciphertextDigest),
      );
      const assignment = view.case.assignments.find(
        (item) =>
          item.stage === (view.case.state === "decided" ? "review" : "initial"),
      );
      if (!assignment)
        throw new Error("案件尚未完成当前阶段的独立人员分配与冲突校验。");
      if (view.case.state === "decided")
        await reviewDispute(view.case.id, operation.current, {
          assigneeId: assignment.assigneeId,
          publisherBps: award * 100,
          reasonCode: reason,
          evidenceRoot,
        });
      else
        await decideDispute(view.case.id, operation.current, {
          publisherBps: award * 100,
          reasonCode: reason,
          evidenceRoot,
        });
      operation.current = "";
      setNotice(
        view.case.state === "decided"
          ? "唯一复核已提交，最终责任可以生效。"
          : "初裁已密封提交；复核窗口结束前信誉不会更新。",
      );
      await load();
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  };
  if (!view)
    return (
      <Page>
        <PageHeader title="案件审理" subtitle="独立初裁与唯一复核" />
        {error ? (
          <InfoNote tone="red">
            <span role="alert">{error}</span>
          </InfoNote>
        ) : (
          <InfoNote>当前没有分配给你的案件。</InfoNote>
        )}
      </Page>
    );
  const stage = view.case.state === "decided" ? "唯一复核" : "独立初裁";
  return (
    <Page>
      <PageHeader
        title="案件审理"
        subtitle={`${stage} · ${view.case.id}`}
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
          同一人员不能复核自己的初裁；第二次复核、未授权费用及无效档位由 Engine
          拒绝。
        </span>
      </InfoNote>
      <div className="grid gap-6 lg:grid-cols-[1.3fr_.8fr]">
        <Panel className="p-5">
          <SectionTitle
            right={<Pill tone="violet">{view.case.evidence.length}/12 类</Pill>}
          >
            脱敏证据清单
          </SectionTitle>
          <ol aria-label="案件证据清单" className="space-y-2">
            {view.case.evidence.map((item) => (
              <li
                key={item.id}
                className="flex gap-2 rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"
              >
                <FileLock2 size={15} />
                <span>
                  <b>{item.category}</b>
                  <span className="block break-all text-[var(--ap-muted)]">
                    {item.ciphertextDigest}
                  </span>
                </span>
              </li>
            ))}
          </ol>
        </Panel>
        <Panel strong className="p-5">
          <SectionTitle right={<Pill tone="cyan">五档判付</Pill>}>
            {stage}
          </SectionTitle>
          <fieldset>
            <legend className="mb-2 text-[12px] text-[var(--ap-muted)]">
              发布者退款比例
            </legend>
            <div className="grid grid-cols-5 gap-2">
              {[0, 25, 50, 75, 100].map((value) => (
                <button
                  type="button"
                  key={value}
                  aria-pressed={award === value}
                  onClick={() => setAward(value)}
                  className={`rounded-lg border p-2 text-[12px] ${award === value ? "border-cyan-300 bg-cyan-300/10" : "border-[var(--ap-border)]"}`}
                >
                  {value}%
                </button>
              ))}
            </div>
          </fieldset>
          <label
            htmlFor="decision-reason"
            className="mb-1 mt-4 block text-[12px] text-[var(--ap-muted)]"
          >
            决定原因
          </label>
          <input
            id="decision-reason"
            className="form-input"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
          <CtaButton
            full
            className="mt-4"
            icon={Scale}
            busy={busy}
            disabled={busy || view.case.evidence.length < 12}
            onClick={() => void submit()}
          >
            提交{stage}
          </CtaButton>
          <p className="mt-3 text-[11px] text-[var(--ap-muted)]">
            完整清单、时间窗、分配、职责分离和费用授权均在 Engine 再校验。
          </p>
        </Panel>
      </div>
    </Page>
  );
}
function message(value: unknown) {
  return value instanceof Error ? value.message : "案件操作失败。";
}
