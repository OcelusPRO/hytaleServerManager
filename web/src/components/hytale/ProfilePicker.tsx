import { useState } from "react";
import { usePolling } from "../../hooks/usePolling";
import { useStatus } from "../../hooks/useStatus";
import { api } from "../../lib/api";
import type { HytaleProfile } from "../../lib/types";
import { Button } from "../ui/Button";
import { Field, Select } from "../ui/Field";
import { useFeedback } from "../ui/feedback";

/** Lets the operator choose which game profile the server runs as. */
export function ProfilePicker() {
  const { status, refresh } = useStatus();
  const { run } = useFeedback();
  const { data: profiles } = usePolling(() => api.get<HytaleProfile[]>("/api/hytale/auth/profiles"), 0);
  const [selected, setSelected] = useState<string>();
  const value = selected ?? status?.hytale.profileUuid ?? profiles?.[0]?.uuid ?? "";

  const save = async () => {
    await run(() => api.post("/api/hytale/auth/profile", { uuid: value }), "Profil sélectionné");
    await refresh();
  };

  return (
    <div className="flex flex-wrap items-end gap-3">
      <Field label="Profil utilisé par le serveur" className="min-w-64 flex-1">
        {(id) => (
          <Select id={id} value={value} onChange={(e) => setSelected(e.target.value)} disabled={!profiles}>
            {profiles?.map((p) => (
              <option key={p.uuid} value={p.uuid}>
                {p.username}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Button variant="primary" onClick={save} disabled={!value || value === status?.hytale.profileUuid}>
        Utiliser ce profil
      </Button>
    </div>
  );
}
