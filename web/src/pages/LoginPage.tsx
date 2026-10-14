import { KeyRound } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Logo } from "../components/layout/Logo";
import { Alert } from "../components/ui/Alert";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Field";
import { api, errorMessage } from "../lib/api";

export function LoginPage({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await api.post("/api/login", { password });
      onSuccess();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="flex min-h-dvh items-center justify-center bg-[radial-gradient(ellipse_at_top,var(--color-zinc-900),var(--color-zinc-950)_60%)] p-4">
      <div className="w-full max-w-sm">
        <Logo size="lg" className="mb-8 justify-center" />
        <Card className="p-6">
          <h1 className="text-lg font-semibold text-zinc-50">Connexion</h1>
          <p className="mt-1 text-sm text-zinc-400">
            Le mot de passe initial est affiché dans les logs du conteneur au premier lancement.
          </p>
          <form onSubmit={submit} className="mt-6 flex flex-col gap-4">
            <Field label="Mot de passe">
              {(id) => (
                <Input
                  id={id}
                  type="password"
                  autoComplete="current-password"
                  autoFocus
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              )}
            </Field>
            {error && <Alert tone="danger">{error}</Alert>}
            <Button type="submit" variant="primary" icon={KeyRound} disabled={busy || !password} className="w-full">
              Se connecter
            </Button>
          </form>
        </Card>
      </div>
    </main>
  );
}
