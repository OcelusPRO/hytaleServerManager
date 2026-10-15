import { Flag, List, Quote, User, type LucideIcon } from "lucide-react";
import { cn } from "../../lib/cn";
import type { ArgumentHint, CommandArg, Suggestion, SuggestionKind } from "../../lib/commandCompletion";
import { translate as t } from "../../lib/i18n";

const kindIcons: Record<Exclude<SuggestionKind, "command">, { icon: LucideIcon; className: string }> = {
  flag: { icon: Flag, className: "text-violet-400" },
  value: { icon: List, className: "text-emerald-400" },
  player: { icon: User, className: "text-sky-400" },
  snippet: { icon: Quote, className: "text-amber-300" },
};

/** Display form of an argument inside a syntax line. */
function argLabel(arg: CommandArg): string {
  if (arg.flag) return arg.takesValue ? `[${arg.name} <${(arg.type || t("console.completion.value")).toLowerCase()}>]` : `[${arg.name}]`;
  return arg.required === false ? `[${arg.name}]` : `<${arg.name}>`;
}

function Syntax({ command, args, current }: { command: string; args: CommandArg[]; current?: CommandArg }) {
  return (
    <li className="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 font-mono text-xs">
      <span className="text-brand-400">/{command}</span>
      {args.length === 0 && <span className="font-sans text-zinc-500">{t("console.completion.noArgument")}</span>}
      {args.map((arg) => (
        <span
          key={arg.name}
          className={cn(
            arg === current || arg.name === current?.name
              ? "rounded bg-brand-500/15 px-1 text-brand-300"
              : arg.flag
                ? "text-zinc-500"
                : "text-zinc-300",
          )}
        >
          {argLabel(arg)}
        </span>
      ))}
    </li>
  );
}

/** Details of the argument being typed. */
function ArgDetails({ arg, inQuotes }: { arg: CommandArg; inQuotes: boolean }) {
  return (
    <div className="mt-2 flex flex-col gap-1 border-t border-white/[0.06] pt-2 text-xs">
      <p className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-mono text-brand-300">{arg.name}</span>
        {arg.type && <span className="text-zinc-500">{arg.type}</span>}
        {arg.default && <span className="text-zinc-500">{t("console.completion.defaultValue", { value: arg.default })}</span>}
        {arg.flag && !arg.takesValue && <span className="text-zinc-500">{t("console.completion.flagOnly")}</span>}
      </p>
      {arg.description && <p className="text-zinc-300">{arg.description}</p>}
      {arg.values && arg.values.length > 0 && (
        <p className="flex flex-wrap gap-1">
          {arg.values.map((v) => (
            <span key={v} className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[11px] text-zinc-300">
              {v}
            </span>
          ))}
        </p>
      )}
      {arg.type?.toLowerCase() === "text" && (
        <p className="text-zinc-500">
          {inQuotes
            ? t("console.completion.closeQuotes")
            : t("console.completion.useQuotes")}
        </p>
      )}
    </div>
  );
}

function SuggestionRow({ s, active, onPick }: { s: Suggestion; active: boolean; onPick: () => void }) {
  const kind = s.kind === "command" ? null : kindIcons[s.kind];
  return (
    <li
      role="option"
      aria-selected={active}
      // mousedown keeps the focus in the input
      onMouseDown={(e) => {
        e.preventDefault();
        onPick();
      }}
      className={cn("flex cursor-pointer items-center gap-3 px-3 py-2", active ? "bg-white/[0.06]" : "hover:bg-white/[0.03]")}
    >
      <span className="flex w-4 shrink-0 justify-center">
        {kind ? <kind.icon className={cn("size-3.5", kind.className)} aria-hidden /> : <span className="font-mono text-xs text-brand-400">/</span>}
      </span>
      <span className="shrink-0 font-mono text-sm text-zinc-100">{s.label}</span>
      {s.detail && <span className="min-w-0 truncate font-mono text-xs text-zinc-500">{s.detail}</span>}
      <span className="ml-auto flex shrink-0 items-center gap-2 pl-2 text-xs text-zinc-500">
        {s.description && <span className="hidden max-w-80 truncate md:inline">{s.description}</span>}
        {s.variants !== undefined && s.variants > 1 && <span>{t("console.completion.variants", { n: s.variants - 1 })}</span>}
        {active && <kbd className="rounded bg-zinc-800 px-1.5 py-0.5 font-sans text-[10px] text-zinc-400">Tab</kbd>}
      </span>
    </li>
  );
}

interface CompletionPanelProps {
  suggestions: Suggestion[];
  hint?: ArgumentHint;
  active: number;
  onPick: (s: Suggestion) => void;
}

/** Floating panel above the console input. */
export function CompletionPanel({ suggestions, hint, active, onPick }: CompletionPanelProps) {
  const showHint = hint && hint.syntaxes.length > 0;
  if (!showHint && suggestions.length === 0) return null;
  return (
    <div className="absolute inset-x-3 bottom-full mb-2 overflow-hidden rounded-xl bg-zinc-900 shadow-2xl ring-1 shadow-black/50 ring-white/10">
      {showHint && (
        <div className="border-b border-white/[0.06] px-3 py-2.5">
          <p className="mb-1.5 text-[11px] font-medium tracking-wide text-zinc-500 uppercase">{t("console.completion.syntax")}</p>
          <ul className="flex flex-col gap-1">
            {hint.syntaxes.slice(0, 4).map((args, i) => (
              <Syntax key={i} command={hint.command} args={args} current={hint.current} />
            ))}
          </ul>
          {hint.current && <ArgDetails arg={hint.current} inQuotes={hint.inQuotes} />}
        </div>
      )}
      {suggestions.length > 0 && (
        <ul role="listbox" className="scrollbar-thin max-h-72 overflow-y-auto py-1">
          {suggestions.map((s, i) => (
            <SuggestionRow key={s.key} s={s} active={i === active} onPick={() => onPick(s)} />
          ))}
        </ul>
      )}
    </div>
  );
}
