// Presentation hints for well-known configuration keys. Unknown keys are
// still editable: the form is derived from the JSON values themselves.

import { translate, type MessageKey } from "./i18n";
import type { ConfigKind, JsonObject, JsonValue } from "./types";

export interface FieldMeta {
  label?: string;
  hint?: string;
  min?: number;
  max?: number;
  step?: number;
  options?: { value: string; label: string }[];
  password?: boolean;
  /** Shown but not editable (internal values). */
  readOnly?: boolean;
}

// Bounds and options; labels and hints come from the configFields messages.
const server: Record<string, Omit<FieldMeta, "label" | "hint">> = {
  Password: { password: true },
  MaxPlayers: { min: 1 },
  MaxViewRadius: { min: 2, max: 32 },
  Version: { readOnly: true },
  "Defaults.GameMode": { options: [{ value: "Adventure", label: "Adventure" }, { value: "Creative", label: "Creative" }] },
};

const world: Record<string, Omit<FieldMeta, "label" | "hint">> = {
  UUID: { readOnly: true },
  "ClientEffects.SunIntensity": { min: 0, max: 1, step: 0.05 },
  "ClientEffects.BloomIntensity": { min: 0, max: 1, step: 0.05 },
  "ClientEffects.SunAngleDegrees": { step: 1 },
};

export function fieldMeta(kind: ConfigKind, path: string[]): FieldMeta {
  const table = kind === "server" ? server : kind === "world" ? world : {};
  const meta: FieldMeta = { ...(table[path.join(".")] ?? {}) };
  if (kind === "server" || kind === "world") {
    const base = `configFields.${kind}.${path.join("_")}`;
    const label = translate(`${base}.label` as MessageKey);
    if (label !== `${base}.label`) {
      meta.label = label;
      meta.hint = translate(`${base}.hint` as MessageKey) || undefined;
    }
  }
  if (meta.options) {
    meta.options = meta.options.map((o) => ({ ...o, label: translate(`configFields.gameModes.${o.value}` as MessageKey) }));
  }
  return meta;
}

/** Turns PascalCase / camelCase keys into readable labels. */
export function humanize(key: string): string {
  const spaced = key
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

export interface ConfigChange {
  path: string[];
  value: JsonValue;
}

export function isObject(v: JsonValue | undefined): v is JsonObject {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Lists the leaf values of next that differ from prev. */
export function diffConfig(prev: JsonObject, next: JsonObject, path: string[] = []): ConfigChange[] {
  const changes: ConfigChange[] = [];
  for (const key of Object.keys(next)) {
    const a = prev[key];
    const b = next[key];
    if (b === undefined) continue;
    if (isObject(a) && isObject(b)) changes.push(...diffConfig(a, b, [...path, key]));
    else if (JSON.stringify(a) !== JSON.stringify(b)) changes.push({ path: [...path, key], value: b });
  }
  return changes;
}

/** Returns a copy of root with value set at path. */
export function setAtPath(root: JsonObject, path: string[], value: JsonValue): JsonObject {
  const [head, ...rest] = path;
  if (head === undefined) return root;
  if (rest.length === 0) return { ...root, [head]: value };
  const child = root[head];
  return { ...root, [head]: setAtPath(isObject(child) ? child : {}, rest, value) };
}
