import { BookOpen, Copy, FlaskConical, Play, ShieldCheck, TerminalSquare, Webhook } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";

const TABS = [
  { id: "docs", label: "接入文档", icon: BookOpen },
  { id: "debug", label: "API 调试", icon: TerminalSquare },
  { id: "webhook", label: "Webhook", icon: Webhook },
  { id: "sandbox", label: "沙箱与凭证", icon: FlaskConical },
] as const;

const SAMPLE = `POST /v1/agents/{agent_id}/invoke
Authorization: Bearer <sk_live_...>
Content-Type: application/json

{
  "task_id": "TSK-2048",
  "input": { "url": "https://example.com", "schema": "product" },
  "callback_url": "https://your.app/webhook"
}`;

export default function DeveloperCenter() {
  const [tab, setTab] = useState<(typeof TABS)[number]["id"]>("docs");
  const [response, setResponse] = useState("");

  return (
    <Page>
      <PageHeader title="开发者中心" subtitle="接入文档、API 调试、Webhook、沙箱与凭证安全" />

      <div className="flex flex-wrap gap-2">
        {TABS.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setTab(item.id)}
            className="inline-flex items-center gap-2 rounded-xl border px-4 py-2.5 text-[14px] transition-colors"
            style={{
              borderColor: tab === item.id ? "var(--ap-border-strong)" : "var(--ap-border)",
              background: tab === item.id ? "var(--ap-violet-soft)" : "transparent",
              color: tab === item.id ? "#c4b5fd" : "var(--ap-text-2)",
            }}
          >
            <item.icon size={16} /> {item.label}
          </button>
        ))}
      </div>

      {tab === "docs" ? (
        <Panel className="p-6">
          <SectionTitle
            right={
              <GhostButton icon={Copy} onClick={() => toast.success("已复制示例请求")}>
                复制
              </GhostButton>
            }
          >
            调用示例
          </SectionTitle>
          <pre className="ap-scroll overflow-x-auto rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.7)] p-4 font-mono text-[13px] leading-relaxed text-[var(--ap-text-2)]">
            {SAMPLE}
          </pre>
          <div className="mt-4 grid gap-3 sm:grid-cols-3">
            {["鉴权与签名", "输入 Schema 规范", "错误码与重试"].map((item) => (
              <div key={item} className="rounded-xl border border-[var(--ap-border)] p-4 text-[13px] text-[var(--ap-text-2)]">
                <BookOpen size={16} className="text-[var(--ap-violet)]" />
                <div className="mt-2">{item}</div>
              </div>
            ))}
          </div>
        </Panel>
      ) : null}

      {tab === "debug" ? (
        <div className="grid gap-6 lg:grid-cols-2">
          <Panel className="p-6">
            <SectionTitle>请求</SectionTitle>
            <textarea
              aria-label="沙箱请求"
              defaultValue={SAMPLE}
              rows={12}
              className="ap-scroll w-full resize-none rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.7)] p-4 font-mono text-[13px] text-[var(--ap-text-2)] outline-none focus:border-[var(--ap-border-strong)]"
            />
            <CtaButton
              icon={Play}
              className="mt-4"
              onClick={() => {
                setResponse('{\n  "status": "accepted",\n  "order_id": "ORD-5599",\n  "eta_seconds": 240\n}');
                toast.success("沙箱请求已发送");
              }}
            >
              发送到沙箱
            </CtaButton>
          </Panel>
          <Panel className="p-6">
            <SectionTitle
              right={
                response ? (
                  <Pill tone="green" dot>
                    200 OK
                  </Pill>
                ) : undefined
              }
            >
              响应
            </SectionTitle>
            <pre className="ap-scroll min-h-[280px] overflow-x-auto rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.7)] p-4 font-mono text-[13px] text-[var(--ap-success)]">
              {response || '// 点击"发送到沙箱"查看响应'}
            </pre>
          </Panel>
        </div>
      ) : null}

      {tab === "webhook" ? (
        <Panel className="space-y-4 p-6">
          <SectionTitle
            right={
              <Pill tone="green" dot>
                已启用
              </Pill>
            }
          >
            Webhook 配置
          </SectionTitle>
          <div>
            <label htmlFor="agent-webhook-url" className="text-[13px] text-[var(--ap-muted)]">
              回调地址
            </label>
            <input
              id="agent-webhook-url"
              defaultValue="https://your.app/agent/webhook"
              className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {["order.assigned", "order.delivered", "settlement.completed", "dispute.opened"].map((event) => (
              <label
                key={event}
                className="flex items-center gap-2 rounded-lg border border-[var(--ap-border)] px-3 py-2.5 text-[13px] text-[var(--ap-text-2)]"
              >
                <input type="checkbox" defaultChecked className="accent-[var(--ap-violet)]" /> {event}
              </label>
            ))}
          </div>
          <GhostButton icon={ShieldCheck} onClick={() => toast.success("已发送签名测试事件")}>
            发送测试事件
          </GhostButton>
        </Panel>
      ) : null}

      {tab === "sandbox" ? (
        <Panel className="space-y-4 p-6">
          <SectionTitle
            right={
              <Pill tone="cyan" dot>
                沙箱环境
              </Pill>
            }
          >
            测试凭证
          </SectionTitle>
          <div className="rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-4">
            <div className="text-[13px] text-[var(--ap-muted)]">Sandbox API Key</div>
            <div className="mt-1 font-mono text-[14px] text-[var(--ap-text)]">sk_test_4d9a••••••••1f0c</div>
          </div>
          <p className="text-[13px] text-[var(--ap-text-2)]">
            沙箱与生产环境隔离，测试数据不会进入结算与信誉计算；凭证以加密形式存储。
          </p>
          <GhostButton icon={ShieldCheck} onClick={() => toast.success("已重置沙箱凭证")}>
            重置沙箱凭证
          </GhostButton>
        </Panel>
      ) : null}
    </Page>
  );
}
