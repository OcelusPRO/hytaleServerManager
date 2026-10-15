import { KeyRound, LogOut, RefreshCw, Save as SaveIcon } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { PasskeysCard } from "../components/account/PasskeysCard";
import { LanguagePicker } from "../components/LanguagePicker";
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
import { translate as t, useI18n, type Lang, type MessageKey } from "../lib/i18n";
import type { AuthMode, Patchline, ServerSettings } from "../lib/types";

type NumberKey = "minMemoryMb" | "maxMemoryMb" | "port" | "stopTimeoutSeconds" | "nativeBackupMinutes";
type BoolKey = "autoStart" | "autoRestart" | "useAotCache" | "nativeBackups" | "backupBeforeRestore" | "disableSentry" | "acceptEarlyPlugins" | "autosaveEnabled" | "autoUpdate" | "sleepEnabled" | "metricsStream";

const NUMBERS: { key: NumberKey; label: MessageKey; hint?: MessageKey; min: number; max: number; step?: number }[] = [
  { key: "minMemoryMb", label: "settings.server.minMemory", hint: "settings.server.minMemoryHint", min: 256, max: 262144, step: 256 },
  { key: "maxMemoryMb", label: "settings.server.maxMemory", hint: "settings.server.maxMemoryHint", min: 1024, max: 262144, step: 256 },
  { key: "port", label: "settings.server.port", hint: "settings.server.portHint", min: 1, max: 65535 },
  { key: "stopTimeoutSeconds", label: "settings.server.stopTimeout", hint: "settings.server.stopTimeoutHint", min: 10, max: 600 },
];

const BOOLEANS: { key: BoolKey; label: MessageKey; description: MessageKey }[] = [
  { key: "autoStart", label: "settings.behaviour.autoStart", description: "settings.behaviour.autoStartHint" },
  { key: "autoRestart", label: "settings.behaviour.autoRestart", description: "settings.behaviour.autoRestartHint" },
  { key: "useAotCache", label: "settings.behaviour.aot", description: "settings.behaviour.aotHint" },
  { key: "backupBeforeRestore", label: "settings.behaviour.backupBeforeRestore", description: "settings.behaviour.backupBeforeRestoreHint" },
  { key: "autosaveEnabled", label: "settings.behaviour.autosave", description: "settings.behaviour.autosaveHint" },
  { key: "autoUpdate", label: "settings.behaviour.autoUpdate", description: "settings.behaviour.autoUpdateHint" },
  { key: "sleepEnabled", label: "settings.behaviour.sleep", description: "settings.behaviour.sleepHint" },
  { key: "metricsStream", label: "settings.behaviour.metricsStream", description: "settings.behaviour.metricsStreamHint" },
  { key: "nativeBackups", label: "settings.behaviour.nativeBackups", description: "settings.behaviour.nativeBackupsHint" },
  { key: "disableSentry", label: "settings.behaviour.disableSentry", description: "settings.behaviour.disableSentryHint" },
  { key: "acceptEarlyPlugins", label: "settings.behaviour.earlyPlugins", description: "settings.behaviour.earlyPluginsHint" },
];

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-x-10 gap-y-4 border-t border-white/[0.06] py-8 first:border-t-0 first:pt-0 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <div>
        <h2 className="text-sm font-semibold text-zinc-100">{title}</h2>
        <p className="mt-1 text-sm text-zinc-400">{description}</p>
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function NumberField({ label, hint, value, min, max, onChange, className }: { label: string; hint: string; value: number; min: number; max: number; onChange: (v: number) => void; className?: string }) {
  return (
    <Field label={label} hint={hint}>
      {(id) => <Input id={id} type="number" min={min} max={max} required className={className} value={value} onChange={(e) => onChange(Number(e.target.value))} />}
    </Field>
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
    await run(() => api.put("/api/settings", form), t("settings.saved"));
    await onSaved();
  };

  // Repeated under each section so that saving never needs scrolling.
  const actions = (
    <div className="mt-4 flex justify-end gap-2">
      <Button variant="ghost" disabled={!dirty} onClick={() => setForm(initial)}>
        {t("settings.cancel")}
      </Button>
      <Button type="submit" variant="primary" icon={SaveIcon} disabled={!dirty}>
        {t("settings.save")}
      </Button>
    </div>
  );

  return (
    <form onSubmit={submit}>
      <Section title={t("settings.server.title")} description={t("settings.server.description")}>
        <Card>
          <CardBody className="grid grid-cols-1 gap-5 sm:grid-cols-2">
            {NUMBERS.map((n) => (
              <Field key={n.key} label={t(n.label)} hint={n.hint && t(n.hint)}>
                {(id) => (
                  <Input id={id} type="number" min={n.min} max={n.max} step={n.step} required value={form[n.key]} onChange={(e) => set(n.key, Number(e.target.value))} />
                )}
              </Field>
            ))}
            <Field label={t("settings.server.authMode")} hint={t("settings.server.authModeHint")}>
              {(id) => (
                <Select id={id} value={form.authMode} onChange={(e) => set("authMode", e.target.value as AuthMode)}>
                  <option value="authenticated">{t("settings.server.authenticated")}</option>
                  <option value="offline">{t("settings.server.offline")}</option>
                </Select>
              )}
            </Field>
            <Field label={t("settings.server.patchline")}>
              {(id) => (
                <Select id={id} value={form.patchline} onChange={(e) => set("patchline", e.target.value as Patchline)}>
                  <option value="release">{t("settings.server.release")}</option>
                  <option value="pre-release">{t("settings.server.preRelease")}</option>
                </Select>
              )}
            </Field>
            <div className="flex flex-col gap-1.5 sm:col-span-2">
              <span className="text-sm font-medium text-zinc-200">{t("settings.server.gameLanguage")}</span>
              <LanguagePicker label={t("settings.server.gameLanguage")} value={form.gameLanguage} onChange={(v) => set("gameLanguage", v)} />
              <span className="text-xs text-zinc-500">{t("settings.server.gameLanguageHint")}</span>
            </div>
          </CardBody>
        </Card>
        {actions}
      </Section>

      <Section title={t("settings.behaviour.title")} description={t("settings.behaviour.description")}>
        <Card>
          <CardBody className="divide-y divide-white/[0.05] py-2">
            {BOOLEANS.map((b) => (
              <Toggle key={b.key} label={t(b.label)} description={t(b.description)} checked={form[b.key]} onChange={(v) => set(b.key, v)} />
            ))}
            {form.autosaveEnabled && (
              <div className="grid grid-cols-1 gap-5 py-3 sm:grid-cols-2">
                <NumberField label={t("settings.behaviour.autosaveFrequency")} hint={t("settings.behaviour.autosaveFrequencyHint")} min={1} max={1440} value={form.autosaveMinutes} onChange={(v) => set("autosaveMinutes", v)} />
                <NumberField label={t("settings.behaviour.autosaveSlots")} hint={t("settings.behaviour.autosaveSlotsHint")} min={1} max={100} value={form.autosaveSlots} onChange={(v) => set("autosaveSlots", v)} />
              </div>
            )}
            {form.autoUpdate && (
              <div className="grid grid-cols-1 gap-5 py-3 sm:grid-cols-2">
                <NumberField label={t("settings.behaviour.updateIdle")} hint={t("settings.behaviour.updateIdleHint")} min={1} max={1440} value={form.autoUpdateIdleMinutes} onChange={(v) => set("autoUpdateIdleMinutes", v)} />
                <NumberField label={t("settings.behaviour.updateForce")} hint={t("settings.behaviour.updateForceHint")} min={1} max={720} value={form.autoUpdateForceHours} onChange={(v) => set("autoUpdateForceHours", v)} />
              </div>
            )}
            {form.sleepEnabled && (
              <div className="py-3">
                <NumberField label={t("settings.behaviour.sleepIdle")} hint={t("settings.behaviour.sleepIdleHint")} min={1} max={1440} className="max-w-40" value={form.sleepIdleMinutes} onChange={(v) => set("sleepIdleMinutes", v)} />
              </div>
            )}
            {form.nativeBackups && (
              <div className="py-3">
                <NumberField label={t("settings.behaviour.nativeBackupFrequency")} hint={t("settings.behaviour.nativeBackupFrequencyHint")} min={5} max={1440} className="max-w-40" value={form.nativeBackupMinutes} onChange={(v) => set("nativeBackupMinutes", v)} />
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
      title: t("settings.account.logoutTitle"),
      message: t("settings.account.logoutMessage"),
      confirmLabel: t("settings.account.logout"),
      danger: true,
    });
    if (!ok) return;
    await run(() => api.post("/api/hytale/auth/logout"), t("settings.account.loggedOut"));
    await refresh();
  };

  const update = async () => {
    const running = server.state === "running";
    const ok =
      !running ||
      (await confirm({ title: t("settings.updates.nowTitle"), message: t("settings.updates.nowMessage"), confirmLabel: t("settings.updates.update") }));
    if (ok) await run(() => api.post("/api/setup/update"), t("settings.updates.started"));
    await refresh();
  };

  const choice = wanted ?? setup.pinnedVersion;
  const versions = [...new Set([setup.gameVersion, ...setup.storedVersions, setup.latestVersion].filter((v): v is string => !!v))];
  const changeVersion = async () => {
    const rollback = choice !== "" && choice !== setup.gameVersion;
    const ok =
      server.state !== "running" ||
      (await confirm({
        title: choice === "" ? t("settings.updates.followTitle") : t("settings.updates.switchTitle", { version: choice }),
        message: t("settings.updates.nowMessage") + (rollback ? t("settings.updates.rollbackWarning") : ""),
        confirmLabel: t("settings.updates.apply"),
        danger: rollback,
      }));
    if (!ok) return;
    await run(() => api.post("/api/setup/version", { version: choice }), t("settings.updates.applied"));
    setWanted(null);
    await refresh();
  };

  return (
    <>
      <Section title={t("settings.account.title")} description={t("settings.account.description")}>
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
                      <p className="text-sm font-medium text-zinc-100">{hytale.profileName ?? t("settings.account.noProfile")}</p>
                      <p className="text-xs text-zinc-500">{t("settings.account.tokensRefreshed", { date: formatDate(hytale.refreshedAt) })}</p>
                    </div>
                  </div>
                  <Button variant="danger" icon={LogOut} onClick={logout}>
                    {t("settings.account.logout")}
                  </Button>
                </div>
                <div className="border-t border-white/[0.06] pt-5">
                  <ProfilePicker />
                </div>
              </>
            ) : (
              <div className="flex flex-col items-start gap-3">
                <p className="text-sm text-zinc-400">{t("settings.account.none")}</p>
                <HytaleConnect align="start" />
              </div>
            )}
          </CardBody>
        </Card>
      </Section>

      <Section title={t("settings.updates.title")} description={t("settings.updates.description")}>
        <Card>
          <CardBody className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-zinc-400">{t("settings.updates.installed")}</span>
                <span className="font-medium text-zinc-100">{setup.gameVersion ?? "—"}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <span className="text-zinc-400">{t("settings.updates.latest")}</span>
                <span className="font-medium text-zinc-100">{setup.latestVersion ?? "—"}</span>
                {setup.updateAvailable ? (
                  <Badge tone="info">{t("settings.updates.available")}</Badge>
                ) : (
                  setup.latestVersion && <Badge tone="success">{t("settings.updates.upToDate")}</Badge>
                )}
              </div>
            </div>
            <Button icon={RefreshCw} disabled={!setup.updateAvailable || setup.activity !== "idle"} onClick={update}>
              {t("settings.updates.updateNow")}
            </Button>
          </CardBody>
          <CardBody className="flex flex-wrap items-end justify-between gap-4 border-t border-white/[0.06]">
            <Field
              label={t("settings.updates.version")}
              hint={setup.pinnedVersion ? t("settings.updates.pinned", { version: setup.pinnedVersion }) : t("settings.updates.keptVersions")}
            >
              {(id) => (
                <Select id={id} value={choice} onChange={(e) => setWanted(e.target.value)}>
                  <option value="">{t("settings.updates.followLatest")}</option>
                  {versions.map((v) => (
                    <option key={v} value={v}>
                      {v}
                      {v === setup.gameVersion
                        ? t("settings.updates.installedSuffix")
                        : setup.storedVersions.includes(v)
                          ? t("settings.updates.storedSuffix")
                          : t("settings.updates.latestSuffix")}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
            <Button disabled={choice === setup.pinnedVersion || setup.activity !== "idle"} onClick={changeVersion}>
              {t("settings.updates.apply")}
            </Button>
          </CardBody>
        </Card>
      </Section>
    </>
  );
}

function InterfaceSection() {
  const { lang, setLang } = useI18n();
  const { notify } = useFeedback();
  const [draft, setDraft] = useState<Lang>(lang);

  // The interface switches language when the choice is saved.
  const save = () => {
    setLang(draft);
    notify("success", t("settings.interface.saved"));
  };

  return (
    <Section title={t("settings.interface.title")} description={t("settings.interface.description")}>
      <Card>
        <CardBody className="flex flex-wrap items-center justify-between gap-4">
          <LanguagePicker label={t("common.language")} value={draft} onChange={setDraft} />
          <div className="flex gap-2">
            <Button variant="ghost" disabled={draft === lang} onClick={() => setDraft(lang)}>
              {t("settings.cancel")}
            </Button>
            <Button variant="primary" icon={SaveIcon} disabled={draft === lang} onClick={save}>
              {t("settings.save")}
            </Button>
          </div>
        </CardBody>
      </Card>
    </Section>
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
      notify("error", t("settings.password.mismatch"));
      return;
    }
    const ok = await run(() => api.post("/api/account/password", { current, next }).then(() => true), t("settings.password.changed"));
    if (ok) {
      setCurrent("");
      setNext("");
      setConfirmation("");
    }
  };

  return (
    <Section title={t("settings.password.title")} description={t("settings.password.description")}>
      <Card>
        <form onSubmit={submit}>
          <CardBody className="grid grid-cols-1 gap-5 sm:grid-cols-3">
            <Field label={t("settings.password.current")}>
              {(id) => <Input id={id} type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />}
            </Field>
            <Field label={t("settings.password.next")}>
              {(id) => <Input id={id} type="password" autoComplete="new-password" minLength={12} required value={next} onChange={(e) => setNext(e.target.value)} />}
            </Field>
            <Field label={t("settings.password.confirmation")}>
              {(id) => <Input id={id} type="password" autoComplete="new-password" minLength={12} required value={confirmation} onChange={(e) => setConfirmation(e.target.value)} />}
            </Field>
          </CardBody>
          <div className="flex justify-end border-t border-white/[0.06] px-5 py-4">
            <Button type="submit" variant="primary" icon={KeyRound}>
              {t("settings.password.change")}
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
      <PageHeader title={t("settings.title")} description={t("settings.description")} />
      <div>
        {data && <ServerSettingsForm initial={data} onSaved={refresh} />}
        <HytaleSection />
        <InterfaceSection />
        <Section title={t("settings.curseforge.title")} description={t("settings.curseforge.description")}>
          <CurseForgeKey />
        </Section>
        <Section title={t("settings.passkeys.title")} description={t("settings.passkeys.description")}>
          <PasskeysCard />
        </Section>
        <PasswordSection />
      </div>
    </div>
  );
}
