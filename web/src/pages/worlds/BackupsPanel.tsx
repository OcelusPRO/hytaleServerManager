import { Archive, Download, History, Plus, Trash2 } from "lucide-react";
import { Badge } from "../../components/ui/Badge";
import { Alert } from "../../components/ui/Alert";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { useFeedback } from "../../components/ui/feedback";
import { Table, Td, Th, Tr } from "../../components/ui/Table";
import { api, seg } from "../../lib/api";
import { formatBytes, formatDate } from "../../lib/format";
import type { Backup } from "../../lib/types";
import { translate as t } from "../../lib/i18n";

interface BackupsPanelProps {
  saveId: string;
  backups: Backup[] | undefined;
  inUse: boolean;
  onChange: () => Promise<void>;
}

export function BackupsPanel({ saveId, backups, inUse, onChange }: BackupsPanelProps) {
  const { run, confirm } = useFeedback();
  const base = `/api/saves/${seg(saveId)}/backups`;

  const create = async () => {
    await run(() => api.post(base), t("worlds.backups.created"));
    await onChange();
  };

  const restore = async (b: Backup) => {
    const ok = await confirm({
      title: t("worlds.backups.restoreTitle"),
      message: t("worlds.backups.restoreMessage", { date: formatDate(b.createdAt) }),
      confirmLabel: t("worlds.backups.restore"),
    });
    if (!ok) return;
    await run(() => api.post(`${base}/${seg(b.id)}/restore`), t("worlds.backups.restored"));
    await onChange();
  };

  const remove = async (b: Backup) => {
    const ok = await confirm({ title: t("worlds.backups.deleteTitle"), message: t("worlds.backups.deleteMessage"), confirmLabel: t("worlds.backups.delete"), danger: true });
    if (!ok) return;
    await run(() => api.del(`${base}/${seg(b.id)}`), t("worlds.backups.deleted"));
    await onChange();
  };

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-zinc-400">{t("worlds.backups.description")}</p>
        <Button variant="primary" icon={Plus} onClick={create}>
          {t("worlds.backups.create")}
        </Button>
      </div>
      {inUse && <Alert tone="warning">{t("worlds.backups.inUse")}</Alert>}
      {backups && backups.length === 0 && (
        <EmptyState icon={Archive} title={t("worlds.backups.empty")} description={t("worlds.backups.emptyHint")} />
      )}
      {backups && backups.length > 0 && (
        <div className="-mx-5 -mb-5 border-t border-white/[0.06]">
          <Table>
            <thead>
              <tr>
                <Th>{t("worlds.backups.date")}</Th>
                <Th className="w-28 text-right">{t("worlds.backups.size")}</Th>
                <Th className="w-56" />
              </tr>
            </thead>
            <tbody>
              {backups.map((b) => (
                <Tr key={b.id}>
                  <Td className="font-medium text-zinc-100">
                    <span className="inline-flex items-center gap-2">
                      {formatDate(b.createdAt)}
                      {b.auto && <Badge tone="info">{t("worlds.backups.auto")}</Badge>}
                    </span>
                  </Td>
                  <Td className="text-right tabular-nums">{formatBytes(b.sizeBytes)}</Td>
                  <Td>
                    <div className="flex items-center justify-end gap-1">
                      <a
                        href={`${base}/${seg(b.id)}/download`}
                        download
                        className="inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
                      >
                        <Download className="size-3.5" aria-hidden /> {t("worlds.backups.download")}
                      </a>
                      <Button size="sm" variant="ghost" icon={History} disabled={inUse} onClick={() => restore(b)}>
                        {t("worlds.backups.restore")}
                      </Button>
                      <Button size="sm" variant="ghost" icon={Trash2} onClick={() => remove(b)} aria-label={t("worlds.backups.deleteBackup")} />
                    </div>
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
