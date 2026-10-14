import type { Tone } from "../components/ui/Badge";
import type { ServerState } from "./types";

export const serverStates: Record<ServerState, { label: string; tone: Tone }> = {
  stopped: { label: "Arrêté", tone: "neutral" },
  running: { label: "En ligne", tone: "success" },
  stopping: { label: "Arrêt en cours", tone: "warning" },
  crashed: { label: "Planté", tone: "danger" },
  sleeping: { label: "En veille", tone: "info" },
};
