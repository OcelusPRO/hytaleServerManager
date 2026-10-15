import type { ReactNode } from "react";
import { cn } from "../../lib/cn";
import type { PlayerRef } from "../../lib/types";
import { translate as t } from "../../lib/i18n";

const palette = [
  "bg-amber-500/15 text-amber-300 ring-amber-500/25",
  "bg-sky-500/15 text-sky-300 ring-sky-500/25",
  "bg-emerald-500/15 text-emerald-300 ring-emerald-500/25",
  "bg-violet-500/15 text-violet-300 ring-violet-500/25",
  "bg-rose-500/15 text-rose-300 ring-rose-500/25",
];

function colorFor(uuid: string) {
  let hash = 0;
  for (const c of uuid) hash = (hash * 31 + c.charCodeAt(0)) | 0;
  return palette[Math.abs(hash) % palette.length];
}

export function displayName(player: PlayerRef): string {
  return player.name || t("players.unknownPlayer");
}

interface PlayerIdentityProps {
  player: PlayerRef;
  detail?: ReactNode;
  size?: "md" | "lg";
}

/** Avatar initial, name and secondary line (UUID by default). */
export function PlayerIdentity({ player, detail, size = "md" }: PlayerIdentityProps) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <span
        className={cn(
          "flex shrink-0 items-center justify-center rounded-full font-semibold ring-1",
          size === "lg" ? "size-12 text-lg" : "size-8 text-xs",
          colorFor(player.uuid),
        )}
        aria-hidden
      >
        {(player.name || "?").slice(0, 1).toUpperCase()}
      </span>
      <div className="min-w-0">
        <p className={cn("truncate font-medium", size === "lg" ? "text-base text-zinc-50" : "text-sm text-zinc-100", !player.name && "italic text-zinc-400")}>
          {displayName(player)}
        </p>
        {detail ? (
          <p className="truncate text-xs text-zinc-500">{detail}</p>
        ) : (
          <p className="truncate font-mono text-[11px] text-zinc-500">{player.uuid}</p>
        )}
      </div>
    </div>
  );
}
