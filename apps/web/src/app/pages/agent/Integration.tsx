import { Activity, Boxes, Cpu, Eye, EyeOff, PlusCircle, RefreshCw, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { Bar, CtaButton, GhostButton, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { MY_AGENTS } from "../../lib/mock";

export default function AgentIntegration() {
  const [reveal, setReveal] = useState(false);
  const navigate = useNavigate();

  return (
    <Page>
      <PageHeader
        title="Agent 管理与接入"
        subtitle="调用配置、凭证脱敏、健康检查与协议校验"
        actions={
          <CtaButton icon={PlusCircle} onClick={() => navigate("/agent/integration/new")}>
            接入新 Agent
          </CtaButton>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        {MY_AGENTS.map((agent) => (
          <Panel key={agent.id} hover className="p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className="grid h-10 w-10 place-items-center rounded-xl bg-[rgba(139,92,246,0.15)] text-[var(--ap-violet)]">
                  <Cpu size={18} />
                </span>
                <div>
                  <div className="text-[15px] text-[var(--ap-text)]">{agent.name}</div>
                  <div className="text-[12px] text-[var(--ap-muted)]">{agent.category}</div>
                </div>
              </div>
              <Pill tone={agent.status === "online" ? "green" : agent.status === "degraded" ? "amber" : "red"} dot>
                {agent.status === "online" ? "在线" : agent.status === "degraded" ? "降级" : "离线"}
              </Pill>
            </div>

            <div className="mt-4 space-y-3 text-[13px]">
              <div>
                <div className="flex items-center justify-between text-[var(--ap-muted)]">
                  <span className="inline-flex items-center gap-1.5">
                    <Activity size={13} /> 健康度
                  </span>
                  <span>{agent.health}%</span>
                </div>
                <div className="mt-1.5">
                  <Bar value={agent.health} tone={agent.health > 90 ? "#34d399" : agent.health > 0 ? "#fbbf24" : "#fb7185"} />
                </div>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="text-[var(--ap-muted)]">调用端点</span>
                <span className="max-w-[150px] truncate text-[var(--ap-text-2)]">{agent.endpoint}</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="inline-flex items-center gap-1.5 text-[var(--ap-muted)]">
                  <ShieldCheck size={13} /> 协议校验
                </span>
                <Pill tone={agent.protocol.includes("✓") ? "green" : "red"}>{agent.protocol}</Pill>
              </div>
            </div>

            <div className="mt-4 flex flex-wrap gap-2">
              <GhostButton
                icon={RefreshCw}
                className="flex-1"
                onClick={() => toast.success(`${agent.name} 健康检查已触发`)}
              >
                健康检查
              </GhostButton>
              <GhostButton className="flex-1">调用配置</GhostButton>
            </div>
          </Panel>
        ))}
      </div>

      <Panel className="p-6">
        <SectionTitle
          right={
            <Pill tone="cyan" dot>
              安全存储
            </Pill>
          }
        >
          凭证与密钥（脱敏展示）
        </SectionTitle>
        <div className="space-y-3">
          {[
            { name: "DataForge · API Key", val: "sk_live_9f2a...4c1d" },
            { name: "LinguaX · Bearer Token", val: "brt_2b8e...77af" },
          ].map((credential) => (
            <div
              key={credential.name}
              className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3"
            >
              <div>
                <div className="text-[14px] text-[var(--ap-text)]">{credential.name}</div>
                <div className="mt-0.5 font-mono text-[13px] text-[var(--ap-muted)]">
                  {reveal ? credential.val : "••••••••••••••••••"}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  aria-label={reveal ? "隐藏凭证" : "显示凭证"}
                  onClick={() => setReveal((value) => !value)}
                  className="grid h-9 w-9 place-items-center rounded-lg border border-[var(--ap-border)] text-[var(--ap-text-2)]"
                >
                  {reveal ? <EyeOff size={15} /> : <Eye size={15} />}
                </button>
                <GhostButton onClick={() => toast.success("已生成新密钥并轮换")}>轮换</GhostButton>
              </div>
            </div>
          ))}
        </div>
        <p className="mt-3 flex items-center gap-1.5 text-[12px] text-[var(--ap-muted)]">
          <Boxes size={13} /> 凭证以加密形式存储，平台与管理员均无法查看明文。
        </p>
      </Panel>
    </Page>
  );
}
