import { ArrowDownRight, ArrowUpRight, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export function Panel({
  children,
  className = "",
  strong = false,
  hover = false,
}: {
  children: ReactNode;
  className?: string;
  strong?: boolean;
  hover?: boolean;
}) {
  return (
    <div
      className={`rounded-2xl ${strong ? "ap-glass-strong" : "ap-glass"} ${hover ? "ap-hoverable" : ""} ${className}`}
    >
      {children}
    </div>
  );
}

export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h1 className="text-[26px] tracking-tight text-[var(--ap-text)]">{title}</h1>
        {subtitle ? <p className="mt-1 text-[14px] text-[var(--ap-muted)]">{subtitle}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export function StatCard({
  label,
  value,
  unit,
  icon: Icon,
  delta,
  accent = "var(--ap-cyan)",
  hint,
}: {
  label: string;
  value: string;
  unit?: string;
  icon?: LucideIcon;
  delta?: number;
  accent?: string;
  hint?: string;
}) {
  return (
    <Panel hover className="p-5">
      <div className="flex items-center justify-between">
        <span className="text-[13px] text-[var(--ap-muted)]">{label}</span>
        {Icon ? (
          <span className="grid h-8 w-8 place-items-center rounded-lg" style={{ background: `${accent}22`, color: accent }}>
            <Icon size={16} />
          </span>
        ) : null}
      </div>
      <div className="mt-3 flex items-baseline gap-1">
        <span className="text-[28px] leading-none text-[var(--ap-text)]">{value}</span>
        {unit ? <span className="text-[13px] text-[var(--ap-muted)]">{unit}</span> : null}
      </div>
      <div className="mt-2 flex items-center gap-2">
        {delta !== undefined ? (
          <span
            className="inline-flex items-center gap-0.5 text-[12px]"
            style={{ color: delta >= 0 ? "var(--ap-success)" : "var(--ap-danger)" }}
          >
            {delta >= 0 ? <ArrowUpRight size={13} /> : <ArrowDownRight size={13} />}
            {Math.abs(delta)}%
          </span>
        ) : null}
        {hint ? <span className="text-[12px] text-[var(--ap-muted)]">{hint}</span> : null}
      </div>
    </Panel>
  );
}

export type Tone = "cyan" | "violet" | "green" | "amber" | "red" | "blue" | "gray";

const TONE: Record<Tone, { bg: string; fg: string; bd: string }> = {
  cyan: { bg: "rgba(34,211,238,.14)", fg: "#67e8f9", bd: "rgba(34,211,238,.35)" },
  violet: { bg: "rgba(139,92,246,.16)", fg: "#c4b5fd", bd: "rgba(139,92,246,.4)" },
  green: { bg: "rgba(52,211,153,.15)", fg: "#6ee7b7", bd: "rgba(52,211,153,.4)" },
  amber: { bg: "rgba(251,191,36,.15)", fg: "#fcd34d", bd: "rgba(251,191,36,.4)" },
  red: { bg: "rgba(251,113,133,.15)", fg: "#fda4af", bd: "rgba(251,113,133,.4)" },
  blue: { bg: "rgba(56,189,248,.15)", fg: "#7dd3fc", bd: "rgba(56,189,248,.4)" },
  gray: { bg: "rgba(114,134,166,.15)", fg: "#b7c6e0", bd: "rgba(114,134,166,.35)" },
};

export function Pill({ children, tone = "gray", dot = false }: { children: ReactNode; tone?: Tone; dot?: boolean }) {
  const palette = TONE[tone];
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12px]"
      style={{ background: palette.bg, color: palette.fg, borderColor: palette.bd }}
    >
      {dot ? <span className="h-1.5 w-1.5 rounded-full" style={{ background: palette.fg }} /> : null}
      {children}
    </span>
  );
}

export function SectionTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
      <h3 className="text-[15px] text-[var(--ap-text)]">{children}</h3>
      {right}
    </div>
  );
}

export function CtaButton({
  children,
  onClick,
  className = "",
  full = false,
  icon: Icon,
}: {
  children: ReactNode;
  onClick?: () => void;
  className?: string;
  full?: boolean;
  icon?: LucideIcon;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`ap-cta inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2.5 text-[14px] ${full ? "w-full" : ""} ${className}`}
    >
      {Icon ? <Icon size={16} /> : null}
      {children}
    </button>
  );
}

export function GhostButton({
  children,
  onClick,
  className = "",
  icon: Icon,
  active = false,
}: {
  children: ReactNode;
  onClick?: () => void;
  className?: string;
  icon?: LucideIcon;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center justify-center gap-2 rounded-xl border px-3.5 py-2.5 text-[14px] transition-colors ${className}`}
      style={{
        borderColor: active ? "var(--ap-border-strong)" : "var(--ap-border)",
        background: active ? "var(--ap-cyan-soft)" : "transparent",
        color: active ? "#a5f3fc" : "var(--ap-text-2)",
      }}
    >
      {Icon ? <Icon size={16} /> : null}
      {children}
    </button>
  );
}

export function Bar({ value, tone = "#22d3ee" }: { value: number; tone?: string }) {
  return (
    <div className="h-1.5 w-full rounded-full" style={{ background: "rgba(255,255,255,.06)" }}>
      <div className="h-full rounded-full" style={{ width: `${Math.min(100, value)}%`, background: tone }} />
    </div>
  );
}

export function InfoNote({ children, tone = "cyan" }: { children: ReactNode; tone?: Tone }) {
  const palette = TONE[tone];
  return (
    <div
      className="rounded-xl border px-4 py-3 text-[13px]"
      style={{ background: palette.bg, color: palette.fg, borderColor: palette.bd }}
    >
      {children}
    </div>
  );
}
