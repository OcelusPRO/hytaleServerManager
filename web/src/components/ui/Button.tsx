import { LoaderCircle, type LucideIcon } from "lucide-react";
import { useState, type ComponentProps, type MouseEvent } from "react";
import { cn } from "../../lib/cn";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md" | "lg";

const variants: Record<Variant, string> = {
  primary: "bg-brand-500 text-zinc-950 hover:bg-brand-400 shadow-sm shadow-brand-500/20",
  secondary: "bg-zinc-800 text-zinc-100 hover:bg-zinc-700 ring-1 ring-inset ring-white/5",
  ghost: "text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100",
  danger: "bg-red-500/10 text-red-400 hover:bg-red-500/20 ring-1 ring-inset ring-red-500/20",
};

const sizes: Record<Size, string> = {
  sm: "h-8 gap-1.5 px-2.5 text-xs",
  md: "h-9 gap-2 px-3.5 text-sm",
  lg: "h-11 gap-2.5 px-5 text-base",
};

const iconSizes: Record<Size, string> = { sm: "size-3.5", md: "size-4", lg: "size-5" };

export interface ButtonProps extends Omit<ComponentProps<"button">, "onClick"> {
  variant?: Variant;
  size?: Size;
  icon?: LucideIcon;
  /** Async handlers show a spinner and disable the button while running. */
  onClick?: (event: MouseEvent<HTMLButtonElement>) => void | Promise<unknown>;
}

export function Button({
  variant = "secondary",
  size = "md",
  icon: Icon,
  className,
  children,
  disabled,
  onClick,
  type = "button",
  ...props
}: ButtonProps) {
  const [busy, setBusy] = useState(false);

  const handleClick = async (event: MouseEvent<HTMLButtonElement>) => {
    if (!onClick) return;
    const result = onClick(event);
    if (result instanceof Promise) {
      setBusy(true);
      try {
        await result;
      } finally {
        setBusy(false);
      }
    }
  };

  const Glyph = busy ? LoaderCircle : Icon;
  return (
    <button
      type={type}
      disabled={disabled || busy}
      onClick={handleClick}
      className={cn(
        "inline-flex shrink-0 cursor-pointer items-center justify-center rounded-lg font-medium whitespace-nowrap transition-colors",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-400",
        "disabled:pointer-events-none disabled:opacity-50",
        variants[variant],
        sizes[size],
        className,
      )}
      {...props}
    >
      {Glyph && <Glyph className={cn(iconSizes[size], busy && "animate-spin")} aria-hidden />}
      {children}
    </button>
  );
}
