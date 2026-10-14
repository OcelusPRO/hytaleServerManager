import { FileArchive, Globe, Plus } from "lucide-react";
import { useRef, useState } from "react";
import { PageHeader } from "../../components/layout/PageHeader";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { EmptyState } from "../../components/ui/EmptyState";
import { useFeedback } from "../../components/ui/feedback";
import { Progress } from "../../components/ui/Progress";
import { usePolling } from "../../hooks/usePolling";
import { useStatus } from "../../hooks/useStatus";
import { api, seg } from "../../lib/api";
import { cn } from "../../lib/cn";
import { formatBytes } from "../../lib/format";
import type { Save, SavesResponse } from "../../lib/types";
import { WorldDetail } from "./WorldDetail";

export function WorldsPage() {
  const { run, prompt } = useFeedback();
  const { status } = useStatus();
  const { data, refresh } = usePolling(() => api.get<SavesResponse>("/api/saves"), 10000);
  const [selectedId, setSelectedId] = useState<string>();
  const [importProgress, setImportProgress] = useState<number | null>(null);
  const importInput = useRef<HTMLInputElement>(null);

  const saves = data?.saves ?? [];
  const selected: Save | undefined =
    saves.find((s) => s.id === selectedId) ?? saves.find((s) => s.id === data?.activeSaveId) ?? saves[0];

  const create = async () => {
    const name = await prompt({ title: "Nouveau monde", label: "Nom du monde", maxLength: 64, confirmLabel: "Créer" });
    if (!name) return;
    const save = await run(() => api.post<Save>("/api/saves", { name }), "Monde créé");
    await refresh();
    if (save) setSelectedId(save.id);
  };

  const importWorld = async (file: File) => {
    const name = await prompt({
      title: "Importer un monde",
      label: "Nom du monde importé",
      defaultValue: file.name.replace(/\.zip$/i, ""),
      maxLength: 64,
      confirmLabel: "Importer",
    });
    if (importInput.current) importInput.current.value = "";
    if (!name) return;
    setImportProgress(0);
    const save = await run(
      () => api.upload<Save>(`/api/saves/import?name=${seg(name)}`, file, setImportProgress),
      "Monde importé",
    );
    setImportProgress(null);
    await refresh();
    if (save) setSelectedId(save.id);
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Mondes"
        description="Chaque monde possède ses propres mods, joueurs et sauvegardes."
        actions={
          <>
            <Button icon={FileArchive} onClick={() => importInput.current?.click()} disabled={importProgress !== null}>
              Importer (.zip)
            </Button>
            <Button variant="primary" icon={Plus} onClick={create}>
              Nouveau monde
            </Button>
          </>
        }
      />
      <input
        ref={importInput}
        type="file"
        accept=".zip"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) void importWorld(file);
        }}
      />
      {importProgress !== null && (
        <Card className="p-4">
          <p className="text-sm text-zinc-300">Import du monde en cours…</p>
          <Progress value={importProgress >= 1 ? -1 : importProgress} className="mt-2" />
        </Card>
      )}

      {data && saves.length === 0 ? (
        <Card>
          <EmptyState
            icon={Globe}
            title="Aucun monde"
            description="Créez un monde : il sera généré au premier démarrage du serveur."
            action={
              <Button variant="primary" icon={Plus} onClick={create}>
                Nouveau monde
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[18rem_minmax(0,1fr)]">
          <Card className="p-2">
            <ul className="flex flex-col gap-1">
              {saves.map((save) => {
                const isSelected = save.id === selected?.id;
                return (
                  <li key={save.id}>
                    <button
                      type="button"
                      onClick={() => setSelectedId(save.id)}
                      className={cn(
                        "flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors",
                        isSelected ? "bg-white/[0.06]" : "hover:bg-white/[0.03]",
                      )}
                    >
                      <span
                        className={cn(
                          "flex size-8 shrink-0 items-center justify-center rounded-lg ring-1",
                          isSelected ? "bg-brand-500/10 text-brand-400 ring-brand-500/25" : "bg-zinc-800 text-zinc-500 ring-white/5",
                        )}
                      >
                        <Globe className="size-4" aria-hidden />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium text-zinc-100">{save.name}</span>
                        <span className="block text-xs text-zinc-500">{formatBytes(save.sizeBytes)}</span>
                      </span>
                      {save.id === data?.runningSaveId ? (
                        <Badge tone="success" dot pulse>
                          En ligne
                        </Badge>
                      ) : save.id === data?.activeSaveId ? (
                        <Badge tone="brand">Actif</Badge>
                      ) : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          </Card>
          {selected && (
            <WorldDetail
              key={selected.id}
              save={selected}
              active={selected.id === data?.activeSaveId}
              inUse={selected.id === data?.runningSaveId}
              serverRunning={status?.server.state === "running"}
              onChange={refresh}
            />
          )}
        </div>
      )}
    </div>
  );
}
