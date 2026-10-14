import { Gauge, Globe, Settings, SquareTerminal, Users, type LucideIcon } from "lucide-react";
import type { ComponentType } from "react";
import { ConsolePage } from "./pages/ConsolePage";
import { DashboardPage } from "./pages/DashboardPage";
import { PlayersPage } from "./pages/players/PlayersPage";
import { SettingsPage } from "./pages/SettingsPage";
import { WorldsPage } from "./pages/worlds/WorldsPage";

export interface AppRoute {
  path: string;
  label: string;
  icon: LucideIcon;
  page: ComponentType;
}

/** Pages of the interface, in navigation order. */
export const ROUTES: AppRoute[] = [
  { path: "/", label: "Tableau de bord", icon: Gauge, page: DashboardPage },
  { path: "/console", label: "Console", icon: SquareTerminal, page: ConsolePage },
  { path: "/worlds", label: "Mondes", icon: Globe, page: WorldsPage },
  { path: "/players", label: "Joueurs", icon: Users, page: PlayersPage },
  { path: "/settings", label: "Paramètres", icon: Settings, page: SettingsPage },
];

/** Paths used by in-page links, kept next to the route table. */
export const paths = {
  dashboard: "/",
  console: "/console",
  worlds: "/worlds",
  players: "/players",
  settings: "/settings",
} as const;
