import { Play, RotateCw, Skull, Square } from "lucide-react";
import { useStatus } from "../hooks/useStatus";
import { api } from "../lib/api";
import { Button } from "./ui/Button";
import { useFeedback } from "./ui/feedback";

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
      title: "Forcer l'arrêt du serveur ?",
      message: "Le processus est tué immédiatement, sans sauvegarde du monde.",
      confirmLabel: "Forcer l'arrêt",
      danger: true,
    });
    if (ok) await call("/api/server/kill", "Serveur arrêté de force")();
  };

  if (server.state === "sleeping") {
    return (
      <>
        <Button icon={Square} onClick={call("/api/server/stop", "Veille annulée, serveur arrêté")} title="Arrête la veille : le serveur ne redémarrera plus tout seul">
          Arrêter
        </Button>
        <Button variant="primary" icon={Play} disabled={!canStart} onClick={call("/api/server/start", "Réveil du serveur")}>
          Réveiller
        </Button>
      </>
    );
  }
  if (!active) {
    return (
      <Button variant="primary" icon={Play} disabled={!canStart} onClick={call("/api/server/start", "Démarrage du serveur")}>
        Démarrer
      </Button>
    );
  }
  return (
    <>
      <Button icon={RotateCw} disabled={!running} onClick={call("/api/server/restart", "Serveur redémarré")}>
        Redémarrer
      </Button>
      <Button icon={Square} disabled={!running} onClick={call("/api/server/stop", "Serveur arrêté")}>
        Arrêter
      </Button>
      <Button variant="danger" icon={Skull} onClick={kill} aria-label="Forcer l'arrêt" title="Forcer l'arrêt" />
    </>
  );
}
