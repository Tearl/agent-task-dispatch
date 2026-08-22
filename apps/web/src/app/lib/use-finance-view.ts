import { useCallback, useEffect, useState } from "react";

export function useFinanceView<T>(loader: () => Promise<T>) {
  const [value, setValue] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((current) => current + 1), []);
  useEffect(() => {
    let active = true;
    setLoading(true); setError(null);
    void loader().then((result) => { if (active) setValue(result); }).catch((reason) => { if (active) setError(reason instanceof Error ? reason.message : "资金视图加载失败。"); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [loader, attempt]);
  return { value, error, loading, reload };
}
