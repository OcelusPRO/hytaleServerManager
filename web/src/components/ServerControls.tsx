import { Play, RotateCw, Skull, Square } from "lucide-react";
import { useStatus } from "../hooks/useStatus";
import { api } from "../lib/api";
import { Button } from "./ui/Button";
import { useFeedback } from "./ui/feedback";
import { translate as t } from "../lib/i18n";

/** Start / stop / restart / kill buttons reflecting the server state. */
export function ServerControls() {
  const { status, refresh } = useStatus();
  const { run, confirm } = useFeedback();
  const server = status?.server;
  const setup = status?.setup;
  if (!server || !setup) return null;

  const running = server.state === "running";
  const active = running || server.state === "stopping";
  const canStart = !active && setup.gameReady && setup.javaReady && !server.installing;

  const call = (path: string, success: string) => async () => {
    await run(() => api.post(path), success);
    await refresh();
  };

  const kill = async () => {
    const ok = await confirm({
      title: t("shell.controls.killTitle"),
      message: t("shell.controls.killMessage"),
      confirmLabel: t("shell.controls.kill"),
      danger: true,
    });
    if (ok) await call("/api/server/kill", t("shell.controls.killed"))();
  };

  if (server.state === "sleeping") {
    return (
      <>
        <Button icon={Square} onClick={call("/api/server/stop", t("shell.controls.sleepCancelled"))} title={t("shell.controls.cancelSleepHint")}>
          {t("shell.controls.cancelSleep")}
        </Button>
        <Button variant="primary" icon={Play} disabled={!canStart} onClick={call("/api/server/start", t("shell.controls.waking"))}>
          {t("shell.controls.wake")}
        </Button>
      </>
    );
  }
  if (!active) {
    return (
      <Button variant="primary" icon={Play} disabled={!canStart} onClick={call("/api/server/start", t("shell.controls.starting"))}>
        {t("shell.controls.start")}
      </Button>
    );
  }
  return (
    <>
      <Button icon={RotateCw} disabled={!running} onClick={call("/api/server/restart", t("shell.controls.restarted"))}>
        {t("shell.controls.restart")}
      </Button>
      <Button icon={Square} disabled={!running} onClick={call("/api/server/stop", t("shell.controls.stopped"))}>
        {t("shell.controls.stop")}
      </Button>
      <Button variant="danger" icon={Skull} onClick={kill} aria-label={t("shell.controls.kill")} title={t("shell.controls.kill")} />
    </>
  );
}
