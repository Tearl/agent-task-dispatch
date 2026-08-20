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
import { useState } from "react";
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

const CATEGORIES = ["数据分析", "翻译", "图像生成", "代码开发", "市场研究", "智能审计"];
const AUTH_TYPES = ["API Key", "Bearer Token", "OAuth 2.0"];

const STEPS = [
  { id: 0, label: "基本信息", icon: Cpu },
  { id: 1, label: "调用配置", icon: Boxes },
  { id: 2, label: "凭证安全", icon: KeyRound },
  { id: 3, label: "协议校验与健康检查", icon: ShieldCheck },
];

type CheckState = "idle" | "running" | "pass" | "fail";

export default function OnboardAgent() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [done, setDone] = useState(false);
  const [name, setName] = useState("");
  const [category, setCategory] = useState("数据分析");
  const [tagline, setTagline] = useState("");
  const [endpoint, setEndpoint] = useState("https://");
  const [webhook, setWebhook] = useState("");
  const [capabilities, setCapabilities] = useState<string[]>(["结构化输出"]);
  const [capabilityInput, setCapabilityInput] = useState("");
  const [auth, setAuth] = useState("API Key");
  const [secret, setSecret] = useState("");
  const [checks, setChecks] = useState<{ label: string; state: CheckState; note: string }[]>([
    { label: "OpenAPI 3.1 协议校验", state: "idle", note: "校验接口 Schema 与必填字段" },
    { label: "连通性与鉴权测试", state: "idle", note: "向端点发送带签名的探针请求" },
    { label: "健康检查（探活）", state: "idle", note: "连续 3 次心跳，统计 P95 延迟" },
    { label: "沙箱试运行", state: "idle", note: "提交一个样例任务验证输入输出" },
  ]);

  const addCapability = () => {
    const value = capabilityInput.trim();
    if (value && !capabilities.includes(value)) setCapabilities((items) => [...items, value]);
    setCapabilityInput("");
  };

  const canNext =
    (step === 0 && Boolean(name.trim()) && Boolean(tagline.trim())) ||
    (step === 1 && endpoint.length > 8) ||
    (step === 2 && secret.trim().length > 0) ||
    step === 3;

  const runChecks = async () => {
    for (let index = 0; index < checks.length; index += 1) {
      setChecks((items) =>
        items.map((check, checkIndex) => (checkIndex === index ? { ...check, state: "running" } : check)),
      );
      await new Promise((resolve) => setTimeout(resolve, 700));
      setChecks((items) =>
        items.map((check, checkIndex) => (checkIndex === index ? { ...check, state: "pass" } : check)),
      );
    }
    toast.success("全部校验通过，Agent 已可上线接单");
  };

  const allPassed = checks.every((check) => check.state === "pass");

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
            {name || "新 Agent"} 已通过协议校验与健康检查，现已上线，可被平台智能匹配。
          </p>
          <div className="mt-6 flex flex-col items-center gap-2 text-[13px] text-[var(--ap-text-2)]">
            <span className="inline-flex items-center gap-2">
              <Cpu size={15} className="text-[var(--ap-violet)]" />
              {name || "新 Agent"} · {category}
            </span>
            <span className="inline-flex items-center gap-2">
              <Boxes size={15} className="text-[var(--ap-cyan)]" />
              {endpoint}
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
        subtitle="填写基本信息、调用配置与凭证，通过协议校验和健康检查后即可上线"
        actions={
          <GhostButton icon={ArrowLeft} onClick={() => navigate("/agent/integration")}>
            取消
          </GhostButton>
        }
      />

      <Panel className="p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          {STEPS.map((item, index) => {
            const active = step === index;
            const passed = step > index;
            return (
              <div key={item.id} className="flex min-w-[160px] flex-1 items-center gap-2">
                <span
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
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="例如：DataForge"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div>
                <div className="text-[13px] font-medium text-[var(--ap-muted)]">能力分类</div>
                <div className="mt-2 flex flex-wrap gap-2">
                  {CATEGORIES.map((item) => (
                    <button
                      key={item}
                      type="button"
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
                        onClick={() => setCapabilities((items) => items.filter((capability) => capability !== item))}
                      >
                        <X size={12} />
                      </button>
                    </span>
                  ))}
                  <div className="inline-flex items-center gap-1 rounded-full border border-dashed border-[var(--ap-border-strong)] px-2 py-1">
                    <input
                      aria-label="添加能力标签"
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
                    <button type="button" aria-label="确认添加标签" onClick={addCapability} className="text-[var(--ap-cyan)]">
                      <Plus size={14} />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ) : null}

          {step === 1 ? (
            <div className="space-y-5">
              <SectionTitle>调用配置</SectionTitle>
              <div>
                <label htmlFor="agent-endpoint" className="text-[13px] text-[var(--ap-muted)]">
                  调用端点 (HTTPS)
                </label>
                <input
                  id="agent-endpoint"
                  value={endpoint}
                  onChange={(event) => setEndpoint(event.target.value)}
                  placeholder="https://api.your-agent.ai/v1/invoke"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 font-mono text-[13px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div>
                <label htmlFor="agent-webhook" className="text-[13px] text-[var(--ap-muted)]">
                  Webhook 回调地址（可选）
                </label>
                <input
                  id="agent-webhook"
                  value={webhook}
                  onChange={(event) => setWebhook(event.target.value)}
                  placeholder="https://your.app/webhook"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 font-mono text-[13px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
              </div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="min-w-0">
                  <div className="text-[13px] font-medium text-[var(--ap-muted)]">协议规范</div>
                  <div className="mt-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-[var(--ap-text-2)]">
                    OpenAPI 3.1
                  </div>
                </div>
                <div className="min-w-0">
                  <label htmlFor="agent-concurrency" className="text-[13px] text-[var(--ap-muted)]">
                    并发上限
                  </label>
                  <input
                    id="agent-concurrency"
                    defaultValue="20"
                    className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                  />
                </div>
              </div>
              <InfoNote tone="cyan">
                <span className="inline-flex items-center gap-1.5">
                  <Info size={14} /> 端点须支持 HTTPS 与签名验证，平台将以脱敏方式记录调用日志。
                </span>
              </InfoNote>
            </div>
          ) : null}

          {step === 2 ? (
            <div className="space-y-5">
              <SectionTitle>凭证安全</SectionTitle>
              <div>
                <div className="text-[13px] font-medium text-[var(--ap-muted)]">鉴权方式</div>
                <div className="mt-2 flex flex-wrap gap-2">
                  {AUTH_TYPES.map((item) => (
                    <button
                      key={item}
                      type="button"
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
                  value={secret}
                  onChange={(event) => setSecret(event.target.value)}
                  placeholder="粘贴凭证，提交后将加密存储"
                  className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 font-mono text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
                />
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
                  <GhostButton icon={ShieldCheck} onClick={runChecks} active={allPassed}>
                    {checks.some((check) => check.state === "running") ? "校验中…" : allPassed ? "重新校验" : "开始校验"}
                  </GhostButton>
                }
              >
                协议校验与健康检查
              </SectionTitle>
              <div className="space-y-3">
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
            </div>
          ) : null}

          <div className="mt-7 flex items-center justify-between gap-3 border-t border-[var(--ap-border)] pt-5">
            <GhostButton
              icon={ArrowLeft}
              onClick={() => setStep((value) => Math.max(0, value - 1))}
              className={step === 0 ? "pointer-events-none opacity-40" : ""}
            >
              上一步
            </GhostButton>
            {step < 3 ? (
              <CtaButton
                onClick={() => {
                  if (canNext) setStep((value) => value + 1);
                }}
                className={!canNext ? "pointer-events-none opacity-50" : ""}
              >
                下一步 <ArrowRight size={16} />
              </CtaButton>
            ) : (
              <CtaButton
                icon={CheckCircle2}
                onClick={finish}
                className={!allPassed ? "pointer-events-none opacity-50" : ""}
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
              <Row label="调用端点" value={endpoint.length > 8 ? endpoint : "未配置"} />
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
