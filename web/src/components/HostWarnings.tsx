import { translate as t } from "../lib/i18n";
import type { HostWarning } from "../lib/types";
import { Alert } from "./ui/Alert";

function Command({ children }: { children: string }) {
  return (
    <pre className="mt-1.5 break-all whitespace-pre-wrap rounded-md bg-zinc-950/70 px-3 py-2 font-mono text-xs text-zinc-200 ring-1 ring-white/[0.06]">
      {children}
    </pre>
  );
}

function UDPBufferWarning({ values }: { values?: Record<string, number> }) {
  const wanted = values?.wanted ?? 25165824;
  return (
    <Alert tone="warning" title={t("features.udpBuffer.title")}>
      <p>{t("features.udpBuffer.explanation", { effective: Math.round((values?.effective ?? 0) / 1024), wanted: Math.round(wanted / (1 << 20)) })}</p>
      <p className="mt-2 text-xs text-zinc-400">{t("features.udpBuffer.linux")}</p>
      <Command>{`sudo sysctl -w net.core.rmem_max=${wanted}\necho "net.core.rmem_max=${wanted}" | sudo tee /etc/sysctl.d/99-hytale.conf`}</Command>
      <p className="mt-2 text-xs text-zinc-400">{t("features.udpBuffer.dockerDesktop")}</p>
      <Command>{`docker run --rm --privileged --pid=host alpine nsenter -t 1 -m -u -n -i sysctl -w net.core.rmem_max=${wanted}`}</Command>
      <p className="mt-2 text-xs text-zinc-400">{t("features.udpBuffer.restart")}</p>
    </Alert>
  );
}

/** Problems reported by the game server that only the host's administrator can fix. */
export function HostWarnings({ warnings }: { warnings: HostWarning[] }) {
  return <>{warnings.map((w) => (w.kind === "udpReceiveBuffer" ? <UDPBufferWarning key={w.kind} values={w.values} /> : null))}</>;
}
