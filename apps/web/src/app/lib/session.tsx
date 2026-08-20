import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

import { CLIENT_ROLES, type RoleId } from "./roles";

interface SessionState {
  connected: boolean;
  address: string;
  role: RoleId;
  authorizedRoles: RoleId[];
  connect: (address: string) => void;
  disconnect: () => void;
  switchRole: (role: RoleId) => void;
  adminAuthed: boolean;
  adminLogin: () => void;
  adminLogout: () => void;
}

const SessionContext = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false);
  const [address, setAddress] = useState("");
  const [role, setRole] = useState<RoleId>("publisher");
  const [adminAuthed, setAdminAuthed] = useState(false);

  const value = useMemo<SessionState>(
    () => ({
      connected,
      address,
      role,
      authorizedRoles: CLIENT_ROLES,
      connect: (nextAddress) => {
        setConnected(true);
        setAddress(nextAddress);
      },
      disconnect: () => {
        setConnected(false);
        setAddress("");
      },
      switchRole: setRole,
      adminAuthed,
      adminLogin: () => setAdminAuthed(true),
      adminLogout: () => setAdminAuthed(false),
    }),
    [connected, address, role, adminAuthed],
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
