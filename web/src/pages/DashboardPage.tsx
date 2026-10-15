import { ArrowRight, Clock, Globe, Package, Users, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { HostWarnings } from "../components/HostWarnings";
import { HytaleConnect } from "../components/hytale/HytaleConnect";
import { ResourceCharts } from "../components/metrics/ResourceCharts";
import { PageHeader } from "../components/layout/PageHeader";
import { ServerControls } from "../components/ServerControls";
import { Alert } from "../components/ui/Alert";
import { StatusDot } from "../components/ui/Badge";
import { Button } from "../components/ui/Button";
import { Card, CardHeader } from "../components/ui/Card";
import { useFeedback } from "../components/ui/feedback";
import { Link, useNavigate } from "react-router";
import { useConsoleStream } from "../hooks/useConsoleStream";
import { usePolling } from "../hooks/usePolling";
import { useStatus } from "../hooks/useStatus";
import { api } from "../lib/api";
import { cn } from "../lib/cn";
import { formatDuration, formatTime } from "../lib/format";
import { serverStates } from "../lib/serverState";
import type { SavesResponse, ServerSettings } from "../lib/types";
import { paths } from "../routes";
import { translate as t, translatePlural as tp, translateServer } from "../lib/i18n";

function Stat({ icon: Icon, label, value, hint }: { icon: LucideIcon; label: string; value: ReactNode; hint?: ReactNode }) {
  return (
    <Card className="p-5">
      <div className="flex items-center justify-between">
        <p className="text-sm text-zinc-400">{label}</p>
        <Icon className="size-4 text-zinc-500" aria-hidden />
      </div>
      <div className="mt-3 truncate text-2xl font-semibold tracking-tight text-zinc-50">{value}</div>
      {hint && <p className="mt-1 truncate text-xs text-zinc-500">{hint}</p>}
    </Card>
  );
}

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-3">
      <dt className="text-sm text-zinc-400">{label}</dt>
      <dd className="truncate text-right text-sm font-medium text-zinc-200">{children}</dd>
    </div>
  );
}

function RecentConsole() {
  const { lines } = useConsoleStream();
  const recent = lines.slice(-12);
  return (
    <Card className="flex flex-col lg:col-span-2">
      <CardHeader
        title={t("pages.dashboard.recentActivity")}
        actions={
          <Link
            to={paths.console}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
          >
            {t("pages.dashboard.openConsole")} <ArrowRight className="size-3.5" />
          </Link>
        }
      />
      <div className="m-5 mt-4 h-72 overflow-hidden rounded-lg bg-zinc-950 px-4 py-3 font-mono text-xs leading-relaxed ring-1 ring-white/[0.06]">
        {recent.length === 0 ? (
          <p className="text-zinc-600">{t("pages.dashboard.noActivity")}</p>
        ) : (
          recent.map((l) => (
            <p key={l.seq} className={cn("truncate", l.stream === "err" ? "text-red-400" : l.stream === "sys" ? "text-sky-400" : l.stream === "in" ? "text-brand-300" : "text-zinc-300")}>
              <span className="mr-3 text-zinc-600">{formatTime(l.time)}</span>
              {l.stream === "sys" ? translateServer(l.text) : l.text}
            </p>
          ))
        )}
      </div>
    </Card>
  );
}

export function DashboardPage() {
  const { status, refresh } = useStatus();
  const { run, confirm } = useFeedback();
  const navigate = useNavigate();
  const { data: saves } = usePolling(() => api.get<SavesResponse>("/api/saves"), 10000);
  const { data: settings } = usePolling(() => api.get<ServerSettings>("/api/settings"), 10000);
  if (!status) return null;

  const { server, setup, hytale } = status;
  const state = serverStates[server.state];
  const running = server.state === "running";
  const activeSave = saves?.saves.find((s) => s.id === server.activeSaveId);
  const accountMissing = settings?.authMode === "authenticated" && !hytale.authenticated;

  const update = async () => {
    const ok =
      !running ||
      (await confirm({
        title: t("pages.dashboard.updateTitle"),
        message: t("pages.dashboard.updateMessage"),
        confirmLabel: t("pages.dashboard.update"),
      }));
    if (ok) await run(() => api.post("/api/setup/update"), t("pages.dashboard.updateStarted"));
    await refresh();
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t("pages.dashboard.title")}
        description={t("pages.dashboard.description")}
        actions={<ServerControls />}
      />

      {accountMissing && (
        <Alert tone="warning" title={t("pages.dashboard.accountDisconnected")}>
          <p>{t("pages.dashboard.accountDisconnectedHint")}</p>
          <div className="mt-3">
            <HytaleConnect label={t("pages.dashboard.reconnect")} align="start" />
          </div>
        </Alert>
      )}
      {setup.updateAvailable && (
        <Alert
          tone="info"
          title={t("pages.dashboard.updateAvailable", { version: setup.latestVersion ?? "" })}
          action={
            <Button size="sm" variant="primary" onClick={update}>
              {t("pages.dashboard.update")}
            </Button>
          }
        >
          {running ? t("pages.dashboard.updateWhenRunning") : t("pages.dashboard.updateWhenStopped")}
        </Alert>
      )}
      {setup.error && (
        <Alert tone="danger" title={t("pages.dashboard.installFailed")}>
          {t("pages.dashboard.installRetry", { error: translateServer(setup.error) })}
        </Alert>
      )}
      {server.state === "sleeping" && (
        <Alert tone="info" title={t("pages.dashboard.asleepTitle")}>
          {t("pages.dashboard.asleepHint")}
        </Alert>
      )}
      <HostWarnings warnings={server.hostWarnings ?? []} />
      {server.state === "crashed" && (
        <Alert tone="danger" title={t("pages.dashboard.crashedTitle")}>
          {t("pages.dashboard.crashedHint", { code: server.exitCode ?? t("common.unknown") })}
        </Alert>
      )}
      {!server.activeSaveId && (
        <Alert tone="warning" title={t("pages.dashboard.noWorld")} action={<Button size="sm" onClick={() => navigate(paths.worlds)}>{t("pages.dashboard.manageWorlds")}</Button>}>
          {t("pages.dashboard.noWorldHint")}
        </Alert>
      )}
      {running && server.saveId !== server.activeSaveId && (
        <Alert tone="info">{t("pages.dashboard.worldChanged")}</Alert>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat
          icon={Package}
          label={t("pages.dashboard.status")}
          value={
            <span className="flex items-center gap-2.5">
              <StatusDot tone={state.tone} pulse={running} className="size-2.5" />
              {t(state.label)}
            </span>
          }
          hint={running ? t("pages.dashboard.pid", { pid: server.pid ?? "" }) : t("pages.dashboard.readyToStart")}
        />
        <Stat icon={Users} label={t("pages.dashboard.players")} value={running ? server.playerCount : "—"} hint={t("pages.dashboard.udpPort", { port: settings?.port ?? "…" })} />
        <Stat icon={Clock} label={t("pages.dashboard.uptime")} value={running ? formatDuration(server.startedAt) : "—"} />
        <Stat icon={Globe} label={t("pages.dashboard.activeWorld")} value={activeSave?.name ?? t("common.none")} hint={activeSave ? tp("pages.dashboard.worldCount", saves?.saves.length ?? 0) : undefined} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <RecentConsole />
        <Card>
          <CardHeader title={t("pages.dashboard.info")} />
          <dl className="divide-y divide-white/[0.05] px-5 pb-2">
            <InfoRow label={t("pages.dashboard.serverVersion")}>{setup.gameVersion ?? "—"}</InfoRow>
            <InfoRow label={t("pages.dashboard.java")}>{setup.javaVersion ?? "—"}</InfoRow>
            <InfoRow label={t("pages.dashboard.profile")}>{hytale.profileName ?? "—"}</InfoRow>
            <InfoRow label={t("pages.dashboard.authentication")}>{settings?.authMode === "offline" ? t("pages.dashboard.offline") : t("pages.dashboard.hytale")}</InfoRow>
            <InfoRow label={t("pages.dashboard.channel")}>{settings?.patchline ?? "—"}</InfoRow>
          </dl>
        </Card>
      </div>

      <ResourceCharts maxMemoryMb={settings?.maxMemoryMb} />
    </div>
  );
}
