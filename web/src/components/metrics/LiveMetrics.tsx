import { useEffect, useRef, useState, type ReactNode } from "react";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import { formatBytes } from "../../lib/format";
import { translate as t, type MessageKey } from "../../lib/i18n";
import type { MetricsStreamOverview, StreamActor, StreamEvent, StreamWorld } from "../../lib/types";
import { en } from "../../locales";
import { Alert } from "../ui/Alert";
import { Badge } from "../ui/Badge";
import { Table, Td, Th, Tr } from "../ui/Table";

// Data of the optional MetricsStream plugin. Nothing is shown (and nothing
// is requested at a fast pace) unless the operator enabled it.

const EVENT_LIMIT = 100;
const KNOWN_EVENTS = new Set(Object.keys(en.features.stream.event));

/** The plugin's overview, or undefined while it is disabled. */
export function useStreamOverview(): MetricsStreamOverview | undefined {
  const { data } = usePolling(() => api.get<MetricsStreamOverview>("/api/metrics-stream"), 5000);
  return data?.enabled ? data : undefined;
}

/** Events received since the page opened, newest last. */
export function useStreamEvents(active: boolean): StreamEvent[] {
  const [events, setEvents] = useState<StreamEvent[]>([]);
  const cursor = useRef(0);

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    let timer: number | undefined;
    const tick = async () => {
      if (!document.hidden) {
        try {
          const res = await api.get<{ events: StreamEvent[]; next: number }>(`/api/metrics-stream/events?after=${cursor.current}&limit=200`);
          if (cancelled) return;
          // The stream restarted with the server: start over.
          if (res.next < cursor.current) setEvents([]);
          cursor.current = res.next;
          if (res.events.length > 0) setEvents((prev) => [...prev, ...res.events].slice(-EVENT_LIMIT));
        } catch {
          /* the overview already reports problems */
        }
      }
      if (!cancelled) timer = window.setTimeout(tick, 3000);
    };
    void tick();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [active]);

  return events;
}

function actorName(actor: unknown): string {
  const a = actor as StreamActor | undefined;
  return a?.name ?? a?.type ?? a?.label ?? "?";
}

function describeEvent(event: StreamEvent): string {
  if (!KNOWN_EVENTS.has(event.type)) return event.type;
  const data = event.data ?? {};
  const params: Record<string, string | number> = {};
  for (const [key, value] of Object.entries(data)) {
    if (typeof value === "string" || typeof value === "number") params[key] = value;
  }
  params.actor = actorName(data.actor ?? data.victim);
  params.reason = event.type === "SERVER_SHUTDOWN" && data.reason ? `(${String(data.reason)})` : "";
  if (typeof data.amount === "number") params.amount = Math.round(data.amount * 10) / 10;
  if (!params.cause) params.cause = "?";
  return t(`features.stream.event.${event.type}` as MessageKey, params);
}

const tpsTone = (w: StreamWorld) => (w.tps["10s"] >= w.target_tps * 0.95 ? "text-emerald-400" : w.tps["10s"] >= w.target_tps * 0.75 ? "text-amber-400" : "text-red-400");

function Metric({ label, value, hint }: { label: string; value: ReactNode; hint?: string }) {
  return (
    <div className="min-w-0 rounded-lg bg-zinc-950/40 px-3 py-2.5 ring-1 ring-white/[0.04]">
      <p className="text-xs leading-tight text-zinc-500">{label}</p>
      <p className="mt-0.5 truncate text-lg font-semibold tabular-nums text-zinc-100">{value}</p>
      {hint && <p className="text-xs break-words text-zinc-500">{hint}</p>}
    </div>
  );
}

function WorldsTable({ worlds }: { worlds: StreamWorld[] }) {
  return (
    <Table>
      <thead>
        <tr>
          <Th>{t("features.stream.world")}</Th>
          <Th>{t("features.stream.tps")}</Th>
          <Th>{t("features.stream.mspt")}</Th>
          <Th>{t("features.stream.players")}</Th>
          <Th>{t("features.stream.entities")}</Th>
          <Th>{t("features.stream.chunks")}</Th>
        </tr>
      </thead>
      <tbody>
        {worlds.map((w) => (
          <Tr key={w.name}>
            <Td className="h-11 font-medium text-zinc-100 max-sm:w-full">
              {w.name} {w.paused && <Badge tone="warning">{t("features.stream.paused")}</Badge>}
            </Td>
            <Td data-label={t("features.stream.tps")} className={`h-11 tabular-nums ${tpsTone(w)}`}>
              {w.tps["10s"].toFixed(1)} / {w.target_tps}
            </Td>
            <Td data-label={t("features.stream.mspt")} className="h-11 tabular-nums">{w.mspt.avg_10s.toFixed(1)} ms</Td>
            <Td data-label={t("features.stream.players")} className="h-11 tabular-nums">{w.players}</Td>
            <Td data-label={t("features.stream.entities")} className="h-11 tabular-nums">{w.entities.toLocaleString()}</Td>
            <Td data-label={t("features.stream.chunks")} className="h-11 tabular-nums">{w.loaded_chunks.toLocaleString()}</Td>
          </Tr>
        ))}
      </tbody>
    </Table>
  );
}

/**
 * Live readings of the plugin, shown at the top of the resources card: the
 * state of the JVM and the performance of each world right now.
 */
export function LiveResources({ overview, running }: { overview: MetricsStreamOverview; running: boolean }) {
  const jvm = overview.jvm;
  const heapMax = jvm && jvm.heap_max > 0 ? jvm.heap_max : jvm?.heap_committed;
  const status = overview.connected ? (
    <Badge tone="success" dot pulse>
      {t("features.stream.connected")}
    </Badge>
  ) : overview.records > 0 ? (
    <Badge tone="warning">{t("features.stream.interrupted")}</Badge>
  ) : null;

  return (
    <div className="flex flex-col gap-3 px-5 pt-4">
      {overview.error && <Alert tone="danger">{t("features.stream.notListening", { error: overview.error })}</Alert>}
      {running && !overview.loaded && <Alert tone="info">{t("features.stream.restartToEnable")}</Alert>}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h3 className="text-sm font-medium text-zinc-200">{t("features.stream.title")}</h3>
        {status}
        {overview.datagrams > 0 && (
          <span className="text-xs text-zinc-500">{t("features.stream.packets", { n: overview.datagrams, lost: overview.lost, rejected: overview.rejected })}</span>
        )}
      </div>
      {overview.records === 0 ? (
        <p className="text-sm text-zinc-400">{t("features.stream.waiting")}</p>
      ) : (
        <>
          {jvm && (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <Metric label={t("features.stream.heap")} value={formatBytes(jvm.heap_used)} hint={heapMax ? `/ ${formatBytes(heapMax)}` : undefined} />
              <Metric label={t("features.stream.cpu")} value={`${Math.round(jvm.process_cpu * 100)} %`} hint={`${jvm.processors} cores · ${Math.round(jvm.system_cpu * 100)} % system`} />
              <Metric label={t("features.stream.threads")} value={jvm.threads} hint={jvm.gc.map((g) => `${g.name}: ${g.count}`).join(" · ")} />
              {overview.server && <Metric label={t("features.stream.players")} value={`${overview.server.online_players} / ${overview.server.max_players}`} />}
            </div>
          )}
          {overview.worlds.length > 0 && <WorldsTable worlds={overview.worlds} />}
        </>
      )}
    </div>
  );
}

/** Gameplay events (blocks, chat, deaths…) reported by the plugin, newest first. */
export function EventLines({ events, limit = 40 }: { events: StreamEvent[]; limit?: number }) {
  const recent = [...events].reverse().slice(0, limit);
  if (recent.length === 0) return <p className="text-zinc-600">{t("features.stream.noEvents")}</p>;
  return (
    <>
      {recent.map((e) => (
        <p key={e.id} className="truncate text-zinc-300">
          <span className="mr-3 text-zinc-600">{new Date(e.timeMs).toLocaleTimeString()}</span>
          {describeEvent(e)}
        </p>
      ))}
    </>
  );
}

const MOON_PHASES = 8;

function formatHour(hour: number): string {
  return `${String(Math.floor(hour) % 24).padStart(2, "0")}:00`;
}

function Stat({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0 rounded-lg bg-zinc-950/40 px-3 py-2.5 ring-1 ring-white/[0.04]">
      <p className="text-xs leading-tight text-zinc-500">{label}</p>
      <p className="mt-1 text-sm font-medium tabular-nums text-zinc-100">{children}</p>
    </div>
  );
}

/**
 * What the plugin knows about the worlds of the running server: tick
 * performance, population, in-game time, and the loaded plugins.
 */
export function WorldLive({ overview }: { overview: MetricsStreamOverview }) {
  const plugins = overview.plugins ?? [];
  return (
    <div className="flex flex-col gap-4 px-5 pb-1">
      <div>
        <h3 className="text-sm font-medium text-zinc-200">{t("worlds.live.title")}</h3>
        <p className="text-xs text-zinc-500">{t("worlds.live.description")}</p>
      </div>
      {overview.worlds.map((w) => (
        <section key={w.name} className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h4 className="text-sm font-semibold text-zinc-100">{w.name}</h4>
            {w.alive && <Badge tone="success">{t("worlds.live.alive")}</Badge>}
            {w.ticking && <Badge tone="info">{t("worlds.live.ticking")}</Badge>}
            {w.paused && <Badge tone="warning">{t("worlds.live.paused")}</Badge>}
          </div>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-4">
            <Stat label={t("worlds.live.tps")}>
              <span className={tpsTone(w)}>{w.tps["10s"].toFixed(1)}</span> / {w.tps["1m"].toFixed(1)} / {w.tps["5m"].toFixed(1)}
              <span className="text-zinc-500"> ({w.target_tps})</span>
            </Stat>
            <Stat label={t("worlds.live.mspt")}>
              {w.mspt.avg_10s.toFixed(1)} / {w.mspt.avg_1m.toFixed(1)} / {w.mspt.avg_5m.toFixed(1)} ms
            </Stat>
            <Stat label={t("worlds.live.msptRange")}>
              {w.mspt.min_10s.toFixed(1)} / {w.mspt.max_10s.toFixed(1)} ms
            </Stat>
            <Stat label={t("worlds.live.tick")}>{w.tick.toLocaleString()}</Stat>
            <Stat label={t("worlds.live.players")}>{w.players}</Stat>
            <Stat label={t("worlds.live.entities")}>{w.entities.toLocaleString()}</Stat>
            <Stat label={t("worlds.live.chunks")}>{w.loaded_chunks.toLocaleString()}</Stat>
            <Stat label={t("worlds.live.time")}>
              {formatHour(w.hour)} · {new Date(w.game_time_epoch_s * 1000).toLocaleDateString()}
            </Stat>
            <Stat label={t("worlds.live.day")}>{Math.round(w.day_progress * 100)} %</Stat>
            <Stat label={t("worlds.live.moon")}>
              {(((w.moon_phase % MOON_PHASES) + MOON_PHASES) % MOON_PHASES) + 1} / {MOON_PHASES}
            </Stat>
          </div>
        </section>
      ))}
      {plugins.length > 0 && (
        <details className="rounded-lg bg-zinc-950/40 ring-1 ring-white/[0.04]">
          <summary className="cursor-pointer px-3 py-2.5 text-sm text-zinc-300">
            {t("worlds.live.plugins")} <span className="text-xs text-zinc-500">· {t("worlds.live.pluginsHint", { count: plugins.length })}</span>
          </summary>
          <ul className="max-h-64 divide-y divide-white/[0.05] overflow-y-auto border-t border-white/[0.06] text-xs">
            {plugins.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-3 px-3 py-1.5">
                <span className="truncate font-mono text-zinc-300">{p.id}</span>
                <span className="shrink-0 text-zinc-500">
                  {p.version} · {p.state}
                </span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
