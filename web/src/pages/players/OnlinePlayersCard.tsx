import { Ban, Crown, LogOut, Search, Users } from "lucide-react";
import { useState, type MouseEvent } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card, CardHeader } from "../../components/ui/Card";
import { EmptyState } from "../../components/ui/EmptyState";
import { Input } from "../../components/ui/Field";
import { Pagination, usePagination } from "../../components/ui/Pagination";
import { Table, Td, Th, Tr } from "../../components/ui/Table";
import { formatDuration } from "../../lib/format";
import type { OnlinePlayer, PlayersResponse } from "../../lib/types";
import { PlayerIdentity } from "./PlayerIdentity";
import { translate as t, translatePlural as tp } from "../../lib/i18n";

const PAGE_SIZE = 50;

interface OnlinePlayersCardProps {
  data: PlayersResponse;
  onOp: (player: OnlinePlayer) => void;
  onKick: (player: OnlinePlayer) => void;
  onBan: (player: OnlinePlayer) => void;
  /** Opens the profile of the player (click on the row). */
  onSelect: (player: OnlinePlayer) => void;
}

/** Online players, kept basic: the details are in the profile dialog. */
export function OnlinePlayersCard({ data, onOp, onKick, onBan, onSelect }: OnlinePlayersCardProps) {
  const operators = new Set(data.operators.map((o) => o.uuid.toLowerCase()));
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const matching = needle ? data.online.filter((p) => p.name.toLowerCase().includes(needle) || p.uuid.toLowerCase().includes(needle)) : data.online;
  const paging = usePagination(matching, PAGE_SIZE, needle);

  // Row buttons must not open the profile.
  const stop = (action: () => void) => (event: MouseEvent) => {
    event.stopPropagation();
    action();
  };

  return (
    <Card>
      <CardHeader
        title={t("players.online.title")}
        description={data.live ? tp("players.online.count", data.online.length) : t("players.online.stopped")}
        actions={
          data.online.length > 0 && (
            <div className="relative w-64 max-w-full">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-500" aria-hidden />
              <Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("players.online.search")} aria-label={t("players.online.search")} className="pl-9" />
            </div>
          )
        }
      />
      {data.online.length === 0 ? (
        <EmptyState icon={Users} title={t("players.online.empty")} />
      ) : matching.length === 0 ? (
        <p className="mt-4 border-t border-white/[0.06] px-5 py-8 text-center text-sm text-zinc-500">{t("players.online.noMatch")}</p>
      ) : (
        <div className="mt-4 border-t border-white/[0.06]">
          <Table>
            <thead>
              <tr>
                <Th>{t("players.online.player")}</Th>
                <Th className="w-44">{t("players.online.since")}</Th>
                <Th className="w-80" />
              </tr>
            </thead>
            <tbody>
              {paging.items.map((p) => {
                const isOp = operators.has(p.uuid.toLowerCase());
                return (
                  <Tr
                    key={p.uuid}
                    className="cursor-pointer"
                    tabIndex={0}
                    onClick={() => onSelect(p)}
                    onKeyDown={(e) => e.key === "Enter" && e.target === e.currentTarget && onSelect(p)}
                  >
                    <Td className="max-sm:w-full">
                      <div className="flex items-center gap-3">
                        <PlayerIdentity player={p} />
                        {isOp && <Badge tone="brand">{t("players.online.operator")}</Badge>}
                      </div>
                    </Td>
                    <Td data-label={t("players.online.since")} className="tabular-nums">
                      {formatDuration(p.joinedAt)}
                    </Td>
                    <Td className="max-sm:ml-auto">
                      <div className="flex items-center justify-end gap-1">
                        {!isOp && (
                          <Button size="sm" variant="ghost" icon={Crown} onClick={stop(() => onOp(p))}>
                            {t("players.online.op")}
                          </Button>
                        )}
                        <Button size="sm" variant="ghost" icon={LogOut} onClick={stop(() => onKick(p))}>
                          {t("players.online.kick")}
                        </Button>
                        <Button size="sm" variant="ghost" icon={Ban} onClick={stop(() => onBan(p))} className="hover:text-red-400">
                          {t("players.online.ban")}
                        </Button>
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
