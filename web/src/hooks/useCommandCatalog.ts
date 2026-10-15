import { useMemo } from "react";
import { useFeedback } from "../components/ui/feedback";
import { api } from "../lib/api";
import type { CommandEntry } from "../lib/commandCompletion";
import { usePolling } from "./usePolling";
import { translate } from "../lib/i18n";

interface CatalogResponse {
  building: boolean;
  entries: CommandEntry[];
}

/**
 * Commands of the server, learnt by the manager after each boot. Polled
 * faster while the catalog is being (re)built.
 */
export function useCommandCatalog() {
  const { run } = useFeedback();
  const { data, refresh } = usePolling(() => api.get<CatalogResponse>("/api/console/commands"), 5000);
  const entries = useMemo(() => data?.entries ?? [], [data]);
  const commandCount = useMemo(() => new Set(entries.map((e) => e.command.split(" ")[0])).size, [entries]);

  return {
    entries,
    commandCount,
    building: data?.building ?? false,
    rebuild: async () => {
      await run(() => api.post("/api/console/commands/refresh"), translate("console.refreshStarted"));
      await refresh();
    },
  };
}
