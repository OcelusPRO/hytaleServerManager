import { Plus, X } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { fieldMeta, humanize, isObject, type FieldMeta } from "../../lib/configSchema";
import type { ConfigKind, JsonObject, JsonValue } from "../../lib/types";
import { Button } from "../ui/Button";
import { Input, Select, Switch } from "../ui/Field";
import { translate as t } from "../../lib/i18n";

interface FormProps {
  kind: ConfigKind;
  value: JsonObject;
  disabled: boolean;
  onChange: (path: string[], value: JsonValue) => void;
}

/** Form generated from a JSON object: one control per value, by type. */
export function ConfigForm(props: FormProps) {
  return <Fields {...props} path={[]} />;
}

function Fields({ kind, value, path, disabled, onChange }: FormProps & { path: string[] }) {
  const entries = Object.entries(value);
  // Plain values first, nested sections after, to keep the form scannable.
  const leaves = entries.filter(([, v]) => !isObject(v) || Object.keys(v).length === 0);
  const sections = entries.filter(([, v]) => isObject(v) && Object.keys(v).length > 0);
  return (
    <div className="flex flex-col">
      {leaves.map(([key, v]) => (
        <Row key={key} kind={kind} path={[...path, key]} name={key} value={v} disabled={disabled} onChange={onChange} />
      ))}
      {sections.map(([key, v]) => {
        const childPath = [...path, key];
        const meta = fieldMeta(kind, childPath);
        return (
          <section key={key} className="mt-4 rounded-lg bg-zinc-950/40 p-4 ring-1 ring-inset ring-white/[0.06]">
            <h3 className="text-sm font-semibold text-zinc-100">{meta.label ?? humanize(key)}</h3>
            {meta.hint && <p className="mt-0.5 text-xs text-zinc-500">{meta.hint}</p>}
            <Fields kind={kind} value={v as JsonObject} path={childPath} disabled={disabled} onChange={onChange} />
          </section>
        );
      })}
    </div>
  );
}

interface RowProps {
  kind: ConfigKind;
  path: string[];
  name: string;
  value: JsonValue;
  disabled: boolean;
  onChange: (path: string[], value: JsonValue) => void;
}

function Row({ kind, path, name, value, disabled, onChange }: RowProps) {
  const id = useId();
  const meta = fieldMeta(kind, path);
  const locked = disabled || meta.readOnly === true;
  const inline = typeof value === "boolean";
  return (
    <div className="flex flex-col gap-1.5 border-b border-white/[0.04] py-3 last:border-b-0 sm:flex-row sm:items-start sm:justify-between sm:gap-6">
      <label htmlFor={id} className="min-w-0 sm:w-1/2">
        <span className="block text-sm font-medium text-zinc-200">{meta.label ?? humanize(name)}</span>
        {meta.hint && <span className="mt-0.5 block text-xs text-zinc-500">{meta.hint}</span>}
      </label>
      <div className={inline ? "shrink-0" : "min-w-0 sm:w-1/2"}>
        <Control id={id} value={value} meta={meta} disabled={locked} onChange={(v) => onChange(path, v)} />
      </div>
    </div>
  );
}

interface ControlProps {
  id: string;
  value: JsonValue;
  meta: FieldMeta;
  disabled: boolean;
  onChange: (value: JsonValue) => void;
}

function Control({ id, value, meta, disabled, onChange }: ControlProps) {
  if (typeof value === "boolean") return <Switch id={id} checked={value} onChange={onChange} disabled={disabled} />;
  if (typeof value === "number") return <NumberControl id={id} value={value} meta={meta} disabled={disabled} onChange={onChange} />;
  if (typeof value === "string" || value === null) {
    if (meta.options) {
      const current = value ?? "";
      // Match case-insensitively: the game may store "ADVENTURE" or "Adventure".
      const match = meta.options.find((o) => o.value.toLowerCase() === current.toLowerCase());
      const options = match ? meta.options : [...meta.options, { value: current, label: current || "(vide)" }];
      return (
        <Select id={id} value={match?.value ?? current} disabled={disabled} onChange={(e) => onChange(keepCase(current, e.target.value))}>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </Select>
      );
    }
    return (
      <Input
        id={id}
        type={meta.password ? "password" : "text"}
        autoComplete="off"
        value={value ?? ""}
        placeholder={value === null ? t("shell.config.unset") : undefined}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }
  if (Array.isArray(value) && value.every((v) => typeof v === "string")) {
    return <StringList id={id} items={value as string[]} disabled={disabled} onChange={onChange} />;
  }
  return <RawJson id={id} value={value} disabled={disabled} onChange={onChange} />;
}

/** Keeps the upper-case style already used by the file. */
function keepCase(current: string, next: string): string {
  return current !== "" && current === current.toUpperCase() ? next.toUpperCase() : next;
}

function NumberControl({ id, value, meta, disabled, onChange }: ControlProps & { value: number }) {
  const [text, setText] = useState(String(value));
  // Follow external changes (reset) without fighting the user's typing.
  useEffect(() => {
    setText((t) => (Number(t) === value ? t : String(value)));
  }, [value]);
  const unsafe = Number.isInteger(value) && !Number.isSafeInteger(value);
  return (
    <Input
      id={id}
      type="number"
      inputMode={Number.isInteger(value) ? "numeric" : "decimal"}
      min={meta.min}
      max={meta.max}
      step={meta.step ?? (Number.isInteger(value) ? 1 : "any")}
      value={text}
      disabled={disabled || unsafe}
      title={unsafe ? t("shell.config.tooLarge") : undefined}
      onChange={(e) => {
        setText(e.target.value);
        const n = e.target.valueAsNumber;
        if (Number.isFinite(n)) onChange(n);
      }}
    />
  );
}

function StringList({ id, items, disabled, onChange }: { id: string; items: string[]; disabled: boolean; onChange: (v: string[]) => void }) {
  return (
    <div id={id} className="flex flex-col gap-2">
      {items.map((item, i) => (
        <div key={i} className="flex gap-2">
          <Input value={item} disabled={disabled} aria-label={t("shell.config.item", { n: i + 1 })} onChange={(e) => onChange(items.map((x, j) => (j === i ? e.target.value : x)))} />
          <Button variant="ghost" icon={X} disabled={disabled} aria-label={t("shell.config.removeItem", { n: i + 1 })} onClick={() => onChange(items.filter((_, j) => j !== i))} />
        </div>
      ))}
      <div>
        <Button size="sm" icon={Plus} disabled={disabled} onClick={() => onChange([...items, ""])}>
          {t("shell.config.add")}
        </Button>
      </div>
    </div>
  );
}

/** Fallback for values without a dedicated control (mixed lists, empty objects). */
function RawJson({ id, value, disabled, onChange }: { id: string; value: JsonValue; disabled: boolean; onChange: (v: JsonValue) => void }) {
  const [text, setText] = useState(() => JSON.stringify(value, null, 2));
  const [invalid, setInvalid] = useState(false);
  return (
    <div className="flex flex-col gap-1">
      <textarea
        id={id}
        rows={Math.min(10, Math.max(2, text.split("\n").length))}
        spellCheck={false}
        value={text}
        disabled={disabled}
        onChange={(e) => {
          setText(e.target.value);
          try {
            onChange(JSON.parse(e.target.value) as JsonValue);
            setInvalid(false);
          } catch {
            setInvalid(true);
          }
        }}
        className="w-full rounded-lg bg-zinc-950/60 px-3 py-2 font-mono text-xs text-zinc-100 ring-1 ring-inset ring-white/10 focus:outline-none focus:ring-2 focus:ring-brand-500/70 disabled:opacity-50"
      />
      {invalid && <p className="text-xs text-red-400">{t("shell.config.invalidJson")}</p>}
    </div>
  );
}
