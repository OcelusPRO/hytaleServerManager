import { ExternalLink, LoaderCircle, LogIn } from "lucide-react";
import type { MouseEvent } from "react";
import { useStatus } from "../../hooks/useStatus";
import { api } from "../../lib/api";
import { cn } from "../../lib/cn";
import { closeLoginPopup, navigateLoginPopup, openLoginPopup, trustedHytaleUrl } from "../../lib/loginPopup";
import type { LoginStatus } from "../../lib/types";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { useFeedback } from "../ui/feedback";
import { translate as t, translateServer } from "../../lib/i18n";

const loginUrlOf = (login?: LoginStatus) => trustedHytaleUrl(login?.verificationUriComplete || login?.verificationUri);

interface HytaleConnectProps {
  label?: string;
  /** Horizontal alignment of the content within its container. */
  align?: "center" | "start";
}

/**
 * Connects the server's Hytale account: a popup opens directly on the
 * Hytale authorization page (code pre-filled). The application closes it
 * automatically once the login completes.
 */
export function HytaleConnect({ label = t("hytale.connect.button"), align = "center" }: HytaleConnectProps) {
  const { status, refresh } = useStatus();
  const { run } = useFeedback();
  const login = status?.hytale.login;
  const loginUrl = loginUrlOf(login);
  const alignment = align === "center" ? "items-center text-center" : "items-start text-left";

  const connect = async () => {
    // Open synchronously within the click so that popup blockers allow it.
    openLoginPopup();
    const url = loginUrlOf(await run(() => api.post<LoginStatus>("/api/hytale/auth/login")));
    if (url) navigateLoginPopup(url);
    else closeLoginPopup();
    await refresh();
  };

  // A link rather than a button: if the browser blocks the popup, the
  // default navigation still opens the Hytale page in a new tab.
  const reopen = (event: MouseEvent<HTMLAnchorElement>) => {
    if (openLoginPopup(event.currentTarget.href)) event.preventDefault();
  };

  const cancel = async () => {
    await run(() => api.post("/api/hytale/auth/cancel"));
    closeLoginPopup();
    await refresh();
  };

  if (login?.state === "pending") {
    return (
      <div className={cn("flex w-full flex-col gap-5", alignment)}>
        <div className="flex items-center gap-2.5 text-sm text-zinc-300">
          <LoaderCircle className="size-4 animate-spin text-brand-400" aria-hidden />
          {t("hytale.connect.pending")}
        </div>
        {login.userCode && (
          <div className="rounded-xl bg-zinc-950/60 px-5 py-3 ring-1 ring-white/10">
            <p className="text-xs text-zinc-500">{t("hytale.connect.code")}</p>
            <p className="mt-1 font-mono text-xl font-semibold tracking-[0.2em] text-brand-300">{login.userCode}</p>
          </div>
        )}
        <div className="flex items-center gap-2">
          {loginUrl && (
            <a
              href={loginUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={reopen}
              className="inline-flex h-9 items-center gap-2 rounded-lg bg-zinc-800 px-3.5 text-sm font-medium text-zinc-100 ring-1 ring-white/5 ring-inset transition-colors hover:bg-zinc-700"
            >
              <ExternalLink className="size-4" aria-hidden />
              {t("hytale.connect.open")}
            </a>
          )}
          <Button variant="ghost" onClick={cancel}>
            {t("hytale.connect.cancel")}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className={cn("flex w-full flex-col gap-4", alignment)}>
      {login?.state === "failed" && (
        <Alert tone="danger" title={t("hytale.connect.failed")} className="w-full text-left">
          {translateServer(login.error ?? "")}
        </Alert>
      )}
      <Button variant="primary" size={align === "center" ? "lg" : "md"} icon={LogIn} onClick={connect}>
        {label}
      </Button>
    </div>
  );
}
