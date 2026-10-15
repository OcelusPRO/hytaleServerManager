import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import { cn } from "../../lib/cn";
import { formatMegabytes, formatSeconds } from "../../lib/format";
import { getLang, translate as t, type MessageKey } from "../../lib/i18n";
import type { MetricPoint, MetricsRange, MetricsSeries } from "../../lib/types";
import { Card, CardHeader } from "../ui/Card";

const RANGES: MetricsRange[] = ["1h", "6h", "24h", "7d", "14d"];
const RANGE_KEY = "hsm.metricsRange";
// The server samples every 5 s: the panel follows at the same pace.
const REFRESH_MS = 5000;

// Chart colors, validated as a pair against the zinc-900 card surface
// (dark mode): Java process in yellow, whole container in violet.
const JAVA = "#c98500";
const CONTAINER = "#9085e9";
const STATE_COLORS = { running: "#199e70", sleeping: "#3987e5", stopped: "#52525b" } as const;
type State = keyof typeof STATE_COLORS;
const STATES = Object.keys(STATE_COLORS) as State[];
const STATE_LABELS: Record<State, MessageKey> = { running: "features.charts.running", sleeping: "features.charts.sleeping", stopped: "features.charts.stopped" };

const CHART_HEIGHT = 96;
const AXIS_WIDTH = 44; // left gutter for the y labels

function loadRange(): MetricsRange {
  try {
    const saved = localStorage.getItem(RANGE_KEY) as MetricsRange | null;
    if (saved && RANGES.includes(saved)) return saved;
  } catch {
    // storage unavailable: default
  }
  return "24h";
}

function saveRange(range: MetricsRange) {
  try {
    localStorage.setItem(RANGE_KEY, range);
  } catch {
    // storage unavailable: not remembered
  }
}

function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? 0));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [ref, width];
}

function dominantState(p: MetricPoint): State {
  if (p.running >= p.sleeping && p.running >= p.stopped && p.running > 0) return "running";
  if (p.sleeping >= p.stopped && p.sleeping > 0) return "sleeping";
  return "stopped";
}

function formatClock(t: number, span: number): string {
  const d = new Date(t * 1000);
  const time = d.toLocaleTimeString(getLang(), { hour: "2-digit", minute: "2-digit" });
  return span > 36 * 3600 ? `${d.toLocaleDateString(getLang(), { day: "2-digit", month: "2-digit" })} ${time}` : time;
}

/** Cores used as a percentage of one core ("docker stats" style). */
function formatCores(cores: number): string {
  const percent = cores * 100;
  // One decimal for the small values of an idle container (0.4 %).
  return `${percent.toLocaleString(getLang(), { maximumFractionDigits: percent < 10 ? 1 : 0 })} %`;
}

/** Rounds a maximum up to a readable axis bound (1, 2, 2.5, 5 × 10ⁿ). */
function niceMax(value: number): number {
  if (value <= 0) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 2.5, 5, 10]) {
    if (value <= step * magnitude) return step * magnitude;
  }
  return 10 * magnitude;
}

interface Geometry {
  width: number;
  from: number;
  to: number;
  step: number;
}

function xOf(g: Geometry, t: number): number {
  const plot = g.width - AXIS_WIDTH;
  return AXIS_WIDTH + ((t + g.step / 2 - g.from) / Math.max(1, g.to - g.from)) * plot;
}

interface Series {
  label: string;
  color: string;
  value: (p: MetricPoint) => number | undefined;
}

interface ChartProps {
  title: string;
  points: MetricPoint[];
  geometry: Geometry;
  series: Series[];
  format: (v: number) => string;
  hover: number | null;
  onHover: (index: number | null) => void;
  minMax?: number;
  reference?: { value: number; label: string };
  stepped?: boolean;
  /** Unit in which the axis bound is rounded (1024 for memory in GB). */
  unit?: number;
}

/** Line chart sharing the time axis and hover with its siblings. */
function LineChart({ title, points, geometry: g, series, format, hover, onHover, minMax = 1, reference, stepped, unit = 1 }: ChartProps) {
  const values = series.map((s) => points.map(s.value));
  const dataMax = Math.max(0, ...values.flat().filter((v): v is number => v !== undefined));
  const yMax = niceMax(Math.max(minMax, dataMax * 1.1, reference ? reference.value * 1.05 : 0) / unit) * unit;
  const y = (v: number) => CHART_HEIGHT - 4 - (v / yMax) * (CHART_HEIGHT - 12);

  // Paths broken at missing values and at holes in time.
  const paths = values.map((vals) => {
    let d = "";
    let previous: { x: number; t: number } | null = null;
    points.forEach((p, i) => {
      const v = vals[i];
      if (v === undefined) {
        previous = null;
        return;
      }
      const x = xOf(g, p.t);
      if (!previous || p.t - previous.t > g.step * 1.5) d += `M${x},${y(v)}`;
      else if (stepped) d += `H${x}V${y(v)}`;
      else d += `L${x},${y(v)}`;
      previous = { x, t: p.t };
    });
    return d;
  });

  const hovered = hover !== null ? points[hover] : undefined;
  // Readout: hovered values, or the latest known ones.
  const readout = series.map((s, i) => {
    const vals = values[i]!;
    const v = hover !== null ? vals[hover] : [...vals].reverse().find((x) => x !== undefined);
    return { ...s, text: v === undefined ? "—" : format(v), v };
  });

  return (
    <figure className="min-w-0">
      <figcaption className="mb-1 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 text-xs">
        <span className="font-medium text-zinc-300">{title}</span>
        <span className="flex flex-wrap items-baseline gap-x-3 tabular-nums text-zinc-400">
          {readout.map((r) => (
            <span key={r.label} className="flex items-center gap-1.5">
              {series.length > 1 && <span className="h-0.5 w-3 rounded-full" style={{ background: r.color }} aria-hidden />}
              {series.length > 1 && <span className="text-zinc-500">{r.label}</span>}
              {r.text}
            </span>
          ))}
          {hovered && <span className="text-zinc-500">{formatClock(hovered.t, g.to - g.from)}</span>}
        </span>
      </figcaption>
      <svg width={g.width} height={CHART_HEIGHT} className="block overflow-visible" role="img" aria-label={`${title} : ${readout.map((r) => `${r.label} ${r.text}`).join(", ")}`}>
        {[0, 0.5, 1].map((f) => (
          <g key={f}>
            <line x1={AXIS_WIDTH} x2={g.width} y1={y(yMax * f)} y2={y(yMax * f)} stroke="rgb(255 255 255 / 0.06)" />
            <text x={AXIS_WIDTH - 6} y={y(yMax * f)} dy="0.32em" textAnchor="end" className="fill-zinc-500 text-[10px] tabular-nums">
              {format(yMax * f)}
            </text>
          </g>
        ))}
        {reference && (
          <g>
            <line x1={AXIS_WIDTH} x2={g.width} y1={y(reference.value)} y2={y(reference.value)} stroke="rgb(161 161 170 / 0.6)" strokeDasharray="4 3" />
            <text x={g.width - 2} y={y(reference.value) - 4} textAnchor="end" className="fill-zinc-400 text-[10px]">
              {reference.label}
            </text>
          </g>
        )}
        {paths.map((d, i) => (
          <path key={series[i]!.label} d={d} fill="none" stroke={series[i]!.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        ))}
        {hovered &&
          readout.map(
            (r) =>
              r.v !== undefined && (
                <circle key={r.label} cx={xOf(g, hovered.t)} cy={y(r.v)} r={4} fill={r.color} stroke="var(--color-zinc-900)" strokeWidth={2} />
              ),
          )}
        <HoverLayer points={points} geometry={g} height={CHART_HEIGHT} hover={hover} onHover={onHover} />
      </svg>
    </figure>
  );
}

/** Crosshair and pointer tracking, shared by every chart of the card. */
function HoverLayer({ points, geometry: g, height, hover, onHover }: { points: MetricPoint[]; geometry: Geometry; height: number; hover: number | null; onHover: (i: number | null) => void }) {
  const pick = (clientX: number, rect: DOMRect) => {
    const x = clientX - rect.left;
    let best: number | null = null;
    let bestDistance = Infinity;
    points.forEach((p, i) => {
      const distance = Math.abs(xOf(g, p.t) - x);
      if (distance < bestDistance) {
        best = i;
        bestDistance = distance;
      }
    });
    onHover(best);
  };
  return (
    <>
      {hover !== null && points[hover] && (
        <line x1={xOf(g, points[hover].t)} x2={xOf(g, points[hover].t)} y1={0} y2={height} stroke="rgb(255 255 255 / 0.25)" />
      )}
      <rect
        x={AXIS_WIDTH}
        y={0}
        width={Math.max(0, g.width - AXIS_WIDTH)}
        height={height}
        fill="transparent"
        onPointerMove={(e) => pick(e.clientX, (e.currentTarget.ownerSVGElement ?? e.currentTarget).getBoundingClientRect())}
        onPointerLeave={() => onHover(null)}
      />
    </>
  );
}

/** Server state over time, one segment per period. */
function StateStrip({ points, geometry: g, hover, onHover }: { points: MetricPoint[]; geometry: Geometry; hover: number | null; onHover: (i: number | null) => void }) {
  const height = 18;
  const plot = g.width - AXIS_WIDTH;
  const segment = (g.step / Math.max(1, g.to - g.from)) * plot;
  const hovered = hover !== null ? points[hover] : undefined;
  return (
    <figure className="min-w-0">
      <figcaption className="mb-1 flex items-baseline justify-between gap-3 text-xs">
        <span className="font-medium text-zinc-300">{t("features.charts.state")}</span>
        <span className="flex flex-wrap items-center gap-3 text-zinc-400">
          {STATES.map((s) => (
            <span key={s} className={cn("flex items-center gap-1.5", hovered && dominantState(hovered) !== s && "opacity-40")}>
              <span className="size-2.5 rounded-sm" style={{ background: STATE_COLORS[s] }} aria-hidden />
              {t(STATE_LABELS[s])}
            </span>
          ))}
        </span>
      </figcaption>
      <svg width={g.width} height={height} className="block" role="img" aria-label={t("features.charts.stateOverTime")}>
        {points.map((p) => (
          <rect key={p.t} x={xOf(g, p.t) - segment / 2} y={2} width={Math.max(1, segment - 1)} height={height - 4} rx={1} fill={STATE_COLORS[dominantState(p)]} />
        ))}
        <HoverLayer points={points} geometry={g} height={height} hover={hover} onHover={onHover} />
      </svg>
    </figure>
  );
}

function TimeAxis({ geometry: g }: { geometry: Geometry }) {
  const ticks = Math.max(2, Math.min(6, Math.floor((g.width - AXIS_WIDTH) / 110)));
  return (
    <div className="relative h-4 text-[10px] text-zinc-500 tabular-nums" style={{ width: g.width }}>
      {Array.from({ length: ticks + 1 }, (_, i) => {
        const time = g.from + ((g.to - g.from) * i) / ticks;
        const x = AXIS_WIDTH + ((g.width - AXIS_WIDTH) * i) / ticks;
        return (
          <span key={i} className="absolute top-0 whitespace-nowrap" style={{ left: x, transform: `translateX(${i === 0 ? "0" : i === ticks ? "-100%" : "-50%"})` }}>
            {formatClock(time, g.to - g.from)}
          </span>
        );
      })}
    </div>
  );
}

function Figure({ label, value, hint, swatch }: { label: string; value: ReactNode; hint?: ReactNode; swatch?: string }) {
  return (
    <div className="min-w-0 rounded-lg bg-zinc-950/40 px-3 py-2.5 ring-1 ring-white/[0.04]">
      <p className="flex items-center gap-1.5 truncate text-xs text-zinc-500">
        {swatch && <span className="size-2 shrink-0 rounded-sm" style={{ background: swatch }} aria-hidden />}
        {label}
      </p>
      <p className="mt-0.5 truncate text-lg font-semibold tabular-nums text-zinc-100">{value}</p>
      {hint && <p className="truncate text-xs text-zinc-500">{hint}</p>}
    </div>
  );
}

/** Resource history of the server: memory, CPU, players and states. */
export function ResourceCharts({ maxMemoryMb }: { maxMemoryMb?: number }) {
  const [range, setRange] = useState<MetricsRange>(loadRange);
  // The hovered point is followed by its time: points shift as new samples arrive.
  const [hoverTime, setHoverTime] = useState<number | null>(null);
  const [ref, width] = useWidth<HTMLDivElement>();
  const { data, refresh } = usePolling(() => api.get<MetricsSeries>(`/api/metrics?range=${range}`), REFRESH_MS);

  useEffect(() => {
    setHoverTime(null);
    void refresh();
  }, [range, refresh]);

  const choose = (r: MetricsRange) => {
    setRange(r);
    saveRange(r);
  };

  const points = data?.points ?? [];
  const hoverIndex = points.findIndex((p) => p.t === hoverTime);
  const hover = hoverIndex >= 0 ? hoverIndex : null;
  const setHover = (i: number | null) => setHoverTime(i === null ? null : (points[i]?.t ?? null));
  const summary = data?.summary;
  const seconds = (s: State) => (summary ? { running: summary.runningSeconds, sleeping: summary.sleepingSeconds, stopped: summary.stoppedSeconds }[s] : 0);
  const total = STATES.reduce((sum, s) => sum + seconds(s), 0);
  const geometry: Geometry | null = data && width > AXIS_WIDTH + 40 ? { width, from: data.from, to: data.to, step: data.step } : null;
  const java = t("features.charts.java");
  const container = t("features.charts.container");

  // Time share of a state, with the container's average usage in it.
  const stateHint = (s: State) => {
    const parts = [formatSeconds(seconds(s))];
    const usage = summary?.states?.[s];
    if (usage?.cpu !== undefined) parts.push(t("features.charts.cpuShort", { value: formatCores(usage.cpu) }));
    if (usage?.memMb !== undefined) parts.push(formatMegabytes(usage.memMb));
    return parts.join(" · ");
  };

  return (
    <Card>
      <CardHeader
        title={t("features.charts.title")}
        description={t("features.charts.description")}
        actions={
          <div className="flex rounded-lg bg-zinc-950/60 p-0.5 ring-1 ring-white/[0.06]" role="group" aria-label={t("features.charts.range")}>
            {RANGES.map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => choose(r)}
                aria-pressed={r === range}
                className={cn("h-7 rounded-md px-2.5 text-xs font-medium transition-colors", r === range ? "bg-zinc-800 text-zinc-100" : "text-zinc-400 hover:text-zinc-200")}
              >
                {r}
              </button>
            ))}
          </div>
        }
      />
      <div className="grid grid-cols-1 gap-2 px-5 pt-4 sm:grid-cols-3">
        {STATES.map((s) => (
          <Figure
            key={s}
            swatch={STATE_COLORS[s]}
            label={t(STATE_LABELS[s])}
            value={summary && total > 0 ? `${Math.round((seconds(s) / total) * 100)} %` : "—"}
            hint={summary && stateHint(s)}
          />
        ))}
      </div>
      <div className="grid grid-cols-2 gap-2 px-5 pt-2 sm:grid-cols-4">
        <Figure
          swatch={JAVA}
          label={t("features.charts.peakMemory")}
          value={summary?.peakMemMb !== undefined ? formatMegabytes(summary.peakMemMb) : "—"}
          hint={maxMemoryMb ? `-Xmx ${formatMegabytes(maxMemoryMb)}` : undefined}
        />
        <Figure
          swatch={CONTAINER}
          label={t("features.charts.containerPeakMemory")}
          value={summary?.containerPeakMemMb !== undefined ? formatMegabytes(summary.containerPeakMemMb) : "—"}
        />
        <Figure
          swatch={CONTAINER}
          label={t("features.charts.avgCpu")}
          value={summary?.containerAvgCpu !== undefined ? formatCores(summary.containerAvgCpu) : "—"}
          hint={data && t("features.charts.cores", { count: data.cpuCount, percent: data.cpuCount * 100 })}
        />
        <Figure label={t("features.charts.peakPlayers")} value={summary ? summary.peakPlayers : "—"} />
      </div>
      <div ref={ref} className="flex flex-col gap-4 px-5 pt-5 pb-4">
        {data && !data.processMetrics && <p className="text-xs text-zinc-500">{t("features.charts.linuxOnly")}</p>}
        {geometry && points.length === 0 && <p className="py-6 text-center text-sm text-zinc-500">{t("features.charts.noData")}</p>}
        {geometry && points.length > 0 && (
          <>
            <StateStrip points={points} geometry={geometry} hover={hover} onHover={setHover} />
            {(data?.processMetrics || data?.containerMetrics) && (
              <>
                <LineChart
                  title={t("features.charts.memory")}
                  points={points}
                  geometry={geometry}
                  series={[
                    { label: container, color: CONTAINER, value: (p) => p.cmem },
                    { label: java, color: JAVA, value: (p) => p.memMb },
                  ]}
                  format={formatMegabytes}
                  hover={hover}
                  onHover={setHover}
                  minMax={1024}
                  unit={1024}
                  reference={maxMemoryMb ? { value: maxMemoryMb, label: "-Xmx" } : undefined}
                />
                <LineChart
                  title={t("features.charts.cpu")}
                  points={points}
                  geometry={geometry}
                  series={[
                    { label: container, color: CONTAINER, value: (p) => (p.ccpu === undefined ? undefined : p.ccpu * 100) },
                    { label: java, color: JAVA, value: (p) => (p.cpu === undefined ? undefined : p.cpu * 100) },
                  ]}
                  format={(v) => formatCores(v / 100)}
                  hover={hover}
                  onHover={setHover}
                  minMax={100}
                />
              </>
            )}
            <LineChart
              title={t("features.charts.players")}
              points={points}
              geometry={geometry}
              series={[{ label: t("features.charts.players"), color: JAVA, value: (p) => p.players }]}
              format={(v) => `${Math.round(v)}`}
              hover={hover}
              onHover={setHover}
              minMax={4}
              stepped
            />
            <TimeAxis geometry={geometry} />
          </>
        )}
      </div>
    </Card>
  );
}
