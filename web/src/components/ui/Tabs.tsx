import { cn } from "../../lib/cn";

interface TabsProps<T extends string> {
  value: T;
  onChange: (value: T) => void;
  items: { value: T; label: string; count?: number }[];
}

export function Tabs<T extends string>({ value, onChange, items }: TabsProps<T>) {
  return (
    <div className="flex flex-wrap gap-x-5 border-b border-white/[0.06] px-5 sm:gap-x-6" role="tablist">
      {items.map((item) => {
        const active = item.value === value;
        return (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(item.value)}
            className={cn(
              "-mb-px flex h-11 cursor-pointer items-center gap-2 border-b-2 text-sm font-medium transition-colors",
              active ? "border-brand-500 text-zinc-100" : "border-transparent text-zinc-400 hover:text-zinc-200",
            )}
          >
            {item.label}
            {item.count !== undefined && (
              <span className="rounded-full bg-zinc-800 px-1.5 py-0.5 text-[11px] leading-none text-zinc-400">{item.count}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
