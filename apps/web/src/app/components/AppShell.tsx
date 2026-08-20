import {
  Bell,
  Check,
  ChevronDown,
  LogOut,
  Menu,
  Repeat,
  Search,
  ServerCog,
  UserCog,
  Wallet,
  X,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";

import { CLIENT_ROLES, ROLES, SHARED_NAV, type RoleId } from "../lib/roles";
import { shortAddr, useSession } from "../lib/session";

function Brand({ accent }: { accent: string }) {
  return (
    <div className="flex items-center gap-3 px-2">
      <span
        className="grid h-10 w-10 rotate-45 place-items-center rounded-xl"
        style={{ background: `linear-gradient(135deg, ${accent}, #0891b2)` }}
      >
        <span className="h-3.5 w-3.5 -rotate-45 rounded bg-[#04121c]/40" />
      </span>
      <div className="leading-tight">
        <div className="text-[15px] text-white">AI Agent</div>
        <div className="text-[11px] text-[var(--ap-muted)]">Platform</div>
      </div>
    </div>
  );
}

function RoleBadge({ roleId }: { roleId: RoleId }) {
  const config = ROLES[roleId];
  return (
    <div className="mx-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] px-3 py-2.5">
      <div className="text-[11px] text-[var(--ap-muted)]">当前角色</div>
      <div className="mt-0.5 flex items-center gap-2 text-[14px]" style={{ color: config.accent }}>
        <span className="h-2 w-2 rounded-full" style={{ background: config.accent }} />
        {config.name}
      </div>
    </div>
  );
}

function TopRoleSwitcher({ roleId }: { roleId: RoleId }) {
  const { switchRole } = useSession();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const config = ROLES[roleId];

  useEffect(() => {
    const closeOnOutsideClick = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) setOpen(false);
    };

    document.addEventListener("mousedown", closeOnOutsideClick);
    return () => document.removeEventListener("mousedown", closeOnOutsideClick);
  }, []);

  const pickRole = (nextRole: RoleId) => {
    setOpen(false);
    switchRole(nextRole);
    navigate(ROLES[nextRole].home);
  };

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        aria-label="切换角色"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((value) => !value)}
        className="flex h-10 w-10 items-center justify-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] text-[var(--ap-text-2)] hover:border-[var(--ap-border-strong)] sm:w-auto sm:px-3"
      >
        <Repeat size={15} style={{ color: config.accent }} />
        <span className="hidden text-[13px] sm:inline">{config.name}</span>
        <ChevronDown size={14} className="hidden text-[var(--ap-muted)] sm:block" />
      </button>

      {open ? (
        <div
          role="menu"
          className="ap-glass-strong absolute right-0 top-12 z-30 w-56 rounded-xl p-1.5 shadow-2xl"
        >
          <div className="px-2.5 py-1.5 text-[11px] text-[var(--ap-muted)]">切换角色工作台</div>
          {CLIENT_ROLES.map((nextRole) => {
            const nextConfig = ROLES[nextRole];
            const active = roleId === nextRole;

            return (
              <button
                key={nextRole}
                type="button"
                role="menuitem"
                onClick={() => pickRole(nextRole)}
                className="flex w-full items-center justify-between rounded-lg px-2.5 py-2.5 text-[13px] transition-colors hover:bg-[rgba(34,211,238,0.06)]"
                style={{
                  background: active ? "var(--ap-cyan-soft)" : "transparent",
                  color: active ? "#a5f3fc" : "var(--ap-text-2)",
                }}
              >
                <span className="flex items-center gap-2">
                  <span className="h-2 w-2 rounded-full" style={{ background: nextConfig.accent }} />
                  {nextConfig.name}
                </span>
                {active ? <Check size={15} /> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function SideNav({
  roleId,
  variant,
  open,
  onClose,
}: {
  roleId: RoleId;
  variant: "client" | "admin";
  open: boolean;
  onClose: () => void;
}) {
  const config = ROLES[roleId];

  return (
    <>
      {open ? (
        <button
          type="button"
          aria-label="关闭导航"
          onClick={onClose}
          className="fixed inset-0 z-40 bg-black/55 backdrop-blur-sm md:hidden"
        />
      ) : null}
      <aside
        className={`ap-glass fixed inset-y-0 left-0 z-50 flex w-[248px] shrink-0 flex-col gap-4 border-r border-[var(--ap-border)] py-5 transition-transform duration-200 md:relative md:z-auto md:translate-x-0 ${
          open ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        <div className="flex items-center justify-between pr-3">
          <Brand accent={config.accent} />
          <button
            type="button"
            aria-label="关闭导航"
            onClick={onClose}
            className="grid h-9 w-9 place-items-center rounded-lg text-[var(--ap-muted)] hover:bg-white/5 hover:text-white md:hidden"
          >
            <X size={18} />
          </button>
        </div>

        {variant === "admin" ? (
          <div className="mx-3 flex items-center gap-2 rounded-xl border border-[rgba(56,189,248,0.3)] bg-[rgba(56,189,248,0.1)] px-3 py-2 text-[12px] text-[var(--ap-info)]">
            <ServerCog size={14} /> 独立管理后台
          </div>
        ) : (
          <RoleBadge roleId={roleId} />
        )}

        <nav className="ap-scroll flex-1 space-y-1 overflow-y-auto px-3">
          {config.nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === config.home}
              onClick={onClose}
              className="flex items-center gap-3 rounded-xl px-3 py-2.5 text-[14px] transition-colors"
              style={({ isActive }) => ({
                background: isActive ? "var(--ap-cyan-soft)" : "transparent",
                color: isActive ? "#a5f3fc" : "var(--ap-text-2)",
                borderLeft: isActive ? `2px solid ${config.accent}` : "2px solid transparent",
              })}
            >
              <item.icon size={17} />
              {item.label}
            </NavLink>
          ))}

          {variant === "client" ? (
            <>
              <div className="my-3 h-px bg-[var(--ap-border)]" />
              {SHARED_NAV.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  onClick={onClose}
                  className="flex items-center gap-3 rounded-xl px-3 py-2.5 text-[14px] transition-colors"
                  style={({ isActive }) => ({
                    background: isActive ? "var(--ap-cyan-soft)" : "transparent",
                    color: isActive ? "#a5f3fc" : "var(--ap-text-2)",
                  })}
                >
                  <item.icon size={17} />
                  {item.label}
                </NavLink>
              ))}
            </>
          ) : null}
        </nav>

        <div className="mx-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-3 text-[11px] text-[var(--ap-muted)]">
          {variant === "admin"
            ? "管理员操作全程审计 · 无法查看用户凭证明文"
            : "Agent 接单零履约金 · 结算后可提取或生息"}
        </div>
      </aside>
    </>
  );
}

function TopBar({
  roleId,
  variant,
  onOpenNav,
}: {
  roleId: RoleId;
  variant: "client" | "admin";
  onOpenNav: () => void;
}) {
  const config = ROLES[roleId];
  const navigate = useNavigate();
  const { address, disconnect, adminLogout } = useSession();

  const logout = () => {
    if (variant === "admin") {
      adminLogout();
      navigate("/admin/login");
    } else {
      disconnect();
      navigate("/");
    }
  };

  return (
    <header className="flex h-16 shrink-0 items-center justify-between gap-2 border-b border-[var(--ap-border)] px-3 sm:gap-4 sm:px-6">
      <div className="flex min-w-0 items-center gap-2 sm:gap-3">
        <button
          type="button"
          aria-label="打开导航"
          onClick={onOpenNav}
          className="grid h-10 w-10 shrink-0 place-items-center rounded-xl border border-[var(--ap-border)] text-[var(--ap-text-2)] md:hidden"
        >
          <Menu size={18} />
        </button>
        <span
          className="truncate rounded-full px-2.5 py-1 text-[11px] sm:px-3 sm:text-[12px]"
          style={{ background: `${config.accent}22`, color: config.accent }}
        >
          {variant === "admin" ? "管理后台" : `当前角色 · ${config.name}`}
        </span>
      </div>

      <div className="flex shrink-0 items-center gap-2 sm:gap-3">
        <div className="hidden items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] px-3 py-2 lg:flex">
          <Search size={15} className="text-[var(--ap-muted)]" />
          <input
            aria-label="全局搜索"
            placeholder="搜索任务、Agent、案件…"
            className="w-52 bg-transparent text-[13px] text-[var(--ap-text)] outline-none placeholder:text-[var(--ap-muted)]"
          />
        </div>
        {variant === "client" ? <TopRoleSwitcher roleId={roleId} /> : null}
        <button
          type="button"
          aria-label="消息中心"
          onClick={() => variant === "client" && navigate("/notifications")}
          className="relative hidden h-10 w-10 place-items-center rounded-xl border border-[var(--ap-border)] text-[var(--ap-text-2)] hover:border-[var(--ap-border-strong)] sm:grid"
        >
          <Bell size={17} />
          <span className="absolute right-2.5 top-2.5 h-2 w-2 rounded-full bg-[var(--ap-danger)]" />
        </button>
        <div className="flex items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] px-2.5 py-2 sm:px-3">
          {variant === "admin" ? (
            <UserCog size={16} className="text-[var(--ap-info)]" />
          ) : (
            <Wallet size={16} className="text-[var(--ap-cyan)]" />
          )}
          <span className="hidden text-[13px] text-[var(--ap-text-2)] sm:inline">
            {variant === "admin" ? "admin@platform" : shortAddr(address) || "0x…钱包"}
          </span>
          <ChevronDown size={14} className="hidden text-[var(--ap-muted)] sm:block" />
        </div>
        <button
          type="button"
          onClick={logout}
          className="grid h-10 w-10 place-items-center rounded-xl border border-[var(--ap-border)] text-[var(--ap-muted)] hover:border-[var(--ap-border-strong)] hover:text-[var(--ap-danger)]"
          title="退出登录"
        >
          <LogOut size={17} />
        </button>
      </div>
    </header>
  );
}

export function AppShell({
  roleId,
  variant = "client",
}: {
  roleId: RoleId;
  variant?: "client" | "admin";
}) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);

  return (
    <div className="ap-app-bg flex h-svh w-full overflow-hidden">
      <div className="ap-grid-texture pointer-events-none absolute inset-0 opacity-20" />
      <SideNav
        roleId={roleId}
        variant={variant}
        open={mobileNavOpen}
        onClose={() => setMobileNavOpen(false)}
      />
      <div className="relative flex min-w-0 flex-1 flex-col">
        <TopBar roleId={roleId} variant={variant} onOpenNav={() => setMobileNavOpen(true)} />
        <main className="ap-scroll flex-1 overflow-y-auto p-4 sm:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export function Page({ children }: { children: ReactNode }) {
  return <div className="mx-auto max-w-[1280px] space-y-6">{children}</div>;
}
