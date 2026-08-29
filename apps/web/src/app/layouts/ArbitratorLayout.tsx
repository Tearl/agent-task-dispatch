import { AppShell } from "../components/AppShell";
import { SessionRecovery } from "../components/SessionRecovery";
import { Navigate } from "react-router";
import { roleAccessDecision } from "../lib/session-guard";
import { useSession } from "../lib/session";

export default function ArbitratorLayout() {
  const session = useSession();
  const access = roleAccessDecision(session, "arbitrator");
  if (access === "loading") return <main role="status" className="ap-app-bg grid min-h-svh place-items-center text-[var(--ap-text-2)]">正在恢复安全会话…</main>;
  if (access === "recovery") return <SessionRecovery />;
  if (access === "login") return <Navigate to="/arbitrator/login" replace />;
  return <AppShell roleId="arbitrator" />;
}
