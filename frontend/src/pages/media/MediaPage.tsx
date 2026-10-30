import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Check,
  Copy,
  Download,
  Grid2X2,
  ImagePlus,
  LayoutList,
  MoreHorizontal,
  Pencil,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { useAuth } from "@/app/auth";
import {
  api,
  absolutePublicMediaUrl,
  isUploadAccepted,
  MEDIA_MAX_BYTES,
  publicMediaPath,
  uploadMediaFile,
} from "@/api";
import type { Media, MediaSort, MediaStatus, MediaUsage } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { errorMessage } from "@/lib/errors";
import { cn } from "@/lib/utils";
import {
  EmptyState,
  ErrorState,
  InlineAlert,
  PageHeader,
  TableSkeleton,
  useDocumentTitle,
} from "@/components/common/page";
import { useConfirm } from "@/components/common/confirm";
import { MediaStatusBadge } from "@/components/common/status-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/misc";
import { formatBytes, formatDate, formatDateTime } from "@/lib/format";
import {
  isImageMedia,
  MediaThumb,
  mediaPreviewUrl,
} from "@/domains/media/media-preview";

type ViewMode = "grid" | "list";

const sortOptions: Array<{ value: MediaSort; label: string }> = [
  { value: "new", label: "Сначала новые" },
  { value: "old", label: "Сначала старые" },
  { value: "name", label: "По имени" },
  { value: "size_desc", label: "По размеру: больше" },
  { value: "size_asc", label: "По размеру: меньше" },
];

const typeOptions: Array<{ value: "all" | "image" | "video" | "pdf"; label: string }> = [
  { value: "all", label: "Все типы" },
  { value: "image", label: "Изображения" },
  { value: "video", label: "Видео" },
  { value: "pdf", label: "PDF" },
];

const statusOptions: Array<{ value: MediaStatus | "all"; label: string }> = [
  { value: "all", label: "Все статусы" },
  { value: "ready", label: "Готово" },
  { value: "processing", label: "Обработка" },
  { value: "failed", label: "Ошибка" },
];

function useMediaFilterParams() {
  const [searchParams, setSearchParams] = useSearchParams();
  const q = searchParams.get("q") ?? "";
  const type = (searchParams.get("type") ?? "all") as "all" | "image" | "video" | "pdf";
  const status = (searchParams.get("status") ?? "all") as MediaStatus | "all";
  const sort = (searchParams.get("sort") ?? "new") as MediaSort;
  const from = searchParams.get("from") ?? "";
  const to = searchParams.get("to") ?? "";
  const min = searchParams.get("min") ?? "";
  const max = searchParams.get("max") ?? "";
  const view = (searchParams.get("view") ?? "grid") as ViewMode;

  const update = (values: Record<string, string | null>, replace = false) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        Object.entries(values).forEach(([key, value]) => {
          if (value === null || value === "" || value === "all") {
            next.delete(key);
          } else if (
            (key === "sort" && value === "new") ||
            (key === "view" && value === "grid")
          ) {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        });
        return next;
      },
      { replace },
    );
  };

  return { q, type, status, sort, from, to, min, max, view, update };
}

function mbToBytes(value: string): number | undefined {
  if (value.trim() === "") return undefined;
  const mb = Number(value.replace(",", "."));
  if (!Number.isFinite(mb) || mb < 0) return undefined;
  return Math.round(mb * 1024 * 1024);
}

function mimeLabel(contentType: string): string {
  if (contentType.startsWith("image/")) return "Изображение";
  if (contentType.startsWith("video/")) return "Видео";
  if (contentType === "application/pdf") return "PDF";
  return "Файл";
}

function copyText(value: string, message: string) {
  if (navigator.clipboard?.writeText) {
    void navigator.clipboard.writeText(value).then(
      () => toast.success(message),
      () => toast.error("Не удалось скопировать"),
    );
    return;
  }
  toast.error("Буфер обмена недоступен");
}

function downloadMedia(media: Media) {
  const link = document.createElement("a");
  link.href = publicMediaPath(media.id);
  link.download = media.original_filename;
  link.rel = "noopener";
  link.target = "_blank";
  document.body.appendChild(link);
  link.click();
  link.remove();
}

type UploadTask = {
  id: string;
  file: File;
  progress: number | null;
  status: "pending" | "uploading" | "success" | "error" | "canceled";
  error?: string;
};

function taskId(file: File, index: number): string {
  return `${index}-${file.name}-${file.size}-${file.lastModified}`;
}

function UploadDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [tasks, setTasks] = useState<UploadTask[]>([]);
  const [dragging, setDragging] = useState(false);
  const controllers = useRef(new Map<string, AbortController>());
  const fileInput = useRef<HTMLInputElement>(null);
  const runningRef = useRef(false);

  useEffect(() => {
    controllers.current.forEach((controller) => controller.abort());
    controllers.current.clear();
    runningRef.current = false;
    setTasks([]);
    setDragging(false);
  }, [open]);

  const addFiles = useCallback((files: FileList | File[]) => {
    const incoming = Array.from(files);
    if (incoming.length === 0) return;
    setTasks((current) => {
      const known = new Set(current.map((task) => task.id));
      const added: UploadTask[] = [];
      incoming.forEach((file, index) => {
        const id = taskId(file, current.length + index);
        if (known.has(id)) return;
        if (!isUploadAccepted(file)) {
          added.push({
            id,
            file,
            progress: null,
            status: "error",
            error: "Неподдерживаемый тип файла",
          });
          return;
        }
        if (file.size > MEDIA_MAX_BYTES) {
          added.push({
            id,
            file,
            progress: null,
            status: "error",
            error: `Файл больше ${formatBytes(MEDIA_MAX_BYTES)}`,
          });
          return;
        }
        added.push({ id, file, progress: null, status: "pending" });
      });
      return [...current, ...added];
    });
  }, []);

  const uploadOne = useCallback(
    async (task: UploadTask) => {
      const controller = new AbortController();
      controllers.current.set(task.id, controller);
      setTasks((current) =>
        current.map((item) =>
          item.id === task.id
            ? { ...item, status: "uploading", progress: 0, error: undefined }
            : item,
        ),
      );
      try {
        await uploadMediaFile(
          task.file,
          (progress) => {
            setTasks((current) =>
              current.map((item) =>
                item.id === task.id ? { ...item, progress } : item,
              ),
            );
          },
          controller.signal,
        );
        setTasks((current) =>
          current.map((item) =>
            item.id === task.id
              ? { ...item, status: "success", progress: 100 }
              : item,
          ),
        );
        void queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      } catch (error) {
        if (controller.signal.aborted) {
          setTasks((current) =>
            current.map((item) =>
              item.id === task.id ? { ...item, status: "canceled" } : item,
            ),
          );
        } else {
          setTasks((current) =>
            current.map((item) =>
              item.id === task.id
                ? {
                    ...item,
                    status: "error",
                    error: error instanceof Error ? error.message : "Ошибка загрузки",
                  }
                : item,
            ),
          );
        }
      } finally {
        controllers.current.delete(task.id);
      }
    },
    [queryClient],
  );

  // Upload files one by one. Any task that is still pending when a previous
  // upload finishes is picked up again on the next effect run.
  useEffect(() => {
    if (!open || runningRef.current) return;
    const next = tasks.find((task) => task.status === "pending");
    if (!next) return;
    runningRef.current = true;
    void uploadOne(next).finally(() => {
      runningRef.current = false;
      setTasks((current) => [...current]);
    });
  }, [open, tasks, uploadOne]);

  const cancelTask = (id: string) => {
    controllers.current.get(id)?.abort();
  };

  const cancelAll = () => {
    controllers.current.forEach((controller) => controller.abort());
  };

  const retry = (task: UploadTask) => {
    setTasks((current) =>
      current.map((item) =>
        item.id === task.id
          ? { ...item, status: "pending", progress: null, error: undefined }
          : item,
      ),
    );
  };

  const done = tasks.filter((task) => task.status === "success").length;
  const failed = tasks.filter((task) => task.status === "error").length;
  const active = tasks.some(
    (task) => task.status === "uploading" || task.status === "pending",
  );

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Загрузка медиа</DialogTitle>
          <DialogDescription>
            Изображения, PDF и MP4 до {formatBytes(MEDIA_MAX_BYTES)}. Можно выбрать
            несколько файлов сразу.
          </DialogDescription>
        </DialogHeader>

        <input
          ref={fileInput}
          type="file"
          multiple
          className="sr-only"
          onChange={(event) => {
            if (event.target.files) addFiles(event.target.files);
            event.target.value = "";
          }}
          aria-label="Выбрать файлы"
        />

        <div
          role="button"
          tabIndex={0}
          onClick={() => fileInput.current?.click()}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              fileInput.current?.click();
            }
          }}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault();
            setDragging(false);
            addFiles(event.dataTransfer.files);
          }}
          className={cn(
            "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-6 text-center transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            dragging
              ? "border-primary bg-primary/5"
              : "border-border bg-muted/20 hover:border-primary/40",
          )}
        >
          <Upload className="h-6 w-6 text-muted-foreground" aria-hidden="true" />
          <p className="text-sm font-medium">
            Перетащите файлы сюда или нажмите, чтобы выбрать
          </p>
          <p className="text-xs text-muted-foreground">
            Поддерживаются JPG, PNG, GIF, WebP, PDF и MP4
          </p>
        </div>

        {tasks.length > 0 && (
          <div className="max-h-64 space-y-2 overflow-y-auto thin-scrollbar">
            {tasks.map((task) => (
              <div
                key={task.id}
                className="rounded-md border border-border bg-card px-3 py-2"
              >
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-xs font-medium" title={task.file.name}>
                      {task.file.name}
                    </p>
                    <p className="text-2xs text-muted-foreground">
                      {formatBytes(task.file.size)}
                      {task.error ? ` · ${task.error}` : ""}
                    </p>
                  </div>
                  <span className="shrink-0 text-2xs text-muted-foreground">
                    {task.status === "success"
                      ? "Готово"
                      : task.status === "error"
                        ? "Ошибка"
                        : task.status === "canceled"
                          ? "Отменено"
                          : task.status === "pending"
                            ? "В очереди"
                            : task.progress === null
                              ? "Загрузка…"
                              : `${task.progress}%`}
                  </span>
                  {task.status === "uploading" && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => cancelTask(task.id)}
                      aria-label={`Отменить загрузку ${task.file.name}`}
                    >
                      <X />
                    </Button>
                  )}
                  {(task.status === "error" || task.status === "canceled") && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => retry(task)}
                      aria-label={`Повторить загрузку ${task.file.name}`}
                    >
                      <RefreshCw />
                    </Button>
                  )}
                  {task.status === "success" && (
                    <Check className="h-4 w-4 shrink-0 text-success" aria-hidden="true" />
                  )}
                </div>
                {(task.status === "uploading" || task.status === "success") && (
                  <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-border">
                    <div
                      className={cn(
                        "h-full transition-all",
                        task.status === "success" ? "bg-success" : "bg-primary",
                      )}
                      style={{ width: `${task.progress ?? 15}%` }}
                    />
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        <DialogFooter className="items-center sm:justify-between">
          <div className="text-xs text-muted-foreground">
            {tasks.length > 0
              ? `Готово: ${done} · Ошибок: ${failed}`
              : "Файлы ещё не выбраны"}
          </div>
          <div className="flex gap-2">
            {active ? (
              <Button variant="outline" onClick={cancelAll}>
                Отменить всё
              </Button>
            ) : (
              <Button variant="outline" onClick={onClose}>
                Закрыть
              </Button>
            )}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function UsageList({ mediaId }: { mediaId: string }) {
  const usageQuery = useQuery({
    queryKey: [...queryKeys.media.detail(mediaId), "usage"],
    queryFn: ({ signal }) => api.media.usage(mediaId, signal),
  });

  if (usageQuery.isLoading) {
    return (
      <div className="space-y-1.5" aria-label="Загрузка использования">
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-4 w-1/2" />
      </div>
    );
  }
  if (usageQuery.error) {
    return (
      <p className="text-xs text-destructive">
        Не удалось загрузить использование: {errorMessage(usageQuery.error)}
      </p>
    );
  }
  const items: MediaUsage[] = usageQuery.data?.items ?? [];
  if (items.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Файл не используется в записях или заявках.
      </p>
    );
  }
  return (
    <ul className="space-y-1.5">
      {items.map((usage) => (
        <li
          key={`${usage.kind}-${usage.id}`}
          className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs"
        >
          <Badge variant={usage.kind === "entry" ? "info" : "secondary"}>
            {usage.kind === "entry" ? "Запись" : "Заявка"}
          </Badge>
          <code className="text-2xs text-foreground">{usage.feature_key}</code>
          {usage.kind === "entry" && (
            <span className="text-muted-foreground">версия {usage.version}</span>
          )}
          <span className="text-muted-foreground">· {usage.status}</span>
          <span className="text-muted-foreground">
            · {formatDateTime(usage.occurred_at)}
          </span>
        </li>
      ))}
    </ul>
  );
}

function MediaDetailDialog({
  media,
  open,
  onClose,
}: {
  media: Media | null;
  open: boolean;
  onClose: () => void;
}) {
  const { principal } = useAuth();
  const canDelete = principal?.role === "admin";
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [filename, setFilename] = useState(media?.original_filename ?? "");

  useEffect(() => {
    setFilename(media?.original_filename ?? "");
  }, [media]);

  const usageQuery = useQuery({
    queryKey: [...queryKeys.media.detail(media?.id ?? ""), "usage"],
    queryFn: ({ signal }) => api.media.usage(media!.id, signal),
    enabled: open && Boolean(media),
  });

  const renameMutation = useMutation({
    mutationFn: (name: string) => api.media.rename(media!.id, name),
    onSuccess: (updated) => {
      toast.success("Имя файла обновлено");
      setFilename(updated.original_filename);
      void queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const deleteMutation = useMutation({
    mutationFn: () => api.media.remove(media!.id),
    onSuccess: () => {
      toast.success("Файл удалён");
      void queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      onClose();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const retryMutation = useMutation({
    mutationFn: () => api.media.retry(media!.id),
    onSuccess: () => {
      toast.success("Обработка запущена повторно");
      void queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const requestDelete = async () => {
    if (!media) return;
    const usages = usageQuery.data?.items ?? [];
    const confirmed = await confirm({
      title: "Удалить файл?",
      description: (
        <div className="space-y-2">
          <p>
            Файл <strong>«{media.original_filename}»</strong> будет удалён из
            медиатеки. Связанные записи и заявки не изменятся автоматически.
          </p>
          {usages.length > 0 ? (
            <div className="rounded-md border border-warning/40 bg-warning/10 p-2 text-xs">
              <p className="font-medium text-foreground">
                Файл используется в {usages.length} объект(ах):
              </p>
              <ul className="mt-1 space-y-0.5 text-muted-foreground">
                {usages.map((usage) => (
                  <li key={`${usage.kind}-${usage.id}`}>
                    {usage.kind === "entry" ? "Запись" : "Заявка"} ·{" "}
                    {usage.feature_key}
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <p className="text-muted-foreground">Файл нигде не используется.</p>
          )}
        </div>
      ),
      confirmLabel: "Удалить",
      destructive: true,
    });
    if (confirmed) deleteMutation.mutate();
  };

  if (!media) return null;

  const renamed = filename.trim() !== media.original_filename;

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle className="truncate">{media.original_filename}</DialogTitle>
          <DialogDescription>
            {mimeLabel(media.content_type)} · {media.content_type} ·{" "}
            {formatBytes(media.size_bytes)}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_16rem]">
          <div className="overflow-hidden rounded-lg border border-border bg-muted/30">
            {isImageMedia(media) && media.status === "ready" ? (
              <img
                src={mediaPreviewUrl(media, "medium")}
                alt={media.original_filename}
                className="max-h-[45vh] w-full object-contain"
              />
            ) : media.content_type.startsWith("video/") && media.status === "ready" ? (
              <video
                src={mediaPreviewUrl(media)}
                controls
                className="max-h-[45vh] w-full bg-black"
              />
            ) : (
              <div className="flex h-48 items-center justify-center">
                <MediaThumb media={media} className="h-24 w-24 rounded-md" />
              </div>
            )}
          </div>

          <div className="space-y-3 text-xs">
            <div className="flex items-center gap-1.5">
              <MediaStatusBadge status={media.status} />
              <Badge variant="muted">{mimeLabel(media.content_type)}</Badge>
            </div>
            <dl className="space-y-1.5">
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">Размер</dt>
                <dd>{formatBytes(media.size_bytes)}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">Загружен</dt>
                <dd>{formatDateTime(media.created_at)}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-muted-foreground">Обновлён</dt>
                <dd>{formatDateTime(media.updated_at)}</dd>
              </div>
            </dl>
            <div className="flex flex-wrap gap-1">
              {["thumbnail", "medium", "large"].map((variant) => (
                <Badge
                  key={variant}
                  variant={media.variants[variant] ? "success" : "muted"}
                >
                  {variant}
                </Badge>
              ))}
            </div>
            {media.status === "failed" && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => retryMutation.mutate()}
                loading={retryMutation.isPending}
              >
                <RefreshCw />
                Повторить обработку
              </Button>
            )}
          </div>
        </div>

        <div className="rounded-lg border border-border p-3">
          <p className="text-xs font-semibold">Использование</p>
          <div className="mt-2">
            <UsageList mediaId={media.id} />
          </div>
        </div>

        <div className="space-y-2">
          <label htmlFor="media-filename" className="text-xs font-medium">
            Отображаемое имя
          </label>
          <div className="flex gap-2">
            <Input
              id="media-filename"
              value={filename}
              onChange={(event) => setFilename(event.target.value)}
              aria-label="Отображаемое имя файла"
            />
            <Button
              variant="outline"
              onClick={() => renameMutation.mutate(filename.trim())}
              disabled={!filename.trim() || !renamed}
              loading={renameMutation.isPending}
            >
              <Pencil />
              Сохранить
            </Button>
          </div>
          <p className="text-2xs text-muted-foreground">
            Меняется только отображаемое имя. Ссылка на файл и опубликованный
            контент продолжают работать.
          </p>
        </div>

        <DialogFooter className="flex-wrap items-center justify-between gap-2">
          {canDelete && (
            <Button
              variant="ghost"
              className="text-destructive hover:text-destructive"
              onClick={() => void requestDelete()}
              disabled={deleteMutation.isPending}
            >
              <Trash2 />
              Удалить
            </Button>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={() =>
                copyText(absolutePublicMediaUrl(media.id), "Ссылка скопирована")
              }
            >
              <Copy />
              Копировать ссылку
            </Button>
            <Button variant="outline" onClick={() => downloadMedia(media)}>
              <Download />
              Скачать
            </Button>
            <Button onClick={onClose}>Закрыть</Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MediaActions({
  media,
  onOpen,
}: {
  media: Media;
  onOpen: () => void;
}) {
  const { principal } = useAuth();
  const canDelete = principal?.role === "admin";
  const queryClient = useQueryClient();
  const confirm = useConfirm();

  const deleteMutation = useMutation({
    mutationFn: () => api.media.remove(media.id),
    onSuccess: () => {
      toast.success("Файл удалён");
      void queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const requestDelete = async () => {
    let usages: MediaUsage[];
    try {
      usages = (await api.media.usage(media.id)).items;
    } catch {
      usages = [];
    }
    const confirmed = await confirm({
      title: "Удалить файл?",
      description: (
        <div className="space-y-2">
          <p>
            Файл <strong>«{media.original_filename}»</strong> будет удалён из
            медиатеки. Связанные записи не изменятся автоматически.
          </p>
          {usages.length > 0 ? (
            <div className="rounded-md border border-warning/40 bg-warning/10 p-2 text-xs">
              <p className="font-medium text-foreground">
                Файл используется в {usages.length} объект(ах):
              </p>
              <ul className="mt-1 space-y-0.5 text-muted-foreground">
                {usages.map((usage) => (
                  <li key={`${usage.kind}-${usage.id}`}>
                    {usage.kind === "entry" ? "Запись" : "Заявка"} ·{" "}
                    {usage.feature_key}
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <p className="text-muted-foreground">Файл нигде не используется.</p>
          )}
        </div>
      ),
      confirmLabel: "Удалить",
      destructive: true,
    });
    if (confirmed) deleteMutation.mutate();
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Действия с ${media.original_filename}`}
        >
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onSelect={onOpen}>
          <Pencil className="h-4 w-4" />
          Подробности
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={() =>
            copyText(absolutePublicMediaUrl(media.id), "Ссылка скопирована")
          }
        >
          <Copy className="h-4 w-4" />
          Копировать ссылку
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => downloadMedia(media)}>
          <Download className="h-4 w-4" />
          Скачать
        </DropdownMenuItem>
        {canDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onSelect={() => void requestDelete()}
              disabled={deleteMutation.isPending}
            >
              <Trash2 className="h-4 w-4" />
              Удалить…
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MediaGridCard({
  media,
  onOpen,
}: {
  media: Media;
  onOpen: () => void;
}) {
  return (
    <div className="group overflow-hidden rounded-lg border border-border bg-card">
      <button
        type="button"
        onClick={onOpen}
        className="block w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={`Открыть ${media.original_filename}`}
      >
        <div className="relative aspect-[4/3] bg-muted/40">
          <MediaThumb media={media} variant="thumbnail" />
          <span className="absolute left-2 top-2">
            <MediaStatusBadge status={media.status} />
          </span>
        </div>
        <div className="p-3">
          <p
            className="truncate text-xs font-medium"
            title={media.original_filename}
          >
            {media.original_filename}
          </p>
          <p className="mt-0.5 truncate text-2xs text-muted-foreground">
            {mimeLabel(media.content_type)} · {formatBytes(media.size_bytes)}
          </p>
          <p className="mt-0.5 text-2xs text-muted-foreground">
            {formatDate(media.created_at)}
          </p>
        </div>
      </button>
      <div className="flex items-center justify-between border-t border-border px-2 py-1">
        <span className="truncate px-1 text-2xs text-muted-foreground">
          {media.content_type}
        </span>
        <MediaActions media={media} onOpen={onOpen} />
      </div>
    </div>
  );
}

function MediaTable({
  items,
  onOpen,
}: {
  items: Media[];
  onOpen: (media: Media) => void;
}) {
  return (
    <div className="table-wrap">
      <table className="data-table min-w-[760px]">
        <thead>
          <tr>
            <th>Файл</th>
            <th>Тип</th>
            <th>Размер</th>
            <th>Статус</th>
            <th>Загружен</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((media) => (
            <tr key={media.id} className="group">
              <td>
                <div className="flex min-w-0 items-center gap-2.5">
                  <button
                    type="button"
                    onClick={() => onOpen(media)}
                    className="h-9 w-9 shrink-0 overflow-hidden rounded border border-border bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={`Открыть ${media.original_filename}`}
                  >
                    <MediaThumb media={media} variant="thumbnail" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onOpen(media)}
                    className="min-w-0 text-left"
                  >
                    <span className="block max-w-[22rem] truncate text-sm font-medium hover:text-primary">
                      {media.original_filename}
                    </span>
                    <code className="block truncate text-2xs text-muted-foreground">
                      {media.content_type}
                    </code>
                  </button>
                </div>
              </td>
              <td>
                <span className="text-xs text-muted-foreground">
                  {mimeLabel(media.content_type)}
                </span>
              </td>
              <td className="text-xs text-muted-foreground">
                {formatBytes(media.size_bytes)}
              </td>
              <td>
                <MediaStatusBadge status={media.status} />
              </td>
              <td className="text-xs text-muted-foreground">
                {formatDateTime(media.created_at)}
              </td>
              <td>
                <div className="flex items-center justify-end gap-1">
                  <Button variant="ghost" size="sm" onClick={() => onOpen(media)}>
                    Открыть
                  </Button>
                  <MediaActions media={media} onOpen={() => onOpen(media)} />
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function MediaPage() {
  useDocumentTitle("Медиа");
  const { q, type, status, sort, from, to, min, max, view, update } =
    useMediaFilterParams();
  const [searchInput, setSearchInput] = useState(q);
  const debouncedSearch = useDebouncedValue(searchInput, 250);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [selected, setSelected] = useState<Media | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  useEffect(() => {
    setSearchInput(q);
  }, [q]);

  useEffect(() => {
    if (debouncedSearch !== q) {
      update({ q: debouncedSearch || null }, true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const minSize = mbToBytes(min);
  const maxSize = mbToBytes(max);

  const mediaQuery = useInfiniteQuery({
    queryKey: queryKeys.media.list({
      q,
      type,
      status,
      sort,
      from,
      to,
      min: minSize,
      max: maxSize,
    }),
    queryFn: ({ pageParam, signal }) =>
      api.media.list(
        {
          q: q || undefined,
          type: type === "all" ? "" : type,
          status: status === "all" ? "" : status,
          sort,
          created_from: from || undefined,
          created_to: to || undefined,
          min_size: minSize,
          max_size: maxSize,
          limit: 60,
          cursor: pageParam ?? undefined,
        },
        signal,
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  });

  const items = useMemo(
    () => mediaQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [mediaQuery.data],
  );

  const filtersActive =
    Boolean(q) ||
    type !== "all" ||
    status !== "all" ||
    sort !== "new" ||
    Boolean(from) ||
    Boolean(to) ||
    Boolean(min) ||
    Boolean(max);

  const resetFilters = () => {
    setSearchInput("");
    update({
      q: null,
      type: null,
      status: null,
      sort: null,
      from: null,
      to: null,
      min: null,
      max: null,
    });
  };

  const openMedia = (media: Media) => {
    setSelected(media);
    setDetailOpen(true);
  };

  return (
    <div className="page-grid">
      <PageHeader
        title="Медиа"
        description="Файлы для контента и форм. В документах хранятся ID объектов, поэтому переименование не ломает опубликованные страницы."
        actions={
          <>
            <div className="flex rounded-md border border-border p-0.5">
              <Button
                variant={view === "grid" ? "secondary" : "ghost"}
                size="icon-sm"
                onClick={() => update({ view: null })}
                aria-label="Сетка"
                aria-pressed={view === "grid"}
              >
                <Grid2X2 />
              </Button>
              <Button
                variant={view === "list" ? "secondary" : "ghost"}
                size="icon-sm"
                onClick={() => update({ view: "list" })}
                aria-label="Список"
                aria-pressed={view === "list"}
              >
                <LayoutList />
              </Button>
            </div>
            <Button onClick={() => setUploadOpen(true)}>
              <Upload />
              Загрузить
            </Button>
          </>
        }
      />

      <div className="mb-4 space-y-3">
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="Поиск по имени файла"
              aria-label="Поиск медиа"
              className="pl-9 pr-9"
            />
            {searchInput && (
              <button
                type="button"
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-1 text-muted-foreground hover:bg-accent"
                onClick={() => setSearchInput("")}
                aria-label="Очистить поиск"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
          <div className="grid grid-cols-2 gap-2 sm:flex">
            <Select value={type} onValueChange={(value) => update({ type: value })}>
              <SelectTrigger className="w-full sm:w-40" aria-label="Тип файла">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {typeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={status}
              onValueChange={(value) => update({ status: value })}
            >
              <SelectTrigger className="w-full sm:w-40" aria-label="Статус файла">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {statusOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={sort}
              onValueChange={(value) => update({ sort: value })}
            >
              <SelectTrigger className="col-span-2 w-full sm:w-44" aria-label="Сортировка">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sortOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <div>
            <label htmlFor="media-from" className="mb-1 block text-2xs font-medium text-muted-foreground">
              Загружен с
            </label>
            <Input
              id="media-from"
              type="date"
              value={from}
              onChange={(event) => update({ from: event.target.value || null })}
              aria-label="Дата загрузки с"
            />
          </div>
          <div>
            <label htmlFor="media-to" className="mb-1 block text-2xs font-medium text-muted-foreground">
              Загружен по
            </label>
            <Input
              id="media-to"
              type="date"
              value={to}
              onChange={(event) => update({ to: event.target.value || null })}
              aria-label="Дата загрузки по"
            />
          </div>
          <div>
            <label htmlFor="media-min" className="mb-1 block text-2xs font-medium text-muted-foreground">
              Размер от, МБ
            </label>
            <Input
              id="media-min"
              type="number"
              min={0}
              step="0.1"
              value={min}
              onChange={(event) => update({ min: event.target.value || null })}
              aria-label="Минимальный размер в мегабайтах"
            />
          </div>
          <div>
            <label htmlFor="media-max" className="mb-1 block text-2xs font-medium text-muted-foreground">
              Размер до, МБ
            </label>
            <Input
              id="media-max"
              type="number"
              min={0}
              step="0.1"
              value={max}
              onChange={(event) => update({ max: event.target.value || null })}
              aria-label="Максимальный размер в мегабайтах"
            />
          </div>
        </div>

        {min && max && minSize !== undefined && maxSize !== undefined && minSize > maxSize && (
          <InlineAlert tone="warning">
            Минимальный размер больше максимального — список будет пустым.
          </InlineAlert>
        )}

        <div className="flex flex-wrap items-center gap-2">
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={resetFilters}>
              <X />
              Сбросить фильтры
            </Button>
          )}
          <Badge variant="muted">
            Загружено: {items.length}
            {mediaQuery.hasNextPage ? "+" : ""}
          </Badge>
          {filtersActive && (
            <span className="text-2xs text-muted-foreground">
              Поиск, тип, даты, размер и статус применяются на сервере.
            </span>
          )}
        </div>
      </div>

      {mediaQuery.isLoading ? (
        view === "grid" ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
            {Array.from({ length: 8 }).map((_, index) => (
              <Skeleton key={index} className="aspect-[4/3] w-full rounded-lg" />
            ))}
          </div>
        ) : (
          <TableSkeleton rows={6} />
        )
      ) : mediaQuery.error ? (
        <ErrorState
          error={mediaQuery.error}
          onRetry={() => void mediaQuery.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={ImagePlus}
          title={filtersActive ? "Файлы не найдены" : "Медиатека пуста"}
          description={
            filtersActive
              ? "Измените фильтры или загрузите новый файл."
              : "Загрузите изображения, PDF или видео, чтобы использовать их в контенте и формах."
          }
          action={
            <Button onClick={() => setUploadOpen(true)}>
              <Upload />
              Загрузить файлы
            </Button>
          }
        />
      ) : (
        <>
          {view === "grid" ? (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              {items.map((media) => (
                <MediaGridCard
                  key={media.id}
                  media={media}
                  onOpen={() => openMedia(media)}
                />
              ))}
            </div>
          ) : (
            <>
              <div className="hidden md:block">
                <MediaTable items={items} onOpen={openMedia} />
              </div>
              <div className="grid grid-cols-2 gap-3 md:hidden">
                {items.map((media) => (
                  <MediaGridCard
                    key={media.id}
                    media={media}
                    onOpen={() => openMedia(media)}
                  />
                ))}
              </div>
            </>
          )}

          {mediaQuery.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                onClick={() => void mediaQuery.fetchNextPage()}
                loading={mediaQuery.isFetchingNextPage}
              >
                Показать ещё
              </Button>
            </div>
          )}
        </>
      )}

      <UploadDialog open={uploadOpen} onClose={() => setUploadOpen(false)} />
      <MediaDetailDialog
        media={selected}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
      />
    </div>
  );
}
