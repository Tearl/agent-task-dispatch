import {
  ArrowLeft,
  ArrowRight,
  Boxes,
  CheckCircle2,
  Circle,
  Cpu,
  Info,
  KeyRound,
  Loader2,
  PartyPopper,
  Plus,
  ShieldCheck,
  Webhook,
  X,
} from "lucide-react";
import { useRef, useState } from "react";
import { useNavigate } from "react-router";
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
import { onboardAgent, validateAgentOnboardingInput, type AgentOnboardingInput } from "../../lib/platform-api";
import { useSession } from "../../lib/session";

const CATEGORIES = ["数据分析", "翻译", "图像生成", "代码开发", "市场研究", "智能审计"];
const AUTH_TYPES = ["API Key", "Bearer Token", "OAuth 2.0"];

const STEPS = [
  { id: 0, label: "基本信息", icon: Cpu },
  { id: 1, label: "运行容量", icon: Boxes },
  { id: 2, label: "凭证安全", icon: KeyRound },
  { id: 3, label: "协议校验与健康检查", icon: ShieldCheck },
];

type CheckState = "idle" | "running" | "pass" | "fail";

export default function OnboardAgent() {
  const navigate = useNavigate();
  const { address } = useSession();
  const [step, setStep] = useState(0);
  const [done, setDone] = useState(false);
  const [name, setName] = useState("");
  const [category, setCategory] = useState("数据分析");
  const [tagline, setTagline] = useState("");
  const [endpointUrl, setEndpointUrl] = useState("");
  const [capabilities, setCapabilities] = useState<string[]>(["结构化输出"]);
  const [capabilityInput, setCapabilityInput] = useState("");
  const [auth, setAuth] = useState("API Key");
  const [secret, setSecret] = useState("");
  const [maxConcurrency, setMaxConcurrency] = useState("20");
  const [overviewPrice, setOverviewPrice] = useState("100");
  const [formalPrice, setFormalPrice] = useState("500");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const operationId = useRef<string | undefined>(undefined);
  const submitInFlight = useRef(false);
  const [checks, setChecks] = useState<{ label: string; state: CheckState; note: string }[]>([
    { label: "创建 Agent 聚合", state: "idle", note: "由 Engine 校验所有者与基本资料" },
    { label: "加密保存调用凭证", state: "idle", note: "明文仅用于本次受保护写入" },
    { label: "发布价格与健康状态", state: "idle", note: "价格约束与健康新鲜度由 Engine 校验" },
    { label: "上线资格与状态迁移", state: "idle", note: "仅执行 Engine 返回的 allowed 操作" },
  ]);

  const addCapability = () => {
    const value = capabilityInput.trim();
    if (value && !capabilities.includes(value)) setCapabilities((items) => [...items, value]);
    setCapabilityInput("");
  };

  const canNext =
    (step === 0 && Boolean(name.trim()) && Boolean(tagline.trim())) ||
    (step === 1 && isProtocolBaseUrl(endpointUrl) && Number.isInteger(Number(maxConcurrency)) && Number(maxConcurrency) > 0) ||
    (step === 2 && secret.trim().length > 0 && /^\d+$/.test(overviewPrice) && /^\d+$/.test(formalPrice)) ||
    step === 3;

  const runChecks = async () => {
    if (submitInFlight.current || allPassed) return;
    submitInFlight.current = true;
    setSubmitting(true);
    setSubmitError(null);
    setChecks((items) => items.map((check) => ({ ...check, state: "running" })));
    try {
      const input: AgentOnboardingInput = {
        operationId: operationId.current ?? crypto.randomUUID(),
        name: name.trim(), category, tagline: tagline.trim(), endpointUrl: endpointUrl.trim(), capabilities,
        controllerAddress: address, maxConcurrency: Number(maxConcurrency),
        credentialType: auth === "Bearer Token" ? "bearer_token" : auth === "OAuth 2.0" ? "oauth_client_secret" : "api_key",
        secret, overviewPrice, formalPrice,
      };
      validateAgentOnboardingInput(input);
      operationId.current = input.operationId;
      await onboardAgent(input);
      setSecret("");
      setChecks((items) => items.map((check) => ({ ...check, state: "pass" })));
      toast.success("Engine 已确认全部上线条件，Agent 已激活");
    } catch (error) {
      const message = error instanceof Error ? error.message : "Agent 接入失败，请重试。";
      setSubmitError(message);
      setChecks((items) => items.map((check) => ({ ...check, state: "fail" })));
    } finally {
      submitInFlight.current = false;
      setSubmitting(false);
    }
  };

  const resetAttempt = () => {
    submitInFlight.current = false;
    operationId.current = undefined;
    setSubmitError(null);
    setChecks((items) => items.map((check) => ({ ...check, state: "idle" })));
    setStep(0);
  };

  const allPassed = checks.every((check) => check.state === "pass");
  const attemptLocked = Boolean(operationId.current);

  const finish = () => {
    setDone(true);
    toast.success(`${name || "新 Agent"} 接入成功`);
  };

  if (done) {
    return (
      <Page>
        <Panel strong className="mx-auto max-w-[560px] p-7 text-center sm:p-10">
          <div className="ap-ring-glow mx-auto grid h-16 w-16 place-items-center rounded-2xl bg-gradient-to-br from-[#8b5cf6] to-[#22d3ee] text-[#04121c]">
            <PartyPopper size={26} />
          </div>
          <h2 className="mt-5 text-[22px] text-white">接入成功</h2>
          <p className="mt-2 text-[14px] text-[var(--ap-muted)]">
            {name || "新 Agent"} 已通过 Engine 上线资格校验，现已激活。
          </p>
          <div className="mt-6 flex flex-col items-center gap-2 text-[13px] text-[var(--ap-text-2)]">
            <span className="inline-flex items-center gap-2">
              <Cpu size={15} className="text-[var(--ap-violet)]" />
              {name || "新 Agent"} · {category}
            </span>
            <Pill tone="green" dot>
              在线 · 100%
            </Pill>
          </div>
          <div className="mt-7 flex flex-wrap justify-center gap-2">
            <CtaButton icon={Boxes} onClick={() => navigate("/agent/integration")}>
              返回 Agent 管理
            </CtaButton>
            <GhostButton icon={Webhook} onClick={() => navigate("/agent/developer")}>
              配置 Webhook
            </GhostButton>
          </div>
        </Panel>
      </Page>
    );
  }

  return (
    <Page>
      <PageHeader
        title="接入新 Agent"
        subtitle="填写基本信息、运行容量、价格与凭证，并由 Engine 确认上线资格"
        actions={
          <GhostButton icon={ArrowLeft} onClick={() => navigate("/agent/integration")}>
            取消
          </GhostButton>
        }
      />

      <Panel className="p-4">
        <div aria-label="Agent 接入进度" className="flex flex-wrap items-center justify-between gap-2">
          {STEPS.map((item, index) => {
            const active = step === index;
            const passed = step > index;
            return (
              <div key={item.id} className="flex min-w-[160px] flex-1 items-center gap-2">
                <span
                  aria-current={active ? "step" : undefined}
                  className="grid h-9 w-9 shrink-0 place-items-center rounded-xl"
                  style={{
                    background: active
                      ? "var(--ap-violet-soft)"
                      : passed
                        ? "rgba(52,211,153,.15)"
                        : "rgba(114,134,166,.12)",
                    color: active ? "#c4b5fd" : passed ? "#6ee7b7" : "var(--ap-muted)",
                  }}
                >
                  {passed ? <CheckCircle2 size={17} /> : <item.icon size={17} />}
                </span>
                <div className="min-w-0">
                  <div className="text-[11px] text-[var(--ap-muted)]">步骤 {index + 1}</div>
                  <div className="truncate text-[13px]" style={{ color: active ? "#e8f0ff" : "var(--ap-text-2)" }}>
                    {item.label}
                  </div>
                </div>
                {index < STEPS.length - 1 ? (
                  <span
                    className="hidden h-px flex-1 lg:block"
                    style={{ background: passed ? "#34d399" : "var(--ap-border)" }}
                  />
                ) : null}
              </div>
            );
          })}
        </div>
      </Panel>

      <div className="grid gap-6 lg:grid-cols-[1.6fr_1fr]">
        <Panel className="p-5 sm:p-6">
          {step === 0 ? (
            <div className="space-y-5">
              <SectionTitle>基本信息</SectionTitle>
              <div>
                <label htmlFor="agent-name" className="text-[13px] text-[var(--ap-muted)]">
                  Agent 名称
                </label>
                <input
                  id="agent-name"
                  disabled={submitting || attemptLocked}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="例如：DataForge"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div>
                <div className="text-[13px] font-medium text-[var(--ap-muted)]">能力分类</div>
                <div role="group" aria-label="能力分类" className="mt-2 flex flex-wrap gap-2">
                  {CATEGORIES.map((item) => (
                    <button
                      key={item}
                      type="button"
                      aria-pressed={category === item}
                      disabled={submitting || attemptLocked}
                      onClick={() => setCategory(item)}
                      className="rounded-lg border px-3 py-2 text-[13px] transition-colors"
                      style={{
                        borderColor: category === item ? "var(--ap-border-strong)" : "var(--ap-border)",
                        background: category === item ? "var(--ap-violet-soft)" : "transparent",
                        color: category === item ? "#c4b5fd" : "var(--ap-text-2)",
                      }}
                    >
                      {item}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label htmlFor="agent-tagline" className="text-[13px] text-[var(--ap-muted)]">
                  一句话简介
                </label>
                <input
                  id="agent-tagline"
                  disabled={submitting || attemptLocked}
                  value={tagline}
                  onChange={(event) => setTagline(event.target.value)}
                  placeholder="描述该 Agent 擅长的任务"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div>
                <div className="text-[13px] font-medium text-[var(--ap-muted)]">能力标签</div>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  {capabilities.map((item) => (
                    <span
                      key={item}
                      className="inline-flex items-center gap-1.5 rounded-full border border-[var(--ap-border)] bg-[rgba(139,92,246,0.12)] px-3 py-1 text-[12px] text-[#c4b5fd]"
                    >
                      {item}
                      <button
                        type="button"
                        aria-label={`移除 ${item}`}
                        disabled={submitting || attemptLocked}
                        onClick={() => setCapabilities((items) => items.filter((capability) => capability !== item))}
                      >
                        <X size={12} />
                      </button>
                    </span>
                  ))}
                  <div className="inline-flex items-center gap-1 rounded-full border border-dashed border-[var(--ap-border-strong)] px-2 py-1">
                    <input
                      aria-label="添加能力标签"
                      disabled={submitting || attemptLocked}
                      value={capabilityInput}
                      onChange={(event) => setCapabilityInput(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          addCapability();
                        }
                      }}
                      placeholder="添加标签"
                      className="w-24 bg-transparent text-[12px] text-white outline-none placeholder:text-[var(--ap-muted)]"
                    />
                    <button type="button" aria-label="确认添加标签" disabled={submitting || attemptLocked} onClick={addCapability} className="text-[var(--ap-cyan)]">
                      <Plus size={14} />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ) : null}

          {step === 1 ? (
            <div className="space-y-5">
              <SectionTitle>运行容量</SectionTitle>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="min-w-0">
                  <label htmlFor="agent-endpoint" className="text-[13px] text-[var(--ap-muted)]">协议基础 URL</label>
                  <input
                    id="agent-endpoint"
                    type="url"
                    disabled={submitting || attemptLocked}
                    value={endpointUrl}
                    onChange={(event) => setEndpointUrl(event.target.value)}
                    placeholder="https://agent.example"
                    className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                  />
                </div>
                <div className="min-w-0">
                  <label htmlFor="agent-concurrency" className="text-[13px] text-[var(--ap-muted)]">
                    并发上限
                  </label>
                  <input
                    id="agent-concurrency"
                    inputMode="numeric"
                    disabled={submitting || attemptLocked}
                    value={maxConcurrency}
                    onChange={(event) => setMaxConcurrency(event.target.value.replace(/\D/g, ""))}
                    className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                  />
                </div>
              </div>
              <InfoNote tone="cyan">
                <span className="inline-flex items-center gap-1.5">
                  <Info size={14} /> Engine 将请求该 HTTPS 基础地址的 /health 并校验协议版本；浏览器不能自行声明健康。
                </span>
              </InfoNote>
            </div>
          ) : null}

          {step === 2 ? (
            <div className="space-y-5">
              <SectionTitle>凭证安全</SectionTitle>
              <div>
                <div className="text-[13px] font-medium text-[var(--ap-muted)]">鉴权方式</div>
                <div role="group" aria-label="鉴权方式" className="mt-2 flex flex-wrap gap-2">
                  {AUTH_TYPES.map((item) => (
                    <button
                      key={item}
                      type="button"
                      aria-pressed={auth === item}
                      disabled={submitting || attemptLocked}
                      onClick={() => setAuth(item)}
                      className="rounded-lg border px-3 py-2 text-[13px] transition-colors"
                      style={{
                        borderColor: auth === item ? "var(--ap-border-strong)" : "var(--ap-border)",
                        background: auth === item ? "var(--ap-violet-soft)" : "transparent",
                        color: auth === item ? "#c4b5fd" : "var(--ap-text-2)",
                      }}
                    >
                      {item}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <label htmlFor="agent-secret" className="text-[13px] text-[var(--ap-muted)]">
                  {auth === "OAuth 2.0" ? "Client Secret" : auth === "Bearer Token" ? "Bearer Token" : "API Key"}
                </label>
                <input
                  id="agent-secret"
                  type="password"
                  autoComplete="off"
                  disabled={submitting || attemptLocked}
                  value={secret}
                  onChange={(event) => setSecret(event.target.value)}
                  placeholder="粘贴凭证，提交后将加密存储"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 font-mono text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label htmlFor="agent-overview-price" className="text-[13px] text-[var(--ap-muted)]">概览价格 (USDC)</label>
                  <input id="agent-overview-price" inputMode="numeric" disabled={submitting || attemptLocked} value={overviewPrice} onChange={(event) => setOverviewPrice(event.target.value.replace(/\D/g, ""))} className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]" />
                </div>
                <div>
                  <label htmlFor="agent-formal-price" className="text-[13px] text-[var(--ap-muted)]">正式套餐总价 (USDC)</label>
                  <input id="agent-formal-price" inputMode="numeric" disabled={submitting || attemptLocked} value={formalPrice} onChange={(event) => setFormalPrice(event.target.value.replace(/\D/g, ""))} className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]" />
                </div>
              </div>
              <InfoNote tone="green">
                <span className="inline-flex items-center gap-1.5">
                  <ShieldCheck size={14} /> 凭证以加密形式存储，平台与管理员均无法查看明文，仅在调用时脱敏使用。
                </span>
              </InfoNote>
            </div>
          ) : null}

          {step === 3 ? (
            <div className="space-y-5">
              <SectionTitle
                right={
                  <GhostButton icon={ShieldCheck} onClick={runChecks} active={allPassed} disabled={submitting || allPassed}>
                    {submitting ? "提交与校验中…" : allPassed ? "Engine 已确认" : "开始接入"}
                  </GhostButton>
                }
              >
                协议校验与健康检查
              </SectionTitle>
              <div aria-live="polite" aria-busy={submitting} className="space-y-3">
                {checks.map((check) => (
                  <div
                    key={check.label}
                    className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3"
                  >
                    <div className="flex items-center gap-3">
                      {check.state === "pass" ? <CheckCircle2 size={18} className="text-[var(--ap-success)]" /> : null}
                      {check.state === "running" ? <Loader2 size={18} className="animate-spin text-[var(--ap-cyan)]" /> : null}
                      {check.state === "fail" ? <X size={18} className="text-[var(--ap-danger)]" /> : null}
                      {check.state === "idle" ? <Circle size={18} className="text-[var(--ap-muted)]" /> : null}
                      <div>
                        <div className="text-[14px] text-[var(--ap-text)]">{check.label}</div>
                        <div className="text-[12px] text-[var(--ap-muted)]">{check.note}</div>
                      </div>
                    </div>
                    <Pill
                      tone={
                        check.state === "pass"
                          ? "green"
                          : check.state === "running"
                            ? "cyan"
                            : check.state === "fail"
                              ? "red"
                              : "gray"
                      }
                    >
                      {check.state === "pass"
                        ? "通过"
                        : check.state === "running"
                          ? "进行中"
                          : check.state === "fail"
                            ? "失败"
                            : "待执行"}
                    </Pill>
                  </div>
                ))}
              </div>
              {submitError ? <div className="space-y-3"><InfoNote tone="red"><span role="alert">{submitError}</span></InfoNote>{operationId.current ? <><p className="text-[12px] text-[var(--ap-muted)]">为保证幂等重试，当前输入已锁定。可原样重试，或放弃本次操作后修改。</p><GhostButton onClick={resetAttempt}>放弃本次操作并重新开始</GhostButton></> : null}</div> : null}
            </div>
          ) : null}

          <div className="mt-7 flex items-center justify-between gap-3 border-t border-[var(--ap-border)] pt-5">
            <GhostButton
              icon={ArrowLeft}
              onClick={() => setStep((value) => Math.max(0, value - 1))}
              disabled={step === 0 || submitting || Boolean(operationId.current)}
              className={step === 0 ? "opacity-40" : ""}
            >
              上一步
            </GhostButton>
            {step < 3 ? (
              <CtaButton
                onClick={() => {
                  if (canNext) setStep((value) => value + 1);
                }}
                disabled={!canNext}
                className={!canNext ? "opacity-50" : ""}
              >
                下一步 <ArrowRight size={16} />
              </CtaButton>
            ) : (
              <CtaButton
                icon={CheckCircle2}
                onClick={finish}
                disabled={!allPassed || submitting}
                className={!allPassed ? "opacity-50" : ""}
              >
                完成接入并上线
              </CtaButton>
            )}
          </div>
        </Panel>

        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle>接入预览</SectionTitle>
            <div className="flex items-center gap-3">
              <span className="grid h-12 w-12 place-items-center rounded-xl bg-gradient-to-br from-[#8b5cf6] to-[#22d3ee] text-[18px] text-[#04121c]">
                {name ? name[0]?.toUpperCase() : <Cpu size={20} />}
              </span>
              <div className="min-w-0">
                <div className="truncate text-[15px] text-[var(--ap-text)]">{name || "未命名 Agent"}</div>
                <Pill tone="violet">{category}</Pill>
              </div>
            </div>
            <p className="mt-3 text-[13px] text-[var(--ap-text-2)]">{tagline || "暂无简介"}</p>
            <div className="mt-4 space-y-2 text-[13px]">
              <Row label="并发上限" value={maxConcurrency || "—"} />
              <Row label="鉴权方式" value={auth} />
              <Row label="能力标签" value={capabilities.join("、") || "—"} />
              <Row label="履约保证金" value="0 USDC" tag="零履约金" />
            </div>
          </Panel>

          <InfoNote tone="cyan">
            <span className="inline-flex items-center gap-1.5">
              <Info size={14} /> 接入不缴纳任务级履约金；上线后由平台按匹配分推荐，无需抢单。
            </span>
          </InfoNote>
        </div>
      </div>
    </Page>
  );
}

function isProtocolBaseUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password && !url.search && !url.hash && (url.pathname === "" || url.pathname === "/");
  } catch {
    return false;
  }
}

function Row({ label, value, tag }: { label: string; value: string; tag?: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="shrink-0 text-[var(--ap-muted)]">{label}</span>
      <span className="flex min-w-0 items-center gap-2">
        {tag ? <Pill tone="green">{tag}</Pill> : null}
        <span className="truncate text-[var(--ap-text-2)]">{value}</span>
      </span>
    </div>
  );
}
