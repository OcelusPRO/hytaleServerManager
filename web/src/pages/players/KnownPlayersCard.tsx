import { Ban, Crown, History, ListPlus, ListX, LogOut, Search, ShieldOff, UserMinus } from "lucide-react";
import { useState, type MouseEvent } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card, CardHeader } from "../../components/ui/Card";
import { EmptyState } from "../../components/ui/EmptyState";
import { Input } from "../../components/ui/Field";
import { Pagination, usePagination } from "../../components/ui/Pagination";
import { Table, Td, Th, Tr } from "../../components/ui/Table";
import { formatDate, formatSeconds } from "../../lib/format";
import { translate as t, translatePlural as tp } from "../../lib/i18n";
import type { KnownPlayer, PlayersResponse } from "../../lib/types";
import { PlayerIdentity } from "./PlayerIdentity";

const PAGE_SIZE = 25;

/** What can be done to a known player, wired by the page. */
export interface KnownPlayerActions {
  onSelect: (player: KnownPlayer) => void;
  onKick: (player: KnownPlayer) => void;
  onBan: (player: KnownPlayer) => void;
  onUnban: (player: KnownPlayer) => void;
  onOp: (player: KnownPlayer) => void;
  onDeop: (player: KnownPlayer) => void;
  onWhitelistAdd: (player: KnownPlayer) => void;
  onWhitelistRemove: (player: KnownPlayer) => void;
}

interface KnownPlayersCardProps extends KnownPlayerActions {
  players: KnownPlayer[];
  roster: PlayersResponse;
}

/**
 * Every player the manager has seen, connected or not. While the server is
 * stopped, the actions change the shared lists and the world receives them
 * at its next boot; only a kick needs the player online.
 */
export function KnownPlayersCard({ players, roster, ...on }: KnownPlayersCardProps) {
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const matching = needle ? players.filter((p) => p.name.toLowerCase().includes(needle) || p.uuid.includes(needle)) : players;
  const paging = usePagination(matching, PAGE_SIZE, needle);

  const key = (uuid: string) => uuid.toLowerCase();
  const online = new Set(roster.online.map((p) => key(p.uuid)));
  const operators = new Set(roster.operators.map((p) => key(p.uuid)));
  const whitelisted = new Set(roster.whitelist.players.map((p) => key(p.uuid)));
  const banned = new Set(roster.bans.map((b) => key(b.player.uuid)));
  const waiting = roster.pending;
  const pending = new Set([...(waiting?.bans ?? []), ...(waiting?.operators ?? []), ...(waiting?.whitelist ?? [])].map(key));

  // Row buttons must not open the profile.
  const stop = (action: () => void) => (event: MouseEvent) => {
    event.stopPropagation();
    action();
  };

  return (
    <Card>
      <CardHeader
        title={t("players.known.title")}
        description={tp("players.known.count", players.length)}
        actions={
          players.length > 0 && (
            <div className="relative w-64 max-w-full">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-500" aria-hidden />
              <Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("players.online.search")} aria-label={t("players.online.search")} className="pl-9" />
            </div>
          )
        }
      />
      {players.length === 0 ? (
        <EmptyState icon={History} title={t("players.known.empty")} description={t("players.known.emptyHint")} />
      ) : matching.length === 0 ? (
        <p className="mt-4 border-t border-white/[0.06] px-5 py-8 text-center text-sm text-zinc-500">{t("players.online.noMatch")}</p>
      ) : (
        <div className="mt-4 border-t border-white/[0.06]">
          <Table>
            <thead>
              <tr>
                <Th>{t("players.online.player")}</Th>
                <Th className="w-48">{t("players.known.lastSeen")}</Th>
                <Th className="w-32">{t("players.known.playtime")}</Th>
                <Th className="w-72" />
              </tr>
            </thead>
            <tbody>
              {paging.items.map((p) => {
                const k = key(p.uuid);
                const isOnline = online.has(k);
                const isOp = operators.has(k);
                const isListed = whitelisted.has(k);
                const isBanned = banned.has(k);
                return (
                  <Tr
                    key={p.uuid}
                    className="cursor-pointer"
                    tabIndex={0}
                    onClick={() => on.onSelect(p)}
                    onKeyDown={(e) => e.key === "Enter" && e.target === e.currentTarget && on.onSelect(p)}
                  >
                    <Td className="max-sm:w-full">
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                        <PlayerIdentity player={p} />
                        <div className="flex flex-wrap gap-1">
                          {isOnline && (
                            <Badge tone="success" dot>
                              {t("players.profile.online")}
                            </Badge>
                          )}
                          {isOp && <Badge tone="brand">{t("players.online.operator")}</Badge>}
                          {isListed && <Badge tone="info">{t("players.known.whitelisted")}</Badge>}
                          {isBanned && <Badge tone="danger">{t("players.known.banned")}</Badge>}
                          {pending.has(k) && <Badge tone="warning">{t("players.pending")}</Badge>}
                        </div>
                      </div>
                    </Td>
                    <Td data-label={t("players.known.lastSeen")} className="tabular-nums">
                      {isOnline ? t("players.known.now") : formatDate(p.lastSeen)}
                    </Td>
                    <Td data-label={t("players.known.playtime")} className="tabular-nums">
                      {p.playtimeSeconds > 0 ? formatSeconds(p.playtimeSeconds) : "—"}
                    </Td>
                    <Td className="max-sm:ml-auto">
                      {/* Clicks between or on (even disabled) buttons must not open the profile. */}
                      <div className="flex items-center justify-end gap-1" onClick={(e) => e.stopPropagation()}>
                        {isOp ? (
                          <Button size="sm" variant="ghost" icon={UserMinus} title={t("players.known.deop")} aria-label={t("players.known.deop")} onClick={stop(() => on.onDeop(p))} />
                        ) : (
                          <Button size="sm" variant="ghost" icon={Crown} title={t("players.known.op")} aria-label={t("players.known.op")} onClick={stop(() => on.onOp(p))} />
                        )}
                        {isListed ? (
                          <Button size="sm" variant="ghost" icon={ListX} title={t("players.known.whitelistRemove")} aria-label={t("players.known.whitelistRemove")} onClick={stop(() => on.onWhitelistRemove(p))} />
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            icon={ListPlus}
                            title={t("players.known.whitelistAdd")}
                            aria-label={t("players.known.whitelistAdd")}
                            onClick={stop(() => on.onWhitelistAdd(p))}
                          />
                        )}
                        {isOnline && (
                          <Button size="sm" variant="ghost" icon={LogOut} title={t("players.online.kick")} aria-label={t("players.online.kick")} onClick={stop(() => on.onKick(p))} />
                        )}
                        {isBanned ? (
                          <Button size="sm" variant="ghost" icon={ShieldOff} onClick={stop(() => on.onUnban(p))}>
                            {t("players.bans.unban")}
                          </Button>
                        ) : (
                          <Button size="sm" variant="ghost" icon={Ban} onClick={stop(() => on.onBan(p))} className="hover:text-red-400">
                            {t("players.online.ban")}
                          </Button>
                        )}
                      </div>
                    </Td>
                  </Tr>
                );
              })}
            </tbody>
          </Table>
          <Pagination {...paging} onChange={paging.setPage} />
        </div>
      )}
    </Card>
  );
}
