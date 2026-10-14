import { useFeedback } from "../../components/ui/feedback";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import type { AccessList, PlayerAction, PlayersResponse } from "../../lib/types";

/** Player data of the active world and the actions that modify it. */
export function usePlayers() {
  const { run } = useFeedback();
  const { data, refresh } = usePolling(() => api.get<PlayersResponse>("/api/players"), 4000);

  const withRefresh = async (action: () => Promise<unknown>, success: string) => {
    const ok = await run(() => action().then(() => true), success);
    // Console commands are applied asynchronously by the server.
    window.setTimeout(() => void refresh(), 800);
    return ok === true;
  };

  return {
    data,
    act: (action: PlayerAction, player: string, success: string, reason = "") =>
      withRefresh(() => api.post("/api/players/action", { action, player, reason }), success),
    remove: (list: AccessList, uuid: string, success: string) =>
      withRefresh(() => api.post("/api/players/remove", { list, uuid }), success),
    setWhitelist: (enabled: boolean) =>
      withRefresh(() => api.put("/api/players/whitelist", { enabled }), enabled ? "Whitelist activée" : "Whitelist désactivée"),
  };
}

export type PlayersApi = ReturnType<typeof usePlayers>;
