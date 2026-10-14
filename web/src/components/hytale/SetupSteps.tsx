import { Check, Circle, LoaderCircle } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import { formatPercent } from "../../lib/format";
import type { HytaleAuthStatus, SetupStatus } from "../../lib/types";
import { Progress } from "../ui/Progress";

type StepState = "done" | "active" | "waiting";

interface StepProps {
  state: StepState;
  title: string;
  detail: ReactNode;
  progress?: number;
}

function Step({ state, title, detail, progress }: StepProps) {
  return (
    <li className="flex gap-3">
      <span
        className={cn(
          "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full",
          state === "done" && "bg-emerald-500/15 text-emerald-400",
          state === "active" && "bg-brand-500/15 text-brand-400",
          state === "waiting" && "text-zinc-600",
        )}
      >
        {state === "done" && <Check className="size-3.5" strokeWidth={3} />}
        {state === "active" && <LoaderCircle className="size-3.5 animate-spin" />}
        {state === "waiting" && <Circle className="size-3.5" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-3">
          <p className={cn("text-sm font-medium", state === "waiting" ? "text-zinc-500" : "text-zinc-200")}>{title}</p>
          {progress !== undefined && progress >= 0 && (
            <span className="text-xs text-zinc-400 tabular-nums">{formatPercent(progress)}</span>
          )}
        </div>
        <p className="mt-0.5 text-xs text-zinc-500">{detail}</p>
        {progress !== undefined && <Progress value={progress} className="mt-2" />}
      </div>
    </li>
  );
}

/** Read-only view of the automatic installation steps. */
export function SetupSteps({ setup, hytale }: { setup: SetupStatus; hytale: HytaleAuthStatus }) {
  const javaActive = setup.activity === "java";
  const gameActive = ["checking", "downloading", "extracting"].includes(setup.activity);
  const accountReady = hytale.authenticated && Boolean(hytale.profileUuid);

  const gameDetail =
    setup.activity === "extracting"
      ? "Installation des fichiers…"
      : setup.activity === "downloading"
        ? "Téléchargement depuis les serveurs Hytale"
        : setup.gameReady
          ? `Version ${setup.gameVersion ?? "installée"}`
          : "Démarre automatiquement après la connexion";

  return (
    <ol className="flex flex-col gap-4">
      <Step
        state={setup.javaReady ? "done" : "active"}
        title="Environnement Java 25"
        detail={setup.javaReady ? (setup.javaVersion ?? "Installé") : "Téléchargement en cours"}
        progress={javaActive ? setup.progress : undefined}
      />
      <Step
        state={accountReady ? "done" : hytale.login.state === "pending" ? "active" : "waiting"}
        title="Compte Hytale"
        detail={accountReady ? `Connecté en tant que ${hytale.profileName ?? "—"}` : "En attente de connexion"}
      />
      <Step
        state={setup.gameReady && !gameActive ? "done" : gameActive ? "active" : "waiting"}
        title="Fichiers du serveur"
        detail={gameDetail}
        progress={gameActive ? setup.progress : undefined}
      />
    </ol>
  );
}
