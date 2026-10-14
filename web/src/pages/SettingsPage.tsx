import { KeyRound, LogOut, RefreshCw, Save as SaveIcon } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { CurseForgeKey } from "../components/curseforge/CurseForgeKey";
import { HytaleConnect } from "../components/hytale/HytaleConnect";
import { ProfilePicker } from "../components/hytale/ProfilePicker";
import { PageHeader } from "../components/layout/PageHeader";
import { Badge } from "../components/ui/Badge";
import { Button } from "../components/ui/Button";
import { Card, CardBody } from "../components/ui/Card";
import { Field, Input, Select, Toggle } from "../components/ui/Field";
import { useFeedback } from "../components/ui/feedback";
import { usePolling } from "../hooks/usePolling";
import { useStatus } from "../hooks/useStatus";
import { api } from "../lib/api";
import { formatDate } from "../lib/format";
import type { AuthMode, Patchline, ServerSettings } from "../lib/types";

type NumberKey = "minMemoryMb" | "maxMemoryMb" | "port" | "stopTimeoutSeconds" | "nativeBackupMinutes";
type BoolKey = "autoStart" | "autoRestart" | "useAotCache" | "nativeBackups" | "backupBeforeRestore" | "disableSentry" | "acceptEarlyPlugins" | "autosaveEnabled" | "autoUpdate" | "sleepEnabled";

const NUMBERS: { key: NumberKey; label: string; hint?: string; min: number; max: number; step?: number }[] = [
  { key: "minMemoryMb", label: "Mémoire minimale", hint: "En Mo (-Xms)", min: 256, max: 262144, step: 256 },
  { key: "maxMemoryMb", label: "Mémoire maximale", hint: "En Mo (-Xmx)", min: 1024, max: 262144, step: 256 },
  { key: "port", label: "Port du jeu (UDP)", hint: "Adaptez aussi la publication Docker", min: 1, max: 65535 },
  { key: "stopTimeoutSeconds", label: "Délai d'arrêt", hint: "Secondes avant arrêt forcé", min: 10, max: 600 },
];

const BOOLEANS: { key: BoolKey; label: string; description: string }[] = [
  { key: "autoStart", label: "Démarrage automatique", description: "Lance le serveur au démarrage du conteneur." },
  { key: "autoRestart", label: "Redémarrage après plantage", description: "Jusqu'à 3 tentatives en 10 minutes." },
  { key: "useAotCache", label: "Cache AOT", description: "Démarrage plus rapide grâce au cache fourni avec le serveur." },
  { key: "backupBeforeRestore", label: "Sauvegarde avant restauration", description: "Crée une sauvegarde de sécurité avant chaque restauration." },
  { key: "autosaveEnabled", label: "Autosave", description: "Sauvegarde périodique du monde actif pendant que le serveur tourne, en rotation." },
  { key: "autoUpdate", label: "Mises à jour automatiques", description: "Arrête le serveur, sauvegarde le monde puis installe la mise à jour quand il est vide, ou après un compte à rebours si des joueurs restent connectés." },
  {
    key: "sleepEnabled",
    label: "Mise en veille",
    description:
      "Arrête le serveur (monde sauvegardé) quand il est vide, puis le relance dès qu'un joueur tente de se connecter. Le premier essai de connexion peut échouer si le démarrage est long : il suffit de réessayer.",
  },
  { key: "nativeBackups", label: "Sauvegardes natives Hytale", description: "Sauvegardes périodiques effectuées par le serveur lui-même." },
  { key: "disableSentry", label: "Désactiver Sentry", description: "N'envoie pas les rapports de plantage à Hypixel." },
  { key: "acceptEarlyPlugins", label: "Early plugins", description: "Non supporté officiellement, peut causer des instabilités." },
];

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-x-10 gap-y-4 border-t border-white/[0.06] py-8 first:border-t-0 first:pt-0 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <div>
        <h2 className="text-sm font-semibold text-zinc-100">{title}</h2>
        <p className="mt-1 text-sm text-zinc-500">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function ServerSettingsForm({ initial, onSaved }: { initial: ServerSettings; onSaved: () => Promise<void> }) {
  const { run } = useFeedback();
  const [form, setForm] = useState(initial);
  const dirty = JSON.stringify(form) !== JSON.stringify(initial);
  const set = <K extends keyof ServerSettings>(key: K, value: ServerSettings[K]) => setForm((f) => ({ ...f, [key]: value }));

  useEffect(() => setForm(initial), [initial]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    await run(() => api.put("/api/settings", form), "Paramètres enregistrés — appliqués au prochain démarrage");
    await onSaved();
  };

  // Repeated under each section so that saving never needs scrolling.
  const actions = (
    <div className="mt-4 flex justify-end gap-2">
      <Button variant="ghost" disabled={!dirty} onClick={() => setForm(initial)}>
        Annuler
      </Button>
      <Button type="submit" variant="primary" icon={SaveIcon} disabled={!dirty}>
        Enregistrer
      </Button>
    </div>
  );

  return (
    <form onSubmit={submit}>
      <Section title="Serveur" description="Ressources et réseau. Appliqué au prochain démarrage.">
        <Card>
          <CardBody className="grid grid-cols-1 gap-5 sm:grid-cols-2">
            {NUMBERS.map((n) => (
              <Field key={n.key} label={n.label} hint={n.hint}>
                {(id) => (
                  <Input id={id} type="number" min={n.min} max={n.max} step={n.step} required value={form[n.key]} onChange={(e) => set(n.key, Number(e.target.value))} />
                )}
              </Field>
            ))}
            <Field label="Authentification des joueurs" hint="Offline : aucune vérification des comptes">
              {(id) => (
                <Select id={id} value={form.authMode} onChange={(e) => set("authMode", e.target.value as AuthMode)}>
                  <option value="authenticated">Comptes Hytale (recommandé)</option>
                  <option value="offline">Offline</option>
                </Select>
              )}
            </Field>
            <Field label="Canal de mise à jour">
              {(id) => (
                <Select id={id} value={form.patchline} onChange={(e) => set("patchline", e.target.value as Patchline)}>
                  <option value="release">Release</option>
                  <option value="pre-release">Pre-release</option>
                </Select>
              )}
            </Field>
          </CardBody>
        </Card>
        {actions}
      </Section>

      <Section title="Comportement" description="Automatisations et options avancées.">
        <Card>
          <CardBody className="divide-y divide-white/[0.05] py-2">
            {BOOLEANS.map((b) => (
              <Toggle key={b.key} label={b.label} description={b.description} checked={form[b.key]} onChange={(v) => set(b.key, v)} />
            ))}
            {form.autosaveEnabled && (
              <div className="grid grid-cols-1 gap-5 py-3 sm:grid-cols-2">
                <Field label="Fréquence de l'autosave" hint="En minutes (1-1440)">
                  {(id) => (
                    <Input id={id} type="number" min={1} max={1440} required value={form.autosaveMinutes} onChange={(e) => set("autosaveMinutes", Number(e.target.value))} />
                  )}
                </Field>
                <Field label="Slots d'autosave" hint="Les plus anciens sont écrasés (1-100)">
                  {(id) => (
                    <Input id={id} type="number" min={1} max={100} required value={form.autosaveSlots} onChange={(e) => set("autosaveSlots", Number(e.target.value))} />
                  )}
                </Field>
              </div>
            )}
            {form.autoUpdate && (
              <div className="grid grid-cols-1 gap-5 py-3 sm:grid-cols-2">
                <Field label="Mise à jour si serveur vide" hint="Minutes sans joueur (1-1440)">
                  {(id) => (
                    <Input id={id} type="number" min={1} max={1440} required value={form.autoUpdateIdleMinutes} onChange={(e) => set("autoUpdateIdleMinutes", Number(e.target.value))} />
                  )}
                </Field>
                <Field label="Mise à jour forcée" hint="Heures d'attente avant un compte à rebours de 30 min (1-720)">
                  {(id) => (
                    <Input id={id} type="number" min={1} max={720} required value={form.autoUpdateForceHours} onChange={(e) => set("autoUpdateForceHours", Number(e.target.value))} />
                  )}
                </Field>
              </div>
            )}
            {form.sleepEnabled && (
              <div className="py-3">
                <Field label="Mise en veille si serveur vide" hint="Minutes sans joueur (1-1440)">
                  {(id) => (
                    <Input id={id} type="number" min={1} max={1440} className="max-w-40" required value={form.sleepIdleMinutes} onChange={(e) => set("sleepIdleMinutes", Number(e.target.value))} />
                  )}
                </Field>
              </div>
            )}
            {form.nativeBackups && (
              <div className="py-3">
                <Field label="Fréquence des sauvegardes natives" hint="En minutes">
                  {(id) => (
                    <Input id={id} type="number" min={5} max={1440} className="max-w-40" value={form.nativeBackupMinutes} onChange={(e) => set("nativeBackupMinutes", Number(e.target.value))} />
                  )}
                </Field>
              </div>
            )}
          </CardBody>
        </Card>
        {actions}
      </Section>
    </form>
  );
}

function HytaleSection() {
  const { status, refresh } = useStatus();
  const { run, confirm } = useFeedback();
  // "" follows the latest build; any other value freezes the game on it.
  const [wanted, setWanted] = useState<string | null>(null);
  if (!status) return null;
  const { hytale, setup, server } = status;

  const logout = async () => {
    const ok = await confirm({
      title: "Déconnecter le compte Hytale ?",
      message: "Le serveur ne pourra plus démarrer en mode authentifié ni se mettre à jour.",
      confirmLabel: "Déconnecter",
      danger: true,
    });
    if (!ok) return;
    await run(() => api.post("/api/hytale/auth/logout"), "Compte déconnecté");
    await refresh();
  };

  const update = async () => {
    const running = server.state === "running";
    const ok =
      !running ||
      (await confirm({ title: "Mettre à jour maintenant ?", message: "Le serveur sera arrêté puis redémarré automatiquement.", confirmLabel: "Mettre à jour" }));
    if (ok) await run(() => api.post("/api/setup/update"), "Mise à jour lancée");
    await refresh();
  };

  const choice = wanted ?? setup.pinnedVersion;
  const versions = [...new Set([setup.gameVersion, ...setup.storedVersions, setup.latestVersion].filter((v): v is string => !!v))];
  const changeVersion = async () => {
    const rollback = choice !== "" && choice !== setup.gameVersion;
    const ok =
      server.state !== "running" ||
      (await confirm({
        title: choice === "" ? "Suivre la dernière version ?" : `Passer à la version ${choice} ?`,
        message: `Le serveur sera arrêté puis redémarré automatiquement.${rollback ? " Attention : un monde sauvegardé avec une version plus récente peut ne pas s'ouvrir avec une version plus ancienne." : ""}`,
        confirmLabel: "Appliquer",
        danger: rollback,
      }));
    if (!ok) return;
    await run(() => api.post("/api/setup/version", { version: choice }), "Version appliquée");
    setWanted(null);
    await refresh();
  };

  return (
    <>
      <Section title="Compte Hytale" description="Utilisé pour authentifier le serveur et télécharger ses mises à jour.">
        <Card>
          <CardBody className="flex flex-col gap-5">
            {hytale.authenticated ? (
              <>
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div className="flex items-center gap-3">
                    <div className="flex size-10 items-center justify-center rounded-full bg-brand-500/10 text-sm font-semibold text-brand-300 ring-1 ring-brand-500/25">
                      {(hytale.profileName ?? "?").slice(0, 1).toUpperCase()}
                    </div>
                    <div>
                      <p className="text-sm font-medium text-zinc-100">{hytale.profileName ?? "Profil non sélectionné"}</p>
                      <p className="text-xs text-zinc-500">Jetons renouvelés le {formatDate(hytale.refreshedAt)}</p>
                    </div>
                  </div>
                  <Button variant="danger" icon={LogOut} onClick={logout}>
                    Déconnecter
                  </Button>
                </div>
                <div className="border-t border-white/[0.06] pt-5">
                  <ProfilePicker />
                </div>
              </>
            ) : (
              <div className="flex flex-col items-start gap-3">
                <p className="text-sm text-zinc-400">Aucun compte connecté.</p>
                <HytaleConnect align="start" />
              </div>
            )}
          </CardBody>
        </Card>
      </Section>

      <Section title="Mises à jour" description="Vérifiées toutes les heures et installées automatiquement quand le serveur est arrêté.">
        <Card>
          <CardBody className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-zinc-400">Version installée</span>
                <span className="font-medium text-zinc-100">{setup.gameVersion ?? "—"}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <span className="text-zinc-400">Dernière version</span>
                <span className="font-medium text-zinc-100">{setup.latestVersion ?? "—"}</span>
                {setup.updateAvailable ? (
                  <Badge tone="info">Disponible</Badge>
                ) : (
                  setup.latestVersion && <Badge tone="success">À jour</Badge>
                )}
              </div>
            </div>
            <Button icon={RefreshCw} disabled={!setup.updateAvailable || setup.activity !== "idle"} onClick={update}>
              Mettre à jour maintenant
            </Button>
          </CardBody>
          <CardBody className="flex flex-wrap items-end justify-between gap-4 border-t border-white/[0.06]">
            <Field
              label="Version du jeu"
              hint={
                setup.pinnedVersion
                  ? `Figée sur ${setup.pinnedVersion} : aucune mise à jour automatique.`
                  : "Les versions remplacées sont conservées pour un retour en arrière."
              }
            >
              {(id) => (
                <Select id={id} value={choice} onChange={(e) => setWanted(e.target.value)}>
                  <option value="">Toujours la dernière (automatique)</option>
                  {versions.map((v) => (
                    <option key={v} value={v}>
                      {v}
                      {v === setup.gameVersion ? " (installée)" : setup.storedVersions.includes(v) ? " (stockée)" : " (dernière)"}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
            <Button
              disabled={choice === setup.pinnedVersion || setup.activity !== "idle"}
              onClick={changeVersion}
            >
              Appliquer
            </Button>
          </CardBody>
        </Card>
      </Section>
    </>
  );
}

function PasswordSection() {
  const { run, notify } = useFeedback();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirmation, setConfirmation] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (next !== confirmation) {
      notify("error", "Les deux nouveaux mots de passe diffèrent.");
      return;
    }
    const ok = await run(() => api.post("/api/account/password", { current, next }).then(() => true), "Mot de passe changé, les autres sessions ont été déconnectées");
    if (ok) {
      setCurrent("");
      setNext("");
      setConfirmation("");
    }
  };

  return (
    <Section title="Sécurité" description="Mot de passe de ce panneau (12 caractères minimum).">
      <Card>
        <form onSubmit={submit}>
          <CardBody className="grid grid-cols-1 gap-5 sm:grid-cols-3">
            <Field label="Mot de passe actuel">
              {(id) => <Input id={id} type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />}
            </Field>
            <Field label="Nouveau mot de passe">
              {(id) => <Input id={id} type="password" autoComplete="new-password" minLength={12} required value={next} onChange={(e) => setNext(e.target.value)} />}
            </Field>
            <Field label="Confirmation">
              {(id) => <Input id={id} type="password" autoComplete="new-password" minLength={12} required value={confirmation} onChange={(e) => setConfirmation(e.target.value)} />}
            </Field>
          </CardBody>
          <div className="flex justify-end border-t border-white/[0.06] px-5 py-4">
            <Button type="submit" variant="primary" icon={KeyRound}>
              Changer le mot de passe
            </Button>
          </div>
        </form>
      </Card>
    </Section>
  );
}

export function SettingsPage() {
  const { data, refresh } = usePolling(() => api.get<ServerSettings>("/api/settings"), 0);
  return (
    <div className="flex flex-col gap-8">
      <PageHeader title="Paramètres" description="Configuration du serveur, du compte Hytale et du panneau." />
      <div>
        {data && <ServerSettingsForm initial={data} onSaved={refresh} />}
        <HytaleSection />
        <Section title="CurseForge" description="Recherche, installation et mise à jour des mods et modpacks depuis CurseForge.">
          <CurseForgeKey />
        </Section>
        <PasswordSection />
      </div>
    </div>
  );
}
