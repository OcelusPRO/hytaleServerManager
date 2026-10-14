import { useEffect, useState } from "react";
import type { ConsoleLine } from "../lib/types";

const MAX_LINES = 3000;

/** Follows the server console through Server-Sent Events. */
export function useConsoleStream() {
  const [lines, setLines] = useState<ConsoleLine[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const source = new EventSource("/api/console/stream");
    source.addEventListener("open", () => setConnected(true));
    // EventSource reconnects by itself and resumes from the last event id.
    source.addEventListener("error", () => setConnected(false));
    source.addEventListener("message", (event: MessageEvent<string>) => {
      const line = JSON.parse(event.data) as ConsoleLine;
      setLines((prev) => {
        const next = prev.length >= MAX_LINES ? prev.slice(prev.length - MAX_LINES + 1) : prev.slice();
        next.push(line);
        return next;
      });
    });
    return () => source.close();
  }, []);

  return { lines, connected, clear: () => setLines([]) };
}
