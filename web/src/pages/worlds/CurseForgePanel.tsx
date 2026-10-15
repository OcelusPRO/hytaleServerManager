import { ChevronDown, Download, ExternalLink, Package, Search } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { Alert } from "../../components/ui/Alert";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { EmptyState } from "../../components/ui/EmptyState";
import { Input, Select } from "../../components/ui/Field";
import { useFeedback } from "../../components/ui/feedback";
import { usePolling } from "../../hooks/usePolling";
import { api, errorMessage, seg } from "../../lib/api";
import { cn } from "../../lib/cn";
import { formatBytes, formatDate } from "../../lib/format";
import type {
  CurseForgeFile,
  CurseForgeInstallResult,
  CurseForgeJob,
  CurseForgeProject,
  CurseForgeSearch,
  CurseForgeStatus,
} from "../../lib/types";
import { paths } from "../../routes";
import { getLang, translate as t, translateServer } from "../../lib/i18n";

type Kind = "mods" | "modpacks";
type Sort = "popularity" | "updated" | "downloads" | "name";

interface CurseForgePanelProps {
  saveId: string;
  /** CurseForge project ids already installed in the world. */
  installed: Set<number>;
  onJob: (jobId: string) => void;
  onChange: () => Promise<void>;
}



export function CurseForgePanel({ saveId, installed, onJob, onChange }: CurseForgePanelProps) {
  const status = usePolling(() => api.get<CurseForgeStatus>("/api/curseforge"), 0);
  const st = status.data;

  if (!st) return null;
  if (!st.configured || !st.ready) {
    return (
      <EmptyState
        icon={Package}
        title={st.configured ? t("worlds.curseforge.unreachable") : t("worlds.curseforge.notConfigured")}
        description={st.error ? translateServer(st.error) : t("worlds.curseforge.notConfiguredHint")}
        action={
          <Link to={paths.settings}>
            <Button variant="primary">{t("worlds.curseforge.openSettings")}</Button>
          </Link>
        }
      />
    );
  }
  return <Browser saveId={saveId} modpacks={st.modpacks} installed={installed} onJob={onJob} onChange={onChange} />;
}

interface BrowserProps extends CurseForgePanelProps {
  modpacks: boolean;
}

function Browser({ saveId, modpacks, installed, onJob, onChange }: BrowserProps) {
  const { run, confirm, notify } = useFeedback();
  const [kind, setKind] = useState<Kind>("mods");
  const [sort, setSort] = useState<Sort>("popularity");
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const [result, setResult] = useState<CurseForgeSearch>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const params = new URLSearchParams({ kind, sort, q: query, page: String(page) });
    api
      .get<CurseForgeSearch>(`/api/curseforge/search?${params}`)
      .then((r) => {
        if (cancelled) return;
        setResult(r);
        setError(undefined);
      })
      .catch((err) => !cancelled && setError(errorMessage(err)))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [kind, sort, query, page]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    setPage(0);
    setQuery(text.trim());
  };

  const installMod = async (project: CurseForgeProject, fileId: number) => {
    const res = await run(() =>
      api.post<CurseForgeInstallResult>(`/api/saves/${seg(saveId)}/curseforge/mods`, { modId: project.id, fileId, dependencies: true }),
    );
    if (!res) return;
    notify("success", res.installed.length > 1 ? t("worlds.curseforge.installedWithDeps", { name: project.name, count: res.installed.length - 1 }) : t("worlds.curseforge.installed", { name: project.name }));
    if (res.warnings?.length) notify("error", res.warnings.map((w) => translateServer(w)).join(" · "));
    await onChange();
  };

  const installPack = async (project: CurseForgeProject, fileId: number) => {
    const ok = await confirm({
      title: t("worlds.curseforge.installPackTitle", { name: project.name }),
      message: t("worlds.curseforge.installPackMessage"),
      confirmLabel: t("worlds.curseforge.install"),
    });
    if (!ok) return;
    const job = await run(() => api.post<CurseForgeJob>(`/api/saves/${seg(saveId)}/curseforge/modpacks`, { modpackId: project.id, fileId }));
    if (job) onJob(job.id);
  };

  const install = kind === "mods" ? installMod : installPack;

  return (
    <div className="flex flex-col gap-4 p-5">
      <form onSubmit={submit} className="flex flex-wrap gap-2">
        {modpacks && (
          <Select
            aria-label={t("worlds.curseforge.contentType")}
            className="w-auto"
            value={kind}
            onChange={(e) => {
              setKind(e.target.value as Kind);
              setPage(0);
            }}
          >
            <option value="mods">{t("worlds.curseforge.mods")}</option>
            <option value="modpacks">{t("worlds.curseforge.modpacks")}</option>
          </Select>
        )}
        <Input className="min-w-48 flex-1" placeholder={t("worlds.curseforge.search")} value={text} onChange={(e) => setText(e.target.value)} />
        <Select
          aria-label={t("worlds.curseforge.sort")}
          className="w-auto"
          value={sort}
          onChange={(e) => {
            setSort(e.target.value as Sort);
            setPage(0);
          }}
        >
          <option value="popularity">{t("worlds.curseforge.popularity")}</option>
          <option value="downloads">{t("worlds.curseforge.downloads")}</option>
          <option value="updated">{t("worlds.curseforge.updated")}</option>
          <option value="name">{t("worlds.curseforge.name")}</option>
        </Select>
        <Button type="submit" variant="primary" icon={Search}>
          {t("worlds.curseforge.searchButton")}
        </Button>
      </form>

      {error && <Alert tone="danger">{error}</Alert>}
      {result && result.items.length === 0 && !error && <EmptyState icon={Search} title={t("worlds.curseforge.noResult")} description={t("worlds.curseforge.noResultHint")} />}

      <div className={cn("flex flex-col gap-3 transition-opacity", loading && "opacity-60")}>
        {result?.items.map((project) => (
          <ProjectRow key={project.id} project={project} kind={kind} installed={installed.has(project.id)} onInstall={install} />
        ))}
      </div>

      {result && result.totalCount > result.pageSize && (
        <div className="flex items-center justify-between text-sm text-zinc-400">
          <Button size="sm" disabled={page === 0 || loading} onClick={() => setPage(page - 1)}>
            {t("worlds.curseforge.previous")}
          </Button>
          <span>
            {t("worlds.curseforge.page", { page: page + 1, pages: Math.ceil(result.totalCount / result.pageSize) })}
          </span>
          <Button size="sm" disabled={(page + 1) * result.pageSize >= result.totalCount || loading} onClick={() => setPage(page + 1)}>
            {t("worlds.curseforge.next")}
          </Button>
        </div>
      )}
    </div>
  );
}

interface ProjectRowProps {
  project: CurseForgeProject;
  kind: Kind;
  installed: boolean;
  onInstall: (project: CurseForgeProject, fileId: number) => Promise<void>;
}

function ProjectRow({ project, kind, installed, onInstall }: ProjectRowProps) {
  const [open, setOpen] = useState(false);
  const latest = project.latest;
  const blocked = latest && !latest.downloadable;

  return (
    <div className="rounded-xl bg-zinc-950/40 ring-1 ring-white/[0.06]">
      <div className="flex items-start gap-4 p-4">
        {project.logoUrl ? (
          <img src={project.logoUrl} alt="" className="size-14 shrink-0 rounded-lg bg-zinc-800 object-cover" loading="lazy" referrerPolicy="no-referrer" />
        ) : (
          <div className="flex size-14 shrink-0 items-center justify-center rounded-lg bg-zinc-800">
            <Package className="size-6 text-zinc-500" />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate text-sm font-semibold text-zinc-100">{project.name}</h3>
            {project.author && <span className="text-xs text-zinc-500">{t("worlds.curseforge.by", { author: project.author })}</span>}
            {installed && <Badge tone="success">{t("worlds.curseforge.installedBadge")}</Badge>}
          </div>
          <p className="mt-1 line-clamp-2 text-sm text-zinc-400">{project.summary}</p>
          <p className="mt-1.5 text-xs text-zinc-500">
            {t("worlds.curseforge.downloadsCount", { count: new Intl.NumberFormat(getLang(), { notation: "compact" }).format(project.downloads) })}
            {latest && ` · ${latest.displayName} · ${formatDate(latest.date)} · ${formatBytes(latest.sizeBytes)}`}
          </p>
          {blocked && (
            <p className="mt-1 text-xs text-amber-400">
              {t("worlds.curseforge.blocked")}
            </p>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-2 sm:flex-row">
          {project.url && (
            <a href={project.url} target="_blank" rel="noreferrer noopener" aria-label={t("worlds.curseforge.open", { name: project.name })}>
              <Button size="sm" variant="ghost" icon={ExternalLink} tabIndex={-1} />
            </a>
          )}
          <Button size="sm" variant="ghost" icon={ChevronDown} onClick={() => setOpen(!open)} aria-expanded={open}>
            {t("worlds.curseforge.versions")}
          </Button>
          {blocked ? (
            <ManualDownload project={project} fileId={latest.id} primary />
          ) : (
            <Button size="sm" variant="primary" icon={Download} disabled={!latest} onClick={() => onInstall(project, 0)}>
              {installed ? t("worlds.curseforge.reinstall") : t("worlds.curseforge.install")}
            </Button>
          )}
        </div>
      </div>
      {open && <FileList project={project} kind={kind} onInstall={onInstall} />}
    </div>
  );
}

/** Link to the CurseForge page of a file whose author forbids third-party downloads. */
function ManualDownload({ project, fileId, primary }: { project: CurseForgeProject; fileId: number; primary?: boolean }) {
  if (!project.url) return null;
  return (
    <a href={`${project.url}/files/${fileId}`} target="_blank" rel="noreferrer noopener">
      <Button size="sm" variant={primary ? "primary" : "secondary"} icon={ExternalLink} tabIndex={-1}>
        {t("worlds.curseforge.downloadOn")}
      </Button>
    </a>
  );
}

const releaseTone = { release: "success", beta: "warning", alpha: "danger" } as const;

function FileList({ project, kind, onInstall }: { project: CurseForgeProject; kind: Kind; onInstall: ProjectRowProps["onInstall"] }) {
  const files = usePolling(() => api.get<CurseForgeFile[]>(`/api/curseforge/mods/${project.id}/files`), 0);

  return (
    <div className="border-t border-white/[0.06] px-4 py-2">
      {files.error && <p className="py-2 text-sm text-red-400">{files.error}</p>}
      {!files.data && !files.error && <p className="py-2 text-sm text-zinc-500">{t("worlds.curseforge.loading")}</p>}
      {files.data?.map((f) => (
        <div key={f.id} className="flex items-center gap-3 py-1.5 text-sm">
          <Badge tone={releaseTone[f.releaseType]}>{f.releaseType}</Badge>
          <span className="min-w-0 flex-1 truncate text-zinc-200">{f.displayName}</span>
          <span className="text-xs text-zinc-500 max-md:hidden">{formatDate(f.date)}</span>
          <span className="w-20 text-right text-xs text-zinc-500 tabular-nums">{formatBytes(f.sizeBytes)}</span>
          {f.downloadable ? (
            <Button size="sm" onClick={() => onInstall(project, f.id)} aria-label={t("worlds.curseforge.installFile", { name: f.displayName, kind })}>
              {t("worlds.curseforge.install")}
            </Button>
          ) : (
            <ManualDownload project={project} fileId={f.id} />
          )}
        </div>
      ))}
    </div>
  );
}
