import { AppShell } from "../components/AppShell";
import { useSession } from "../lib/session";

export default function SharedLayout() {
  const { role } = useSession();
  return <AppShell roleId={role === "admin" ? "publisher" : role} />;
}
