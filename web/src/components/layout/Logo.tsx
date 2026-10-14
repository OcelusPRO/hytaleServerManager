import { cn } from "../../lib/cn";

interface LogoProps {
  className?: string;
  /** "sm" adds the product name beside the icon (navigation header); "lg" is the bare logo for full-page screens. */
  size?: "sm" | "lg";
}

export function Logo({ className, size = "sm" }: LogoProps) {
  const small = size === "sm";
  return (
    <div className={cn("flex items-center gap-3", className)}>
      <img
        src="/logo.png"
        alt={small ? "" : "HSM — Hytale Server Manager"}
        draggable={false}
        className={cn("select-none", small ? "h-14 w-auto shrink-0" : "h-auto w-56 max-w-full")}
      />
      {small && (
        <div className="min-w-0 leading-tight">
          <p className="text-sm font-semibold text-zinc-100">Hytale</p>
          <p className="text-xs text-zinc-500">Server Manager</p>
        </div>
      )}
    </div>
  );
}
