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
import { OnlinePlayersCard } from "./OnlinePlayersCard";
import { displayName, PlayerIdentity } from "./PlayerIdentity";
import { usePlayers } from "./usePlayers";

/** Name when known (commands prefer it), UUID otherwise. */
const target = (p: PlayerRef) => p.name || p.uuid;

export function PlayersPage() {
  const { data, act, remove, setWhitelist } = usePlayers();
  const { confirm, prompt } = useFeedback();
  const [banShown, setBanShown] = useState<BanEntry | null>(null);
  if (!data) return null;

  const live = data.live;

  const ban = async (player: OnlinePlayer | PlayerRef | string) => {
    const name = typeof player === "string" ? player : target(player);
    const reason = await prompt({
      title: `Bannir ${name} ?`,
      label: "Raison du bannissement",
      placeholder: "Optionnelle",
      maxLength: 200,
      confirmLabel: "Bannir",
      optional: true,
    });
    if (reason === null) return false;
    return act("ban", name, `${name} a été banni`, reason);
  };

  const kick = async (p: OnlinePlayer) => {
    if (await confirm({ title: `Expulser ${p.name} ?`, confirmLabel: "Expulser" })) await act("kick", p.name, `${p.name} a été expulsé`);
  };

  const unban = (b: BanEntry) => remove("bans", b.player.uuid, `${displayName(b.player)} a été débanni`);

  const deop = async (p: PlayerRef) => {
    const ok = await confirm({ title: `Retirer ${displayName(p)} des opérateurs ?`, confirmLabel: "Retirer", danger: true });
    if (ok) await remove("operators", p.uuid, `${displayName(p)} n'est plus opérateur`);
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Joueurs" description="Joueurs connectés, whitelist, bannissements et opérateurs du monde actif." />

      {!live && (
        <Alert tone="info">
          Le serveur est arrêté : vous pouvez retirer des joueurs des listes, mais les ajouts nécessitent que le serveur soit en ligne.
        </Alert>
      )}

      <OnlinePlayersCard
        data={data}
        onOp={(p) => void act("op", p.name, `${p.name} est maintenant opérateur`)}
        onKick={(p) => void kick(p)}
        onBan={(p) => void ban(p)}
      />

      <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-3">
        <AccessListCard
          title="Whitelist"
          description={data.whitelist.enabled ? "Seuls ces joueurs peuvent rejoindre." : "Désactivée : tout le monde peut rejoindre."}
          icon={ListChecks}
          headerAction={
            <Switch
              label="Activer la whitelist"
              checked={data.whitelist.enabled}
              onChange={(enabled) => void setWhitelist(enabled)}
            />
          }
          items={data.whitelist.players}
          itemKey={(p) => p.uuid}
          emptyText="Aucun joueur dans la whitelist."
          addLabel="Ajouter à la whitelist"
          canAdd={live}
          onAdd={(name) => act("whitelist-add", name, `${name} ajouté à la whitelist`)}
          renderItem={(p) => (
            <>
              <PlayerIdentity player={p} />
              <Button
                size="sm"
                variant="ghost"
                icon={UserMinus}
                onClick={() => remove("whitelist", p.uuid, `${displayName(p)} retiré de la whitelist`)}
              >
                Retirer
              </Button>
            </>
          )}
        />

        <AccessListCard
          title="Bannissements"
          description="Joueurs interdits de connexion."
          icon={Gavel}
          items={data.bans}
          itemKey={(b) => b.player.uuid}
          emptyText="Aucun joueur banni."
          addLabel="Bannir un joueur"
          canAdd={live}
          onAdd={(name) => ban(name)}
          renderItem={(b) => (
            <>
              <PlayerIdentity player={b.player} detail={b.reason || (b.timestamp ? `Banni le ${formatDate(b.timestamp)}` : "Aucune raison")} />
              <div className="flex shrink-0 items-center gap-1">
                <Button size="sm" variant="ghost" icon={Eye} onClick={() => setBanShown(b)}>
                  Voir
                </Button>
                <Button size="sm" variant="ghost" icon={ShieldOff} onClick={() => unban(b)} aria-label={`Débannir ${displayName(b.player)}`} title="Débannir" />
              </div>
            </>
          )}
        />

        <AccessListCard
          title="Opérateurs"
          description="Accès à toutes les commandes du serveur."
          icon={ShieldCheck}
          items={data.operators}
          itemKey={(p) => p.uuid}
          emptyText="Aucun opérateur."
          addLabel="Promouvoir opérateur"
          canAdd={live}
          onAdd={(name) => act("op", name, `${name} est maintenant opérateur`)}
          renderItem={(p) => (
            <>
              <PlayerIdentity player={p} />
              <Button size="sm" variant="ghost" icon={UserMinus} onClick={() => deop(p)}>
                Retirer
              </Button>
            </>
          )}
        />
      </div>

      {banShown && <BanDetailsDialog ban={banShown} onClose={() => setBanShown(null)} onUnban={() => unban(banShown)} />}
    </div>
  );
}
