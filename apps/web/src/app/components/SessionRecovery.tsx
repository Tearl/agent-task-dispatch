import { useSession } from "../lib/session";

export function SessionRecovery() {
  const { restoreError, retrySession } = useSession();
  if (!restoreError) return null;
  return (
    <main role="alert" className="ap-app-bg grid min-h-svh place-items-center p-6 text-center">
      <div className="max-w-md">
        <h1 className="text-lg text-white">无法恢复安全会话</h1>
        <p className="mt-2 text-sm text-[var(--ap-text-2)]">{restoreError}</p>
        <button type="button" onClick={retrySession} className="mt-5 rounded-xl bg-[var(--ap-violet)] px-4 py-2 text-sm font-medium text-white">
          重试会话恢复
        </button>
      </div>
    </main>
  );
}
