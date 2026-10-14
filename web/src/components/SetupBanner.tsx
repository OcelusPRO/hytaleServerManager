import { Download } from "lucide-react";
import { useStatus } from "../hooks/useStatus";
import { formatPercent } from "../lib/format";
import { Progress } from "./ui/Progress";

const labels = {
  java: "Téléchargement de Java",
  checking: "Recherche de la dernière version",
  downloading: "Téléchargement du serveur Hytale",
  extracting: "Installation du serveur Hytale",
} as const;

/** Slim progress strip shown while the provisioner works in background. */
export function SetupBanner() {
  const { status } = useStatus();
  const setup = status?.setup;
  if (!setup || setup.activity === "idle" || (setup.activity === "checking" && setup.gameReady)) return null;

  return (
    <div className="flex items-center gap-4 rounded-xl bg-zinc-900/70 px-4 py-3 ring-1 ring-white/[0.06]">
      <Download className="size-4 shrink-0 text-brand-400" aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-3">
          <p className="truncate text-sm font-medium text-zinc-200">{labels[setup.activity]}</p>
          {setup.progress >= 0 && <span className="text-xs text-zinc-400 tabular-nums">{formatPercent(setup.progress)}</span>}
        </div>
        <Progress value={setup.progress} className="mt-2" />
      </div>
    </div>
  );
}
