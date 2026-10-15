// Interface translations. English is the default; the browser's language
// is used when supported, and the operator's choice is remembered.
//
// Messages live in src/locales/sections: each section holds the English
// text and its translations side by side, the translations being typed
// after the English shape so that a missing key fails the build.

import { useSyncExternalStore } from "react";
import { en, fr, type Messages } from "../locales";
import { serverMessagesFr } from "../locales/serverMessages.fr";

export type Lang = "en" | "fr";

export const LANGUAGES: ReadonlyArray<{ code: Lang; label: string }> = [
  { code: "en", label: "English" },
  { code: "fr", label: "Français" },
];

const dictionaries: Record<Lang, Messages> = { en, fr };
const STORAGE_KEY = "hsm.lang";

/** Dotted paths of every message ("dashboard.title"). */
type Leaves<T, P extends string = ""> = {
  [K in keyof T & string]: T[K] extends string ? `${P}${K}` : Leaves<T[K], `${P}${K}.`>;
}[keyof T & string];
export type MessageKey = Leaves<Messages>;

export type Params = Record<string, string | number>;

function supported(code: string | null | undefined): Lang | undefined {
  const base = code?.toLowerCase().split("-")[0];
  return LANGUAGES.find((l) => l.code === base)?.code;
}

function detect(): Lang {
  try {
    const saved = supported(globalThis.localStorage?.getItem(STORAGE_KEY));
    if (saved) return saved;
  } catch {
    // storage unavailable
  }
  if (typeof navigator === "undefined") return "en"; // tests
  for (const code of navigator.languages ?? [navigator.language]) {
    const lang = supported(code);
    if (lang) return lang;
  }
  return "en";
}

// ---- language store, shared by React and plain modules ----

let current: Lang = detect();
const listeners = new Set<() => void>();
if (typeof document !== "undefined") document.documentElement.lang = current;

export function getLang(): Lang {
  return current;
}

export function setLang(lang: Lang) {
  if (lang === current) return;
  current = lang;
  document.documentElement.lang = lang;
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    // not remembered
  }
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// ---- lookup ----

function lookup(dict: Messages, key: string): string | undefined {
  let node: unknown = dict;
  for (const part of key.split(".")) {
    node = (node as Record<string, unknown> | undefined)?.[part];
  }
  return typeof node === "string" ? node : undefined;
}

function interpolate(text: string, params?: Params): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in params ? String(params[name]) : match));
}

/** Translates key in the current language (plain modules). */
export function translate(key: MessageKey, params?: Params): string {
  return interpolate(lookup(dictionaries[current], key) ?? lookup(en, key) ?? key, params);
}

/**
 * Picks the singular or plural form: key + "One" / key + "Other", the
 * count being available as {count}.
 */
export function translatePlural(key: string, count: number, params?: Params): string {
  const form = new Intl.PluralRules(current).select(count) === "one" ? "One" : "Other";
  return translate(`${key}${form}` as MessageKey, { count, ...params });
}

// ---- messages coming from the server (always in English) ----

interface ServerPattern {
  regex: RegExp;
  translation: string;
}

const serverPatterns: Partial<Record<Lang, ServerPattern[]>> = {};

function patternsFor(lang: Lang): ServerPattern[] {
  if (lang !== "fr") return [];
  serverPatterns.fr ??= serverMessagesFr.map(([source, translation]) => ({
    regex: new RegExp("^" + source.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/\\\{\d+\\\}/g, "(.+?)") + "$", "s"),
    translation,
  }));
  return serverPatterns.fr;
}

/**
 * Translates a message produced by the server (error, console line). Values
 * inserted by the server are translated too when they are messages
 * themselves (wrapped errors); unknown messages are shown as they are.
 */
export function translateServer(message: string, lang: Lang = current, depth = 0): string {
  if (lang === "en" || !message || depth > 4) return message;
  if (message.includes("\n")) {
    return message
      .split("\n")
      .map((line) => translateServer(line, lang, depth))
      .join("\n");
  }
  for (const { regex, translation } of patternsFor(lang)) {
    const m = regex.exec(message);
    if (m) {
      return translation.replace(/\{(\d+)\}/g, (_, i: string) => translateServer(m[Number(i) + 1] ?? "", lang, depth + 1));
    }
  }
  return message;
}

// ---- React ----

export interface I18n {
  lang: Lang;
  /** BCP 47 locale for Intl formatting. */
  locale: string;
  t: (key: MessageKey, params?: Params) => string;
  tp: (key: string, count: number, params?: Params) => string;
  tServer: (message: string) => string;
  setLang: (lang: Lang) => void;
}

/** Current language and translation helpers; re-renders on change. */
export function useI18n(): I18n {
  const lang = useSyncExternalStore(subscribe, getLang);
  return {
    lang,
    locale: lang,
    t: translate,
    tp: translatePlural,
    tServer: (message) => translateServer(message, lang),
    setLang,
  };
}
