import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import { translate as t } from "../../lib/i18n";
import { Button } from "./Button";

/** Slice of a list for the current page; the page is clamped when the list shrinks. */
export function usePagination<T>(items: T[], pageSize: number, resetKey?: unknown) {
  const [requested, setPage] = useState(0);
  const pages = Math.max(1, Math.ceil(items.length / pageSize));
  const page = Math.min(requested, pages - 1);

  // A new search starts back at the first page.
  useEffect(() => setPage(0), [resetKey]);

  return {
    page,
    pages,
    setPage,
    items: items.slice(page * pageSize, (page + 1) * pageSize),
    from: items.length === 0 ? 0 : page * pageSize + 1,
    to: Math.min(items.length, (page + 1) * pageSize),
    total: items.length,
  };
}

interface PaginationProps {
  page: number;
  pages: number;
  from: number;
  to: number;
  total: number;
  onChange: (page: number) => void;
}

/** Previous/next controls with the range shown; renders nothing for a single page. */
export function Pagination({ page, pages, from, to, total, onChange }: PaginationProps) {
  if (pages <= 1) return null;
  return (
    <nav className="flex items-center justify-between gap-3 border-t border-white/[0.06] px-5 py-3" aria-label={t("shell.pagination.label")}>
      <p className="text-xs text-zinc-500 tabular-nums">{t("shell.pagination.range", { from, to, total })}</p>
      <div className="flex items-center gap-2">
        <span className="text-xs text-zinc-400 tabular-nums">{t("shell.pagination.page", { page: page + 1, pages })}</span>
        <Button size="sm" variant="ghost" icon={ChevronLeft} disabled={page === 0} onClick={() => onChange(page - 1)} aria-label={t("shell.pagination.previous")} title={t("shell.pagination.previous")} />
        <Button size="sm" variant="ghost" icon={ChevronRight} disabled={page >= pages - 1} onClick={() => onChange(page + 1)} aria-label={t("shell.pagination.next")} title={t("shell.pagination.next")} />
      </div>
    </nav>
  );
}
