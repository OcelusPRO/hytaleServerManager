import { ArrowRight, Clock, Globe, Package, Users, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { HytaleConnect } from "../components/hytale/HytaleConnect";
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
        title="Activité récente"
        actions={
          <Link
            to={paths.console}
            className="inline-flex h-8 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
          >
            Ouvrir la console <ArrowRight className="size-3.5" />
          </Link>
        }
      />
      <div className="m-5 mt-4 h-72 overflow-hidden rounded-lg bg-zinc-950 px-4 py-3 font-mono text-xs leading-relaxed ring-1 ring-white/[0.06]">
        {recent.length === 0 ? (
          <p className="text-zinc-600">Aucune activité pour le moment.</p>
        ) : (
          recent.map((l) => (
            <p key={l.seq} className={cn("truncate", l.stream === "err" ? "text-red-400" : l.stream === "sys" ? "text-sky-400" : l.stream === "in" ? "text-brand-300" : "text-zinc-300")}>
              <span className="mr-3 text-zinc-600">{formatTime(l.time)}</span>
              {l.text}
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
        title: "Mettre à jour le serveur ?",
        message: "Le serveur va être arrêté, mis à jour puis redémarré automatiquement.",
        confirmLabel: "Mettre à jour",
      }));
    if (ok) await run(() => api.post("/api/setup/update"), "Mise à jour lancée");
    await refresh();
  };

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Tableau de bord"
        description="État et contrôle de votre serveur Hytale."
        actions={<ServerControls />}
      />

      {accountMissing && (
        <Alert tone="warning" title="Compte Hytale déconnecté">
          <p>Reconnectez le compte pour que les joueurs puissent rejoindre le serveur.</p>
          <div className="mt-3">
            <HytaleConnect label="Reconnecter le compte" align="start" />
          </div>
        </Alert>
      )}
      {setup.updateAvailable && (
        <Alert
          tone="info"
          title={`Mise à jour disponible : ${setup.latestVersion}`}
          action={
            <Button size="sm" variant="primary" onClick={update}>
              Mettre à jour
            </Button>
          }
        >
          {running ? "Elle sera appliquée au prochain arrêt du serveur, ou maintenant." : "Elle va être installée automatiquement."}
        </Alert>
      )}
      {setup.error && (
        <Alert tone="danger" title="Échec de l'installation automatique">
          {setup.error} — nouvel essai dans une minute.
        </Alert>
      )}
      {server.state === "sleeping" && (
        <Alert tone="info" title="Serveur en veille">
          Aucun joueur depuis un moment : le serveur s'est arrêté après avoir sauvegardé le monde. Il redémarre automatiquement dès qu'un joueur tente de se connecter.
        </Alert>
      )}
      {server.state === "crashed" && (
        <Alert tone="danger" title="Le serveur s'est arrêté de manière inattendue">
          Code de sortie {server.exitCode ?? "inconnu"}. Consultez la console pour plus de détails.
        </Alert>
      )}
      {!server.activeSaveId && (
        <Alert tone="warning" title="Aucun monde actif" action={<Button size="sm" onClick={() => navigate(paths.worlds)}>Gérer les mondes</Button>}>
          Créez ou sélectionnez un monde avant de démarrer le serveur.
        </Alert>
      )}
      {running && server.saveId !== server.activeSaveId && (
        <Alert tone="info">Le monde actif a changé : redémarrez le serveur pour l'utiliser.</Alert>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Stat
          icon={Package}
          label="Statut"
          value={
            <span className="flex items-center gap-2.5">
              <StatusDot tone={state.tone} pulse={running} className="size-2.5" />
              {state.label}
            </span>
          }
          hint={running ? `PID ${server.pid}` : "Prêt à démarrer"}
        />
        <Stat icon={Users} label="Joueurs connectés" value={running ? server.playerCount : "—"} hint={`Port UDP ${settings?.port ?? "…"}`} />
        <Stat icon={Clock} label="En ligne depuis" value={running ? formatDuration(server.startedAt) : "—"} />
        <Stat icon={Globe} label="Monde actif" value={activeSave?.name ?? "Aucun"} hint={activeSave ? `${saves?.saves.length} monde(s) au total` : undefined} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <RecentConsole />
        <Card>
          <CardHeader title="Informations" />
          <dl className="divide-y divide-white/[0.05] px-5 pb-2">
            <InfoRow label="Version du serveur">{setup.gameVersion ?? "—"}</InfoRow>
            <InfoRow label="Java">{setup.javaVersion ?? "—"}</InfoRow>
            <InfoRow label="Profil Hytale">{hytale.profileName ?? "—"}</InfoRow>
            <InfoRow label="Authentification">{settings?.authMode === "offline" ? "Offline" : "Hytale"}</InfoRow>
            <InfoRow label="Canal">{settings?.patchline ?? "—"}</InfoRow>
          </dl>
        </Card>
      </div>
    </div>
  );
}
