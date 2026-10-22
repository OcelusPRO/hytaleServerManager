import { Bug, ChevronRight, CircleCheck, Copy, RotateCcw, Settings, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { PageHeader } from "../components/layout/PageHeader";
import { Alert } from "../components/ui/Alert";
import { Badge, type Tone } from "../components/ui/Badge";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { EmptyState } from "../components/ui/EmptyState";
import { Tabs } from "../components/ui/Tabs";
import { useFeedback } from "../components/ui/feedback";
import { usePolling } from "../hooks/usePolling";
import { useStatus } from "../hooks/useStatus";
import { api, seg } from "../lib/api";
import { cn } from "../lib/cn";
import { formatDate, formatDelay, formatTimestamp } from "../lib/format";
import { translate as t, translatePlural as tp, translateServer, type MessageKey } from "../lib/i18n";
import type { ErrorEvent, ErrorException, ErrorFrame, ErrorIssue, ErrorIssueSummary, ErrorOccurrence, ErrorReportsOverview } from "../lib/types";
import { paths } from "../routes";

type Filter = "open" | "resolved" | "all";

const LEVELS = ["fatal", "error", "warning", "info", "debug"];
const levelTones: Record<string, Tone> = { fatal: "danger", error: "danger", warning: "warning", info: "info", debug: "neutral" };

/** Frames shown before the "more" button, the closest to the error. */
const FRAME_PREVIEW = 12;
/** Occurrences shown before the "show all" button, the latest. */
const HISTORY_PREVIEW = 20;

function LevelBadge({ level }: { level: string }) {
  const known = LEVELS.includes(level);
  return <Badge tone={levelTones[level] ?? "neutral"}>{known ? t(`errors.level.${level}` as MessageKey) : level}</Badge>;
}

function frameText(f: ErrorFrame): string {
  const where = [f.module, f.function].filter(Boolean).join(".");
  const file = f.filename ? `${f.filename}${f.line ? `:${f.line}` : ""}` : f.line ? `:${f.line}` : "";
  return `at ${where || "?"}${file ? `(${file})` : ""}`;
}

/** An exception with its frames, innermost first as in a Java stack trace. */
function ExceptionBlock({ exception, cause }: { exception: ErrorException; cause: boolean }) {
  const [all, setAll] = useState(false);
  const frames = [...(exception.frames ?? [])].reverse();
  const shown = all ? frames : frames.slice(0, FRAME_PREVIEW);
  return (
    <div className="min-w-0">
      <p className="font-mono text-xs break-words text-zinc-100">
        {cause && <span className="text-zinc-500">{t("errors.detail.causedBy")} </span>}
        <span className="font-semibold text-red-300">{[exception.module, exception.type].filter(Boolean).join(".")}</span>
        {exception.value && <span>: {exception.value}</span>}
      </p>
      {shown.length > 0 && (
        <ol className="mt-1.5 overflow-x-auto font-mono text-[11px] leading-5 whitespace-pre">
          {shown.map((f, i) => (
            <li key={i} className={cn("pl-4", f.inApp ? "text-amber-200" : "text-zinc-500")}>
              {frameText(f)}
            </li>
          ))}
        </ol>
      )}
      {!all && frames.length > FRAME_PREVIEW && (
        <button type="button" className="mt-1 cursor-pointer pl-4 text-xs text-brand-300 hover:text-brand-200" onClick={() => setAll(true)}>
          {t("errors.detail.otherFrames", { n: frames.length - FRAME_PREVIEW })}
        </button>
      )}
    </div>
  );
}

function stackTraceText(ev: ErrorEvent): string {
  return [...(ev.exceptions ?? [])]
    .reverse()
    .map((ex, i) => {
      const head = `${i > 0 ? "Caused by: " : ""}${[ex.module, ex.type].filter(Boolean).join(".")}${ex.value ? `: ${ex.value}` : ""}`;
      const frames = [...(ex.frames ?? [])].reverse().map((f) => `\t${frameText(f)}`);
      return [head, ...frames].join("\n");
    })
    .join("\n");
}

/** Consecutive occurrences raised by the same server process. */
interface LaunchGroup {
  launch?: string;
  occurrences: ErrorOccurrence[];
}

function groupByLaunch(occurrences: ErrorOccurrence[]): LaunchGroup[] {
  const groups: LaunchGroup[] = [];
  for (const o of occurrences) {
    const current = groups[groups.length - 1];
    if (current && current.launch === o.launch) current.occurrences.push(o);
    else groups.push({ launch: o.launch, occurrences: [o] });
  }
  return groups;
}

/** Every appearance of the error, newest first, grouped by server start. */
function OccurrenceHistory({ issue }: { issue: ErrorIssue }) {
  const { notify } = useFeedback();
  const [all, setAll] = useState(false);
  const newest = [...issue.occurrences].reverse();
  const shown = all ? newest : newest.slice(0, HISTORY_PREVIEW);
  const usual = issue.last.exceptions?.at(-1)?.value ?? issue.last.message;
  if (newest.length === 0) return null;

  const copy = async () => {
    const lines = newest.map((o) => [o.time, o.eventId ?? "", o.message ?? ""].join("\t"));
    try {
      await navigator.clipboard.writeText(lines.join("\n"));
      notify("success", t("errors.detail.copied"));
    } catch {
      // clipboard unavailable (insecure context)
    }
  };

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-xs font-medium text-zinc-400">{t("errors.detail.history")}</h3>
          {issue.count > issue.occurrences.length && (
            <p className="mt-0.5 text-xs text-zinc-500">{t("errors.detail.historyPartial", { shown: issue.occurrences.length, count: issue.count })}</p>
          )}
        </div>
        <Button size="sm" variant="ghost" icon={Copy} onClick={copy}>
          {t("errors.detail.copyTimes")}
        </Button>
      </div>
      <div className="mt-2 flex flex-col gap-3">
        {groupByLaunch(shown).map((group, i) => (
          <div key={i} className="rounded-lg bg-zinc-950/60 ring-1 ring-white/[0.04]">
            <p className="border-b border-white/[0.04] px-3 py-2 text-xs text-zinc-400">
              {group.launch ? t("errors.detail.launch", { date: formatTimestamp(group.launch) }) : t("errors.detail.launchUnknown")}
            </p>
            <ol className="divide-y divide-white/[0.03]">
              {group.occurrences.map((o) => (
                <li key={o.eventId ?? o.time} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-1.5 text-xs">
                  <time dateTime={o.time} className="font-mono text-zinc-200 tabular-nums">
                    {formatTimestamp(o.time)}
                  </time>
                  {group.launch && (
                    <span className="text-zinc-500">
                      {t("errors.detail.afterLaunch", { delay: formatDelay((Date.parse(o.time) - Date.parse(group.launch)) / 1000) })}
                    </span>
                  )}
                  {o.message && o.message !== usual && <span className="min-w-0 truncate font-mono text-zinc-400">{o.message}</span>}
                  {o.eventId && <span className="ml-auto font-mono text-[11px] text-zinc-600 max-sm:hidden">{o.eventId.slice(0, 8)}</span>}
                </li>
              ))}
            </ol>
          </div>
        ))}
      </div>
      {!all && newest.length > HISTORY_PREVIEW && (
        <button type="button" className="mt-2 cursor-pointer text-xs text-brand-300 hover:text-brand-200" onClick={() => setAll(true)}>
          {t("errors.detail.showAll", { n: newest.length })}
        </button>
      )}
    </div>
  );
}

function Meta({ label, value }: { label: string; value?: string | number }) {
  if (value === undefined || value === "") return null;
  return (
    <div className="min-w-0">
      <dt className="text-xs text-zinc-500">{label}</dt>
      <dd className="mt-0.5 truncate text-sm text-zinc-200" title={String(value)}>
        {value}
      </dd>
    </div>
  );
}

function IssueDetail({ id, onChange }: { id: string; onChange: () => Promise<void> }) {
  const { run, confirm, notify } = useFeedback();
  const { data: issue, error, refresh } = usePolling(() => api.get<ErrorIssue>(`/api/errors/${seg(id)}`), 0);

  if (error) return <p className="px-5 pb-5 text-sm text-red-400">{error}</p>;
  if (!issue) return <p className="px-5 pb-5 text-sm text-zinc-500">{t("errors.detail.loading")}</p>;
  const ev = issue.last;
  const exceptions = [...(ev.exceptions ?? [])].reverse(); // thrown first, then its causes
  const tags = Object.entries(ev.tags ?? {}).sort(([a], [b]) => a.localeCompare(b));

  const toggle = async () => {
    await run(() => api.patch(`/api/errors/${seg(id)}`, { resolved: !issue.resolved }), t(issue.resolved ? "errors.detail.reopened" : "errors.detail.resolved"));
    await Promise.all([refresh(), onChange()]);
  };
  const remove = async () => {
    const ok = await confirm({ title: t("errors.detail.deleteTitle"), message: t("errors.detail.deleteMessage"), confirmLabel: t("errors.detail.delete"), danger: true });
    if (!ok) return;
    await run(() => api.del(`/api/errors/${seg(id)}`), t("errors.detail.deleted"));
    await onChange();
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(`${issue.title}\n${stackTraceText(ev)}`);
      notify("success", t("errors.detail.copied"));
    } catch {
      // clipboard unavailable (insecure context)
    }
  };

  return (
    <div className="flex flex-col gap-5 border-t border-white/[0.06] bg-zinc-950/30 px-5 py-5">
      <div className="flex flex-wrap gap-2">
        <Button size="sm" icon={issue.resolved ? RotateCcw : CircleCheck} onClick={toggle}>
          {t(issue.resolved ? "errors.detail.reopen" : "errors.detail.resolve")}
        </Button>
        {exceptions.length > 0 && (
          <Button size="sm" variant="ghost" icon={Copy} onClick={copy}>
            {t("errors.detail.copy")}
          </Button>
        )}
        <Button size="sm" variant="danger" icon={Trash2} onClick={remove} className="ml-auto">
          {t("errors.detail.delete")}
        </Button>
      </div>

      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Meta label={t("errors.detail.firstSeen")} value={formatDate(issue.firstSeen)} />
        <Meta label={t("errors.detail.lastSeen")} value={formatDate(issue.lastSeen)} />
        <Meta label={t("errors.detail.occurrences")} value={issue.count} />
        {issue.resolvedAt && <Meta label={t("errors.detail.resolvedAt")} value={formatDate(issue.resolvedAt)} />}
        <Meta label={t("errors.detail.release")} value={ev.release} />
        <Meta label={t("errors.detail.logger")} value={ev.logger} />
        <Meta label={t("errors.detail.environment")} value={ev.environment} />
        <Meta label={t("errors.detail.serverName")} value={ev.serverName} />
      </dl>

      {ev.message && ev.message !== issue.title && (
        <div>
          <h3 className="text-xs font-medium text-zinc-400">{t("errors.detail.message")}</h3>
          <p className="mt-1 font-mono text-xs break-words whitespace-pre-wrap text-zinc-200">{ev.message}</p>
        </div>
      )}

      {exceptions.length > 0 && (
        <div>
          <h3 className="text-xs font-medium text-zinc-400">{t("errors.detail.stacktrace")}</h3>
          <div className="mt-2 flex flex-col gap-4 rounded-lg bg-zinc-950/60 p-4 ring-1 ring-white/[0.04]">
            {exceptions.map((ex, i) => (
              <ExceptionBlock key={i} exception={ex} cause={i > 0} />
            ))}
          </div>
        </div>
      )}

      <OccurrenceHistory issue={issue} />

      {tags.length > 0 && (
        <div>
          <h3 className="text-xs font-medium text-zinc-400">{t("errors.detail.tags")}</h3>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {tags.map(([k, v]) => (
              <span key={k} className="max-w-full truncate rounded-md bg-zinc-800/80 px-2 py-1 font-mono text-[11px] text-zinc-300" title={`${k}=${v}`}>
                <span className="text-zinc-500">{k}=</span>
                {v}
              </span>
            ))}
          </div>
        </div>
      )}

      <details className="group">
        <summary className="cursor-pointer text-xs font-medium text-zinc-400 hover:text-zinc-200">{t("errors.detail.raw")}</summary>
        {ev.rawDropped ? (
          <p className="mt-2 text-xs text-zinc-500">{t("errors.detail.rawDropped")}</p>
        ) : (
          <pre className="mt-2 max-h-96 overflow-auto rounded-lg bg-zinc-950/60 p-3 font-mono text-[11px] leading-5 text-zinc-300 ring-1 ring-white/[0.04]">
            {JSON.stringify(ev.raw, null, 2)}
          </pre>
        )}
      </details>
    </div>
  );
}

function IssueRow({ issue, open, onToggle, onChange }: { issue: ErrorIssueSummary; open: boolean; onToggle: () => void; onChange: () => Promise<void> }) {
  return (
    <li>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full cursor-pointer items-start gap-3 px-5 py-3.5 text-left transition-colors hover:bg-white/[0.02]"
      >
        <ChevronRight className={cn("mt-1 size-4 shrink-0 text-zinc-500 transition-transform", open && "rotate-90")} aria-hidden />
        <div className="min-w-0 flex-1">
          <p className={cn("truncate font-mono text-sm", issue.resolved ? "text-zinc-500 line-through" : "text-zinc-100")} title={issue.title}>
            {issue.title}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500">
            {issue.culprit && <span className="min-w-0 truncate font-mono">{issue.culprit}</span>}
            {issue.plugin && <Badge tone="brand">{t("errors.plugin", { name: issue.plugin })}</Badge>}
            {issue.regressed && (
              <span title={issue.resolvedAt ? t("errors.regressedHint", { date: formatDate(issue.resolvedAt) }) : undefined}>
                <Badge tone="warning">{t("errors.regressed")}</Badge>
              </span>
            )}
          </div>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1 text-right">
          <LevelBadge level={issue.level} />
          <span className="text-xs text-zinc-400 tabular-nums">{tp("errors.count", issue.count)}</span>
          <span className="text-xs text-zinc-500 max-sm:hidden">{t("errors.lastSeen", { date: formatDate(issue.lastSeen) })}</span>
        </div>
      </button>
      {/* Keyed on the last occurrence: a new one is fetched as it arrives. */}
      {open && <IssueDetail key={issue.lastSeen} id={issue.id} onChange={onChange} />}
    </li>
  );
}

function StatusAlerts({ overview }: { overview: ErrorReportsOverview }) {
  const { status: server } = useStatus();
  const { status } = overview;
  const running = server?.server.state === "running";
  const fwd = status.forward;
  const relayFailing = !!fwd?.lastErrorAt && (!fwd.lastSentAt || Date.parse(fwd.lastErrorAt) > Date.parse(fwd.lastSentAt));

  return (
    <>
      {!status.enabled && (
        <Alert
          title={t("errors.disabledTitle")}
          action={
            <Link
              to={paths.settings}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-zinc-800 px-2.5 text-xs font-medium text-zinc-100 ring-1 ring-white/5 ring-inset transition-colors hover:bg-zinc-700"
            >
              <Settings className="size-3.5" aria-hidden />
              {t("errors.openSettings")}
            </Link>
          }
        >
          {t("errors.disabledHint")}
        </Alert>
      )}
      {status.enabled && running && !status.loaded && (
        <Alert tone="warning" title={t("errors.restartTitle")}>
          {t("errors.restartHint")}
        </Alert>
      )}
      {!status.listening && status.error && (
        <Alert tone="danger">{t("errors.listenError", { port: status.port, error: status.error })}</Alert>
      )}
      {fwd && relayFailing && (
        <Alert tone="warning" title={t("errors.relayFailed")}>
          {t("errors.relayFailedHint", { error: translateServer(fwd.lastError ?? ""), date: formatDate(fwd.lastErrorAt) })}
        </Alert>
      )}
      {fwd && !relayFailing && fwd.sent > 0 && <p className="text-xs text-zinc-500">{t("errors.relayOk", { sent: fwd.sent })}</p>}
      {fwd && fwd.dropped > 0 && <p className="text-xs text-amber-400">{t("errors.relayDropped", { n: fwd.dropped })}</p>}
    </>
  );
}

export function ErrorsPage() {
  const { run, confirm } = useFeedback();
  const { data, refresh } = usePolling(() => api.get<ErrorReportsOverview>("/api/errors"), 10000);
  const [filter, setFilter] = useState<Filter>("open");
  const [openId, setOpenId] = useState<string>();

  const issues = data?.issues ?? [];
  const counts = { open: issues.filter((i) => !i.resolved).length, resolved: issues.filter((i) => i.resolved).length, all: issues.length };
  const shown = issues.filter((i) => filter === "all" || (filter === "resolved") === i.resolved);

  const clear = async () => {
    const ok = await confirm({ title: t("errors.clearTitle"), message: t("errors.clearMessage"), confirmLabel: t("errors.clear"), danger: true });
    if (!ok) return;
    await run(() => api.del("/api/errors"), t("errors.cleared"));
    await refresh();
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t("common.nav.errors")}
        description={t("errors.description")}
        actions={
          data && (
            <>
              <Badge tone={data.status.enabled && data.status.listening ? "success" : "neutral"} dot>
                {data.status.enabled && data.status.listening ? t("errors.receiving") : t("errors.notReceiving")}
              </Badge>
              <Button size="sm" variant="ghost" icon={Trash2} disabled={issues.length === 0} onClick={clear}>
                {t("errors.clear")}
              </Button>
            </>
          )
        }
      />
      {data && <StatusAlerts overview={data} />}
      <Card>
        <Tabs
          value={filter}
          onChange={setFilter}
          items={[
            { value: "open", label: t("errors.tabs.open"), count: counts.open },
            { value: "resolved", label: t("errors.tabs.resolved"), count: counts.resolved },
            { value: "all", label: t("errors.tabs.all"), count: counts.all },
          ]}
        />
        {shown.length === 0 ? (
          <EmptyState icon={Bug} title={issues.length === 0 ? t("errors.emptyTitle") : t("errors.emptyFiltered")} description={issues.length === 0 ? t("errors.emptyHint") : undefined} />
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {shown.map((issue) => (
              <IssueRow
                key={issue.id}
                issue={issue}
                open={openId === issue.id}
                onToggle={() => setOpenId((id) => (id === issue.id ? undefined : issue.id))}
                onChange={refresh}
              />
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
