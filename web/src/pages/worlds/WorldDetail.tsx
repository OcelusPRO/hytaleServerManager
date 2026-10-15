import { Pencil, Star, Trash2 } from "lucide-react";
import { useCallback, useState } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { useFeedback } from "../../components/ui/feedback";
import { JobCard } from "../../components/curseforge/JobCard";
import { Tabs } from "../../components/ui/Tabs";
import { usePolling } from "../../hooks/usePolling";
import { api, seg } from "../../lib/api";
import { formatBytes, formatDate } from "../../lib/format";
import type { Backup, Mod, Save } from "../../lib/types";
import { BackupsPanel } from "./BackupsPanel";
import { ConfigPanel } from "./ConfigPanel";
import { CurseForgePanel } from "./CurseForgePanel";
import { ModsPanel } from "./ModsPanel";
import { translate as t } from "../../lib/i18n";

interface WorldDetailProps {
  save: Save;
  active: boolean;
  inUse: boolean;
  serverRunning: boolean;
  onChange: () => Promise<void>;
}

type Tab = "mods" | "curseforge" | "config" | "backups";

export function WorldDetail({ save, active, inUse, serverRunning, onChange }: WorldDetailProps) {
  const { run, confirm, prompt } = useFeedback();
  const [tab, setTab] = useState<Tab>("mods");
  const [jobId, setJobId] = useState<string>();
  const [jobVersion, setJobVersion] = useState(0);
  const base = `/api/saves/${seg(save.id)}`;
  const mods = usePolling(() => api.get<Mod[]>(`${base}/mods`), 0);
  const backups = usePolling(() => api.get<Backup[]>(`${base}/backups`), 0);

  const refreshMods = mods.refresh;
  const jobFinished = useCallback(() => {
    setJobVersion((v) => v + 1);
    void refreshMods();
  }, [refreshMods]);

  const activate = async () => {
    await run(
      () => api.post(`${base}/activate`),
      serverRunning ? t("worlds.detail.activatedRestart") : t("worlds.detail.activated"),
    );
    await onChange();
  };

  const rename = async () => {
    const name = await prompt({ title: t("worlds.detail.renameTitle"), label: t("worlds.detail.name"), defaultValue: save.name, maxLength: 64, confirmLabel: t("worlds.detail.rename") });
    if (!name) return;
    await run(() => api.patch(base, { name }), t("worlds.detail.renamed"));
    await onChange();
  };

  const remove = async () => {
    const ok = await confirm({
      title: t("worlds.detail.deleteTitle", { name: save.name }),
      message: t("worlds.detail.deleteMessage"),
      confirmLabel: t("worlds.detail.delete"),
      danger: true,
    });
    if (!ok) return;
    await run(() => api.del(base), t("worlds.detail.deleted"));
    await onChange();
  };

  return (
    <Card className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-4 p-5">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold text-zinc-50">{save.name}</h2>
            {active && <Badge tone="brand">{t("worlds.detail.active")}</Badge>}
            {inUse && (
              <Badge tone="success" dot pulse>
                {t("worlds.detail.inUse")}
              </Badge>
            )}
          </div>
          <p className="mt-1 text-sm text-zinc-500">
            {t("worlds.detail.created", { date: formatDate(save.createdAt), size: formatBytes(save.sizeBytes) })}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {!active && (
            <Button variant="primary" icon={Star} onClick={activate}>
              {t("worlds.detail.activate")}
            </Button>
          )}
          <Button icon={Pencil} onClick={rename}>
            {t("worlds.detail.rename")}
          </Button>
          <Button variant="danger" icon={Trash2} disabled={inUse} onClick={remove} aria-label={t("worlds.detail.deleteWorld")} title={t("worlds.detail.deleteWorld")} />
        </div>
      </div>
      <Tabs
        value={tab}
        onChange={setTab}
        items={[
          { value: "mods", label: t("worlds.detail.tabMods"), count: mods.data?.length },
          { value: "curseforge", label: t("worlds.detail.tabCurseForge") },
          { value: "config", label: t("worlds.detail.tabConfig") },
          { value: "backups", label: t("worlds.detail.tabBackups"), count: backups.data?.length },
        ]}
      />
      {jobId && (
        <div className="pt-4">
          <JobCard key={jobId} jobId={jobId} onFinished={jobFinished} onDismiss={() => setJobId(undefined)} />
        </div>
      )}
      {tab === "mods" ? (
        <ModsPanel saveId={save.id} mods={mods.data} inUse={inUse} onChange={mods.refresh} onJob={setJobId} jobVersion={jobVersion} />
      ) : tab === "curseforge" ? (
        <CurseForgePanel
          saveId={save.id}
          installed={new Set(mods.data?.flatMap((m) => (m.curseforge ? [m.curseforge.projectId] : [])))}
          onJob={setJobId}
          onChange={mods.refresh}
        />
      ) : tab === "config" ? (
        <ConfigPanel saveId={save.id} inUse={inUse} />
      ) : (
        <BackupsPanel saveId={save.id} backups={backups.data} inUse={inUse} onChange={backups.refresh} />
      )}
    </Card>
  );
}
