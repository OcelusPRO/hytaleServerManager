import type { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cn } from "../../lib/cn";

export function Table({ className, ...props }: HTMLAttributes<HTMLTableElement>) {
  return (
    <div className="scrollbar-thin overflow-x-auto">
      <table className={cn("w-full text-left text-sm max-sm:block max-sm:[&_tbody]:block max-sm:[&_thead]:hidden", className)} {...props} />
    </div>
  );
}

export function Th({ className, ...props }: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        "h-10 border-b border-white/[0.06] px-5 text-xs font-medium tracking-wide text-zinc-500 uppercase first:pl-5 last:pr-5",
        className,
      )}
      {...props}
    />
  );
}

export function Td({ className, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  // On a phone a row becomes a wrapping line: no horizontal scrolling. A cell
  // with a data-label shows it in front of its value.
  return (
    <td
      className={cn(
        "h-14 border-b border-white/[0.04] px-5 align-middle text-zinc-300",
        "max-sm:h-auto max-sm:min-w-0 max-sm:border-0 max-sm:p-0 max-sm:py-1",
        "max-sm:data-label:before:mr-1 max-sm:data-label:before:text-zinc-500 max-sm:data-label:before:content-[attr(data-label)]",
        className,
      )}
      {...props}
    />
  );
}

export function Tr({ className, ...props }: HTMLAttributes<HTMLTableRowElement>) {
  return <tr className={cn("transition-colors last:[&>td]:border-0 hover:bg-white/[0.02]", "max-sm:flex max-sm:flex-wrap max-sm:items-center max-sm:gap-x-3 max-sm:border-b max-sm:border-white/[0.04] max-sm:px-4 max-sm:py-2 max-sm:last:border-0", className)} {...props} />;
}
