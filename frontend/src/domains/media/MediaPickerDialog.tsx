import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ImagePlus, Loader2, Search, Upload } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  isUploadAccepted,
  MEDIA_MAX_BYTES,
  uploadMediaFile,
} from "@/api";
import type { Media } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { formatBytes } from "@/lib/format";
import { cn, uid } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { EmptyState, ErrorState, LoadingPage } from "@/components/common/page";
import { MediaStatusBadge } from "@/components/common/status-badges";
import { MediaThumb } from "./media-preview";

type Accept = "image" | "video" | "all";

function accepts(media: Media, accept: Accept): boolean {
  if (accept === "image") return media.content_type.startsWith("image/");
  if (accept === "video") return media.content_type.startsWith("video/");
  return true;
}

export function MediaPickerDialog({
  open,
  onClose,
  onSelect,
  mode = "single",
  accept = "all",
  initialSelected = [],
  title = "Выбрать медиа",
}: {
  open: boolean;
  onClose: () => void;
  onSelect: (media: Media[]) => void;
  mode?: "single" | "multiple";
  accept?: Accept;
  initialSelected?: string[];
  title?: string;
}) {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [selectedIds, setSelectedIds] = useState<string[]>(initialSelected);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState<number | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      setSelectedIds(initialSelected);
      setSearch("");
      setUploading(false);
      setProgress(null);
    }
    // initialSelected intentionally only applied when opening.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const mediaQuery = useQuery({
    queryKey: queryKeys.media.list({ status: "ready", limit: 100 }),
    queryFn: ({ signal }) => api.media.list({ status: "ready", limit: 100 }, signal),
    enabled: open,
  });

  const items = useMemo(
    () =>
      (mediaQuery.data?.items ?? []).filter(
        (media) =>
          accepts(media, accept) &&
          (!search.trim() ||
            media.original_filename.toLowerCase().includes(search.trim().toLowerCase())),
      ),
    [mediaQuery.data, accept, search],
  );

  const selectedMedia = useMemo(
    () => (mediaQuery.data?.items ?? []).filter((media) => selectedIds.includes(media.id)),
    [mediaQuery.data, selectedIds],
  );

  const toggle = (media: Media) => {
    if (mode === "single") {
      setSelectedIds([media.id]);
      return;
    }
    setSelectedIds((current) =>
      current.includes(media.id)
        ? current.filter((id) => id !== media.id)
        : [...current, media.id],
    );
  };

  const handleUpload = async (file: File | undefined) => {
    if (!file) return;
    if (!isUploadAccepted(file)) {
      toast.error("Неподдерживаемый тип файла");
      return;
    }
    if (file.size > MEDIA_MAX_BYTES) {
      toast.error(`Файл больше ${formatBytes(MEDIA_MAX_BYTES)}`);
      return;
    }
    setUploading(true);
    setProgress(0);
    try {
      const uploaded = await uploadMediaFile(file, setProgress);
      await queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      if (uploaded.status === "ready" && accepts(uploaded, accept)) {
        setSelectedIds((current) =>
          mode === "single" ? [uploaded.id] : [...current, uploaded.id],
        );
      }
      toast.success("Файл загружен");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось загрузить файл");
    } finally {
      setUploading(false);
      setProgress(null);
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const confirm = () => {
    onSelect(selectedMedia);
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-5xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Выбираются ID медиаобъектов, а не URL. После выбора блок сохраняет
            ссылку на файл.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Поиск по имени файла"
              className="pl-9"
              aria-label="Поиск медиа"
            />
          </div>
          <input
            ref={fileInput}
            type="file"
            className="sr-only"
            accept={accept === "image" ? "image/*" : accept === "video" ? "video/mp4" : undefined}
            onChange={(event) => void handleUpload(event.target.files?.[0])}
            aria-label="Загрузить новый файл"
          />
          <Button
            type="button"
            variant="outline"
            onClick={() => fileInput.current?.click()}
            disabled={uploading}
          >
            {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
            Загрузить новый
          </Button>
        </div>

        {uploading && (
          <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>Загрузка файла…</span>
              <span>{progress === null ? "…" : `${progress}%`}</span>
            </div>
            <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-border">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${progress ?? 15}%` }}
              />
            </div>
          </div>
        )}

        <div className="max-h-[50vh] min-h-[16rem] overflow-y-auto rounded-md border border-border p-3 thin-scrollbar">
          {mediaQuery.isLoading ? (
            <LoadingPage label="Загружаем медиатеку…" />
          ) : mediaQuery.error ? (
            <ErrorState
              error={mediaQuery.error}
              onRetry={() => void mediaQuery.refetch()}
            />
          ) : items.length === 0 ? (
            <EmptyState
              icon={ImagePlus}
              title={search ? "Файлы не найдены" : "Нет доступных файлов"}
              description={
                search
                  ? "Измените запрос или загрузите новый файл."
                  : "Загрузите изображение или видео, чтобы использовать его в контенте."
              }
              action={
                <Button variant="outline" onClick={() => fileInput.current?.click()}>
                  <Upload />
                  Загрузить файл
                </Button>
              }
              className="border-0"
            />
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4">
              {items.map((media) => {
                const selected = selectedIds.includes(media.id);
                return (
                  <button
                    key={media.id}
                    type="button"
                    onClick={() => toggle(media)}
                    aria-pressed={selected}
                    className={cn(
                      "group relative overflow-hidden rounded-md border text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                      selected
                        ? "border-primary ring-1 ring-primary/30"
                        : "border-border hover:border-primary/40",
                    )}
                  >
                    <div className="aspect-[4/3] bg-muted/40">
                      <MediaThumb media={media} variant="thumbnail" />
                    </div>
                    <div className="p-2">
                      <p className="truncate text-xs font-medium" title={media.original_filename}>
                        {media.original_filename}
                      </p>
                      <p className="mt-0.5 text-2xs text-muted-foreground">
                        {formatBytes(media.size_bytes)} · {media.content_type}
                      </p>
                    </div>
                    {selected && (
                      <span className="absolute right-2 top-2 flex h-5 w-5 items-center justify-center rounded-full bg-primary text-primary-foreground">
                        <Check className="h-3 w-3" />
                      </span>
                    )}
                    {media.status !== "ready" && (
                      <span className="absolute left-2 top-2">
                        <MediaStatusBadge status={media.status} />
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>

        <DialogFooter className="items-center sm:justify-between">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="muted">Выбрано: {selectedIds.length}</Badge>
            {mode === "multiple" && <span>Можно выбрать несколько файлов</span>}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={onClose}>
              Отмена
            </Button>
            <Button onClick={confirm} disabled={selectedIds.length === 0}>
              <Check />
              Выбрать
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function createPendingUploadId(): string {
  return uid("upload");
}
