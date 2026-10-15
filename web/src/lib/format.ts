// Display helpers, in the interface language.

import { getLang, translate, type MessageKey } from "./i18n";

const units: MessageKey[] = ["common.units.bytes", "common.units.kilobytes", "common.units.megabytes", "common.units.gigabytes", "common.units.terabytes"];

export function formatBytes(bytes: number): string {
  if (!bytes) return `0 ${translate(units[0]!)}`;
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = (bytes / 1024 ** i).toLocaleString(getLang(), { maximumFractionDigits: i ? 1 : 0, minimumFractionDigits: i ? 1 : 0 });
  return `${value} ${translate(units[i]!)}`;
}

/** Megabytes, shown in GB above 1024. */
export function formatMegabytes(mb: number): string {
  return mb >= 1024
    ? `${(mb / 1024).toLocaleString(getLang(), { maximumFractionDigits: 1 })} ${translate("common.units.gigabytes")}`
    : `${Math.round(mb)} ${translate("common.units.megabytes")}`;
}

export function formatDate(value?: string): string {
  return value ? new Intl.DateTimeFormat(getLang(), { dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) : "—";
}

export function formatTime(value?: string): string {
  return value ? new Intl.DateTimeFormat(getLang(), { timeStyle: "medium" }).format(new Date(value)) : "";
}

/** Two units of a duration in seconds ("2 h 5 min"). */
export function formatSeconds(total: number): string {
  let s = Math.max(0, Math.floor(total));
  const d = Math.floor(s / 86400);
  s %= 86400;
  const h = Math.floor(s / 3600);
  s %= 3600;
  const m = Math.floor(s / 60);
  s %= 60;
  const u = (key: "days" | "hours" | "minutes" | "seconds", n: number) => translate(`common.units.${key}`, { n });
  if (d) return `${u("days", d)} ${u("hours", h)}`;
  if (h) return `${u("hours", h)} ${u("minutes", m)}`;
  if (m) return `${u("minutes", m)} ${u("seconds", s)}`;
  return u("seconds", s);
}

/** Time elapsed since fromIso. */
export function formatDuration(fromIso?: string): string {
  if (!fromIso) return "—";
  return formatSeconds((Date.now() - new Date(fromIso).getTime()) / 1000);
}

export function formatPercent(fraction: number): string {
  return new Intl.NumberFormat(getLang(), { style: "percent", maximumFractionDigits: 0 }).format(Math.min(Math.max(fraction, 0), 1));
}
