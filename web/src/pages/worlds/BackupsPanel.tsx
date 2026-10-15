import { Archive, Download, History, Pencil, Plus, Trash2 } from "lucide-react";
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
  const { run, confirm, prompt } = useFeedback();
  const base = `/api/saves/${seg(saveId)}/backups`;

  const create = async () => {
    const name = await prompt({ title: t("worlds.backups.create"), label: t("worlds.backups.nameLabel"), placeholder: t("worlds.backups.namePlaceholder"), maxLength: 80, optional: true, confirmLabel: t("worlds.backups.create") });
    if (name === null) return;
    const note = await prompt({ title: t("worlds.backups.create"), label: t("worlds.backups.noteLabel"), maxLength: 500, optional: true, confirmLabel: t("worlds.backups.create") });
    if (note === null) return;
    await run(() => api.post(base, { name, note }), t("worlds.backups.created"));
    await onChange();
  };

  const rename = async (b: Backup) => {
    const name = await prompt({ title: t("worlds.backups.renameTitle"), label: t("worlds.backups.nameLabel"), defaultValue: b.name, maxLength: 80, confirmLabel: t("worlds.backups.rename") });
    if (name === null) return;
    const note = await prompt({ title: t("worlds.backups.renameTitle"), label: t("worlds.backups.noteLabel"), defaultValue: b.note ?? "", maxLength: 500, optional: true, confirmLabel: t("worlds.backups.rename") });
    if (note === null) return;
    await run(() => api.patch(`${base}/${seg(b.id)}`, { name, note }), t("worlds.backups.renamed"));
    await onChange();
  };

  const label = (b: Backup) => b.name || formatDate(b.createdAt);

  const origin = (b: Backup) => {
    switch (b.origin) {
      case "autosave":
        return t("worlds.backups.originAutosave", { slot: b.slot ?? "?" });
      case "restore": {
        const source = backups?.find((x) => x.id === b.detail);
        return t("worlds.backups.originRestore", { name: source ? label(source) : (b.detail ?? "") });
      }
      case "update":
        return t("worlds.backups.originUpdate");
      default:
        return t("worlds.backups.originManual");
    }
  };

  const restore = async (b: Backup) => {
    const ok = await confirm({
      title: t("worlds.backups.restoreTitle"),
      message: t("worlds.backups.restoreMessage", { date: label(b) }),
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
                <Th>{t("worlds.backups.name")}</Th>
                <Th>{t("worlds.backups.date")}</Th>
                <Th className="w-28 text-right">{t("worlds.backups.size")}</Th>
                <Th className="w-56" />
              </tr>
            </thead>
            <tbody>
              {backups.map((b) => (
                <Tr key={b.id}>
                  <Td className="font-medium text-zinc-100">
                    <span className="inline-flex flex-wrap items-center gap-2">
                      {b.name || t("worlds.backups.unnamed")}
                      <Badge tone={b.auto ? "info" : "neutral"}>{b.auto ? t("worlds.backups.auto") : t("worlds.backups.manual")}</Badge>
                    </span>
                    <span className="block text-xs font-normal text-zinc-500">{origin(b)}</span>
                    {b.note && <span className="block max-w-md text-xs font-normal whitespace-pre-line text-zinc-400">{b.note}</span>}
                  </Td>
                  <Td className="whitespace-nowrap">{formatDate(b.createdAt)}</Td>
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
                      <Button size="sm" variant="ghost" icon={Pencil} onClick={() => rename(b)} aria-label={t("worlds.backups.rename")} />
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
