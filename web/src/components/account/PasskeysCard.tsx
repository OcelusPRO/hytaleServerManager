import { Fingerprint, Pencil, Plus, Trash2 } from "lucide-react";
import { usePolling } from "../../hooks/usePolling";
import { api } from "../../lib/api";
import { formatDate } from "../../lib/format";
import { createPasskey, isCancellation, passkeysSupported, type CeremonyOptions } from "../../lib/webauthn";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Card, CardBody } from "../ui/Card";
import { Toggle } from "../ui/Field";
import { useFeedback } from "../ui/feedback";
import { translate as t } from "../../lib/i18n";

interface Passkey {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

interface PasskeysResponse {
  passkeys: Passkey[];
  passwordDisabled: boolean;
}

/** Default label for a new passkey, from the browser's platform. */
function defaultName(): string {
  const ua = navigator.userAgent;
  if (/iPhone|iPad/.test(ua)) return "iPhone / iPad";
  if (/Android/.test(ua)) return "Android";
  if (/Mac OS X/.test(ua)) return "Mac";
  if (/Windows/.test(ua)) return "Windows";
  return t("features.passkeys.defaultName");
}

/** Passkeys of the panel, and the switch turning the password off. */
export function PasskeysCard() {
  const { run, confirm, prompt, notify } = useFeedback();
  const { data, refresh } = usePolling(() => api.get<PasskeysResponse>("/api/account/passkeys"), 0);
  const supported = passkeysSupported();
  const passkeys = data?.passkeys ?? [];

  const add = async () => {
    const name = await prompt({ title: t("features.passkeys.newTitle"), label: t("features.passkeys.nameLabel"), defaultValue: defaultName(), maxLength: 64, confirmLabel: t("features.passkeys.continue") });
    if (!name) return;
    try {
      const options = await api.post<CeremonyOptions>("/api/account/passkeys/begin");
      await api.post("/api/account/passkeys", await createPasskey(options, name));
      notify("success", t("features.passkeys.registered"));
    } catch (err) {
      if (!isCancellation(err)) await run(() => Promise.reject(err));
    }
    await refresh();
  };

  const rename = async (p: Passkey) => {
    const name = await prompt({ title: t("features.passkeys.renameTitle"), label: t("features.passkeys.name"), defaultValue: p.name, maxLength: 64, confirmLabel: t("features.passkeys.rename") });
    if (!name || name === p.name) return;
    await run(() => api.patch(`/api/account/passkeys/${encodeURIComponent(p.id)}`, { name }), t("features.passkeys.renamed"));
    await refresh();
  };

  const remove = async (p: Passkey) => {
    const ok = await confirm({ title: t("features.passkeys.deleteTitle", { name: p.name }), message: t("features.passkeys.deleteMessage"), confirmLabel: t("features.passkeys.delete"), danger: true });
    if (!ok) return;
    await run(() => api.del(`/api/account/passkeys/${encodeURIComponent(p.id)}`), t("features.passkeys.deleted"));
    await refresh();
  };

  const setPasswordLogin = async (enabled: boolean) => {
    if (!enabled) {
      const ok = await confirm({
        title: t("features.passkeys.disableTitle"),
        message: t("features.passkeys.disableMessage"),
        confirmLabel: t("features.passkeys.disable"),
      });
      if (!ok) return;
    }
    await run(() => api.put("/api/account/password-login", { enabled }), enabled ? t("features.passkeys.passwordEnabled") : t("features.passkeys.passwordDisabled"));
    await refresh();
  };

  return (
    <Card>
      <CardBody className="flex flex-col gap-4">
        {!supported && <Alert tone="warning">{t("features.passkeys.secureContext")}</Alert>}
        {passkeys.length === 0 ? (
          <p className="text-sm text-zinc-400">{t("features.passkeys.empty")}</p>
        ) : (
          <ul className="divide-y divide-white/[0.05]">
            {passkeys.map((p) => (
              <li key={p.id} className="flex items-center gap-3 py-2.5">
                <Fingerprint className="size-4 shrink-0 text-zinc-500" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-zinc-200">{p.name}</p>
                  <p className="truncate text-xs text-zinc-500">
                    {t("features.passkeys.addedOn", { date: formatDate(p.createdAt) })} · {p.lastUsedAt ? t("features.passkeys.usedOn", { date: formatDate(p.lastUsedAt) }) : t("features.passkeys.neverUsed")}
                  </p>
                </div>
                <Button size="sm" variant="ghost" icon={Pencil} aria-label={t("features.passkeys.renameLabel", { name: p.name })} title={t("features.passkeys.rename")} onClick={() => rename(p)} />
                <Button size="sm" variant="ghost" icon={Trash2} aria-label={t("features.passkeys.deleteLabel", { name: p.name })} title={t("features.passkeys.delete")} onClick={() => remove(p)} />
              </li>
            ))}
          </ul>
        )}
        <div>
          <Button icon={Plus} disabled={!supported} onClick={add}>
            {t("features.passkeys.add")}
          </Button>
        </div>
        {data && (
          <div className="border-t border-white/[0.06] pt-2">
            <Toggle
              label={t("features.passkeys.passwordLogin")}
              description={
                passkeys.length === 0
                  ? t("features.passkeys.passwordLoginNoPasskey")
                  : t("features.passkeys.passwordLoginHint")
              }
              checked={!data.passwordDisabled}
              disabled={passkeys.length === 0 && !data.passwordDisabled}
              onChange={(v) => void setPasswordLogin(v)}
            />
          </div>
        )}
      </CardBody>
    </Card>
  );
}
