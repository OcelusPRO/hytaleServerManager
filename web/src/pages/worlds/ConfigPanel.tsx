import { RotateCcw, Save, Settings2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ConfigForm } from "../../components/config/ConfigForm";
import { Alert } from "../../components/ui/Alert";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { Select } from "../../components/ui/Field";
import { useFeedback } from "../../components/ui/feedback";
import { usePolling } from "../../hooks/usePolling";
import { api, errorMessage, seg } from "../../lib/api";
import { diffConfig, setAtPath } from "../../lib/configSchema";
import type { ConfigFile, ConfigKind, JsonObject, JsonValue } from "../../lib/types";
import { translate as t, translatePlural as tp } from "../../lib/i18n";

const KIND_LABEL = { server: "worlds.config.kindServer", world: "worlds.config.kindWorld", mod: "worlds.config.kindMod" } as const satisfies Record<ConfigKind, string>;

interface ConfigPanelProps {
  saveId: string;
  inUse: boolean;
}

export function ConfigPanel({ saveId, inUse }: ConfigPanelProps) {
  const files = usePolling(() => api.get<ConfigFile[]>(`/api/saves/${seg(saveId)}/configs`), 0);
  const [fileId, setFileId] = useState<string>();

  const list = files.data;
  if (files.error) {
    return (
      <div className="p-5">
        <Alert tone="danger">{files.error}</Alert>
      </div>
    );
  }
  if (!list) return null;
  if (list.length === 0) {
    return (
      <div className="p-5">
        <EmptyState
          icon={Settings2}
          title={t("worlds.config.empty")}
          description={t("worlds.config.emptyHint")}
        />
      </div>
    );
  }

  const selected = list.find((f) => f.id === fileId) ?? list[0];
  if (!selected) return null;
  const groups = (Object.keys(KIND_LABEL) as ConfigKind[])
    .map((kind) => [kind, list.filter((f) => f.kind === kind)] as const)
    .filter(([, items]) => items.length > 0);

  return (
    <div className="flex flex-col gap-4 p-5">
      {inUse && (
        <Alert tone="warning" title={t("worlds.config.runningTitle")}>
          {t("worlds.config.runningHint")}
        </Alert>
      )}
      <Select value={selected.id} onChange={(e) => setFileId(e.target.value)} aria-label={t("worlds.config.file")} className="sm:max-w-sm">
        {groups.map(([kind, items]) => (
          <optgroup key={kind} label={t(KIND_LABEL[kind])}>
            {items.map((f) => (
              <option key={f.id} value={f.id}>
                {f.group ? `${f.group} / ${f.label}` : f.label}
              </option>
            ))}
          </optgroup>
        ))}
      </Select>
      <ConfigEditor key={selected.id} saveId={saveId} file={selected} readOnly={inUse} />
    </div>
  );
}

function ConfigEditor({ saveId, file, readOnly }: { saveId: string; file: ConfigFile; readOnly: boolean }) {
  const { run } = useFeedback();
  const url = `/api/saves/${seg(saveId)}/configs/file?file=${seg(file.id)}`;
  const [original, setOriginal] = useState<JsonObject>();
  const [draft, setDraft] = useState<JsonObject>();
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      const doc = await api.get<JsonObject>(url);
      setOriginal(doc);
      setDraft(doc);
      setError(undefined);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, [url]);

  useEffect(() => {
    void load();
  }, [load]);

  const changes = useMemo(() => (original && draft ? diffConfig(original, draft) : []), [original, draft]);
  const update = useCallback((path: string[], value: JsonValue) => setDraft((d) => d && setAtPath(d, path, value)), []);

  const save = async () => {
    setSaving(true);
    let saved = false;
    try {
      await run(async () => {
        await api.patch(url, { changes });
        saved = true;
      }, t("worlds.config.saved"));
      // On failure the draft is kept so nothing typed is lost.
      if (saved) await load();
    } finally {
      setSaving(false);
    }
  };

  if (error) return <Alert tone="danger">{error}</Alert>;
  if (!draft) return null;

  const count = changes.length;
  return (
    <div className="flex flex-col gap-3">
      <p className="font-mono text-xs text-zinc-500">{file.id}</p>
      <ConfigForm kind={file.kind} value={draft} disabled={readOnly || saving} onChange={update} />
      <div className="sticky bottom-0 -mx-5 flex items-center justify-end gap-2 border-t border-white/[0.06] bg-zinc-900/95 px-5 py-3 backdrop-blur">
        <span className="mr-auto text-xs text-zinc-500">
          {count === 0 ? t("worlds.config.noChange") : tp("worlds.config.changes", count)}
        </span>
        <Button icon={RotateCcw} disabled={count === 0 || saving} onClick={() => setDraft(original)}>
          {t("worlds.config.cancel")}
        </Button>
        <Button variant="primary" icon={Save} disabled={count === 0 || readOnly || saving} onClick={save}>
          {t("worlds.config.save")}
        </Button>
      </div>
    </div>
  );
}
