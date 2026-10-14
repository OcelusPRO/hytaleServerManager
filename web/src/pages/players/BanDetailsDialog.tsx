import { ShieldOff } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "../../components/ui/Button";
import { Modal } from "../../components/ui/feedback";
import { formatDate } from "../../lib/format";
import type { BanEntry } from "../../lib/types";
import { displayName, PlayerIdentity } from "./PlayerIdentity";

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[8rem_minmax(0,1fr)] gap-3 py-2.5">
      <dt className="text-sm text-zinc-500">{label}</dt>
      <dd className="text-sm break-words text-zinc-200">{children}</dd>
    </div>
  );
}

interface BanDetailsDialogProps {
  ban: BanEntry;
  onClose: () => void;
  onUnban: () => Promise<unknown>;
}

/** Profile of a banned player with the reason and the unban action. */
export function BanDetailsDialog({ ban, onClose, onUnban }: BanDetailsDialogProps) {
  return (
    <Modal label={`Bannissement de ${displayName(ban.player)}`} onDismiss={onClose}>
      <PlayerIdentity player={ban.player} size="lg" />
      <dl className="mt-5 divide-y divide-white/[0.05] border-y border-white/[0.06]">
        <Detail label="Raison">{ban.reason || <span className="text-zinc-500">Aucune raison indiquée</span>}</Detail>
        <Detail label="Banni par">{ban.by ? displayName(ban.by) : "—"}</Detail>
        <Detail label="Date">{formatDate(ban.timestamp)}</Detail>
        <Detail label="Expiration">{ban.expiresOn ? formatDate(ban.expiresOn) : "Permanent"}</Detail>
        <Detail label="UUID">
          <span className="font-mono text-xs text-zinc-400">{ban.player.uuid}</span>
        </Detail>
      </dl>
      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Fermer
        </Button>
        <Button
          variant="primary"
          icon={ShieldOff}
          onClick={async () => {
            await onUnban();
            onClose();
          }}
        >
          Débannir
        </Button>
      </div>
    </Modal>
  );
}
