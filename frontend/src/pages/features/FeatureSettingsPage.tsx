import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, CloudUpload, Loader2, Save, Send } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/api";
import { useAuth } from "@/app/auth";
import type { EntryDetail, JsonObject, JsonValue } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { errorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { useFeatureContext } from "@/pages/features/FeatureLayout";
import { useUnsavedChangesGuard, ErrorState, LoadingPage, useDocumentTitle } from "@/components/common/page";
import { InlineAlert } from "@/components/common/page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EntryFieldsForm, validateEntryFields } from "@/domains/content-editor/EntryForm";
import { useContentAutosave } from "@/domains/content-editor/useContentAutosave";
import { parsedSchema } from "@/domains/content-schema/schema";
import { starterMetadata } from "@/domains/content-editor/blocks";

function currentData(entry: EntryDetail | null): JsonObject | null {
  return entry?.draft?.data ?? entry?.dev?.data ?? entry?.prod?.data ?? null;
}

export default function FeatureSettingsPage() {
  const { feature } = useFeatureContext();
  const { principal } = useAuth();
  const canPromote = principal?.role === "admin" || principal?.role === "moderator";
  const queryClient = useQueryClient();
  const settingsKey = queryKeys.content.settings(feature.id);
  const [metadata, setMetadata] = useState<JsonObject>({});
  const [initialized, setInitialized] = useState(false);
  const [promotionError, setPromotionError] = useState<ApiError | null>(null);
  const initializedRef = useRef(false);
  const guardBypassRef = useRef(false);

  useDocumentTitle(`Тексты страницы — ${feature.name}`);

  const catalogQuery = useQuery({
    queryKey: queryKeys.content.catalog,
    queryFn: ({ signal }) => api.siteBuilder.contentFeatures(signal),
  });
  const activeFeature = catalogQuery.data?.items.find((item) => item.id === feature.id);
  const schema = activeFeature?.settingsSchema ?? null;
  const fields = useMemo(() => parsedSchema(schema).fields.filter((field) => field.type !== "blocks"), [schema]);
  const settingsQuery = useQuery({
    queryKey: settingsKey,
    queryFn: async ({ signal }) => {
      try {
        return await api.entries.getSettings(feature.id, signal);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    enabled: Boolean(activeFeature?.settingsSchema),
  });
  const entry = settingsQuery.data ?? null;
  const data = useMemo(() => metadata, [metadata]);

  const autosave = useContentAutosave({
    featureId: feature.id,
    entryId: entry?.id ?? null,
    revision: entry?.revision ?? 1,
    data,
    enabled: initialized && feature.status !== "archived",
    createEntry: (featureId, payload) => api.entries.createSettings(featureId, payload),
    onCreated: (created) => {
      queryClient.setQueryData(settingsKey, created);
      void queryClient.invalidateQueries({ queryKey: queryKeys.entries.list(feature.id) });
      guardBypassRef.current = true;
      toast.success("Черновик настроек создан");
    },
    onSaved: (saved) => {
      queryClient.setQueryData(settingsKey, saved);
    },
  });
  const markLoaded = autosave.markLoaded;

  useEffect(() => {
    if (initializedRef.current || catalogQuery.isLoading || settingsQuery.isLoading) return;
    if (!schema) return;
    const initial = currentData(settingsQuery.data ?? null) ?? starterMetadata(fields, null);
    const baseline = JSON.stringify(initial);
    setMetadata(initial);
    markLoaded(baseline, settingsQuery.data?.id ?? null, settingsQuery.data?.revision ?? 1);
    initializedRef.current = true;
    setInitialized(true);
  }, [markLoaded, catalogQuery.isLoading, fields, schema, settingsQuery.data, settingsQuery.isLoading]);

  const dirty = autosave.dirty;
  useUnsavedChangesGuard(dirty, guardBypassRef);
  const issues = useMemo(() => validateEntryFields(fields, metadata), [fields, metadata]);

  const publishMutation = useMutation({
    mutationFn: async () => {
      if (!(await autosave.flush())) throw new Error("Не удалось сохранить черновик настроек перед публикацией.");
      const id = autosave.getEntryId();
      if (!id) throw new Error("Сначала измените и сохраните настройки.");
      return api.entries.publish(id, autosave.getRevision());
    },
    onSuccess: (saved) => {
      queryClient.setQueryData(settingsKey, saved);
      autosave.markPublished(saved.id, saved.revision);
      toast.success("Тексты страницы опубликованы в dev");
    },
    onError: (error) => toast.error(errorMessage(error)),
  });
  const promoteMutation = useMutation({
    mutationFn: async () => {
      if (dirty) throw new Error("Сначала сохраните и опубликуйте изменения в dev.");
      if (!entry?.dev) throw new Error("Сначала опубликуйте настройки в dev.");
      return api.entries.promote(entry.id, entry.revision);
    },
    onSuccess: (saved) => {
      setPromotionError(null);
      queryClient.setQueryData(settingsKey, saved);
      toast.success("Тексты страницы продвинуты в prod");
    },
    onError: (error) => {
      if (error instanceof ApiError && error.details.length > 0) {
        setPromotionError(error);
        return;
      }
      toast.error(errorMessage(error));
    },
  });

  const setValue = useCallback((key: string, value: JsonValue) => {
    setMetadata((current) => ({ ...current, [key]: value }));
  }, []);
  const saveNow = async () => {
    const saved = await autosave.save(true);
    if (saved) toast.success("Черновик настроек сохранён");
    else toast.error(errorMessage(autosave.error));
  };

  if (catalogQuery.isLoading || settingsQuery.isLoading) return <LoadingPage label="Загружаем настройки контента…" />;
  if (catalogQuery.error) return <ErrorState error={catalogQuery.error} title="Не удалось загрузить схему настроек" onRetry={() => void catalogQuery.refetch()} />;
  if (settingsQuery.error) return <ErrorState error={settingsQuery.error} title="Не удалось загрузить настройки страницы" onRetry={() => void settingsQuery.refetch()} />;
  if (!activeFeature?.settingsSchema) {
    return <ErrorState error={new Error("Сборка не объявляет схему общих настроек")} title="У этой фичи нет общих настроек" />;
  }

  const hasDraft = Boolean(entry?.draft || dirty);
  const pendingDraft = Boolean(entry?.draft && (!entry.dev || entry.draft.version > entry.dev.version));

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-border bg-card p-4">
        <Link to={`/features/${feature.id}/content`} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-3.5 w-3.5" />
          К товарам коллекции
        </Link>
        <div className="mt-3 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-lg font-semibold">Тексты страницы «{feature.name}»</h1>
            <p className="mt-1 text-sm text-muted-foreground">Общие тексты и настройки коллекции. Они публикуются независимо от снапшота сайта.</p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              {autosave.status === "saving" ? <Badge variant="info"><Loader2 className="h-3 w-3 animate-spin" />Сохранение</Badge> : dirty ? <Badge variant="warning"><CloudUpload className="h-3 w-3" />Есть изменения</Badge> : hasDraft ? <Badge variant="success"><Check className="h-3 w-3" />Сохранено</Badge> : entry?.dev ? <Badge variant="success"><Check className="h-3 w-3" />Опубликовано</Badge> : <Badge variant="muted">Черновик отсутствует</Badge>}
              {entry?.dev && <Badge variant="secondary">dev v{entry.dev.version}</Badge>}
              {entry?.prod && <Badge variant="success">prod v{entry.prod.version}</Badge>}
            </div>
            {autosave.lastSavedAt && <p className="mt-2 text-2xs text-muted-foreground">Сохранено {formatDateTime(autosave.lastSavedAt.toISOString())}</p>}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => void saveNow()} disabled={!dirty || feature.status === "archived"} loading={autosave.status === "saving"}><Save />Сохранить</Button>
            <Button onClick={() => publishMutation.mutate()} disabled={feature.status === "archived" || issues.length > 0 || (!dirty && !entry?.draft)} loading={publishMutation.isPending}><Send />Опубликовать в dev</Button>
            {canPromote && <Button variant="outline" onClick={() => promoteMutation.mutate()} disabled={feature.status === "archived" || !entry?.dev || pendingDraft || dirty} loading={promoteMutation.isPending}>Продвинуть в prod</Button>}
          </div>
        </div>
      </div>

      {autosave.status === "error" && Boolean(autosave.error) && <InlineAlert tone="danger" title="Не удалось сохранить настройки">{errorMessage(autosave.error)}</InlineAlert>}
      {autosave.conflict && <InlineAlert tone="danger" title="Конфликт версий">{autosave.conflict} Обновите страницу и повторите изменения.</InlineAlert>}
      {issues.length > 0 && <InlineAlert tone="warning" title="Заполните обязательные поля перед публикацией">{issues.slice(0, 5).map((issue) => issue.message).join(". ")}</InlineAlert>}
      {promotionError && (
        <InlineAlert tone="danger" title="Настройки не подходят для активной prod-схемы">
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
      )}
      <div className="rounded-lg border border-border bg-card p-4">
        <EntryFieldsForm
          fields={fields}
          metadata={metadata}
          onChange={setValue}
          disabled={feature.status === "archived"}
          issues={issues}
          featureKey={activeFeature.key}
          buildDigest={activeFeature.buildDigest}
          schemaKind="settings"
        />
      </div>
    </div>
  );
}
