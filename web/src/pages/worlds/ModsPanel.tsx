import { ArrowUpCircle, Puzzle, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Alert } from "../../components/ui/Alert";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { Checkbox } from "../../components/ui/Field";
import { FileDrop } from "../../components/ui/FileDrop";
import { useFeedback } from "../../components/ui/feedback";
import { Table, Td, Th, Tr } from "../../components/ui/Table";
import { api, seg } from "../../lib/api";
import { formatBytes, formatDate } from "../../lib/format";
import type { CurseForgeJob, CurseForgeUpdate, Mod } from "../../lib/types";
import { translate as t, translatePlural as tp } from "../../lib/i18n";

interface ModsPanelProps {
  saveId: string;
  mods: Mod[] | undefined;
  inUse: boolean;
  onChange: () => Promise<void>;
  onJob: (jobId: string) => void;
  /** Changes whenever a CurseForge operation has finished, to recheck updates. */
  jobVersion: number;
}

export function ModsPanel({ saveId, mods, inUse, onChange, onJob, jobVersion }: ModsPanelProps) {
  const { run, confirm } = useFeedback();
  const base = `/api/saves/${seg(saveId)}/mods`;
  const [updates, setUpdates] = useState<Map<string, CurseForgeUpdate>>(new Map());
  const tracked = mods?.some((m) => m.curseforge) ?? false;

  // Silent check: without a key or network the list is simply empty.
  useEffect(() => {
    if (!tracked) {
      setUpdates(new Map());
      return;
    }
    let cancelled = false;
    api
      .get<CurseForgeUpdate[]>(`/api/saves/${seg(saveId)}/curseforge/updates`)
      .then((list) => !cancelled && setUpdates(new Map(list.map((u) => [u.fileName, u]))))
      .catch(() => !cancelled && setUpdates(new Map()));
    return () => {
      cancelled = true;
    };
  }, [saveId, tracked, jobVersion]);

  const update = async (fileNames: string[]) => {
    const job = await run(() => api.post<CurseForgeJob>(`/api/saves/${seg(saveId)}/curseforge/updates`, { fileNames }));
    if (job) onJob(job.id);
  };

  const toggle = async (mod: Mod, enabled: boolean) => {
    await run(() => api.patch(`${base}/${seg(mod.fileName)}`, { enabled }));
    await onChange();
  };

  const remove = async (mod: Mod) => {
    const ok = await confirm({ title: t("worlds.mods.deleteTitle", { name: mod.fileName }), confirmLabel: t("worlds.mods.delete"), danger: true });
    if (!ok) return;
    await run(() => api.del(`${base}/${seg(mod.fileName)}`), t("worlds.mods.deleted"));
    await onChange();
  };

  const upload = async (file: File, onProgress: (f: number) => void) => {
    await run(() => api.upload(base, file, onProgress), t("worlds.mods.added", { name: file.name }));
    await onChange();
  };

  return (
    <div className="flex flex-col gap-4 p-5">
      {inUse && <Alert tone="info">{t("worlds.mods.inUse")}</Alert>}
      {updates.size > 0 && (
        <Alert
          tone="info"
          title={tp("worlds.mods.updates", updates.size)}
          action={
            <Button size="sm" variant="primary" icon={ArrowUpCircle} onClick={() => update([])}>
              {t("worlds.mods.updateAll")}
            </Button>
          }
        />
      )}
      <FileDrop accept=".jar,.zip" multiple title={t("worlds.mods.add")} hint={t("worlds.mods.addHint")} onFile={upload} />
      {mods && mods.length === 0 && (
        <EmptyState icon={Puzzle} title={t("worlds.mods.empty")} description={t("worlds.mods.emptyHint")} />
      )}
      {mods && mods.length > 0 && (
        <div className="-mx-5 -mb-5 border-t border-white/[0.06]">
          <Table>
            <thead>
              <tr>
                <Th className="w-12">{t("worlds.mods.enabled")}</Th>
                <Th>{t("worlds.mods.file")}</Th>
                <Th className="w-28 text-right">{t("worlds.mods.size")}</Th>
                <Th className="w-44 max-md:hidden">{t("worlds.mods.added_on")}</Th>
                <Th className="w-40" />
              </tr>
            </thead>
            <tbody>
              {mods.map((mod) => (
                <Tr key={mod.fileName}>
                  <Td>
                    <Checkbox checked={mod.enabled} onChange={(v) => toggle(mod, v)} label={t("worlds.mods.enable", { name: mod.fileName })} />
                  </Td>
                  <Td>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={mod.enabled ? "font-medium text-zinc-100" : "text-zinc-500 line-through"}>{mod.fileName}</span>
                      {mod.curseforge && (
                        <span title={mod.curseforge.pack ? t("worlds.mods.modpack", { name: mod.curseforge.pack }) : undefined}>
                          <Badge tone="brand" className="h-5">
                            CurseForge
                          </Badge>
                        </span>
                      )}
                    </div>
                  </Td>
                  <Td className="text-right tabular-nums">{formatBytes(mod.sizeBytes)}</Td>
                  <Td className="text-zinc-400 max-md:hidden">{formatDate(mod.modifiedAt)}</Td>
                  <Td className="text-right whitespace-nowrap">
                    {updates.has(mod.fileName) && (
                      <Button size="sm" icon={ArrowUpCircle} onClick={() => update([mod.fileName])} title={t("worlds.mods.updateTo", { version: updates.get(mod.fileName)?.latest.displayName ?? "" })}>
                        {t("worlds.mods.update")}
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" icon={Trash2} onClick={() => remove(mod)} aria-label={t("worlds.mods.deleteMod", { name: mod.fileName })} />
                  </Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
    </div>
  );
}
