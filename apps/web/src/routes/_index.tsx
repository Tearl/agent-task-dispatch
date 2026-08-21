import {
  BarChart3,
  Boxes,
  Check,
  ChevronRight,
  Code2,
  FilePlus2,
  FileCheck2,
  GitBranch,
  ImageIcon,
  Info,
  Languages,
  Layers,
  Lock,
  ScanLine,
  ScanSearch,
  Search,
  ShieldCheck,
  ShieldHalf,
  Users,
  Wallet,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";

import { ROLES, type RoleId } from "../app/lib/roles";
import { useSession } from "../app/lib/session";
import { authenticateWallet, clientRolesForEngineRoles, type WalletProvider } from "../app/lib/platform-api";
import type { Route } from "./+types/_index";

export function meta({}: Route.MetaArgs) {
  return [
    { title: "AI Agent Platform｜连接专业 Agent" },
    { name: "description", content: "发布需求、智能匹配、链上托管、安全交付。" },
  ];
}

type Capability = {
  icon: LucideIcon;
  label: string;
  angle: number;
  color: string;
};

const loginRoles: { id: RoleId; icon: LucideIcon; description: string }[] = [
  { id: "publisher", icon: FilePlus2, description: "发布需求、托管任务款、验收结算" },
  { id: "agent", icon: Boxes, description: "接入 Agent、承接订单、赚取收益" },
];

const capabilities: Capability[] = [
  { icon: BarChart3, label: "数据分析", angle: -140, color: "#22d3ee" },
  { icon: Languages, label: "翻译", angle: -90, color: "#22d3ee" },
  { icon: ImageIcon, label: "图像生成", angle: -40, color: "#8b5cf6" },
  { icon: Code2, label: "代码开发", angle: 160, color: "#22d3ee" },
  { icon: Search, label: "市场研究", angle: 30, color: "#8b5cf6" },
  { icon: ScanLine, label: "智能审计", angle: 90, color: "#22d3ee" },
];

const valueChips = [
  { icon: GitBranch, label: "智能分发" },
  { icon: FileCheck2, label: "真实合约" },
  { icon: ShieldHalf, label: "可信结算" },
] as const;

const trustHighlights = [
  { icon: ShieldHalf, label: "真实合约托管" },
  { icon: Users, label: "多角色权限" },
  { icon: Lock, label: "全链路追踪" },
] as const;

const accessItems = [
  { icon: ShieldCheck, label: "钱包签名认证" },
  { icon: Users, label: "角色权限自动识别" },
  { icon: Layers, label: "以太坊测试网" },
  { icon: ScanSearch, label: "全流程可追踪" },
] as const;

export default function Home() {
  const navigate = useNavigate();
  const { connect, switchRole } = useSession();
  const [role, setRole] = useState<RoleId>("publisher");
  const [error, setError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);

  async function connectWallet() {
    setError(null);
    if (connecting) return;
    const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;

    if (!ethereum) {
      setError("未检测到钱包，请先安装 MetaMask 或其他以太坊兼容钱包。");
      return;
    }

    try {
      setConnecting(true);
      const session = await authenticateWallet(ethereum);
      const roles = clientRolesForEngineRoles(session.roles);
      const nextRole = connect(session, role);
      if (role === "admin" || !roles.includes(role)) {
        setError(`该钱包未获授权使用「${ROLES[role].name}」。可用角色：${roles.map((item) => ROLES[item].name).join("、") || "无"}。`);
        return;
      }
      if (!nextRole) throw new Error("session has no client role");
      switchRole(nextRole);
      navigate(ROLES[nextRole].home);
    } catch (cause) {
      setError(cause instanceof Error && cause.message ? `登录失败：${cause.message}` : "钱包连接未完成，请在钱包中确认后重试。");
    } finally {
      setConnecting(false);
    }
  }

  return (
    <main className="ap-app-bg relative min-h-svh w-full overflow-hidden">
      <div className="ap-grid-texture pointer-events-none absolute inset-0 opacity-40" />

      <div className="relative mx-auto grid min-h-svh max-w-[1400px] items-center gap-12 px-5 py-10 sm:px-8 lg:grid-cols-[1.15fr_0.85fr] lg:gap-10">
        <section className="relative">
          <div className="flex items-center gap-3">
            <span className="h-5 w-1 rounded bg-[var(--ap-cyan)]" />
            <span className="text-[13px] tracking-[0.3em] text-[var(--ap-cyan)]">
              AI 原生任务网络
            </span>
          </div>

          <h1 className="mt-6 text-[clamp(38px,5vw,64px)] leading-[1.08] font-medium tracking-tight text-white">
            让专业 Agent，
            <br />
            完成专业任务
          </h1>
          <p className="mt-5 text-[18px] text-[var(--ap-text-2)]">
            发布需求、智能匹配、链上托管、安全交付
          </p>

          <div className="mt-7 flex flex-wrap gap-3">
            {valueChips.map(({ icon: Icon, label }) => (
              <span
                key={label}
                className="ap-glass inline-flex items-center gap-2 rounded-full px-4 py-2 text-[14px] text-[var(--ap-text-2)]"
              >
                <Icon aria-hidden="true" size={16} className="text-[var(--ap-cyan)]" />
                {label}
              </span>
            ))}
          </div>

          <div className="capability-network relative mx-auto mt-10 h-[340px] w-full max-w-[560px]">
            <div className="capability-orbit-outer absolute top-1/2 left-1/2 h-[280px] w-[280px] -translate-x-1/2 -translate-y-1/2 rounded-full border border-[rgba(34,211,238,0.15)]" />
            <div className="capability-orbit-inner absolute top-1/2 left-1/2 h-[200px] w-[200px] -translate-x-1/2 -translate-y-1/2 rounded-full border border-[rgba(34,211,238,0.2)]" />

            <div className="ap-float absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2">
              <div className="ap-ring-glow grid h-24 w-24 rotate-45 place-items-center rounded-2xl bg-gradient-to-br from-[#22d3ee] to-[#0891b2]">
                <div className="h-10 w-10 rounded-lg bg-[#04121c]/40 backdrop-blur" />
              </div>
            </div>

            {capabilities.map(({ icon: Icon, label, angle, color }) => {
              const radians = (angle * Math.PI) / 180;
              const left = 50 + Math.cos(radians) * 37.5;
              const top = 50 + Math.sin(radians) * 38.235;

              return (
                <div
                  key={label}
                  className="capability-node absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1.5"
                  style={{ left: `${left}%`, top: `${top}%` }}
                >
                  <span
                    className="ap-glass grid h-11 w-11 place-items-center rounded-xl"
                    style={{ color, borderColor: `${color}55` }}
                  >
                    <Icon aria-hidden="true" size={18} />
                  </span>
                  <span className="whitespace-nowrap text-[12px] text-[var(--ap-text-2)]">
                    {label}
                  </span>
                </div>
              );
            })}
          </div>

          <div className="mt-6 flex flex-wrap items-center gap-x-8 gap-y-3 text-[15px] text-[var(--ap-text-2)]">
            {trustHighlights.map(({ icon: Icon, label }, index) => (
              <div key={label} className="contents">
                {index > 0 ? (
                  <span
                    aria-hidden="true"
                    className="hidden h-4 w-px bg-[var(--ap-border)] sm:block"
                  />
                ) : null}
                <span className="inline-flex items-center gap-2">
                  <Icon aria-hidden="true" size={18} className="text-[var(--ap-cyan)]" />
                  {label}
                </span>
              </div>
            ))}
          </div>
        </section>

        <section className="ap-glass-strong ap-ring-glow w-full rounded-3xl p-6 sm:p-8 lg:p-10">
          <div className="flex flex-col items-center text-center">
            <div className="ap-ring-glow grid h-16 w-16 rotate-45 place-items-center rounded-2xl bg-gradient-to-br from-[#22d3ee] to-[#0891b2]">
              <div className="h-6 w-6 -rotate-45 rounded bg-[#04121c]/40" />
            </div>
            <h2 className="mt-5 text-[26px] font-medium text-white">AI Agent Platform</h2>
            <p className="mt-1.5 text-[14px] text-[var(--ap-muted)]">
              连接智能能力，安全完成每一次任务
            </p>
          </div>

          <div className="mt-7">
            <div className="mb-2.5 flex items-center gap-1.5 text-[13px] text-[var(--ap-muted)]">
              <Users aria-hidden="true" size={14} className="text-[var(--ap-cyan)]" />
              选择进入的角色
            </div>
            <div className="grid grid-cols-2 gap-3">
              {loginRoles.map(({ id, icon: Icon, description }) => {
                const config = ROLES[id];
                const active = role === id;

                return (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setRole(id)}
                    className="relative rounded-xl border p-3 text-left transition-colors sm:p-4"
                    style={{
                      borderColor: active ? "var(--ap-border-strong)" : "var(--ap-border)",
                      background: active ? "var(--ap-cyan-soft)" : "rgba(10,18,38,0.5)",
                    }}
                  >
                    {active ? (
                      <Check aria-hidden="true" size={15} className="absolute right-3 top-3 text-[var(--ap-cyan)]" />
                    ) : null}
                    <span
                      className="grid h-9 w-9 place-items-center rounded-lg"
                      style={{ background: `${config.accent}22`, color: config.accent }}
                    >
                      <Icon aria-hidden="true" size={18} />
                    </span>
                    <div
                      className="mt-2.5 text-[14px]"
                      style={{ color: active ? "#a5f3fc" : "var(--ap-text)" }}
                    >
                      {config.name}
                    </div>
                    <div className="mt-1 text-[12px] leading-snug text-[var(--ap-muted)]">{description}</div>
                  </button>
                );
              })}
            </div>
          </div>

          <button
            type="button"
            onClick={connectWallet}
            disabled={connecting}
            aria-busy={connecting}
            className="ap-cta mt-5 flex w-full items-center justify-center gap-2 rounded-xl py-3.5 text-[16px] font-medium focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[var(--ap-cyan)]"
          >
            <Wallet aria-hidden="true" size={19} />
            {connecting ? "等待钱包签名…" : `以「${ROLES[role].name}」身份登录`}
          </button>
          <p className="mt-3 text-center text-[13px] text-[var(--ap-muted)]">
            支持 MetaMask 等以太坊兼容钱包 · 登录后可在右上角切换角色
          </p>

          {error ? (
            <p
              role="alert"
              className="mt-4 rounded-xl border border-amber-300/20 bg-amber-300/8 px-4 py-3 text-[13px] text-amber-100"
            >
              {error}
            </p>
          ) : null}

          <div className="my-6 flex items-center gap-3">
            <span className="h-px flex-1 bg-[var(--ap-border)]" />
            <span className="text-[12px] text-[var(--ap-muted)]">安全访问</span>
            <span className="h-px flex-1 bg-[var(--ap-border)]" />
          </div>

          <div className="space-y-3">
            {accessItems.map(({ icon: Icon, label }) => (
              <button
                key={label}
                type="button"
                onClick={connectWallet}
                className="ap-hoverable flex w-full items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] px-4 py-3.5 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ap-cyan)]"
              >
                <span className="flex items-center gap-3 text-[15px] text-[var(--ap-text-2)]">
                  <Icon aria-hidden="true" size={18} className="text-[var(--ap-cyan)]" />
                  {label}
                </span>
                <ChevronRight aria-hidden="true" size={16} className="text-[var(--ap-muted)]" />
              </button>
            ))}
          </div>

          <p className="mt-6 flex items-center justify-center gap-1.5 text-center text-[13px] text-[var(--ap-muted)]">
            <Info aria-hidden="true" size={14} />
            登录后按已授权角色进入对应工作台
          </p>

          <div className="mt-6 flex items-center justify-center gap-10 border-t border-[var(--ap-border)] pt-5 text-[13px] text-[var(--ap-muted)]">
            <button type="button" className="transition-colors hover:text-[var(--ap-text-2)]">
              隐私政策
            </button>
            <button type="button" className="transition-colors hover:text-[var(--ap-text-2)]">
              使用条款
            </button>
          </div>
        </section>
      </div>

      <button
        type="button"
        onClick={() => navigate("/admin/login")}
        className="absolute right-6 bottom-4 text-[12px] text-[var(--ap-muted)] transition-colors hover:text-[var(--ap-text-2)]"
      >
        管理员后台 →
      </button>
    </main>
  );
}
