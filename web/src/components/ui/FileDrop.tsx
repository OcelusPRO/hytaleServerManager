import { Upload } from "lucide-react";
import { useRef, useState, type DragEvent } from "react";
import { cn } from "../../lib/cn";
import { formatPercent } from "../../lib/format";
import { Progress } from "./Progress";

interface FileDropProps {
  accept: string;
  multiple?: boolean;
  title: string;
  hint: string;
  /** Uploads one file, reporting progress between 0 and 1. */
  onFile: (file: File, onProgress: (fraction: number) => void) => Promise<unknown>;
}

/** Drag & drop area with a file picker fallback and upload progress. */
export function FileDrop({ accept, multiple, title, hint, onFile }: FileDropProps) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [upload, setUpload] = useState<{ name: string; progress: number } | null>(null);

  const send = async (files: File[]) => {
    for (const file of multiple ? files : files.slice(0, 1)) {
      setUpload({ name: file.name, progress: 0 });
      await onFile(file, (progress) => setUpload({ name: file.name, progress }));
    }
    setUpload(null);
    if (input.current) input.current.value = "";
  };

  const onDrop = (event: DragEvent) => {
    event.preventDefault();
    setOver(false);
    void send([...event.dataTransfer.files]);
  };

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={onDrop}
      className={cn(
        "rounded-xl border border-dashed px-5 py-6 transition-colors",
        over ? "border-brand-500 bg-brand-500/5" : "border-white/10 bg-zinc-950/30",
      )}
    >
      {upload ? (
        <div>
          <div className="flex items-baseline justify-between gap-3 text-sm">
            <span className="truncate text-zinc-300">{upload.name}</span>
            <span className="text-xs text-zinc-400 tabular-nums">{formatPercent(upload.progress)}</span>
          </div>
          <Progress value={upload.progress} className="mt-2" />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => input.current?.click()}
          className="flex w-full cursor-pointer items-center justify-center gap-3 text-left"
        >
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-zinc-800 ring-1 ring-white/5">
            <Upload className="size-4 text-zinc-400" aria-hidden />
          </span>
          <span>
            <span className="block text-sm font-medium text-zinc-200">{title}</span>
            <span className="block text-xs text-zinc-500">{hint}</span>
          </span>
        </button>
      )}
      <input
        ref={input}
        type="file"
        accept={accept}
        multiple={multiple}
        className="hidden"
        onChange={(e) => void send([...(e.target.files ?? [])])}
      />
    </div>
  );
}
