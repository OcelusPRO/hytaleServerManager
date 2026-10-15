import { CircleAlert, CircleCheck, X } from "lucide-react";
import { createContext, use, useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { errorMessage } from "../../lib/api";
import { cn } from "../../lib/cn";
import { Button } from "./Button";
import { Field, Input } from "./Field";
import { translate as t, useI18n } from "../../lib/i18n";

// ---------------------------------------------------------------- toasts

type ToastKind = "success" | "error";
interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ConfirmOptions {
  title: string;
  message?: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
}

interface PromptOptions {
  title: string;
  label: string;
  defaultValue?: string;
  confirmLabel?: string;
  maxLength?: number;
  placeholder?: string;
  /** Allows submitting an empty value. */
  optional?: boolean;
}

interface Feedback {
  notify: (kind: ToastKind, message: string) => void;
  /** Runs an action, reporting failures (and optionally success) as toasts. */
  run: <T>(action: () => Promise<T>, success?: string) => Promise<T | undefined>;
  confirm: (options: ConfirmOptions) => Promise<boolean>;
  /** Asks for a single line of text; resolves to null when cancelled. */
  prompt: (options: PromptOptions) => Promise<string | null>;
}

const FeedbackContext = createContext<Feedback | null>(null);

export function useFeedback(): Feedback {
  const ctx = use(FeedbackContext);
  if (!ctx) throw new Error("useFeedback outside FeedbackProvider");
  return ctx;
}

let nextToastId = 1;

export function FeedbackProvider({ children }: { children: ReactNode }) {
  useI18n(); // dialogs follow the interface language
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [dialog, setDialog] = useState<(ConfirmOptions & { resolve: (ok: boolean) => void }) | null>(null);
  const [promptState, setPromptState] = useState<(PromptOptions & { resolve: (v: string | null) => void }) | null>(null);

  const dismiss = useCallback((id: number) => setToasts((all) => all.filter((t) => t.id !== id)), []);

  const notify = useCallback(
    (kind: ToastKind, message: string) => {
      const id = nextToastId++;
      setToasts((all) => [...all.slice(-3), { id, kind, message }]);
      window.setTimeout(() => dismiss(id), kind === "error" ? 8000 : 4000);
    },
    [dismiss],
  );

  const run = useCallback(
    async <T,>(action: () => Promise<T>, success?: string) => {
      try {
        const result = await action();
        if (success) notify("success", success);
        return result;
      } catch (err) {
        notify("error", errorMessage(err));
        return undefined;
      }
    },
    [notify],
  );

  const confirm = useCallback(
    (options: ConfirmOptions) => new Promise<boolean>((resolve) => setDialog({ ...options, resolve })),
    [],
  );

  const prompt = useCallback(
    (options: PromptOptions) => new Promise<string | null>((resolve) => setPromptState({ ...options, resolve })),
    [],
  );

  const close = (ok: boolean) => {
    dialog?.resolve(ok);
    setDialog(null);
  };

  const closePrompt = (value: string | null) => {
    promptState?.resolve(value);
    setPromptState(null);
  };

  return (
    <FeedbackContext value={{ notify, run, confirm, prompt }}>
      {children}
      <div className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-full max-w-sm flex-col gap-2">
        {toasts.map((t) => (
          <Toast key={t.id} item={t} onClose={() => dismiss(t.id)} />
        ))}
      </div>
      {dialog && <ConfirmDialog options={dialog} onClose={close} />}
      {promptState && <PromptDialog options={promptState} onClose={closePrompt} />}
    </FeedbackContext>
  );
}

function Toast({ item, onClose }: { item: ToastItem; onClose: () => void }) {
  const Icon = item.kind === "success" ? CircleCheck : CircleAlert;
  return (
    <div className="pointer-events-auto flex animate-fade-in items-start gap-3 rounded-xl bg-zinc-900 px-4 py-3 shadow-xl ring-1 shadow-black/40 ring-white/10">
      <Icon className={cn("mt-0.5 size-4 shrink-0", item.kind === "success" ? "text-emerald-400" : "text-red-400")} />
      <p className="min-w-0 flex-1 text-sm text-zinc-200">{item.message}</p>
      <button type="button" onClick={onClose} className="cursor-pointer text-zinc-500 hover:text-zinc-300" aria-label={t("shell.dialog.close")}>
        <X className="size-4" />
      </button>
    </div>
  );
}

// ---------------------------------------------------------------- dialogs

/** Centered dialog over a dimmed backdrop; Escape or a backdrop click dismisses it. */
export function Modal({ children, onDismiss, label, wide }: { children: ReactNode; onDismiss: () => void; label: string; wide?: boolean }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onDismiss();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onDismiss]);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onDismiss} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={label}
        className={cn("relative max-h-[90vh] w-full animate-fade-in overflow-y-auto rounded-2xl bg-zinc-900 p-6 shadow-2xl ring-1 ring-white/10", wide ? "max-w-3xl" : "max-w-md")}
      >
        {children}
      </div>
    </div>
  );
}

function PromptDialog({ options, onClose }: { options: PromptOptions; onClose: (value: string | null) => void }) {
  const [value, setValue] = useState(options.defaultValue ?? "");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (value.trim() || options.optional) onClose(value.trim());
  };
  return (
    <Modal label={options.title} onDismiss={() => onClose(null)}>
      <form onSubmit={submit}>
        <h2 className="text-base font-semibold text-zinc-100">{options.title}</h2>
        <Field label={options.label} className="mt-4">
          {(id) => (
            <Input id={id} autoFocus value={value} maxLength={options.maxLength} placeholder={options.placeholder} onChange={(e) => setValue(e.target.value)} />
          )}
        </Field>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => onClose(null)}>
            {t("shell.dialog.cancel")}
          </Button>
          <Button type="submit" variant="primary" disabled={!value.trim() && !options.optional}>
            {options.confirmLabel ?? t("shell.dialog.ok")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function ConfirmDialog({ options, onClose }: { options: ConfirmOptions; onClose: (ok: boolean) => void }) {
  const confirmRef = useRef<HTMLButtonElement>(null);
  useEffect(() => confirmRef.current?.focus(), []);

  return (
    <Modal label={options.title} onDismiss={() => onClose(false)}>
      <h2 className="text-base font-semibold text-zinc-100">{options.title}</h2>
      {options.message && <div className="mt-2 text-sm text-zinc-400">{options.message}</div>}
      <div className="mt-6 flex justify-end gap-2">
        <Button variant="ghost" onClick={() => onClose(false)}>
          {t("shell.dialog.cancel")}
        </Button>
        <Button ref={confirmRef} variant={options.danger ? "danger" : "primary"} onClick={() => onClose(true)}>
          {options.confirmLabel ?? t("shell.dialog.confirm")}
        </Button>
      </div>
    </Modal>
  );
}
