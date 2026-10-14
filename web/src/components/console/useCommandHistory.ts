import { useRef } from "react";

const MAX_ENTRIES = 100;

/** Bounded history of sent commands, navigable with the arrow keys. */
export function useCommandHistory() {
  const entries = useRef<string[]>([]);
  const cursor = useRef(0);
  return {
    push(command: string) {
      if (entries.current.at(-1) !== command) entries.current.push(command);
      if (entries.current.length > MAX_ENTRIES) entries.current.shift();
      cursor.current = entries.current.length;
    },
    /** Moves in the history; returns undefined at the boundaries. */
    navigate(direction: -1 | 1): string | undefined {
      const next = cursor.current + direction;
      if (next < 0 || next > entries.current.length) return undefined;
      cursor.current = next;
      return entries.current[next] ?? "";
    },
  };
}
