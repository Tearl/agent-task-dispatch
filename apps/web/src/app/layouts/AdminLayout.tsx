import { AppShell } from "../components/AppShell";
import { SessionRecovery } from "../components/SessionRecovery";
import { Navigate } from "react-router";
import { useSession } from "../lib/session";

export default function AdminLayout() {
  const { loading, restoreError, connected, authorizedRoles } = useSession();
  if (loading) return <main role="status" className="ap-app-bg grid min-h-svh place-items-center text-[var(--ap-text-2)]">正在恢复安全会话…</main>;
  if (restoreError) return <SessionRecovery />;
  if (!connected || !authorizedRoles.includes("admin")) return <Navigate to="/admin/login" replace />;
  return <AppShell roleId="admin" variant="admin" />;
}
