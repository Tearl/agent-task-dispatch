export const sessionCookieName = "agent_platform_session";
export const shouldClearSessionAfterLogout = (status: number) => status === 204 || status === 401;

export type PublicSession = {
  sessionId: string;
  userId: string;
  walletAddress: string;
  roles: string[];
  expiresAt: string;
};

type EngineSession = PublicSession & { token: string };

export function splitSession(value: unknown): { token: string; publicSession: PublicSession } {
  if (!value || typeof value !== "object") throw new Error("invalid engine session");
  const session = value as EngineSession;
  if (typeof session.token !== "string" || session.token.length < 32) throw new Error("invalid engine session");
  return { token: session.token, publicSession: parsePublicSession(session) };
}

export function parsePublicSession(value: unknown): PublicSession {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("invalid engine session");
  const session = value as Record<string, unknown>;
  if (typeof session.sessionId !== "string" || typeof session.userId !== "string" || typeof session.walletAddress !== "string" || !Array.isArray(session.roles) || !session.roles.every((role) => typeof role === "string") || typeof session.expiresAt !== "string") throw new Error("invalid engine session");
  return { sessionId: session.sessionId, userId: session.userId, walletAddress: session.walletAddress, roles: session.roles as string[], expiresAt: session.expiresAt };
}

export function sessionCookie(token: string, expiresAt: string, production = process.env.NODE_ENV === "production") {
  const expires = new Date(expiresAt);
  if (!Number.isFinite(expires.getTime())) throw new Error("invalid session expiry");
  return { name: sessionCookieName, value: token, httpOnly: true, sameSite: "strict" as const, secure: production, path: "/", expires };
}
