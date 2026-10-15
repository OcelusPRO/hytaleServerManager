import { ShieldOff } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "../../components/ui/Button";
import { Modal } from "../../components/ui/feedback";
import { formatDate } from "../../lib/format";
import type { BanEntry } from "../../lib/types";
import { displayName, PlayerIdentity } from "./PlayerIdentity";
import { translate as t } from "../../lib/i18n";

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-0.5 py-2.5 sm:grid-cols-[8rem_minmax(0,1fr)] sm:gap-3">
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
    <Modal label={t("players.details.title", { name: displayName(ban.player) })} onDismiss={onClose}>
      <PlayerIdentity player={ban.player} size="lg" />
      <dl className="mt-5 divide-y divide-white/[0.05] border-y border-white/[0.06]">
        <Detail label={t("players.details.reason")}>{ban.reason || <span className="text-zinc-500">{t("players.details.noReason")}</span>}</Detail>
        <Detail label={t("players.details.by")}>{ban.by ? displayName(ban.by) : "—"}</Detail>
        <Detail label={t("players.details.date")}>{formatDate(ban.timestamp)}</Detail>
        <Detail label={t("players.details.expires")}>{ban.expiresOn ? formatDate(ban.expiresOn) : t("players.details.permanent")}</Detail>
        <Detail label="UUID">
          <span className="font-mono text-xs text-zinc-400">{ban.player.uuid}</span>
        </Detail>
      </dl>
      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          {t("players.details.close")}
        </Button>
        <Button
          variant="primary"
          icon={ShieldOff}
          onClick={async () => {
            await onUnban();
            onClose();
          }}
        >
          {t("players.details.unban")}
        </Button>
      </div>
    </Modal>
  );
}
