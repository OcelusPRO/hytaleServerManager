import type { Tone } from "../components/ui/Badge";
import type { MessageKey } from "./i18n";
import type { ServerState } from "./types";

export const serverStates: Record<ServerState, { label: MessageKey; tone: Tone }> = {
  stopped: { label: "common.states.stopped", tone: "neutral" },
  running: { label: "common.states.running", tone: "success" },
  stopping: { label: "common.states.stopping", tone: "warning" },
  crashed: { label: "common.states.crashed", tone: "danger" },
  sleeping: { label: "common.states.sleeping", tone: "info" },
};
