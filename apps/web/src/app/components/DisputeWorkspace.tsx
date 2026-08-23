import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  FileLock2,
  RefreshCw,
  ShieldCheck,
  Snowflake,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import {
  addDisputeClaim,
  appendDisputeEvidence,
  openDisputeCase,
  readDisputes,
  reconcileDisputeFreeze,
  recordDisputeFreeze,
  requestEvidenceAccess,
  settleDispute,
  sha256Digest,
  submitDisputeFreezeTransaction,
  type DisputeView,
  type WalletProvider,
} from "../lib/platform-api";
import {
  CtaButton,
  GhostButton,
  InfoNote,
  PageHeader,
  Panel,
  Pill,
  SectionTitle,
} from "./kit/primitives";

const labels: Record<string, string> = {
  soft_lock_pending: "软锁待链上确认",
  frozen: "已确认冻结",
  evidence: "举证期",
  decided: "初裁已作出",
  review_pending: "复核中",
  final: "最终完成",
  orphaned: "链重组待恢复",
};
const categories = [
  "specification",
  "overview",
  "acceptance",
  "formal_versions",
  "feedback",
  "change_orders",
  "executions",
  "usage",
  "messages",
  "callbacks",
  "fees",
  "policy",
];

export function DisputeWorkspace({ side }: { side: "publisher" | "agent" }) {
  const [items, setItems] = useState<DisputeView[]>([]),
    [selected, setSelected] = useState(""),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const [taskID, setTaskID] = useState(""),
    [deliveryUnitID, setDeliveryUnitID] = useState(""),
    [statement, setStatement] = useState(""),
    [claimText, setClaimText] = useState("");
  const [category, setCategory] = useState(categories[0]),
    [objectKey, setObjectKey] = useState(""),
    [ciphertextDigest, setCiphertextDigest] = useState(""),
    [objectVersion, setObjectVersion] = useState(""),
    [keyReference, setKeyReference] = useState("");
  const [settlementBPS, setSettlementBPS] = useState(5000),
    [agreement, setAgreement] = useState(""),
    [publisherSignature, setPublisherSignature] = useState(""),
    [agentSignature, setAgentSignature] = useState(""),
    [settlementMessage, setSettlementMessage] = useState("");
  const operation = useRef<string>(undefined);
  const view = useMemo(
    () => items.find((item) => item.case.id === selected) ?? items[0],
    [items, selected],
  );
  const load = async () => {
    setError("");
    try {
      const result = await readDisputes();
      setItems(result.cases);
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
  const act = async (
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
  const settlementPayload = async () => {
    const evidenceRoot = await sha256Digest(
      view?.case.evidence.map((item) => item.ciphertextDigest) ?? [],
    );
    const agreementHash = await sha256Digest(agreement);
    const message = `AgentTaskDisputeSettlement\nCase ID: ${view?.case.id ?? ""}\nAgreement Hash: ${agreementHash}\nEvidence Root: ${evidenceRoot}\nPublisher BPS: ${settlementBPS}`;
    return { evidenceRoot, agreementHash, message };
  };

  if (!view)
    return (
      <>
        <PageHeader
          title={side === "publisher" ? "争议处理" : "Agent 争议处理"}
          subtitle="权威案件、链上冻结与加密证据"
        />
        {error ? (
          <InfoNote tone="red">
            <span role="alert">{error}</span>
          </InfoNote>
        ) : null}
        <Panel className="p-5">
          <SectionTitle>提交首项主张</SectionTitle>
          <form
            className="grid gap-3 sm:grid-cols-2"
            onSubmit={(event) => {
              event.preventDefault();
              operation.current ??= crypto.randomUUID();
              void act(
                "open",
                async () => {
                  await openDisputeCase(taskID, operation.current!, {
                    deliveryUnitId: deliveryUnitID,
                    kind: "delivery_failure",
                    reasonCode: "acceptance_failed",
                    statementHash: await sha256Digest(statement),
                  });
                  operation.current = undefined;
                },
                "案件已创建。当前仅为软锁，链上确认前不会显示已冻结。",
              );
            }}
          >
            <Field id="dispute-task" label="任务 ID">
              <input
                id="dispute-task"
                className="form-input"
                required
                value={taskID}
                onChange={(e) => setTaskID(e.target.value)}
              />
            </Field>
            <Field id="dispute-unit" label="交付单元 ID">
              <input
                id="dispute-unit"
                className="form-input"
                required
                value={deliveryUnitID}
                onChange={(e) => setDeliveryUnitID(e.target.value)}
              />
            </Field>
            <Field id="dispute-statement" label="主张说明" wide>
              <textarea
                id="dispute-statement"
                className="form-input min-h-24"
                required
                value={statement}
                onChange={(e) => setStatement(e.target.value)}
              />
            </Field>
            <CtaButton
              type="submit"
              busy={busy === "open"}
              disabled={Boolean(busy)}
            >
              创建软锁案件
            </CtaButton>
          </form>
        </Panel>
      </>
    );

  const pendingTx =
      readPending(view.case.id) || view.case.freezeTransactionHash || "",
    frozen =
      Boolean(view.case.frozenAt) &&
      !["soft_lock_pending", "orphaned"].includes(view.case.state);
  return (
    <>
      <PageHeader
        title={side === "publisher" ? "争议处理" : "Agent 争议处理"}
        subtitle="权威案件、链上冻结、WORM 证据与最终责任"
        actions={
          <GhostButton
            icon={RefreshCw}
            disabled={Boolean(busy)}
            onClick={() => void load()}
          >
            刷新
          </GhostButton>
        }
      />
      {error ? (
        <div
          role="alert"
          className="rounded-xl border border-rose-300/30 bg-rose-300/10 p-3 text-[13px] text-rose-100"
        >
          {error}
        </div>
      ) : null}
      {notice ? (
        <div
          role="status"
          className="rounded-xl border border-cyan-300/30 bg-cyan-300/10 p-3 text-[13px] text-cyan-100"
        >
          {notice}
        </div>
      ) : null}
      {items.length > 1 ? (
        <div className="flex flex-wrap gap-2" aria-label="争议案件列表">
          {items.map((item) => (
            <button
              type="button"
              key={item.case.id}
              aria-pressed={item.case.id === view.case.id}
              onClick={() => setSelected(item.case.id)}
              className="rounded-lg border border-[var(--ap-border)] px-3 py-2 text-[12px]"
            >
              {short(item.case.id)} · {labels[item.case.state]}
            </button>
          ))}
        </div>
      ) : null}
      <div className="grid gap-6 xl:grid-cols-[1.35fr_.8fr]">
        <div className="space-y-6">
          <Panel className="p-5">
            <SectionTitle
              right={
                <Pill dot tone={frozen ? "red" : "amber"}>
                  {labels[view.case.state] ?? view.case.state}
                </Pill>
              }
            >
              案件时间线
            </SectionTitle>
            <ol aria-label="争议案件时间线" className="space-y-3">
              <Timeline
                label="案件与主张已登记"
                value={view.case.softLockedAt}
              />
              <Timeline
                label="冻结交易已提交"
                value={view.case.freezeSubmittedAt}
              />
              <Timeline
                label="canonical 冻结已确认"
                value={view.case.frozenAt}
              />
              <Timeline label="最终结果" value={view.case.finalizedAt} />
            </ol>
          </Panel>
          <Panel className="p-5">
            <SectionTitle
              right={<Pill tone="cyan">{view.case.claims.length} 项</Pill>}
            >
              主张与反请求
            </SectionTitle>
            <div className="space-y-2">
              {view.case.claims.map((claim) => (
                <div
                  key={claim.id}
                  className="rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"
                >
                  <b>
                    {claim.side === "publisher" ? "发布者" : "Agent"} ·{" "}
                    {claim.kind}
                  </b>
                  <div className="mt-1 text-[var(--ap-muted)]">
                    {claim.reasonCode} · {short(claim.statementHash)}
                  </div>
                </div>
              ))}
            </div>
            <form
              className="mt-4 flex gap-2"
              onSubmit={(event) => {
                event.preventDefault();
                void act(
                  "claim",
                  async () =>
                    addDisputeClaim(view.case.id, crypto.randomUUID(), {
                      kind: "counterclaim",
                      reasonCode: "independent_claim",
                      statementHash: await sha256Digest(claimText),
                    }),
                  "独立主张已追加，不会被对方先行申请阻断。",
                );
              }}
            >
              <label className="sr-only" htmlFor="counterclaim">
                反请求说明
              </label>
              <input
                id="counterclaim"
                className="form-input"
                required
                value={claimText}
                onChange={(e) => setClaimText(e.target.value)}
                placeholder="补充独立主张"
              />
              <CtaButton
                type="submit"
                disabled={Boolean(busy) || !claimText.trim()}
              >
                追加
              </CtaButton>
            </form>
          </Panel>
          <Panel className="p-5">
            <SectionTitle right={<Pill tone="violet">加密只追加</Pill>}>
              证据清单
            </SectionTitle>
            {view.case.evidence.map((evidence) => (
              <article
                key={evidence.id}
                className="mb-2 flex items-start justify-between gap-3 rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"
              >
                <span className="flex min-w-0 gap-2">
                  <FileLock2 size={15} />
                  <span>
                    <b>{evidence.category}</b>
                    <span className="block break-all text-[var(--ap-muted)]">
                      {evidence.objectKey} · WORM {evidence.objectVersionId}
                    </span>
                  </span>
                </span>
                <GhostButton
                  disabled={Boolean(busy)}
                  onClick={() =>
                    void act(
                      `access:${evidence.id}`,
                      () =>
                        requestEvidenceAccess(
                          view.case.id,
                          evidence.id,
                          "case_review",
                        ),
                      "已签发 5 分钟限域访问授权。",
                    )
                  }
                >
                  限时访问
                </GhostButton>
              </article>
            ))}
            <form
              className="mt-4 grid gap-3 sm:grid-cols-2"
              onSubmit={(event) => {
                event.preventDefault();
                void act(
                  "evidence",
                  () =>
                    appendDisputeEvidence(view.case.id, crypto.randomUUID(), {
                      claimId:
                        view.case.claims.find((claim) => claim.side === side)
                          ?.id ?? view.case.claims[0].id,
                      category,
                      objectKey,
                      ciphertextDigest,
                      envelopeKeyReference: keyReference,
                      objectVersionId: objectVersion,
                      retentionMode: "COMPLIANCE",
                      retainUntil: new Date(
                        Date.now() + 365 * 86400000,
                      ).toISOString(),
                    }),
                  "加密证据元数据已追加；原记录不可修改或删除。",
                );
              }}
            >
              <Field id="evidence-category" label="证据类别">
                <select
                  id="evidence-category"
                  className="form-input"
                  value={category}
                  onChange={(e) => setCategory(e.target.value)}
                >
                  {categories.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
              </Field>
              <Field id="evidence-object" label="WORM 对象键">
                <input
                  id="evidence-object"
                  className="form-input"
                  required
                  value={objectKey}
                  onChange={(e) => setObjectKey(e.target.value)}
                />
              </Field>
              <Field id="evidence-digest" label="密文 SHA-256">
                <input
                  id="evidence-digest"
                  className="form-input"
                  required
                  pattern="sha256:[0-9a-f]{64}"
                  value={ciphertextDigest}
                  onChange={(e) => setCiphertextDigest(e.target.value)}
                />
              </Field>
              <Field id="evidence-version" label="对象版本">
                <input
                  id="evidence-version"
                  className="form-input"
                  required
                  value={objectVersion}
                  onChange={(e) => setObjectVersion(e.target.value)}
                />
              </Field>
              <Field id="evidence-key-ref" label="信封密钥引用" wide>
                <input
                  id="evidence-key-ref"
                  className="form-input"
                  required
                  value={keyReference}
                  onChange={(e) => setKeyReference(e.target.value)}
                  placeholder="kms:key/case-id（不提交明文密钥）"
                />
              </Field>
              <CtaButton type="submit" disabled={!frozen || Boolean(busy)}>
                追加证据
              </CtaButton>
            </form>
          </Panel>
        </div>
        <div className="space-y-6">
          <Panel strong className="p-5">
            <div
              className={`flex items-center gap-2 text-[13px] ${frozen ? "text-rose-200" : "text-amber-200"}`}
            >
              {frozen ? <Snowflake size={16} /> : <Clock3 size={16} />}{" "}
              {frozen ? "链上资金已确认冻结" : "仅软锁，链上尚未确认"}
            </div>
            <div className="mt-3 text-[26px] text-white">
              {view.case.frozenAmount}{" "}
              <span className="text-[12px] text-[var(--ap-muted)]">
                {view.case.asset}
              </span>
            </div>
            {!frozen ? (
              <InfoNote tone="amber">
                <span role="status">
                  交易提交不等于冻结；只有 canonical DisputeFrozen
                  才进入冻结状态。
                </span>
              </InfoNote>
            ) : (
              <InfoNote tone="green">
                <span className="inline-flex gap-2">
                  <ShieldCheck size={14} />
                  冻结根 {short(view.case.freezeRoot)}
                </span>
              </InfoNote>
            )}
            <CtaButton
              full
              className="mt-4"
              busy={busy === "freeze"}
              disabled={Boolean(busy) || frozen}
              onClick={() =>
                void act(
                  "freeze",
                  async () => {
                    if (!pendingTx) {
                      const provider = (
                        window as typeof window & { ethereum?: WalletProvider }
                      ).ethereum;
                      if (!provider)
                        throw new Error("未检测到以太坊兼容钱包。");
                      const tx = await submitDisputeFreezeTransaction(
                        provider,
                        view,
                      );
                      rememberPending(view.case.id, tx);
                      await recordDisputeFreeze(
                        view.case.id,
                        `${view.case.id}:freeze-submit:${tx}`,
                        tx,
                      );
                    } else {
                      await reconcileDisputeFreeze(view.case.id, pendingTx);
                      forgetPending(view.case.id);
                    }
                  },
                  pendingTx
                    ? "canonical 冻结已确认。"
                    : "冻结交易已提交，等待 canonical 投影。",
                )
              }
            >
              {pendingTx ? "检查冻结确认" : "钱包提交冻结"}
            </CtaButton>
          </Panel>
          <Panel className="p-5">
            <SectionTitle>案件截止时间</SectionTitle>
            <dl className="space-y-3">
              <Datum
                label="申请截止"
                value={date(view.context.disputeDeadline)}
              />
              <Datum
                label="举证截止"
                value={date(view.case.evidenceDeadline)}
              />
              <Datum
                label="初裁截止"
                value={date(view.case.decisionDeadline)}
              />
              <Datum label="复核截止" value={date(view.case.reviewDeadline)} />
            </dl>
          </Panel>
          <Panel className="p-5">
            <SectionTitle>双方签名和解</SectionTitle>
            <form
              className="space-y-3"
              onSubmit={(event) => {
                event.preventDefault();
                void act(
                  "settlement",
                  async () => {
                    const payload = await settlementPayload();
                    if (payload.message !== settlementMessage)
                      throw new Error(
                        "协议或判付比例已变化，请重新生成签名消息。",
                      );
                    await settleDispute(view.case.id, crypto.randomUUID(), {
                      publisherBps: settlementBPS,
                      reasonCode: "signed_settlement",
                      evidenceRoot: payload.evidenceRoot,
                      agreementHash: payload.agreementHash,
                      publisherSignature,
                      agentSignature,
                    });
                  },
                  "双方签名和解已验证并最终化；原始签名不会持久化。",
                );
              }}
            >
              <label
                className="block text-[12px] text-[var(--ap-muted)]"
                htmlFor="settlement-bps"
              >
                发布者退款基点（0–10000）
              </label>
              <input
                id="settlement-bps"
                className="form-input"
                type="number"
                min={0}
                max={10000}
                value={settlementBPS}
                onChange={(event) =>
                  setSettlementBPS(Number(event.target.value))
                }
              />
              <label
                className="block text-[12px] text-[var(--ap-muted)]"
                htmlFor="settlement-agreement"
              >
                和解协议正文
              </label>
              <textarea
                id="settlement-agreement"
                className="form-input min-h-20"
                required
                value={agreement}
                onChange={(event) => setAgreement(event.target.value)}
              />
              <GhostButton
                disabled={!agreement || Boolean(busy)}
                onClick={() =>
                  void settlementPayload().then((payload) =>
                    setSettlementMessage(payload.message),
                  )
                }
              >
                生成双方签名消息
              </GhostButton>
              {settlementMessage ? (
                <textarea
                  aria-label="待签名和解消息"
                  className="form-input min-h-32 font-mono text-[10px]"
                  readOnly
                  value={settlementMessage}
                />
              ) : null}
              <label
                className="block text-[12px] text-[var(--ap-muted)]"
                htmlFor="publisher-settlement-signature"
              >
                发布者签名
              </label>
              <input
                id="publisher-settlement-signature"
                className="form-input"
                required
                pattern="0x[0-9a-fA-F]{130}"
                value={publisherSignature}
                onChange={(event) => setPublisherSignature(event.target.value)}
              />
              <label
                className="block text-[12px] text-[var(--ap-muted)]"
                htmlFor="agent-settlement-signature"
              >
                Agent 控制器签名
              </label>
              <input
                id="agent-settlement-signature"
                className="form-input"
                required
                pattern="0x[0-9a-fA-F]{130}"
                value={agentSignature}
                onChange={(event) => setAgentSignature(event.target.value)}
              />
              <CtaButton
                type="submit"
                full
                disabled={
                  !settlementMessage ||
                  Boolean(busy) ||
                  view.case.state === "final"
                }
                busy={busy === "settlement"}
              >
                验证并提交和解
              </CtaButton>
            </form>
          </Panel>
          <Panel className="p-5">
            <SectionTitle>最终责任</SectionTitle>
            {view.case.decisions.length ? (
              view.case.decisions.map((decision) => (
                <div
                  key={decision.id}
                  className="mb-2 rounded-xl border border-[var(--ap-border)] p-3 text-[12px]"
                >
                  <b>
                    {decision.kind} · 发布者 {decision.publisherBps / 100}%
                  </b>
                  <div className="text-[var(--ap-muted)]">
                    {decision.reasonCode} · {decision.decidedBy}
                  </div>
                </div>
              ))
            ) : (
              <p className="text-[12px] text-[var(--ap-muted)]">
                申请本身不会改变信誉；仅最终责任结果生效。
              </p>
            )}
          </Panel>
        </div>
      </div>
    </>
  );
}
function Field({
  id,
  label,
  wide = false,
  children,
}: {
  id: string;
  label: string;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={wide ? "sm:col-span-2" : ""}>
      <label
        htmlFor={id}
        className="mb-1.5 block text-[12px] text-[var(--ap-muted)]"
      >
        {label}
      </label>
      {children}
    </div>
  );
}
function Timeline({ label, value }: { label: string; value?: string }) {
  return (
    <li className="flex gap-3 text-[12px]">
      <span>
        {value ? (
          <CheckCircle2 size={16} className="text-cyan-300" />
        ) : (
          <AlertTriangle size={16} className="text-[var(--ap-muted)]" />
        )}
      </span>
      <span>
        <b>{label}</b>
        <span className="block text-[var(--ap-muted)]">{date(value)}</span>
      </span>
    </li>
  );
}
function Datum({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[10px] text-[var(--ap-muted)]">{label}</dt>
      <dd className="mt-1 text-[12px]">{value}</dd>
    </div>
  );
}
function message(value: unknown) {
  return value instanceof Error ? value.message : "争议操作失败。";
}
function date(value?: string) {
  if (!value) return "尚未开始";
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime())
    ? parsed.toLocaleString("zh-CN")
    : "—";
}
function short(value?: string) {
  if (!value) return "—";
  return value.length > 22 ? `${value.slice(0, 12)}…${value.slice(-7)}` : value;
}
function storageKey(caseID: string) {
  return `dispute-freeze:${caseID}`;
}
function readPending(caseID: string) {
  try {
    return typeof window === "undefined"
      ? ""
      : (sessionStorage.getItem(storageKey(caseID)) ?? "");
  } catch {
    return "";
  }
}
function rememberPending(caseID: string, tx: string) {
  try {
    sessionStorage.setItem(storageKey(caseID), tx);
  } catch {
    /* canonical projection remains authoritative */
  }
}
function forgetPending(caseID: string) {
  try {
    sessionStorage.removeItem(storageKey(caseID));
  } catch {
    /* expires with session */
  }
}
