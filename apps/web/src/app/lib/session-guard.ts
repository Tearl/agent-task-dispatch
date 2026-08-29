export type RoleAccessDecision = "loading" | "recovery" | "login" | "allow";

export function roleAccessDecision(
  state: {
    loading: boolean;
    restoreError: unknown;
    connected: boolean;
    authorizedRoles: readonly string[];
  },
  requiredRole: string,
): RoleAccessDecision {
  if (state.loading) return "loading";
  if (state.restoreError) return "recovery";
  if (!state.connected || !state.authorizedRoles.includes(requiredRole)) return "login";
  return "allow";
}
