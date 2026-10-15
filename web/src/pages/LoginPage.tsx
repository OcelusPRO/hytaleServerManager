import { Fingerprint, KeyRound } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Logo } from "../components/layout/Logo";
import { Alert } from "../components/ui/Alert";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Field";
import { api, errorMessage } from "../lib/api";
import { translate as t } from "../lib/i18n";
import { getPasskey, isCancellation, passkeysSupported, type CeremonyOptions } from "../lib/webauthn";

interface LoginMethods {
  password: boolean;
  passkey: boolean;
}

export function LoginPage({ onSuccess }: { onSuccess: () => void }) {
  const [methods, setMethods] = useState<LoginMethods>();
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .get<LoginMethods>("/api/login/methods")
      .then(setMethods)
      .catch(() => setMethods({ password: true, passkey: false }));
  }, []);

  const attempt = async (login: () => Promise<void>) => {
    setBusy(true);
    setError(undefined);
    try {
      await login();
      onSuccess();
    } catch (err) {
      if (!isCancellation(err)) setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void attempt(() => api.post("/api/login", { password }));
  };

  const withPasskey = () =>
    attempt(async () => {
      const options = await api.post<CeremonyOptions>("/api/login/passkey/begin");
      await api.post("/api/login/passkey/finish", await getPasskey(options));
    });

  if (!methods) return null;
  const passkeyAvailable = methods.passkey && passkeysSupported();

  return (
    <main className="flex min-h-dvh items-center justify-center bg-[radial-gradient(ellipse_at_top,var(--color-zinc-900),var(--color-zinc-950)_60%)] p-4">
      <div className="w-full max-w-sm">
        <Logo size="lg" className="mb-8 justify-center" />
        <Card className="p-6">
          <h1 className="text-lg font-semibold text-zinc-50">{t("pages.login.title")}</h1>
          {methods.password && <p className="mt-1 text-sm text-zinc-400">{t("pages.login.initialPassword")}</p>}
          <div className="mt-6 flex flex-col gap-4">
            {passkeyAvailable && (
              <Button variant="primary" icon={Fingerprint} disabled={busy} onClick={() => void withPasskey()} className="w-full">
                {t("pages.login.withPasskey")}
              </Button>
            )}
            {methods.passkey && !passkeysSupported() && <Alert tone="warning">{t("pages.login.secureContext")}</Alert>}
            {methods.password && passkeyAvailable && (
              <div className="flex items-center gap-3 text-xs text-zinc-500">
                <span className="h-px flex-1 bg-white/[0.06]" />
                {t("pages.login.or")}
                <span className="h-px flex-1 bg-white/[0.06]" />
              </div>
            )}
            {methods.password && (
              <form onSubmit={submit} className="flex flex-col gap-4">
                <Field label={t("pages.login.password")}>
                  {(id) => (
                    <Input
                      id={id}
                      type="password"
                      autoComplete="current-password"
                      autoFocus={!passkeyAvailable}
                      required
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                  )}
                </Field>
                <Button type="submit" variant={passkeyAvailable ? "secondary" : "primary"} icon={KeyRound} disabled={busy || !password} className="w-full">
                  {t("pages.login.submit")}
                </Button>
              </form>
            )}
            {!methods.password && <p className="text-xs text-zinc-500">{t("pages.login.passwordDisabled")}</p>}
            {error && <Alert tone="danger">{error}</Alert>}
          </div>
        </Card>
      </div>
    </main>
  );
}
