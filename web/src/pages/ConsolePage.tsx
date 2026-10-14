import { ArrowDownToLine, Eraser, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { CommandInput } from "../components/console/CommandInput";
import { PageHeader } from "../components/layout/PageHeader";
import { Badge } from "../components/ui/Badge";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { useFeedback } from "../components/ui/feedback";
import { useCommandCatalog } from "../hooks/useCommandCatalog";
import { useConsoleStream } from "../hooks/useConsoleStream";
import { usePolling } from "../hooks/usePolling";
import { useStatus } from "../hooks/useStatus";
import { api } from "../lib/api";
import { cn } from "../lib/cn";
import { formatTime } from "../lib/format";
import type { ConsoleStream, PlayersResponse } from "../lib/types";

const streamColors: Record<ConsoleStream, string> = {
  out: "text-zinc-300",
  err: "text-red-400",
  in: "text-brand-300",
  sys: "text-sky-400 italic",
};

export function ConsolePage() {
  const { lines, connected, clear } = useConsoleStream();
  const { status } = useStatus();
  const { run } = useFeedback();
  const [follow, setFollow] = useState(true);
  const output = useRef<HTMLDivElement>(null);
  const running = status?.server.state === "running";
  const catalog = useCommandCatalog();
  const { data: players } = usePolling(() => api.get<PlayersResponse>("/api/players"), 10000);
  const onlineNames = useMemo(() => players?.online.map((p) => p.name) ?? [], [players]);

  useEffect(() => {
    if (follow && output.current) output.current.scrollTop = output.current.scrollHeight;
  }, [lines, follow]);

  const onScroll = () => {
    const el = output.current;
    if (el) setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
  };

  const send = async (command: string) =>
    (await run(() => api.post("/api/server/command", { command }).then(() => true))) === true;

  return (
    <div className="flex h-[calc(100dvh-4rem)] flex-col gap-6 max-lg:h-[calc(100dvh-10rem)]">
      <PageHeader
        title="Console"
        description="Sortie du serveur en temps réel et envoi de commandes."
        actions={
          <>
            {catalog.building ? (
              <Badge tone="info" dot pulse>
                Analyse des commandes…
              </Badge>
            ) : (
              <Button
                size="sm"
                variant="ghost"
                icon={RefreshCw}
                disabled={!running}
                onClick={catalog.rebuild}
                title="Relire la liste des commandes du serveur (après l'ajout d'un mod)"
              >
                {catalog.entries.length ? `${catalog.commandCount} commandes` : "Charger les commandes"}
              </Button>
            )}
            <Badge tone={connected ? "success" : "warning"} dot>
              {connected ? "Connectée" : "Reconnexion…"}
            </Badge>
            <Button size="sm" variant="ghost" icon={Eraser} onClick={clear}>
              Effacer
            </Button>
          </>
        }
      />
      <Card className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <div
          ref={output}
          onScroll={onScroll}
          className="scrollbar-thin min-h-0 flex-1 overflow-y-auto bg-zinc-950/80 px-4 py-3 font-mono text-[13px] leading-6"
          role="log"
          aria-live="off"
        >
          {lines.length === 0 && <p className="text-zinc-600">En attente de sortie du serveur…</p>}
          {lines.map((l) => (
            <div key={l.seq} className={cn("break-words whitespace-pre-wrap", streamColors[l.stream])}>
              <span className="mr-3 text-zinc-600 select-none">{formatTime(l.time)}</span>
              {l.text}
            </div>
          ))}
        </div>
        {!follow && (
          <Button size="sm" icon={ArrowDownToLine} className="absolute right-4 bottom-20 shadow-lg" onClick={() => setFollow(true)}>
            Suivre
          </Button>
        )}
        <CommandInput entries={catalog.entries} onlinePlayers={onlineNames} disabled={!running} onSubmit={send} />
      </Card>
    </div>
  );
}
