import { useFeedback } from "../../components/ui/feedback";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import type { AccessList, KnownPlayer, PlayerAction, PlayersResponse } from "../../lib/types";
import { translate as t } from "../../lib/i18n";

/** Player data of the active world and the actions that modify it. */
export function usePlayers() {
  const { run } = useFeedback();
  const { data, refresh } = usePolling(() => api.get<PlayersResponse>("/api/players"), 4000);
  const { data: known, refresh: refreshKnown } = usePolling(() => api.get<KnownPlayer[]>("/api/players/known"), 15000);

  const withRefresh = async (action: () => Promise<unknown>, success: string) => {
    const ok = await run(() => action().then(() => true), success);
    // Console commands are applied asynchronously by the server.
    window.setTimeout(() => void Promise.all([refresh(), refreshKnown()]), 800);
    return ok === true;
  };

  return {
    data,
    known,
    act: (action: PlayerAction, player: string, success: string, reason = "") =>
      withRefresh(() => api.post("/api/players/action", { action, player, reason }), success),
    remove: (list: AccessList, uuid: string, success: string) =>
      withRefresh(() => api.post("/api/players/remove", { list, uuid }), success),
    setWhitelist: (enabled: boolean) =>
      withRefresh(() => api.put("/api/players/whitelist", { enabled }), enabled ? t("players.whitelist.enabled") : t("players.whitelist.disabled")),
  };
}

export type PlayersApi = ReturnType<typeof usePlayers>;
