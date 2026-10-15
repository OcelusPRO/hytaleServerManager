import { Plus, type LucideIcon } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Field";
import { translate as t } from "../../lib/i18n";

interface AccessListCardProps<T> {
  title: string;
  description: string;
  icon: LucideIcon;
  items: T[];
  itemKey: (item: T) => string;
  renderItem: (item: T) => ReactNode;
  emptyText: string;
  /** Element shown at the right of the title (e.g. a switch). */
  headerAction?: ReactNode;
  addLabel: string;
  /** Adds a player by name; additions need the server to be running. */
  onAdd: (name: string) => Promise<boolean>;
  canAdd: boolean;
}

/** A titled list of players with an inline "add by name" form. */
export function AccessListCard<T>({
  title,
  description,
  icon: Icon,
  items,
  itemKey,
  renderItem,
  emptyText,
  headerAction,
  addLabel,
  onAdd,
  canAdd,
}: AccessListCardProps<T>) {
  const [name, setName] = useState("");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (await onAdd(name.trim())) setName("");
  };

  return (
    <Card className="flex flex-col">
      <div className="flex items-start justify-between gap-3 px-5 pt-5">
        <div className="flex min-w-0 items-start gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-zinc-800 ring-1 ring-white/5">
            <Icon className="size-4 text-zinc-400" aria-hidden />
          </span>
          <div className="min-w-0">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              {title}
              <span className="rounded-full bg-zinc-800 px-1.5 py-0.5 text-[11px] leading-none font-medium text-zinc-400">{items.length}</span>
            </h2>
            <p className="mt-0.5 text-xs text-zinc-500">{description}</p>
          </div>
        </div>
        {headerAction}
      </div>

      <form onSubmit={submit} className="flex gap-2 px-5 pt-4">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={canAdd ? t("players.namePlaceholder") : t("players.online.stopped")}
          pattern="[A-Za-z0-9_]{1,32}"
          disabled={!canAdd}
          aria-label={addLabel}
          className="flex-1"
        />
        <Button type="submit" icon={Plus} disabled={!canAdd || !name.trim()} aria-label={addLabel} title={addLabel} />
      </form>

      <ul className="mt-4 flex-1 divide-y divide-white/[0.05] border-t border-white/[0.06]">
        {items.length === 0 ? (
          <li className="px-5 py-8 text-center text-sm text-zinc-500">{emptyText}</li>
        ) : (
          items.map((item) => (
            <li key={itemKey(item)} className="flex items-center justify-between gap-3 px-5 py-3">
              {renderItem(item)}
            </li>
          ))
        )}
      </ul>
    </Card>
  );
}
