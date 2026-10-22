import { Bug, Gauge, Globe, Settings, SquareTerminal, Users, type LucideIcon } from "lucide-react";
import type { ComponentType } from "react";
import type { MessageKey } from "./lib/i18n";
import { ConsolePage } from "./pages/ConsolePage";
import { DashboardPage } from "./pages/DashboardPage";
import { ErrorsPage } from "./pages/ErrorsPage";
import { PlayersPage } from "./pages/players/PlayersPage";
import { SettingsPage } from "./pages/SettingsPage";
import { WorldsPage } from "./pages/worlds/WorldsPage";

export interface AppRoute {
  path: string;
  label: MessageKey;
  icon: LucideIcon;
  page: ComponentType;
}

/** Pages of the interface, in navigation order. */
export const ROUTES: AppRoute[] = [
  { path: "/", label: "common.nav.dashboard", icon: Gauge, page: DashboardPage },
  { path: "/console", label: "common.nav.console", icon: SquareTerminal, page: ConsolePage },
  { path: "/worlds", label: "common.nav.worlds", icon: Globe, page: WorldsPage },
  { path: "/players", label: "common.nav.players", icon: Users, page: PlayersPage },
  { path: "/errors", label: "common.nav.errors", icon: Bug, page: ErrorsPage },
  { path: "/settings", label: "common.nav.settings", icon: Settings, page: SettingsPage },
];

/** Paths used by in-page links, kept next to the route table. */
export const paths = {
  dashboard: "/",
  console: "/console",
  worlds: "/worlds",
  players: "/players",
  errors: "/errors",
  settings: "/settings",
} as const;
