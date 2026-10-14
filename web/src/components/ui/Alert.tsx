import { CircleAlert, CircleCheck, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../../lib/cn";

type AlertTone = "info" | "success" | "warning" | "danger";

const styles: Record<AlertTone, { box: string; icon: string; Icon: LucideIcon }> = {
  info: { box: "bg-sky-500/[0.07] ring-sky-500/20", icon: "text-sky-400", Icon: Info },
  success: { box: "bg-emerald-500/[0.07] ring-emerald-500/20", icon: "text-emerald-400", Icon: CircleCheck },
  warning: { box: "bg-amber-500/[0.07] ring-amber-500/20", icon: "text-amber-400", Icon: TriangleAlert },
  danger: { box: "bg-red-500/[0.07] ring-red-500/20", icon: "text-red-400", Icon: CircleAlert },
};

interface AlertProps {
  tone?: AlertTone;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  className?: string;
}

export function Alert({ tone = "info", title, children, action, className }: AlertProps) {
  const { box, icon, Icon } = styles[tone];
  return (
    <div className={cn("flex items-start gap-3 rounded-xl px-4 py-3 ring-1 ring-inset", box, className)} role="status">
      <Icon className={cn("mt-0.5 size-4 shrink-0", icon)} aria-hidden />
      <div className="min-w-0 flex-1 text-sm">
        {title && <p className="font-medium text-zinc-100">{title}</p>}
        {children && <div className={cn("text-zinc-300", title && "mt-0.5")}>{children}</div>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}
