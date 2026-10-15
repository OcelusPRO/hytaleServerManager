import { Eye, Gavel, ListChecks, ShieldCheck, ShieldOff, UserMinus } from "lucide-react";
import { useState } from "react";
import { PageHeader } from "../../components/layout/PageHeader";
import { Alert } from "../../components/ui/Alert";
import { Button } from "../../components/ui/Button";
import { Switch } from "../../components/ui/Field";
import { useFeedback } from "../../components/ui/feedback";
import { formatDate } from "../../lib/format";
import type { BanEntry, OnlinePlayer, PlayerRef } from "../../lib/types";
import { AccessListCard } from "./AccessListCard";
import { BanDetailsDialog } from "./BanDetailsDialog";
import { useStreamOverview } from "../../components/metrics/LiveMetrics";
import { OnlinePlayersCard } from "./OnlinePlayersCard";
import { PlayerDetailsDialog } from "./PlayerDetailsDialog";
import { displayName, PlayerIdentity } from "./PlayerIdentity";
import { usePlayers } from "./usePlayers";
import { translate as t } from "../../lib/i18n";

/** Name when known (commands prefer it), UUID otherwise. */
const target = (p: PlayerRef) => p.name || p.uuid;

export function PlayersPage() {
  const { data, act, remove, setWhitelist } = usePlayers();
  const { confirm, prompt } = useFeedback();
  const [banShown, setBanShown] = useState<BanEntry | null>(null);
  const stream = useStreamOverview();
  const [selected, setSelected] = useState<string | null>(null);
  if (!data) return null;

  const live = data.live;
  // Followed by id: the dialog closes by itself when the player leaves.
  const selectedPlayer = data.online.find((p) => p.uuid === selected);

  const ban = async (player: OnlinePlayer | PlayerRef | string) => {
    const name = typeof player === "string" ? player : target(player);
    const reason = await prompt({
      title: t("players.ban.title", { name }),
      label: t("players.ban.reason"),
      placeholder: t("players.ban.optional"),
      maxLength: 200,
      confirmLabel: t("players.ban.confirm"),
      optional: true,
    });
    if (reason === null) return false;
    return act("ban", name, t("players.ban.done", { name }), reason);
  };

  const kick = async (p: OnlinePlayer) => {
    if (await confirm({ title: t("players.kick.title", { name: p.name }), confirmLabel: t("players.kick.confirm") })) await act("kick", p.name, t("players.kick.done", { name: p.name }));
  };

  const unban = (b: BanEntry) => remove("bans", b.player.uuid, t("players.ban.unbanned", { name: displayName(b.player) }));

  const deop = async (p: PlayerRef) => {
    const ok = await confirm({ title: t("players.op.deopTitle", { name: displayName(p) }), confirmLabel: t("players.op.remove"), danger: true });
    if (ok) await remove("operators", p.uuid, t("players.op.deopped", { name: displayName(p) }));
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t("players.title")} description={t("players.description")} />

      {!live && (
        <Alert tone="info">
          {t("players.stoppedHint")}
        </Alert>
      )}

      <OnlinePlayersCard
        data={data}
        onOp={(p) => void act("op", p.name, t("players.op.done", { name: p.name }))}
        onKick={(p) => void kick(p)}
        onBan={(p) => void ban(p)}
        onSelect={(p) => setSelected(p.uuid)}
      />

      <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-3">
        <AccessListCard
          title={t("players.whitelist.title")}
          description={data.whitelist.enabled ? t("players.whitelist.enabledHint") : t("players.whitelist.disabledHint")}
          icon={ListChecks}
          headerAction={
            <Switch
              label={t("players.whitelist.toggle")}
              checked={data.whitelist.enabled}
              onChange={(enabled) => void setWhitelist(enabled)}
            />
          }
          items={data.whitelist.players}
          itemKey={(p) => p.uuid}
          emptyText={t("players.whitelist.empty")}
          addLabel={t("players.whitelist.add")}
          canAdd={live}
          onAdd={(name) => act("whitelist-add", name, t("players.whitelist.added", { name }))}
          renderItem={(p) => (
            <>
              <PlayerIdentity player={p} />
              <Button
                size="sm"
                variant="ghost"
                icon={UserMinus}
                onClick={() => remove("whitelist", p.uuid, t("players.whitelist.removed", { name: displayName(p) }))}
              >
                {t("players.whitelist.remove")}
              </Button>
            </>
          )}
        />

        <AccessListCard
          title={t("players.bans.title")}
          description={t("players.bans.description")}
          icon={Gavel}
          items={data.bans}
          itemKey={(b) => b.player.uuid}
          emptyText={t("players.bans.empty")}
          addLabel={t("players.bans.add")}
          canAdd={live}
          onAdd={(name) => ban(name)}
          renderItem={(b) => (
            <>
              <PlayerIdentity player={b.player} detail={b.reason || (b.timestamp ? t("players.bans.bannedOn", { date: formatDate(b.timestamp) }) : t("players.bans.noReason"))} />
              <div className="flex shrink-0 items-center gap-1">
                <Button size="sm" variant="ghost" icon={Eye} onClick={() => setBanShown(b)}>
                  {t("players.bans.view")}
                </Button>
                <Button size="sm" variant="ghost" icon={ShieldOff} onClick={() => unban(b)} aria-label={t("players.bans.unbanPlayer", { name: displayName(b.player) })} title={t("players.bans.unban")} />
              </div>
            </>
          )}
        />

        <AccessListCard
          title={t("players.operators.title")}
          description={t("players.operators.description")}
          icon={ShieldCheck}
          items={data.operators}
          itemKey={(p) => p.uuid}
          emptyText={t("players.operators.empty")}
          addLabel={t("players.operators.add")}
          canAdd={live}
          onAdd={(name) => act("op", name, t("players.op.done", { name }))}
          renderItem={(p) => (
            <>
              <PlayerIdentity player={p} />
              <Button size="sm" variant="ghost" icon={UserMinus} onClick={() => deop(p)}>
                {t("players.operators.remove")}
              </Button>
            </>
          )}
        />
      </div>

      {selectedPlayer && <PlayerDetailsDialog key={selectedPlayer.uuid} player={selectedPlayer} roster={data} stream={stream} onClose={() => setSelected(null)} />}

      {banShown && <BanDetailsDialog ban={banShown} onClose={() => setBanShown(null)} onUnban={() => unban(banShown)} />}
    </div>
  );
}
