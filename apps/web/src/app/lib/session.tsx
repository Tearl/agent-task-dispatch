import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { RoleId } from "./roles";
import { clientRolesForEngineRoles, readSession, revokeSession, type PublicSession } from "./platform-api";

interface SessionState {
  connected: boolean;
  loading: boolean;
  restoreError: string | null;
  address: string;
  role: RoleId;
  authorizedRoles: RoleId[];
  retrySession: () => void;
  connect: (session: PublicSession, preferredRole?: RoleId) => RoleId | null;
  disconnect: () => Promise<void>;
  switchRole: (role: RoleId) => void;
  adminAuthed: boolean;
  adminLogin: () => void;
  adminLogout: () => void;
}

const SessionContext = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [restoreError, setRestoreError] = useState<string | null>(null);
  const [restoreAttempt, setRestoreAttempt] = useState(0);
  const restoreGeneration = useRef(0);
  const [address, setAddress] = useState("");
  const [role, setRole] = useState<RoleId>("publisher");
  const [authorizedRoles, setAuthorizedRoles] = useState<RoleId[]>([]);
  const [adminAuthed, setAdminAuthed] = useState(false);

  const applySession = (session: PublicSession, preferredRole?: RoleId) => {
    const roles = clientRolesForEngineRoles(session.roles);
    const nextRole = preferredRole && preferredRole !== "admin" && roles.includes(preferredRole) ? preferredRole : roles[0] ?? null;
    setConnected(true);
    setAddress(session.walletAddress);
    setAuthorizedRoles(roles);
    if (nextRole) setRole(nextRole);
    return nextRole;
  };

  useEffect(() => {
    let active = true;
    const generation = ++restoreGeneration.current;
    setLoading(true);
    setRestoreError(null);
    void readSession().then((session) => {
      if (!active || generation !== restoreGeneration.current) return;
      if (session) applySession(session);
      else {
        setConnected(false);
        setAddress("");
        setAuthorizedRoles([]);
      }
    }).catch((error) => {
      if (active && generation === restoreGeneration.current) setRestoreError(error instanceof Error ? error.message : "会话恢复失败，请重试。");
    }).finally(() => { if (active && generation === restoreGeneration.current) setLoading(false); });
    return () => { active = false; };
  }, [restoreAttempt]);

  const value = useMemo<SessionState>(
    () => ({
      connected,
      loading,
      restoreError,
      address,
      role,
      authorizedRoles,
      retrySession: () => setRestoreAttempt((attempt) => attempt + 1),
      connect: (session, preferredRole) => {
        restoreGeneration.current += 1;
        setLoading(false);
        setRestoreError(null);
        return applySession(session, preferredRole);
      },
      disconnect: async () => {
        await revokeSession();
        restoreGeneration.current += 1;
        setConnected(false);
        setAddress("");
        setAuthorizedRoles([]);
      },
      switchRole: (nextRole) => { if (authorizedRoles.includes(nextRole)) setRole(nextRole); },
      adminAuthed,
      adminLogin: () => setAdminAuthed(true),
      adminLogout: () => setAdminAuthed(false),
    }),
    [connected, loading, restoreError, address, role, adminAuthed, authorizedRoles],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}


export function useSession() {
  const context = useContext(SessionContext);
  if (!context) throw new Error("useSession must be used within SessionProvider");
  return context;
}

export function shortAddr(address: string) {
  return address ? `${address.slice(0, 6)}…${address.slice(-4)}` : "";
}
