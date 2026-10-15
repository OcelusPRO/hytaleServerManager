import { Ban, Crown, LogOut, Users } from "lucide-react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card, CardHeader } from "../../components/ui/Card";
import { EmptyState } from "../../components/ui/EmptyState";
import { Table, Td, Th, Tr } from "../../components/ui/Table";
import { formatDuration } from "../../lib/format";
import type { OnlinePlayer, PlayersResponse } from "../../lib/types";
import { PlayerIdentity } from "./PlayerIdentity";
import { translate as t, translatePlural as tp } from "../../lib/i18n";

interface OnlinePlayersCardProps {
  data: PlayersResponse;
  onOp: (player: OnlinePlayer) => void;
  onKick: (player: OnlinePlayer) => void;
  onBan: (player: OnlinePlayer) => void;
}

export function OnlinePlayersCard({ data, onOp, onKick, onBan }: OnlinePlayersCardProps) {
  const operators = new Set(data.operators.map((o) => o.uuid.toLowerCase()));

  return (
    <Card>
      <CardHeader title={t("players.online.title")} description={data.live ? tp("players.online.count", data.online.length) : t("players.online.stopped")} />
      {data.online.length === 0 ? (
        <EmptyState icon={Users} title={t("players.online.empty")} />
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
              {data.online.map((p) => {
                const isOp = operators.has(p.uuid.toLowerCase());
                return (
                  <Tr key={p.uuid}>
                    <Td>
                      <div className="flex items-center gap-3">
                        <PlayerIdentity player={p} />
                        {isOp && <Badge tone="brand">{t("players.online.operator")}</Badge>}
                      </div>
                    </Td>
                    <Td className="tabular-nums">{formatDuration(p.joinedAt)}</Td>
                    <Td>
                      <div className="flex items-center justify-end gap-1">
                        {!isOp && (
                          <Button size="sm" variant="ghost" icon={Crown} onClick={() => onOp(p)}>
                            {t("players.online.op")}
                          </Button>
                        )}
                        <Button size="sm" variant="ghost" icon={LogOut} onClick={() => onKick(p)}>
                          {t("players.online.kick")}
                        </Button>
                        <Button size="sm" variant="ghost" icon={Ban} onClick={() => onBan(p)} className="hover:text-red-400">
                          {t("players.online.ban")}
                        </Button>
                      </div>
                    </Td>
                  </Tr>
                );
              })}
            </tbody>
          </Table>
        </div>
      )}
    </Card>
  );
}
