import { HytaleConnect } from "../components/hytale/HytaleConnect";
import { ProfilePicker } from "../components/hytale/ProfilePicker";
import { SetupSteps } from "../components/hytale/SetupSteps";
import { Logo } from "../components/layout/Logo";
import { Card } from "../components/ui/Card";
import type { HytaleAuthStatus, SetupStatus } from "../lib/types";

interface OnboardingPageProps {
  setup: SetupStatus;
  hytale: HytaleAuthStatus;
}

/**
 * First-run screen: the only thing asked to the operator is to connect a
 * Hytale account. Everything else is downloaded automatically.
 */
export function OnboardingPage({ setup, hytale }: OnboardingPageProps) {
  const needsProfile = hytale.authenticated && !hytale.profileUuid;

  return (
    <main className="flex min-h-dvh items-center justify-center bg-[radial-gradient(ellipse_at_top,var(--color-zinc-900),var(--color-zinc-950)_60%)] p-4">
      <div className="w-full max-w-md">
        <Logo size="lg" className="mb-8 justify-center" />
        <Card>
          <div className="px-6 pt-8 pb-6 text-center">
            <h1 className="text-xl font-semibold tracking-tight text-zinc-50">
              {needsProfile ? "Choisissez un profil" : "Connectez votre compte Hytale"}
            </h1>
            <p className="mx-auto mt-2 max-w-xs text-sm text-zinc-400">
              {needsProfile
                ? "Votre compte possède plusieurs profils : sélectionnez celui qui hébergera le serveur."
                : "C'est la seule étape : le serveur se télécharge et se configure ensuite automatiquement."}
            </p>
            <div className="mt-6 flex justify-center">{needsProfile ? <ProfilePicker /> : <HytaleConnect />}</div>
          </div>
          <div className="border-t border-white/[0.06] px-6 py-5">
            <SetupSteps setup={setup} hytale={hytale} />
          </div>
        </Card>
        <p className="mt-6 text-center text-xs text-zinc-500">
          La connexion se fait sur le site officiel de Hytale, aucun mot de passe ne transite par ce panneau.
        </p>
      </div>
    </main>
  );
}
