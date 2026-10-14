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
    await run(() => api.post(base), "Sauvegarde créée");
    await onChange();
  };

  const restore = async (b: Backup) => {
    const ok = await confirm({
      title: "Restaurer cette sauvegarde ?",
      message: `Le contenu actuel du monde sera remplacé par la sauvegarde du ${formatDate(b.createdAt)}.`,
      confirmLabel: "Restaurer",
    });
    if (!ok) return;
    await run(() => api.post(`${base}/${seg(b.id)}/restore`), "Sauvegarde restaurée");
    await onChange();
  };

  const remove = async (b: Backup) => {
    const ok = await confirm({ title: "Supprimer cette sauvegarde ?", message: "Cette action est définitive.", confirmLabel: "Supprimer", danger: true });
    if (!ok) return;
    await run(() => api.del(`${base}/${seg(b.id)}`), "Sauvegarde supprimée");
    await onChange();
  };

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-zinc-400">Archives complètes du monde, de ses mods et de sa configuration.</p>
        <Button variant="primary" icon={Plus} onClick={create}>
          Créer une sauvegarde
        </Button>
      </div>
      {inUse && <Alert tone="warning">Le serveur tourne sur ce monde : arrêtez-le pour pouvoir restaurer une sauvegarde.</Alert>}
      {backups && backups.length === 0 && (
        <EmptyState icon={Archive} title="Aucune sauvegarde" description="Créez une sauvegarde avant toute modification importante." />
      )}
      {backups && backups.length > 0 && (
        <div className="-mx-5 -mb-5 border-t border-white/[0.06]">
          <Table>
            <thead>
              <tr>
                <Th>Date</Th>
                <Th className="w-28 text-right">Taille</Th>
                <Th className="w-56" />
              </tr>
            </thead>
            <tbody>
              {backups.map((b) => (
                <Tr key={b.id}>
                  <Td className="font-medium text-zinc-100">
                    <span className="inline-flex items-center gap-2">
                      {formatDate(b.createdAt)}
                      {b.auto && <Badge tone="info">Auto</Badge>}
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
                        <Download className="size-3.5" aria-hidden /> Télécharger
                      </a>
                      <Button size="sm" variant="ghost" icon={History} disabled={inUse} onClick={() => restore(b)}>
                        Restaurer
                      </Button>
                      <Button size="sm" variant="ghost" icon={Trash2} onClick={() => remove(b)} aria-label="Supprimer la sauvegarde" />
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
