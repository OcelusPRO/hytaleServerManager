import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "../lib/api";

interface Polling<T> {
  data: T | undefined;
  error: string | undefined;
  refresh: () => Promise<void>;
}

/**
 * Loads data immediately and then every intervalMs while the page is
 * visible. A zero interval loads only once (refresh on demand).
 */
export function usePolling<T>(fetcher: () => Promise<T>, intervalMs: number): Polling<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string>();
  const fetcherRef = useRef(fetcher);
  useEffect(() => {
    fetcherRef.current = fetcher;
  });

  const refresh = useCallback(async () => {
    try {
      setData(await fetcherRef.current());
      setError(undefined);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    let timer: number | undefined;
    let cancelled = false;
    const tick = async () => {
      if (!document.hidden) await refresh();
      if (!cancelled && intervalMs > 0) timer = window.setTimeout(tick, intervalMs);
    };
    const onVisible = () => {
      if (!document.hidden && intervalMs > 0) {
        window.clearTimeout(timer);
        void tick();
      }
    };
    void tick();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refresh, intervalMs]);

  return { data, error, refresh };
}
