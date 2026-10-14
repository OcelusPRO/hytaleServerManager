import { createContext, use, type ReactNode } from "react";
import { api } from "../lib/api";
import type { HytaleAuthStatus, ServerStatus, SetupStatus } from "../lib/types";
import { usePolling } from "./usePolling";

interface Status {
  setup: SetupStatus;
  hytale: HytaleAuthStatus;
  server: ServerStatus;
}

interface StatusContextValue {
  status: Status | undefined;
  refresh: () => Promise<void>;
}

const StatusContext = createContext<StatusContextValue | null>(null);

async function loadStatus(): Promise<Status> {
  const [setup, hytale, server] = await Promise.all([
    api.get<SetupStatus>("/api/setup"),
    api.get<HytaleAuthStatus>("/api/hytale/auth"),
    api.get<ServerStatus>("/api/server/status"),
  ]);
  return { setup, hytale, server };
}

/** Shares the installation, Hytale account and server status app-wide. */
export function StatusProvider({ children }: { children: ReactNode }) {
  const { data, refresh } = usePolling(loadStatus, 2000);
  return <StatusContext value={{ status: data, refresh }}>{children}</StatusContext>;
}

export function useStatus(): StatusContextValue {
  const ctx = use(StatusContext);
  if (!ctx) throw new Error("useStatus outside StatusProvider");
  return ctx;
}
