import { CornerDownLeft, SendHorizontal } from "lucide-react";
import { useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { applySuggestion, complete, type CommandEntry, type Suggestion } from "../../lib/commandCompletion";
import { Button } from "../ui/Button";
import { CompletionPanel } from "./CompletionPanel";
import { useCommandHistory } from "./useCommandHistory";
import { translate as t } from "../../lib/i18n";

interface CommandInputProps {
  entries: CommandEntry[];
  onlinePlayers: string[];
  disabled: boolean;
  /** Sends the command; resolves to true when it was accepted. */
  onSubmit: (command: string) => Promise<boolean>;
}

export function CommandInput({ entries, onlinePlayers, disabled, onSubmit }: CommandInputProps) {
  const [value, setValue] = useState("");
  // Completion works on the text before the caret, wherever it is.
  const [caret, setCaret] = useState(0);
  const [active, setActive] = useState(0);
  const [open, setOpen] = useState(true);
  const input = useRef<HTMLInputElement>(null);
  const history = useCommandHistory();

  const beforeCaret = value.slice(0, caret);
  const completion = useMemo(() => complete(beforeCaret, entries, onlinePlayers), [beforeCaret, entries, onlinePlayers]);
  const suggestions = open ? completion.suggestions : [];
  const hint = open ? completion.hint : undefined;

  const change = (next: string, nextCaret = next.length) => {
    setValue(next);
    setCaret(nextCaret);
    setActive(0);
    setOpen(true);
  };

  /** Follows the caret when it moves without the text changing. */
  const syncCaret = () => {
    const position = input.current?.selectionStart ?? value.length;
    if (position !== caret) {
      setCaret(position);
      setActive(0);
    }
  };

  const accept = (s: Suggestion) => {
    const edit = applySuggestion(value, caret, s);
    change(edit.value, edit.caret);
    const el = input.current;
    if (!el) return;
    el.focus();
    // Place the caret once React has rendered the new value (e.g. between
    // the quotes of an inserted ""). Rendering moves the browser caret to
    // the end first, so the caret state is set again afterwards.
    requestAnimationFrame(() => {
      el.setSelectionRange(edit.caret, edit.caret);
      setCaret(edit.caret);
    });
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const command = value.trim();
    if (!command) return;
    if (await onSubmit(command)) {
      history.push(command);
      change("");
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    const listOpen = suggestions.length > 0;
    switch (event.key) {
      case "Tab":
        // Tab never leaves the console input.
        event.preventDefault();
        if (listOpen) accept(suggestions[active] ?? suggestions[0]!);
        return;
      case "Escape":
        setOpen(false);
        return;
      case "ArrowDown":
      case "ArrowUp": {
        event.preventDefault();
        const step = event.key === "ArrowDown" ? 1 : -1;
        if (listOpen) {
          setActive((i) => (i + step + suggestions.length) % suggestions.length);
          return;
        }
        const recalled = history.navigate(step === -1 ? -1 : 1);
        if (recalled !== undefined) {
          setValue(recalled);
          setCaret(recalled.length);
          setOpen(false);
        }
        return;
      }
    }
  };

  return (
    <form onSubmit={submit} className="relative border-t border-white/[0.06] p-3">
      {!disabled && <CompletionPanel suggestions={suggestions} hint={hint} active={active} onPick={accept} />}
      <div className="flex items-center gap-2">
        <span className="pl-2 font-mono text-sm text-brand-400 select-none">&gt;</span>
        <input
          ref={input}
          value={value}
          onChange={(e) => change(e.target.value, e.target.selectionStart ?? e.target.value.length)}
          onSelect={syncCaret}
          onKeyUp={syncCaret}
          onClick={syncCaret}
          onKeyDown={onKeyDown}
          onBlur={() => setOpen(false)}
          onFocus={() => setOpen(true)}
          placeholder={disabled ? t("console.input.stopped") : t("console.input.placeholder")}
          disabled={disabled}
          maxLength={1024}
          autoComplete="off"
          spellCheck={false}
          role="combobox"
          aria-expanded={suggestions.length > 0}
          aria-autocomplete="list"
          className="h-9 min-w-0 flex-1 bg-transparent font-mono text-sm text-zinc-100 placeholder:text-zinc-500 focus:outline-none disabled:opacity-50"
        />
        <span className="hidden items-center gap-1 text-[11px] text-zinc-500 sm:flex">
          <kbd className="rounded bg-zinc-800 px-1.5 py-0.5">Tab</kbd> {t("console.input.complete")}
          <kbd className="ml-2 inline-flex items-center rounded bg-zinc-800 px-1.5 py-0.5">
            <CornerDownLeft className="size-3" />
          </kbd>
          {t("console.input.send")}
        </span>
        <Button type="submit" variant="primary" icon={SendHorizontal} disabled={disabled || !value.trim()}>
          {t("console.input.sendButton")}
        </Button>
      </div>
    </form>
  );
}
