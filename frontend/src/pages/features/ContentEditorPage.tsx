import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowLeft,
  Check,
  CloudUpload,
  Eye,
  FileClock,
  FileUp,
  History,
  Loader2,
  Monitor,
  Redo2,
  RefreshCw,
  Save,
  Send,
  Smartphone,
  Tablet,
  Undo2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/api";
import { useAuth } from "@/app/auth";
import type {
  EntryDetail,
  EntryVersion,
  JsonObject,
  JsonValue,
} from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { errorMessage } from "@/lib/errors";
import { formatDateTime, truncate } from "@/lib/format";
import { cn, isRecord } from "@/lib/utils";
import { entryTitleField } from "@/lib/json";
import { useFeatureContext } from "@/pages/features/FeatureLayout";
import { useConfirm } from "@/components/common/confirm";
import {
  EmptyState,
  ErrorState,
  InlineAlert,
  LoadingPage,
  useDocumentTitle,
  useUnsavedChangesGuard,
} from "@/components/common/page";
import { VersionStatusBadge } from "@/components/common/status-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/misc";
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { ContentPreview } from "@/domains/content-editor/ContentPreview";
import {
  EntryFieldsForm,
  validateEntryFields,
} from "@/domains/content-editor/EntryForm";
import {
  blocksKeyFromFields,
  buildDocumentData,
  deriveDocumentTitle,
  documentMetadata,
  parseBlocks,
  starterMetadata,
} from "@/domains/content-editor/blocks";
import type { DocumentState } from "@/domains/content-editor/types";
import { useContentAutosave } from "@/domains/content-editor/useContentAutosave";
import { useEditorHistory } from "@/domains/content-editor/useEditorHistory";
import { parsedSchema } from "@/domains/content-schema/schema";
import type { SchemaFieldNode } from "@/domains/content-schema/types";
import { DiffList, DiffSummaryBadges } from "@/domains/versioning/DiffView";
import { diffJson } from "@/domains/versioning/diff";

type PreviewWidth = "desktop" | "tablet" | "mobile";

const widthOptions: Array<{
  value: PreviewWidth;
  label: string;
  icon: typeof Monitor;
}> = [
  { value: "desktop", label: "Десктоп", icon: Monitor },
  { value: "tablet", label: "Планшет", icon: Tablet },
  { value: "mobile", label: "Телефон", icon: Smartphone },
];

function collectMediaIds(
  state: DocumentState,
  fields: SchemaFieldNode[],
): string[] {
  const ids = new Set<string>();
  state.blocks.forEach((block) => {
    if (block.type === "image" && block.data.mediaId) ids.add(block.data.mediaId);
    if (block.type === "video" && block.data.mediaId) ids.add(block.data.mediaId);
    if (block.type === "gallery") {
      block.data.mediaIds.forEach((id) => ids.add(id));
    }
  });
  const addMedia = (value: unknown) => {
    if (typeof value === "string" && value) ids.add(value);
    if (Array.isArray(value)) value.forEach(addMedia);
  };
  const walk = (field: SchemaFieldNode, value: JsonValue | undefined) => {
    if (field.type === "media" || field.type === "file") {
      addMedia(value);
      return;
    }
    if (field.type === "object") {
      if (!isRecord(value)) return;
      field.children.forEach((child) => walk(child, value[child.key]));
      return;
    }
    if (field.type === "array") {
      if (!Array.isArray(value)) return;
      if (field.itemType === "media" || field.itemType === "file") addMedia(value);
      if (field.itemType === "object") {
        value.forEach((item) => {
          if (!isRecord(item)) return;
          field.children.forEach((child) => walk(child, item[child.key]));
        });
      }
    }
  };
  fields.forEach((field) => walk(field, state.metadata[field.key]));
  return Array.from(ids);
}

function SaveStateBadge({
  status,
  dirty,
  hasSavedDraft,
  hasPublished,
}: {
  status: string;
  dirty: boolean;
  hasSavedDraft: boolean;
  hasPublished: boolean;
}) {
  if (status === "saving") {
    return (
      <Badge variant="warning">
        <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" />
        Сохранение
      </Badge>
    );
  }
  if (status === "conflict") {
    return (
      <Badge variant="destructive">
        <AlertTriangle className="h-3 w-3" aria-hidden="true" />
        Конфликт версий
      </Badge>
    );
  }
  if (status === "error") {
    return (
      <Badge variant="destructive">
        <AlertTriangle className="h-3 w-3" aria-hidden="true" />
        Ошибка сохранения
      </Badge>
    );
  }
  if (dirty) {
    return (
      <Badge variant="warning">
        <CloudUpload className="h-3 w-3" aria-hidden="true" />
        Есть изменения
      </Badge>
    );
  }
  return hasSavedDraft ? (
    <Badge variant="success">
      <Check className="h-3 w-3" aria-hidden="true" />
      Сохранено
    </Badge>
  ) : hasPublished ? (
    <Badge variant="success">
      <Check className="h-3 w-3" aria-hidden="true" />
      Опубликовано
    </Badge>
  ) : (
    <Badge variant="muted">Черновик отсутствует</Badge>
  );
}

export default function ContentEditorPage() {
  const { feature } = useFeatureContext();
  const { principal } = useAuth();
  const canPromote = principal?.role === "admin" || principal?.role === "moderator";
  const { entryId } = useParams<{ entryId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const isNew = !entryId;
  const featureArchived = feature.status === "archived";

  useDocumentTitle(
    isNew ? `Новая запись — ${feature.name}` : `Запись — ${feature.name}`,
  );

  const schemaQuery = useQuery({
    queryKey: queryKeys.content.catalog,
    queryFn: ({ signal }) => api.siteBuilder.contentFeatures(signal),
  });
  const activeContentFeature = schemaQuery.data?.items.find((item) => item.id === feature.id);
  const entryQuery = useQuery({
    queryKey: queryKeys.entries.detail(entryId ?? ""),
    queryFn: ({ signal }) => api.entries.get(entryId!, signal),
    enabled: Boolean(entryId),
  });

  const schema = activeContentFeature?.schema ?? null;
  const fields = useMemo(() => parsedSchema(schema).fields, [schema]);
  const blocksKey = useMemo(() => blocksKeyFromFields(fields), [fields]);

  const {
    value: editorState,
    update: applyUpdate,
    replace: replaceState,
    undo,
    redo,
    canUndo,
    canRedo,
  } = useEditorHistory<DocumentState>({ metadata: {}, blocks: [] });

  const editorIdentity = `${feature.id}:${entryId ?? "new"}`;
  const [seededIdentity, setSeededIdentity] = useState<string | null>(null);
  const seeded = seededIdentity === editorIdentity;
  const guardBypassRef = useRef(false);
  const [preview, setPreview] = useState(false);
  const [previewWidth, setPreviewWidth] = useState<PreviewWidth>("desktop");
  const [historyOpen, setHistoryOpen] = useState(false);
  const [selectedVersion, setSelectedVersion] = useState<EntryVersion | null>(
    null,
  );
  const [blockWarnings, setBlockWarnings] = useState<string[]>([]);
  const importInput = useRef<HTMLInputElement>(null);
  const [importing, setImporting] = useState(false);
  const [promotionError, setPromotionError] = useState<ApiError | null>(null);

  const data = useMemo(
    () =>
      buildDocumentData(editorState.metadata, editorState.blocks, blocksKey),
    [editorState, blocksKey],
  );

  const autosave = useContentAutosave({
    featureId: feature.id,
    entryId: entryId ?? null,
    revision:
      entryQuery.data && entryQuery.data.id === entryId
        ? entryQuery.data.revision
        : 1,
    data,
    enabled: seeded && !featureArchived,
    onCreated: (entry) => {
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.list(feature.id),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
      // The dirty flag has not re-rendered yet, so let the guard through
      // explicitly; otherwise the user is stuck on /content/new with the
      // "unsaved changes" dialog right after the first autosave.
      guardBypassRef.current = true;
      navigate(`/features/${feature.id}/content/${entry.id}`, { replace: true });
      toast.success("Черновик создан");
    },
    onSaved: (entry) => {
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.list(feature.id),
      });
    },
  });

  const { markLoaded } = autosave;

  /**
   * Fills the editor with a document and resets the autosave baseline, so the
   * loaded state is never treated as a local change.
   */
  const applySeed = useCallback(
    (next: DocumentState, entryIdValue: string | null, revision: number) => {
      const baseline = JSON.stringify(
        buildDocumentData(next.metadata, next.blocks, blocksKey),
      );
      replaceState(next);
      markLoaded(baseline, entryIdValue, revision);
    },
    [blocksKey, markLoaded, replaceState],
  );

  const stateFromData = useCallback(
    (source: JsonObject | null): DocumentState => {
      if (!source) {
        return { metadata: starterMetadata(fields, blocksKey), blocks: [] };
      }
      const parsed = parseBlocks(
        source[blocksKey ?? ""] as JsonValue | undefined,
      );
      setBlockWarnings(parsed.warnings);
      return {
        metadata: documentMetadata(source, blocksKey),
        blocks: parsed.blocks,
      };
    },
    [blocksKey, fields],
  );

  const buildState = useCallback(
    (entry: EntryDetail | null | undefined): DocumentState =>
      stateFromData(
        entry
          ? entry.draft?.data ?? entry.dev?.data ?? entry.prod?.data ?? null
          : null,
      ),
    [stateFromData],
  );

  // Seed the editor once the schema and the entry are known. The baseline is
  // set at the same time, so the freshly loaded document is not "dirty".
  useEffect(() => {
    if (seeded) return;
    if (schemaQuery.isLoading) return;
    if (entryId) {
      if (entryQuery.isLoading) return;
      if (!entryQuery.data) return;
    }
    const entry = entryQuery.data;
    if (entryId && (!entry || entry.id !== entryId)) return;

    applySeed(
      buildState(entry),
      entry?.id ?? entryId ?? null,
      entry?.revision ?? 1,
    );
    setSeededIdentity(editorIdentity);
  }, [
    seeded,
    editorIdentity,
    schemaQuery.isLoading,
    entryId,
    entryQuery.isLoading,
    entryQuery.data,
    applySeed,
    buildState,
  ]);

  const dirty = autosave.dirty;
  const hasDraft = Boolean(entryQuery.data?.draft);
  const hasPublishableContent = hasDraft || dirty;
  const guard = useUnsavedChangesGuard(dirty, guardBypassRef);

  const updateMetadata = useCallback(
    (key: string, value: JsonValue) => {
      applyUpdate(
        (current) => ({
          ...current,
          metadata: { ...current.metadata, [key]: value },
        }),
        true,
      );
    },
    [applyUpdate],
  );


  const title = useMemo(
    () => deriveDocumentTitle(editorState.metadata, editorState.blocks, fields),
    [editorState, fields],
  );

  const titleField = useMemo(() => {
    // Keep the title card and the preview/list derivation in sync: both use
    // the schema-aware "first sensible string" pick from lib/json.
    const picked = entryTitleField(fields);
    if (!picked) return undefined;
    return fields.find(
      (field) => field.key === picked.key && field.type !== "blocks",
    );
  }, [fields]);
  const blockingIssues = useMemo(
    () => validateEntryFields(fields, editorState.metadata),
    [fields, editorState.metadata],
  );
  const preservedBlocks = editorState.blocks;
  const editableFields = useMemo(() => {
    const titleKey = titleField?.key ?? null;
    return fields.filter(
      (field) =>
        field.type !== "blocks" &&
        field.key !== blocksKey &&
        field.key !== titleKey &&
        field.key.trim(),
    );
  }, [fields, blocksKey, titleField]);



  const publishMutation = useMutation({
    mutationFn: async () => {
      const saved = await autosave.flush();
      if (!saved) {
        throw new Error(
          autosave.conflict
            ? "Сначала разрешите конфликт версий: черновик не сохранён."
            : "Не удалось сохранить черновик перед публикацией.",
        );
      }
      const currentId = autosave.getEntryId();
      if (!currentId) throw new Error("Черновик ещё не создан.");
      return api.entries.publish(currentId, autosave.getRevision());
    },
    onSuccess: (entry) => {
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      autosave.markPublished(entry.id, entry.revision);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.list(feature.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.versions(entry.id),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
      toast.success(
        entry.dev
          ? `Запись опубликована в dev: v${entry.dev.version}`
          : "Запись опубликована в dev",
      );
    },
    onError: (error) => {
      toast.error(errorMessage(error), {
        description:
          "Публичная версия не изменилась. Проверьте состояние черновика и повторите публикацию после исправления проблемы.",
      });
    },
  });

  const promoteMutation = useMutation({
    mutationFn: async () => {
      const currentId = autosave.getEntryId();
      if (!currentId) throw new Error("Черновик ещё не создан.");
      return api.entries.promote(currentId, autosave.getRevision());
    },
    onSuccess: (entry) => {
      setPromotionError(null);
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries.list(feature.id) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
      toast.success(entry.prod ? `Запись в prod: v${entry.prod.version}` : "Запись продвинута в prod");
    },
    onError: (error) => {
      if (error instanceof ApiError && error.details.length > 0) {
        setPromotionError(error);
        return;
      }
      toast.error(errorMessage(error));
    },
  });

  const handlePublish = async () => {
    if (!hasPublishableContent) {
      toast.info("Нет новых изменений для публикации", {
        description: "Измените содержимое, чтобы создать черновик для публикации.",
      });
      return;
    }
    if (blockingIssues.length > 0) {
      toast.error("Заполните обязательные поля перед публикацией", {
        description: blockingIssues
            .slice(0, 3)
            .map((issue) => `${issue.message} (${issue.path})`)
            .join(". "),
      });
      return;
    }
    const mediaIds = collectMediaIds(editorState, fields);
    if (mediaIds.length > 0) {
      const results = await Promise.allSettled(
        mediaIds.map((id) => api.media.get(id)),
      );
      const unavailable = results
        .map((result, index) => ({ result, id: mediaIds[index]! }))
        .filter(
          ({ result }) =>
            result.status === "rejected" ||
            (result.status === "fulfilled" && result.value.status !== "ready"),
        )
        .map(({ id }) => id);
      if (unavailable.length > 0) {
        toast.error("Связанные медиа недоступны для публикации", {
          description: `Не удалось подтвердить готовность: ${unavailable.join(", ")}`,
        });
        return;
      }
    }
    const confirmed = await confirm({
      title: "Опубликовать запись?",
      description: (
        <div className="space-y-2">
          <p>
            Публикация создаёт новую неизменяемую версию и переключает публичный
            API на неё. Версия сохранится в истории; последующие правки создадут
            новый черновик.
          </p>
          <p>
            Чтобы обновить страницы сайта, сохраните черновик и создайте снапшот в разделе «Сайт → Снапшоты».
          </p>
          {entryQuery.data?.prod && (
            <p>
              Текущая опубликованная версия: v{entryQuery.data.prod.version}.
            </p>
          )}
          {mediaIds.length > 0 && (
            <p>Проверено связанных медиа: {mediaIds.length}.</p>
          )}
        </div>
      ),
      confirmLabel: "Опубликовать",
    });
    if (!confirmed) return;
    publishMutation.mutate();
  };

  const handleSaveNow = async () => {
    const ok = await autosave.save(true);
    if (ok) {
      toast.success("Черновик сохранён");
    } else if (autosave.conflict) {
      toast.error("Конфликт версий", { description: autosave.conflict });
    } else {
      toast.error(errorMessage(autosave.error));
    }
  };

  const handleImportFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!file) return;
    setImporting(true);
    try {
      const parsed: unknown = JSON.parse(await file.text());
      if (Array.isArray(parsed)) {
        if (parsed.length === 0 || parsed.some((item) => !isRecord(item))) {
          throw new Error("Массив должен содержать хотя бы один объект записи.");
        }
        const result = await api.entries.import(feature.id, parsed as JsonObject[]);
        await queryClient.invalidateQueries({ queryKey: queryKeys.entries.list(feature.id) });
        toast.success(`Создано черновиков: ${result.imported}`);
        navigate(`/features/${feature.id}/content`, { replace: true });
        return;
      }
      if (!isRecord(parsed)) throw new Error("Файл должен содержать JSON-объект или массив объектов.");
      const result = await api.entries.import(
        feature.id,
        parsed as JsonObject,
        entryId,
        entryId ? autosave.getRevision() : undefined,
      );
      const imported = result.items[0];
      if (!imported) throw new Error("Сервер не вернул импортированную запись.");
      queryClient.setQueryData(queryKeys.entries.detail(imported.id), imported);
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries.list(feature.id) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.overview });
      guardBypassRef.current = true;
      if (entryId) {
        applySeed(stateFromData(imported.draft?.data ?? null), imported.id, imported.revision);
      } else {
        navigate(`/features/${feature.id}/content/${imported.id}`, { replace: true });
      }
      toast.success("JSON проверен и загружен в черновик");
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setImporting(false);
    }
  };

  /**
   * Conflict resolution. Both options first read the current server state:
   * "reload" discards the local changes, "keep mine" re-sends the local
   * document on top of the fresh server revision.
   */
  const handleReloadFromServer = async () => {
    if (!entryId) {
      toast.info("Черновик ещё не создан — серверных изменений нет.");
      return;
    }
    const result = await entryQuery.refetch();
    const latest = result.data;
    if (!latest) {
      toast.error("Не удалось загрузить актуальное состояние записи.");
      return;
    }
    applySeed(buildState(latest), latest.id, latest.revision);
    autosave.clearConflict();
    toast.success("Документ загружен из сервера");
  };

  const handleKeepLocal = async () => {
    if (!entryId) {
      autosave.clearConflict();
      await autosave.save(true);
      return;
    }
    const result = await entryQuery.refetch();
    const latest = result.data;
    if (!latest) {
      toast.error("Не удалось загрузить актуальное состояние записи.");
      return;
    }
    autosave.clearConflict();
    autosave.adoptServerRevision(latest.revision);
    const ok = await autosave.save(true);
    if (ok) {
      toast.success("Локальная версия сохранена поверх серверной");
    } else {
      toast.error(errorMessage(autosave.error));
    }
  };

  const versionsQuery = useQuery({
    queryKey: queryKeys.entries.versions(entryId ?? ""),
    queryFn: ({ signal }) => api.entries.versions(entryId!, signal),
    enabled: historyOpen && Boolean(entryId),
  });

  const discardDraftMutation = useMutation({
    mutationFn: () => api.entries.discardDraft(entryId!),
    onSuccess: (entry) => {
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.versions(entry.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.list(feature.id),
      });
      applySeed(buildState(entry), entry.id, entry.revision);
      toast.success("Черновик отменён. Загружена опубликованная версия.");
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const rollbackMutation = useMutation({
    mutationFn: (version: number) => api.entries.rollback(entryId!, version, "prod"),
    onSuccess: (entry) => {
      queryClient.setQueryData(queryKeys.entries.detail(entry.id), entry);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.versions(entry.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.entries.list(feature.id),
      });
      toast.success(
        `Опубликованная версия переключена на v${entry.prod?.version ?? "—"}.`,
      );
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const handleDiscardDraft = async () => {
    const confirmed = await confirm({
      title: "Отменить черновик?",
      description:
        "Несохранённый черновик будет отменён, редактор вернётся к опубликованной версии. Сама опубликованная версия останется без изменений, история версий сохранится.",
      confirmLabel: "Отменить черновик",
      destructive: true,
    });
    if (confirmed) discardDraftMutation.mutate();
  };

  const handleRestoreVersion = async (version: EntryVersion) => {
    const confirmed = await confirm({
      title: `Восстановить v${version.version} как черновик?`,
      description:
        "Текущий черновик будет заменён содержимым выбранной версии. Изменения сохранятся автоматически. Опубликованная версия не изменится.",
      confirmLabel: "Восстановить",
    });
    if (!confirmed) return;
    replaceState(stateFromData(version.data));
    setHistoryOpen(false);
    setSelectedVersion(null);
    toast.success(`Версия v${version.version} загружена в черновик`);
  };

  const publishedData = entryQuery.data?.prod?.data ?? null;
  const diffEntries = useMemo(() => {
    if (!selectedVersion) return [];
    return diffJson(publishedData, selectedVersion.data);
  }, [publishedData, selectedVersion]);

  if (schemaQuery.isLoading || (entryId && entryQuery.isLoading)) {
    return <LoadingPage label="Загружаем редактор…" />;
  }

  if (schemaQuery.error) {
    return (
      <ErrorState
        error={schemaQuery.error}
        title="Не удалось загрузить схему раздела"
        onRetry={() => void schemaQuery.refetch()}
      />
    );
  }

  if (!activeContentFeature) {
    return (
      <EmptyState
        title="Фича не входит в активный dev-снапшот"
        description="Её содержимое можно редактировать после добавления в dev-снапшот."
        action={<Button variant="outline" asChild><Link to="/content">К контенту</Link></Button>}
      />
    );
  }

  if (entryId && entryQuery.error) {
    const notFound =
      entryQuery.error instanceof ApiError && entryQuery.error.status === 404;
    return (
      <div className="space-y-4">
        <ErrorState
          error={entryQuery.error}
          title={notFound ? "Запись не найдена" : "Не удалось загрузить запись"}
          onRetry={() => void entryQuery.refetch()}
        />
        <Button variant="outline" asChild>
          <Link to={`/features/${feature.id}/content`}>
            <ArrowLeft />
            К списку записей
          </Link>
        </Button>
      </div>
    );
  }

  const entry = entryQuery.data;
  const archived = Boolean(entry?.archived_at) || featureArchived;
  const hasPublished = Boolean(entry?.prod);
  // A draft newer than the published dev version is not part of dev yet, so
  // promoting would silently push stale content to prod.
  const pendingDraft = Boolean(
    entry?.draft && (!entry.dev || entry.draft.version > entry.dev.version),
  );

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-border bg-card">
        <div className="flex flex-col gap-3 border-b border-border p-3 sm:p-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Link
                to={`/features/${feature.id}/content`}
                className="inline-flex items-center gap-1 hover:text-foreground"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                {feature.name}
              </Link>
              <span aria-hidden="true">/</span>
              <span className="truncate">{isNew ? "Новая запись" : title}</span>
            </div>
            <div className="mt-1.5 flex flex-wrap items-center gap-2">
              <h1 className="truncate text-base font-semibold">
                {isNew ? "Новая запись" : title}
              </h1>
              <SaveStateBadge
                status={autosave.status}
                dirty={dirty}
                hasSavedDraft={hasDraft}
                hasPublished={Boolean(entry?.dev || entry?.prod)}
              />
              {entry?.dev && (
                <Badge variant="secondary">dev v{entry.dev.version}</Badge>
              )}
              {hasPublished && entry?.prod && (
                <Badge variant="success">
                  prod v{entry.prod.version}
                </Badge>
              )}
              {hasDraft && entry?.draft && (
                <Badge variant="warning">Черновик v{entry.draft.version}</Badge>
              )}
              {entry?.archived_at && <Badge variant="muted">В архиве</Badge>}
            </div>
            <p className="mt-1 text-2xs text-muted-foreground">
              {dirty
                ? "Есть несохранённые изменения"
                : hasDraft
                  ? autosave.lastSavedAt
                    ? `Черновик сохранён ${formatDateTime(
                        autosave.lastSavedAt.toISOString(),
                      )}`
                    : "Черновик сохранён на сервере"
                  : entry?.dev
                    ? `Текущая версия опубликована в dev: v${entry.dev.version}`
                    : "Черновик ещё не сохранялся на сервере"}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={importInput}
              type="file"
              accept="application/json,.json"
              className="sr-only"
              aria-label="Импортировать контент из JSON"
              onChange={(event) => void handleImportFile(event)}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => importInput.current?.click()}
              loading={importing}
              disabled={archived || importing}
            >
              <FileUp />
              Импорт JSON
            </Button>
            <Button
              variant={preview ? "secondary" : "outline"}
              size="sm"
              onClick={() => setPreview((current) => !current)}
            >
              {preview ? <X /> : <Eye />}
              {preview ? "Закрыть предпросмотр" : "Предпросмотр"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => void handleSaveNow()}
              loading={autosave.status === "saving"}
              disabled={archived}
            >
              <Save />
              Сохранить сейчас
            </Button>
            <Button
              size="sm"
              onClick={() => void handlePublish()}
              loading={publishMutation.isPending}
              disabled={
                archived ||
                blockingIssues.length > 0 ||
                !hasPublishableContent ||
                publishMutation.isPending
              }
              title={
                blockingIssues.length > 0
                  ? "Исправьте ошибки формы, чтобы опубликовать"
                  : !hasPublishableContent
                    ? "Сначала измените содержимое, чтобы создать черновик"
                    : undefined
              }
            >
              <Send />
              {blockingIssues.length > 0
                ? `Опубликовать (${blockingIssues.length})`
                : hasPublishableContent
                  ? "Опубликовать"
                  : "Нет изменений"}
            </Button>
            {canPromote && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => promoteMutation.mutate()}
                loading={promoteMutation.isPending}
                disabled={archived || !entry?.dev || pendingDraft || dirty}
                title={
                  !entry?.dev
                    ? "Сначала опубликуйте запись в dev"
                    : dirty || pendingDraft
                      ? "Сначала опубликуйте черновик в dev — иначе в prod уйдёт предыдущая версия"
                      : "Продвинуть текущую dev-версию в prod"
                }
              >
                В prod
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Дополнительные действия"
                >
                  <RefreshCw />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuItem
                  onSelect={() => setHistoryOpen(true)}
                  disabled={!entryId}
                >
                  <History className="h-4 w-4" />
                  История версий
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => undo()}
                  disabled={!canUndo}
                >
                  <Undo2 className="h-4 w-4" />
                  Отменить изменение
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => redo()}
                  disabled={!canRedo}
                >
                  <Redo2 className="h-4 w-4" />
                  Повторить изменение
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => void handleDiscardDraft()}
                  disabled={!hasDraft || !hasPublished || archived}
                >
                  <FileClock className="h-4 w-4" />
                  Отменить черновик
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link to={`/features/${feature.id}/content`}>
                    <FileClock className="h-4 w-4" />
                    К списку записей
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>

        {feature.type === "form" && (
          <div className="border-b border-border p-3 sm:p-4">
            <InlineAlert tone="info" title="Редактируются тексты формы">
              Схема отправки задаётся сборкой. Заявки посетителей находятся в разделе «Заявки».
            </InlineAlert>
          </div>
        )}

        {promotionError && (
          <div className="border-b border-border p-3 sm:p-4">
            <InlineAlert tone="danger" title="Контент не подходит для активной prod-схемы">
              <p>{promotionError.message}</p>
              <ul className="mt-2 list-disc space-y-1 pl-5">
                {promotionError.details.map((detail, index) => (
                  <li key={`${detail.path ?? "field"}-${index}`}>
                    {detail.path && <code className="font-medium">{detail.path}: </code>}
                    {detail.message}
                  </li>
                ))}
              </ul>
            </InlineAlert>
          </div>
        )}

        {autosave.conflict && (
          <div className="border-b border-border p-3 sm:p-4">
            <InlineAlert
              tone="danger"
              title="Конфликт версий"
              actions={
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void handleReloadFromServer()}
                  >
                    <RefreshCw />
                    Обновить из сервера
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void handleKeepLocal()}
                  >
                    <Save />
                    Оставить мой вариант
                  </Button>
                </>
              }
            >
              {autosave.conflict} Локальные изменения остались в редакторе и не были
              отправлены повторно, пока вы не выберете один из вариантов.
            </InlineAlert>
          </div>
        )}

        {autosave.status === "error" && Boolean(autosave.error) && (
          <div className="border-b border-border p-3 sm:p-4">
            <InlineAlert
              tone="danger"
              title="Не удалось сохранить черновик"
              actions={
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void handleSaveNow()}
                >
                  <RefreshCw />
                  Повторить
                </Button>
              }
            >
              {errorMessage(autosave.error)} Изменения остались в редакторе.
            </InlineAlert>
          </div>
        )}

        {archived && (
          <div className="border-b border-border p-3 sm:p-4">
            <InlineAlert tone="warning" title="Редактирование заблокировано">
              {entry?.archived_at
                ? "Запись в архиве. Снимите её с архива, чтобы продолжить редактирование."
                : "Раздел в архиве. Снимите его с архива в настройках, чтобы продолжить работу."}
            </InlineAlert>
          </div>
        )}
      </div>

      {preview ? (
        <div className="rounded-lg border border-border bg-card p-3 sm:p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="warning">Предпросмотр черновика</Badge>
              <p className="text-xs text-muted-foreground">
                Это не опубликованная версия. Публичный API отдаёт только явно
                опубликованные данные.
              </p>
            </div>
            <div className="flex items-center gap-1 rounded-md border border-border p-1">
              {widthOptions.map((option) => {
                const Icon = option.icon;
                return (
                  <button
                    key={option.value}
                    type="button"
                    onClick={() => setPreviewWidth(option.value)}
                    aria-pressed={previewWidth === option.value}
                    className={cn(
                      "flex h-8 w-9 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                      previewWidth === option.value && "bg-accent text-foreground",
                    )}
                    title={option.label}
                  >
                    <Icon className="h-4 w-4" />
                    <span className="sr-only">{option.label}</span>
                  </button>
                );
              })}
            </div>
          </div>
          <div className="rounded-md border border-border bg-background p-3 sm:p-6">
            <ContentPreview
              metadata={editorState.metadata}
              blocks={editorState.blocks}
              fields={fields}
              width={previewWidth}
            />
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <div className="rounded-lg border border-border bg-card p-3 sm:p-4">
            <Label htmlFor="entry-title" className="text-xs text-muted-foreground">
              Название записи
              {titleField?.required && <span className="ml-0.5 text-destructive">*</span>}
            </Label>
            {titleField &&
              blockingIssues.some((issue) => issue.path === titleField.key) && (
                <p className="mt-1.5 text-xs text-destructive">
                  {blockingIssues.find((issue) => issue.path === titleField.key)
                    ?.message}
                </p>
              )}
            {titleField ? (
              <Input
                id="entry-title"
                className="mt-1.5 h-auto py-2 text-lg font-semibold"
                value={
                  typeof editorState.metadata[titleField.key] === "string"
                    ? (editorState.metadata[titleField.key] as string)
                    : ""
                }
                placeholder={titleField.placeholder || "Без названия"}
                disabled={archived}
                maxLength={titleField.maxLength}
                onChange={(event) =>
                  updateMetadata(titleField.key, event.target.value)
                }
              />
            ) : (
              <p className="mt-1.5 text-sm text-muted-foreground">
                В схеме нет строкового поля для названия.
              </p>
            )}
          </div>

          {blocksKey && (
            <div className="rounded-lg border border-border bg-card p-3 sm:p-4">
              <h2 className="text-sm font-semibold">Блочные данные (legacy)</h2>
              {blockWarnings.length > 0 && (
                <div className="mt-2">
                  <InlineAlert tone="warning" title="Часть блочных данных не загружена">
                    <ul className="list-disc pl-4">
                      {blockWarnings.map((warning) => (
                        <li key={warning}>{warning}</li>
                      ))}
                    </ul>
                  </InlineAlert>
                </div>
              )}
              <p className="mt-2 text-xs leading-5 text-muted-foreground">
                Тип поля «Блоки» удалён из редактора. Сохранённые блочные данные
                ({preservedBlocks.length} блок(ов)) не изменяются и не удаляются:
                при сохранении черновика они переносятся как есть.
              </p>
            </div>
          )}

          {blockingIssues.length > 0 && (
            <div className="rounded-lg border border-destructive/50 bg-destructive/5 p-3 sm:p-4">
              <h2 className="text-sm font-semibold text-destructive">
                Исправьте ошибки перед публикацией
              </h2>
              <ul className="mt-2 space-y-1 text-xs text-muted-foreground">
                {blockingIssues.slice(0, 8).map((issue) => (
                  <li key={issue.path + issue.message} className="flex gap-1.5">
                    <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" />
                    {issue.message}
                    <code className="text-2xs">{issue.path}</code>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <EntryFieldsForm
            fields={editableFields}
            metadata={editorState.metadata}
            onChange={updateMetadata}
            disabled={archived}
            issues={blockingIssues}
            featureKey={activeContentFeature.key}
            buildDigest={activeContentFeature.buildDigest}
            schemaKind="content"
          />
        </div>
      )}

      <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>История версий</DialogTitle>
            <DialogDescription>
              Опубликованные версии неизменяемы. Откат переключает публичную версию
              на ранее опубликованную.
            </DialogDescription>
          </DialogHeader>

          {versionsQuery.isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          ) : versionsQuery.error ? (
            <ErrorState
              error={versionsQuery.error}
              title="Не удалось загрузить историю"
              onRetry={() => void versionsQuery.refetch()}
            />
          ) : (versionsQuery.data?.items.length ?? 0) === 0 ? (
            <EmptyState
              title="История пуста"
              description="Версии появятся после первого сохранения черновика."
            />
          ) : (
            <div className="space-y-4">
              <ul className="divide-y divide-border rounded-md border border-border">
                {versionsQuery.data?.items.map((version) => (
                  <li
                    key={version.id}
                    className="flex flex-wrap items-center justify-between gap-2 px-3 py-2.5"
                  >
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium">v{version.version}</span>
                        <VersionStatusBadge status={version.status} />
                        <span className="text-2xs text-muted-foreground">
                          {formatDateTime(version.created_at)}
                        </span>
                      </div>
                      <p className="mt-0.5 truncate text-2xs text-muted-foreground">
                        {truncate(JSON.stringify(version.data), 120)}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setSelectedVersion(version)}
                      >
                        Сравнить
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => void handleRestoreVersion(version)}
                      >
                        Как черновик
                      </Button>
                      {canPromote && version.status === "published" && (
                        <Button
                          size="sm"
                          variant="ghost"
                          loading={rollbackMutation.isPending}
                          onClick={() => rollbackMutation.mutate(version.version)}
                        >
                          Опубликовать снова
                        </Button>
                      )}
                    </div>
                  </li>
                ))}
              </ul>

              {selectedVersion && (
                <div className="space-y-2">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-semibold">
                      Сравнение: опубликованная ↔ v{selectedVersion.version}
                    </h3>
                    <DiffSummaryBadges entries={diffEntries} />
                  </div>
                  <DiffList entries={diffEntries} />
                </div>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>

      {guard.dialog}
    </div>
  );
}
