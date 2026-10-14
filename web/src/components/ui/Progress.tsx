import { cn } from "../../lib/cn";

interface ProgressProps {
  /** Fraction between 0 and 1, or a negative value for an indeterminate bar. */
  value: number;
  className?: string;
}

export function Progress({ value, className }: ProgressProps) {
  const indeterminate = value < 0;
  return (
    <div
      className={cn("relative h-1.5 w-full overflow-hidden rounded-full bg-zinc-800", className)}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={indeterminate ? undefined : Math.round(value * 100)}
    >
      {indeterminate ? (
        <div className="absolute inset-y-0 w-1/3 animate-indeterminate rounded-full bg-brand-500" />
      ) : (
        <div
          className="h-full rounded-full bg-brand-500 transition-[width] duration-500"
          style={{ width: `${Math.min(Math.max(value, 0), 1) * 100}%` }}
        />
      )}
    </div>
  );
}
