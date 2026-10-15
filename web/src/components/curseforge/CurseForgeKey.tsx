import { KeyRound, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import type { CurseForgeStatus } from "../../lib/types";
import { Alert } from "../ui/Alert";
import { Badge } from "../ui/Badge";
import { Button } from "../ui/Button";
import { Card, CardBody } from "../ui/Card";
import { Field, Input } from "../ui/Field";
import { useFeedback } from "../ui/feedback";
import { translate as t, translateServer } from "../../lib/i18n";

/** Settings form for the CurseForge API key. */
export function CurseForgeKey() {
  const { run, confirm } = useFeedback();
  const status = usePolling(() => api.get<CurseForgeStatus>("/api/curseforge"), 0);
  const [key, setKey] = useState("");
  const st = status.data;

  const save = async (event: FormEvent) => {
    event.preventDefault();
    const ok = await run(() => api.put("/api/curseforge/key", { apiKey: key }).then(() => true), t("hytale.curseforgeKey.saved"));
    if (ok) setKey("");
    await status.refresh();
  };

  const remove = async () => {
    const ok = await confirm({
      title: t("hytale.curseforgeKey.deleteTitle"),
      message: t("hytale.curseforgeKey.deleteMessage"),
      confirmLabel: t("hytale.curseforgeKey.delete"),
      danger: true,
    });
    if (!ok) return;
    await run(() => api.del("/api/curseforge/key"), t("hytale.curseforgeKey.deleted"));
    await status.refresh();
  };

  return (
    <Card>
      <form onSubmit={save}>
        <CardBody className="flex flex-col gap-4">
          {st && (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              {!st.configured && <Badge>{t("hytale.curseforgeKey.notConfigured")}</Badge>}
              {st.configured && st.ready && <Badge tone="success">{t("hytale.curseforgeKey.connected", { hint: st.keyHint ?? "" })}</Badge>}
              {st.configured && !st.ready && <Badge tone="danger">{t("hytale.curseforgeKey.unusable", { hint: st.keyHint ?? "" })}</Badge>}
            </div>
          )}
          {st?.error && <Alert tone="danger">{translateServer(st.error)}</Alert>}
          <Field
            label={st?.configured ? t("hytale.curseforgeKey.replace") : t("hytale.curseforgeKey.label")}
            hint={t("hytale.curseforgeKey.hint")}
          >
            {(id) => <Input id={id} type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} />}
          </Field>
        </CardBody>
        <div className="flex justify-end gap-2 border-t border-white/[0.06] px-5 py-4">
          {st?.configured && (
            <Button variant="danger" icon={Trash2} onClick={remove}>
              {t("hytale.curseforgeKey.delete")}
            </Button>
          )}
          <Button type="submit" variant="primary" icon={KeyRound} disabled={key.trim() === ""}>
            {t("hytale.curseforgeKey.save")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
