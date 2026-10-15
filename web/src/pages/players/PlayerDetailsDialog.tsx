import type { ReactNode } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Modal } from "../../components/ui/feedback";
import { usePolling } from "../../hooks/usePolling";
import { api, seg } from "../../lib/api";
import { formatBytes, formatDate, formatDuration, formatSeconds } from "../../lib/format";
import { translate as t } from "../../lib/i18n";
import type { OnlinePlayer, PlayerDetails, PlayersResponse, StreamPlayer, StreamVital } from "../../lib/types";
import { displayName, PlayerIdentity } from "./PlayerIdentity";

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mt-5">
      <h3 className="text-xs font-medium tracking-wide text-zinc-500 uppercase">{title}</h3>
      <dl className="mt-1 divide-y divide-white/[0.05] border-y border-white/[0.06]">{children}</dl>
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-0.5 py-2 sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-3">
      <dt className="text-sm text-zinc-500">{label}</dt>
      <dd className="text-sm break-words text-zinc-200 tabular-nums">{children}</dd>
    </div>
  );
}

const vital = (v: StreamVital) => `${Math.round(v.value)} / ${Math.round(v.max)}`;
const ms = (v: number) => `${Math.round(v)} ms`;
const yesNo = (v: boolean) => (v ? t("players.profile.yes") : t("players.profile.no"));

interface PlayerDetailsDialogProps {
  player: OnlinePlayer;
  /** Roster of the world: operators, whitelist and bans. */
  roster: PlayersResponse;
  /** Live data of the MetricsStream plugin; undefined while the plugin is off. */
  stream?: { players: StreamPlayer[] };
  onClose: () => void;
}

/** Everything known about a player: the manager's records and the live data of the plugin. */
export function PlayerDetailsDialog({ player, roster, stream, onClose }: PlayerDetailsDialogProps) {
  const { data } = usePolling(() => api.get<PlayerDetails>(`/api/players/${seg(player.uuid)}`), 0);
  const key = player.uuid.toLowerCase();
  const isOp = roster.operators.some((o) => o.uuid.toLowerCase() === key);
  const whitelisted = roster.whitelist.players.some((p) => p.uuid.toLowerCase() === key);
  const ban = roster.bans.find((b) => b.player.uuid.toLowerCase() === key);
  const stored = data?.registered;
  const live = stream?.players.find((p) => p.uuid.toLowerCase() === key);

  return (
    <Modal wide label={t("players.profile.title", { name: displayName(player) })} onDismiss={onClose}>
      <PlayerIdentity player={player} size="lg" />

      <Section title={t("players.profile.status")}>
        <Row label={t("players.profile.online")}>
          <Badge tone="success" dot pulse>
            {t("players.profile.online")}
          </Badge>{" "}
          · {t("players.profile.connectedFor")} {formatDuration(player.joinedAt)}
        </Row>
        <Row label={t("players.profile.operator")}>{yesNo(isOp)}</Row>
        <Row label={t("players.profile.whitelisted")}>{yesNo(whitelisted)}</Row>
        <Row label={t("players.profile.banned")}>{ban ? `${t("players.profile.yes")} — ${ban.reason || t("players.details.noReason")}` : t("players.profile.no")}</Row>
      </Section>

      <Section title={t("players.profile.identity")}>
        <Row label={t("players.profile.uuid")}>
          <span className="font-mono text-xs text-zinc-300">{player.uuid}</span>
        </Row>
        <Row label={t("players.profile.previousNames")}>{stored?.previousNames?.length ? stored.previousNames.join(", ") : "—"}</Row>
      </Section>

      <Section title={t("players.profile.stored")}>
        {data && !stored ? (
          <p className="py-3 text-sm text-zinc-500">{t("players.profile.neverSeen")}</p>
        ) : (
          <>
            <Row label={t("players.profile.firstSeen")}>{formatDate(stored?.firstSeen)}</Row>
            <Row label={t("players.profile.lastSeen")}>{formatDate(stored?.lastSeen)}</Row>
            <Row label={t("players.profile.connections")}>{stored?.connectionCount ?? "—"}</Row>
            <Row label={t("players.profile.playtime")}>{stored ? formatSeconds(stored.playtimeSeconds) : "—"}</Row>
          </>
        )}
      </Section>

      <Section title={t("players.profile.banHistory")}>
        {stored?.banHistory?.length ? (
          [...stored.banHistory].reverse().map((b) => (
            <Row key={b.bannedAt} label={formatDate(b.bannedAt)}>
              {b.reason || t("players.details.noReason")}
              <span className="block text-xs text-zinc-500">
                {b.unbannedAt ? `→ ${formatDate(b.unbannedAt)}` : b.expiresOn ? `→ ${formatDate(b.expiresOn)}` : t("players.profile.banCurrent")}
              </span>
            </Row>
          ))
        ) : (
          <p className="py-3 text-sm text-zinc-500">{t("players.profile.noBans")}</p>
        )}
      </Section>

      <Section title={t("players.profile.live")}>
        {!stream ? (
          <p className="py-3 text-sm text-zinc-500">{t("players.profile.liveOff")}</p>
        ) : !live ? (
          <p className="py-3 text-sm text-zinc-500">{t("players.profile.liveWaiting")}</p>
        ) : (
          <>
            <Row label={t("players.profile.world")}>{live.world || "—"}</Row>
            <Row label={t("players.profile.gameMode")}>{live.game_mode || "—"}</Row>
            <Row label={t("players.profile.language")}>{live.language || "—"}</Row>
            <Row label={t("players.profile.pingNow")}>{ms(live.ping_ms.last)}</Row>
            <Row label={t("players.profile.pingAvg")}>{ms(live.ping_ms.avg)}</Row>
            <Row label={t("players.profile.pingRange")}>
              {ms(live.ping_ms.min)} / {ms(live.ping_ms.max)}
            </Row>
            <Row label={t("players.profile.position")}>
              {live.position.x.toFixed(1)}, {live.position.y.toFixed(1)}, {live.position.z.toFixed(1)}
            </Row>
            <Row label={t("players.profile.orientation")}>
              {live.position.yaw.toFixed(1)}° / {live.position.pitch.toFixed(1)}°
            </Row>
            <Row label={t("players.profile.health")}>{vital(live.vitals.health)}</Row>
            <Row label={t("players.profile.stamina")}>{vital(live.vitals.stamina)}</Row>
            <Row label={t("players.profile.mana")}>{vital(live.vitals.mana)}</Row>
            <Row label={t("players.profile.oxygen")}>{vital(live.vitals.oxygen)}</Row>
            <Row label={t("players.profile.sent")}>
              {formatBytes(live.network.bytes_sent)} · {live.network.packets_sent.toLocaleString()} {t("players.profile.packetsSent").toLowerCase()}
            </Row>
            <Row label={t("players.profile.received")}>
              {formatBytes(live.network.bytes_received)} · {live.network.packets_received.toLocaleString()} {t("players.profile.packetsReceived").toLowerCase()}
            </Row>
            <Row label={t("players.profile.queued")}>{live.network.queued_packets}</Row>
          </>
        )}
      </Section>

      <div className="mt-6 flex justify-end">
        <Button variant="ghost" onClick={onClose}>
          {t("players.profile.close")}
        </Button>
      </div>
    </Modal>
  );
}
