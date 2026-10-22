import { LogOut } from "lucide-react";
import { NavLink } from "react-router";
import { useStatus } from "../../hooks/useStatus";
import { cn } from "../../lib/cn";
import { serverStates } from "../../lib/serverState";
import { Badge } from "../ui/Badge";
import type { AppRoute } from "../../routes";
import { Logo } from "./Logo";
import { translate as t } from "../../lib/i18n";

interface SidebarProps {
  items: AppRoute[];
  onLogout: () => void;
}

export function Sidebar({ items, onLogout }: SidebarProps) {
  const { status } = useStatus();
  const server = status?.server;
  const state = server ? serverStates[server.state] : undefined;

  return (
    <aside className="flex flex-col border-white/[0.06] bg-zinc-900/40 max-lg:border-b lg:fixed lg:inset-y-0 lg:w-64 lg:border-r">
      <div className="flex h-20 items-center justify-between gap-4 px-5">
        <Logo />
        {state && (
          <Badge tone={state.tone} dot pulse={server?.state === "running"} className="lg:hidden">
            {t(state.label)}
          </Badge>
        )}
      </div>

      <nav className="grid grid-cols-6 gap-1 px-2 pb-3 lg:flex lg:flex-1 lg:flex-col lg:px-3 lg:pt-2">
        {items.map(({ path, label, icon: Icon }) => (
          <NavLink
            key={path}
            to={path}
            end
            className={({ isActive }) =>
              cn(
                "group flex min-w-0 items-center rounded-lg font-medium transition-colors",
                "max-lg:min-h-14 max-lg:flex-col max-lg:justify-center max-lg:gap-1 max-lg:px-1 max-lg:text-center max-lg:text-[11px] max-lg:leading-tight",
                "lg:h-9 lg:shrink-0 lg:gap-3 lg:px-3 lg:text-sm",
                isActive ? "active bg-white/[0.06] text-zinc-50" : "text-zinc-400 hover:bg-white/[0.03] hover:text-zinc-200",
              )
            }
          >
            <Icon className="size-4 text-zinc-500 group-[.active]:text-brand-400" aria-hidden />
            {t(label)}
          </NavLink>
        ))}
      </nav>

      <div className="hidden border-t border-white/[0.06] p-3 lg:block">
        {state && (
          <div className="mb-2 flex items-center justify-between rounded-lg px-3 py-2">
            <span className="text-xs text-zinc-500">{t("shell.sidebar.server")}</span>
            <Badge tone={state.tone} dot pulse={server?.state === "running"}>
              {t(state.label)}
            </Badge>
          </div>
        )}
        <button
          type="button"
          onClick={onLogout}
          className="flex h-9 w-full cursor-pointer items-center gap-3 rounded-lg px-3 text-sm font-medium text-zinc-400 transition-colors hover:bg-white/[0.03] hover:text-zinc-200"
        >
          <LogOut className="size-4 text-zinc-500" aria-hidden />
          {t("shell.sidebar.logout")}
        </button>
      </div>
    </aside>
  );
}
