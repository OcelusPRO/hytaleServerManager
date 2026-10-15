import { useId } from "react";
import { cn } from "../lib/cn";
import { LANGUAGES, type Lang } from "../lib/i18n";

// Flags are drawn in SVG: Windows does not render flag emojis.

function FlagGB() {
  const id = useId();
  return (
    <svg viewBox="0 0 60 30" className="h-3.5 w-7 shrink-0 rounded-[2px] ring-1 ring-white/10" aria-hidden>
      <clipPath id={`${id}s`}>
        <path d="M0,0 v30 h60 v-30 z" />
      </clipPath>
      <clipPath id={`${id}t`}>
        <path d="M30,15 h30 v15 z v15 h-30 z h-30 v-15 z v-15 h30 z" />
      </clipPath>
      <g clipPath={`url(#${id}s)`}>
        <path d="M0,0 v30 h60 v-30 z" fill="#012169" />
        <path d="M0,0 L60,30 M60,0 L0,30" stroke="#fff" strokeWidth="6" />
        <path d="M0,0 L60,30 M60,0 L0,30" clipPath={`url(#${id}t)`} stroke="#C8102E" strokeWidth="4" />
        <path d="M30,0 v30 M0,15 h60" stroke="#fff" strokeWidth="10" />
        <path d="M30,0 v30 M0,15 h60" stroke="#C8102E" strokeWidth="6" />
      </g>
    </svg>
  );
}

function FlagFR() {
  return (
    <svg viewBox="0 0 3 2" className="h-3.5 w-[21px] shrink-0 rounded-[2px] ring-1 ring-white/10" aria-hidden>
      <rect width="1" height="2" fill="#002654" />
      <rect x="1" width="1" height="2" fill="#fff" />
      <rect x="2" width="1" height="2" fill="#CE1126" />
    </svg>
  );
}

const FLAGS: Record<Lang, () => React.JSX.Element> = { en: FlagGB, fr: FlagFR };

/** Language choice as flag-labelled radio buttons. */
export function LanguagePicker({ value, onChange, label }: { value: Lang; onChange: (lang: Lang) => void; label: string }) {
  const name = useId();
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-2">
      {LANGUAGES.map((l) => {
        const Flag = FLAGS[l.code];
        const checked = l.code === value;
        return (
          <label
            key={l.code}
            className={cn(
              "flex h-9 cursor-pointer items-center gap-2.5 rounded-lg px-3 text-sm ring-1 transition-colors has-focus-visible:outline-2 has-focus-visible:outline-brand-400",
              checked ? "bg-brand-500/10 text-zinc-100 ring-brand-500/40" : "bg-zinc-950/40 text-zinc-300 ring-white/[0.08] hover:bg-white/[0.03]",
            )}
          >
            <input type="radio" name={name} value={l.code} checked={checked} onChange={() => onChange(l.code)} className="sr-only" />
            <Flag />
            {l.label}
          </label>
        );
      })}
    </div>
  );
}
