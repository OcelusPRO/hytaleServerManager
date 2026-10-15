import { translate } from "./i18n";
// Console autocompletion: pure functions turning the typed text and the
// server's command catalog into suggestions and an argument hint.

export interface CommandArg {
  /** "player" for a positional argument, "--title" for a flag. */
  name: string;
  required?: boolean;
  flag?: boolean;
  /** False for boolean flags such as "--force". */
  takesValue?: boolean;
  type?: string;
  default?: string;
  description?: string;
  /** Accepted values when the type is an enumeration. */
  values?: string[];
}

export interface CommandEntry {
  /** Command path without slash, e.g. "tp all". */
  command: string;
  usage: string;
  description?: string;
  args: CommandArg[];
}

export type SuggestionKind = "command" | "flag" | "value" | "player" | "snippet";

export interface Suggestion {
  key: string;
  /** Full input after accepting the suggestion. */
  value: string;
  label: string;
  /** Secondary text: syntax of a command, type of a flag… */
  detail?: string;
  description?: string;
  /** Number of syntaxes of the command (shown as "+n variantes"). */
  variants?: number;
  kind: SuggestionKind;
  /** Caret position after accepting (end of value when undefined). */
  caret?: number;
}

export interface ArgumentHint {
  command: string;
  /** Syntaxes compatible with what has been typed. */
  syntaxes: CommandArg[][];
  /** Argument being typed, when it can be determined. */
  current?: CommandArg;
  /** True while typing a quoted value. */
  inQuotes: boolean;
}

export interface Completion {
  suggestions: Suggestion[];
  hint?: ArgumentHint;
}

const MAX_SUGGESTIONS = 8;
const PLAYER_ARG = /player|joueur|target|username/i;

// ------------------------------------------------------------ tokenizing

interface Tokens {
  /** Completed tokens, quotes removed ("" gives an empty token). */
  done: string[];
  /** Token under the cursor, quotes removed (may be empty). */
  current: string;
  /** Raw input up to the start of the current token. */
  prefix: string;
  /** True when the cursor is inside an unterminated quoted value. */
  inQuotes: boolean;
}

/**
 * Splits the input like the Hytale server does: spaces separate
 * arguments and "double quotes" group words (an empty "" is still an
 * argument). The server has no escape sequence: a quote always toggles.
 */
export function tokenize(input: string): Tokens {
  const done: string[] = [];
  let current = "";
  let started = false; // the current token exists, even if empty ("")
  let start = 0;
  let inQuotes = false;
  for (let i = 0; i < input.length; i++) {
    const c = input[i]!;
    if (c === '"') {
      inQuotes = !inQuotes;
      started = true;
    } else if (c === " " && !inQuotes) {
      if (started || current) done.push(current);
      current = "";
      started = false;
      start = i + 1;
    } else {
      current += c;
      started = true;
    }
  }
  return { done, current, prefix: input.slice(0, start), inQuotes };
}

/** Writes a value so that the server reads it as one argument. */
export function quote(value: string): string {
  return /\s/.test(value) || value === "" ? `"${value}"` : value;
}

/** Values containing a quote cannot be written for the server. */
const writable = (value: string) => !value.includes('"');

// ------------------------------------------------------------ argument state

interface ArgState {
  positional: number;
  used: Set<string>;
  /** Flag waiting for its value. */
  pending?: CommandArg;
}

function flagName(token: string): string {
  return token.split("=")[0]!.toLowerCase();
}

/** Replays the completed argument tokens against a syntax. */
function walk(args: CommandArg[], tokens: string[]): ArgState {
  const state: ArgState = { positional: 0, used: new Set() };
  for (const tok of tokens) {
    if (state.pending) {
      state.pending = undefined;
      continue;
    }
    if (tok.startsWith("--")) {
      const name = flagName(tok);
      state.used.add(name);
      const flag = args.find((a) => a.flag && a.name.toLowerCase() === name);
      if (flag?.takesValue && !tok.includes("=")) state.pending = flag;
      continue;
    }
    state.positional++;
  }
  return state;
}

const positionals = (args: CommandArg[]) => args.filter((a) => !a.flag);
const flags = (args: CommandArg[]) => args.filter((a) => a.flag);

// ------------------------------------------------------------ completion

export function complete(input: string, entries: CommandEntry[], onlinePlayers: string[]): Completion {
  const text = input.replace(/^\//, "");
  if (!text.trim()) return { suggestions: [] };
  const tokens = tokenize(text);
  const typed = tokens.done.map((t) => t.toLowerCase());

  // Deepest command whose words are all typed: we complete its arguments.
  const candidates = entries.filter((e) => {
    const w = e.command.toLowerCase().split(" ");
    return typed.length >= w.length && w.every((word, i) => typed[i] === word);
  });
  const depth = Math.max(0, ...candidates.map((e) => e.command.split(" ").length));
  const matched = candidates.filter((e) => e.command.split(" ").length === depth);

  const commands = tokens.inQuotes ? [] : commandSuggestions(typed, tokens.current.toLowerCase(), entries);
  if (matched.length === 0) return { suggestions: commands };

  const argTokens = tokens.done.slice(depth);
  // Keep the syntaxes that can still accept what was typed.
  const states = matched.map((e) => ({ entry: e, state: walk(e.args, argTokens) }));
  const compatible = states.filter(({ entry, state }) => state.positional <= positionals(entry.args).length);
  const usable = compatible.length ? compatible : states;

  const current = currentArg(usable, tokens.current);
  const hint: ArgumentHint = {
    command: matched[0]!.command,
    syntaxes: usable.map(({ entry }) => entry.args),
    current,
    inQuotes: tokens.inQuotes,
  };

  const raw = `/${tokens.prefix}`;
  const suggestions = argumentSuggestions(usable, tokens, raw, onlinePlayers);
  return { suggestions: [...commands, ...suggestions].slice(0, MAX_SUGGESTIONS), hint };
}

/** The argument the cursor is on, if unambiguous. */
function currentArg(usable: { entry: CommandEntry; state: ArgState }[], current: string): CommandArg | undefined {
  for (const { entry, state } of usable) {
    if (state.pending) return state.pending;
    if (current.startsWith("--")) {
      const name = flagName(current);
      const exact = flags(entry.args).find((a) => a.name.toLowerCase() === name);
      if (exact) return exact;
      continue;
    }
    const positional = positionals(entry.args)[state.positional];
    if (positional) return positional;
  }
  return undefined;
}

function argumentSuggestions(
  usable: { entry: CommandEntry; state: ArgState }[],
  tokens: Tokens,
  prefix: string,
  onlinePlayers: string[],
): Suggestion[] {
  const out = new Map<string, Suggestion>();
  const add = (s: Suggestion) => {
    if (!out.has(s.key)) out.set(s.key, s);
  };
  const { current, inQuotes } = tokens;

  /** Values for arg, typed after `before` (e.g. "--style="). */
  const offerValues = (arg: CommandArg, typedValue: string, before: string) => {
    const lower = typedValue.toLowerCase();
    const candidates = new Set<string>(arg.values ?? []);
    if (PLAYER_ARG.test(arg.name) || PLAYER_ARG.test(arg.type ?? "")) onlinePlayers.forEach((p) => candidates.add(p));
    for (const v of candidates) {
      if (!writable(v) || !v.toLowerCase().startsWith(lower) || (v.toLowerCase() === lower && !inQuotes)) continue;
      const isPlayer = !arg.values?.includes(v);
      add({
        key: `${isPlayer ? "player" : "value"}:${v}`,
        value: `${before}${quote(v)} `,
        label: v,
        detail: isPlayer ? translate("console.completion.onlinePlayer") : arg.name,
        kind: isPlayer ? "player" : "value",
      });
    }
    // Free text: help with quoting, and offer the default value.
    if (!arg.values?.length && arg.type?.toLowerCase() === "text" && typedValue === "" && !inQuotes) {
      add({
        key: `snippet:quotes:${arg.name}`,
        value: `${before}""`,
        caret: before.length + 1,
        label: '""',
        detail: translate("console.completion.quotedText"),
        description: translate("console.completion.quotedTextHint"),
        kind: "snippet",
      });
    }
    if (arg.default && writable(arg.default) && !arg.values?.length && typedValue === "" && !inQuotes) {
      add({ key: `default:${arg.name}`, value: `${before}${quote(arg.default)} `, label: quote(arg.default), detail: translate("console.completion.defaultHint"), kind: "value" });
    }
  };

  for (const { entry, state } of usable) {
    // "--style=ma": value written in the same token as the flag.
    const eq = current.indexOf("=");
    if (!inQuotes && current.startsWith("--") && eq > 0) {
      const flag = flags(entry.args).find((a) => a.name.toLowerCase() === current.slice(0, eq).toLowerCase());
      if (flag?.takesValue) offerValues(flag, current.slice(eq + 1), prefix + current.slice(0, eq + 1));
      continue;
    }
    if (state.pending) {
      offerValues(state.pending, current, prefix);
      continue;
    }
    if (!current.startsWith("-") || inQuotes) {
      const positional = positionals(entry.args)[state.positional];
      if (positional) offerValues(positional, current, prefix);
    }
    if (!inQuotes && (current === "" || current.startsWith("-"))) {
      const lower = current.toLowerCase();
      for (const f of flags(entry.args)) {
        const name = f.name.toLowerCase();
        if (state.used.has(name) || !name.startsWith(lower) || name === lower) continue;
        add({
          key: `flag:${f.name}`,
          value: `${prefix}${f.name} `,
          label: f.name,
          detail: f.takesValue ? `<${f.type?.toLowerCase() || translate("console.completion.value")}>` : undefined,
          description: f.description,
          kind: "flag",
        });
      }
    }
  }
  return [...out.values()];
}

/** Commands (or subcommands) that extend what has been typed. */
function commandSuggestions(typed: string[], current: string, entries: CommandEntry[]): Suggestion[] {
  const byCommand = new Map<string, CommandEntry[]>();
  for (const e of entries) {
    const w = e.command.toLowerCase().split(" ");
    if (w.length !== typed.length + 1) continue;
    if (!typed.every((t, i) => t === w[i])) continue;
    const last = w[w.length - 1]!;
    if (!last.startsWith(current) || (last === current && e.args.length === 0)) continue;
    const list = byCommand.get(e.command) ?? [];
    list.push(e);
    byCommand.set(e.command, list);
  }
  return [...byCommand.entries()]
    .sort(([a], [b]) => a.length - b.length || a.localeCompare(b))
    .map(([command, list]) => ({
      key: `cmd:${command}`,
      value: `/${command} `,
      label: command,
      detail: list.find((e) => e.usage)?.usage,
      description: list.find((e) => e.description)?.description,
      variants: list.length,
      kind: "command" as const,
    }));
}

// ------------------------------------------------------------ editing

export interface Edit {
  value: string;
  caret: number;
}

/**
 * Applies a suggestion computed for the text before the caret: the word
 * under the caret is replaced, the rest of the line is kept.
 */
export function applySuggestion(value: string, caret: number, s: Suggestion): Edit {
  let rest = value.slice(caret);
  // Drop the remainder of the word being completed ("--sec|ondary" or the
  // closing part of a quoted value), up to the next separator.
  const end = rest.search(/\s/);
  rest = end === -1 ? "" : rest.slice(end);
  if (s.value.endsWith(" ")) rest = rest.replace(/^\s+/, "");
  return { value: s.value + rest, caret: s.caret ?? s.value.length };
}
