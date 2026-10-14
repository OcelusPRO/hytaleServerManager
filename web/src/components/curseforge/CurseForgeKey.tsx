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

/** Settings form for the CurseForge API key. */
export function CurseForgeKey() {
  const { run, confirm } = useFeedback();
  const status = usePolling(() => api.get<CurseForgeStatus>("/api/curseforge"), 0);
  const [key, setKey] = useState("");
  const st = status.data;

  const save = async (event: FormEvent) => {
    event.preventDefault();
    const ok = await run(() => api.put("/api/curseforge/key", { apiKey: key }).then(() => true), "Clé CurseForge enregistrée");
    if (ok) setKey("");
    await status.refresh();
  };

  const remove = async () => {
    const ok = await confirm({
      title: "Supprimer la clé CurseForge ?",
      message: "Les mods déjà installés restent en place, mais la recherche et les mises à jour ne seront plus disponibles.",
      confirmLabel: "Supprimer",
      danger: true,
    });
    if (!ok) return;
    await run(() => api.del("/api/curseforge/key"), "Clé supprimée");
    await status.refresh();
  };

  return (
    <Card>
      <form onSubmit={save}>
        <CardBody className="flex flex-col gap-4">
          {st && (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              {!st.configured && <Badge>Non configuré</Badge>}
              {st.configured && st.ready && <Badge tone="success">Connecté {st.keyHint}</Badge>}
              {st.configured && !st.ready && <Badge tone="danger">Clé inutilisable {st.keyHint}</Badge>}
            </div>
          )}
          {st?.error && <Alert tone="danger">{st.error}</Alert>}
          <Field
            label={st?.configured ? "Remplacer la clé API" : "Clé API CurseForge"}
            hint="Générez-la sur console.curseforge.com. Elle est stockée chiffrée et n'est jamais renvoyée à l'interface."
          >
            {(id) => <Input id={id} type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} />}
          </Field>
        </CardBody>
        <div className="flex justify-end gap-2 border-t border-white/[0.06] px-5 py-4">
          {st?.configured && (
            <Button variant="danger" icon={Trash2} onClick={remove}>
              Supprimer
            </Button>
          )}
          <Button type="submit" variant="primary" icon={KeyRound} disabled={key.trim() === ""}>
            Vérifier et enregistrer
          </Button>
        </div>
      </form>
    </Card>
  );
}
