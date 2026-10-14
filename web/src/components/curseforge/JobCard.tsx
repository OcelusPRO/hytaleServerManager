import { useEffect, useRef, useState } from "react";
import { usePolling } from "../../hooks/usePolling";
import { api, seg } from "../../lib/api";
import type { CurseForgeJob } from "../../lib/types";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Progress } from "../ui/Progress";

interface JobCardProps {
  jobId: string;
  /** Called once when the job has finished (successfully or not). */
  onFinished: () => void;
  onDismiss: () => void;
}

/** Follows a background CurseForge operation until it ends. */
export function JobCard({ jobId, onFinished, onDismiss }: JobCardProps) {
  const [ended, setEnded] = useState(false);
  const job = usePolling(() => api.get<CurseForgeJob>(`/api/curseforge/jobs/${seg(jobId)}`), ended ? 0 : 1000);
  const j = job.data;
  const finished = useRef(false);

  useEffect(() => {
    if (j && j.state !== "running" && !finished.current) {
      finished.current = true;
      setEnded(true);
      onFinished();
    }
  }, [j, onFinished]);

  if (!j) return null;
  const warnings = j.warnings ?? [];

  if (j.state === "running") {
    return (
      <div className="mx-5 mb-4 flex flex-col gap-2 rounded-xl bg-zinc-950/50 p-4 ring-1 ring-white/[0.06]">
        <div className="flex items-center justify-between gap-3 text-sm">
          <span className="font-medium text-zinc-100">{j.title}</span>
          <span className="truncate text-zinc-400">{j.message}</span>
        </div>
        <Progress value={j.progress} />
      </div>
    );
  }
  return (
    <div className="mx-5 mb-4">
      <Alert
        tone={j.state === "failed" ? "danger" : warnings.length > 0 ? "warning" : "success"}
        title={j.state === "failed" ? `${j.title} : échec` : `${j.title} : terminé`}
        action={
          <Button size="sm" variant="ghost" onClick={onDismiss}>
            Fermer
          </Button>
        }
      >
        {j.error && <p>{j.error}</p>}
        {warnings.length > 0 && (
          <ul className="mt-1 list-disc space-y-0.5 pl-4 text-xs text-zinc-400">
            {warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        )}
      </Alert>
    </div>
  );
}
