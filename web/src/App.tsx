import { useCallback, useEffect, useState } from "react";
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router";
import { Sidebar } from "./components/layout/Sidebar";
import { SetupBanner } from "./components/SetupBanner";
import { useFeedback } from "./components/ui/feedback";
import { StatusProvider, useStatus } from "./hooks/useStatus";
import { api, onUnauthorized } from "./lib/api";
import { closeLoginPopup } from "./lib/loginPopup";
import { LoginPage } from "./pages/LoginPage";
import { OnboardingPage } from "./pages/OnboardingPage";
import { paths, ROUTES } from "./routes";

type Session = "loading" | "anonymous" | "authenticated";

export function App() {
  const [session, setSession] = useState<Session>("loading");

  useEffect(() => {
    api
      .get<{ authenticated: boolean }>("/api/session")
      .then((s) => setSession(s.authenticated ? "authenticated" : "anonymous"))
      .catch(() => setSession("anonymous"));
    return onUnauthorized(() => setSession("anonymous"));
  }, []);

  const logout = useCallback(async () => {
    await api.post("/api/logout").catch(() => undefined);
    setSession("anonymous");
  }, []);

  if (session === "loading") return null;
  if (session === "anonymous") return <LoginPage onSuccess={() => setSession("authenticated")} />;
  return (
    <StatusProvider>
      <Workspace onLogout={logout} />
    </StatusProvider>
  );
}

/** Shows onboarding until the server can be installed, then the shell. */
function Workspace({ onLogout }: { onLogout: () => void }) {
  const { status } = useStatus();
  const loginState = status?.hytale.login.state;

  // Whatever screen is displayed, close the Hytale popup once the login
  // flow is over (approved, failed or cancelled).
  useEffect(() => {
    if (loginState && loginState !== "pending") closeLoginPopup();
  }, [loginState]);

  if (!status) return null;

  const { setup, hytale } = status;
  const accountReady = hytale.authenticated && Boolean(hytale.profileUuid);
  if (!setup.gameReady && !accountReady) return <OnboardingPage setup={setup} hytale={hytale} />;
  return (
    <Routes>
      <Route element={<Shell onLogout={onLogout} />}>
        {ROUTES.map(({ path, page: Page }) => (
          <Route key={path} path={path} element={<Page />} />
        ))}
        <Route path="*" element={<Navigate to={paths.dashboard} replace />} />
      </Route>
    </Routes>
  );
}

/** Application layout: sidebar, background setup progress and the page. */
function Shell({ onLogout }: { onLogout: () => void }) {
  const { run } = useFeedback();
  const { pathname } = useLocation();
  const current = ROUTES.find((r) => r.path === pathname);

  useEffect(() => {
    document.title = current ? `${current.label} · Hytale Server Manager` : "Hytale Server Manager";
  }, [current]);

  return (
    <div className="min-h-dvh">
      <Sidebar items={ROUTES} onLogout={() => void run(async () => onLogout())} />
      <main className="lg:pl-64">
        <div className="mx-auto flex max-w-6xl flex-col gap-6 px-4 py-8 sm:px-6 lg:px-8">
          <SetupBanner />
          <Outlet />
        </div>
      </main>
    </div>
  );
}
