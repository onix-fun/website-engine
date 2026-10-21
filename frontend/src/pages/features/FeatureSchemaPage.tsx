import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Eye,
  GitCompare,
  History,
  RefreshCw,
  RotateCcw,
  Save,
  Send,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/api";
import type {
  FeatureDetail,
  FeatureDraftState,
  FeatureVersion,
  JsonObject,
} from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { errorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { prettyJson } from "@/lib/json";
import { useFeatureContext } from "@/pages/features/FeatureLayout";
import { useAuth } from "@/app/auth";
import { useConfirm } from "@/components/common/confirm";
import {
  CopyButton,
  ErrorState,
  InlineAlert,
  LoadingPage,
  useDocumentTitle,
  useUnsavedChangesGuard,
} from "@/components/common/page";
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
import { SchemaBuilder } from "@/domains/schema-builder/SchemaBuilder";
import { createField, parsedSchema, schemaFromFields, validateFields } from "@/domains/schema-builder/schema";
import type { SchemaFieldNode } from "@/domains/schema-builder/types";
import { diffJson } from "@/domains/versioning/diff";
import { DiffList, DiffSummaryBadges } from "@/domains/versioning/DiffView";
import { VersionStatusBadge } from "@/components/common/status-badges";
import { cn } from "@/lib/utils";

function starterFields(type: FeatureDetail["type"]): SchemaFieldNode[] {
  if (type === "form") {
    return [
      createField("string", {
        key: "name",
        label: "Имя",
        required: true,
        minLength: 1,
        maxLength: 120,
      }),
      createField("string", { key: "email", label: "Email", required: true }),
      createField("text", {
        key: "message",
        label: "Сообщение",
        required: true,
        maxLength: 4000,
      }),
    ];
  }
  return [
    createField("string", {
      key: "title",
      label: "Заголовок",
      required: true,
      minLength: 1,
    }),
    createField("text", { key: "description", label: "Описание" }),
    createField("blocks", { key: "blocks", label: "Содержание" }),
  ];
}

function versionLabel(version: FeatureVersion): string {
  return `v${version.version}`;
}

function flattenSchemaPaths(
  nodes: SchemaFieldNode[],
  prefix = "",
): { node: SchemaFieldNode; path: string }[] {
  return nodes.flatMap((node) => {
    const path = prefix ? `${prefix}.${node.key}` : node.key;
    return [{ node, path }, ...flattenSchemaPaths(node.children, path)];
  });
}

function compatibilityWarnings(
  fields: SchemaFieldNode[],
  published: FeatureVersion | null | undefined,
): string[] {
  if (!published) return [];
  const previous = parsedSchema(published.schema).fields;
  const oldMap = new Map(flattenSchemaPaths(previous).map(({ node, path }) => [path, node]));
  const newMap = new Map(flattenSchemaPaths(fields).map(({ node, path }) => [path, node]));
  const warnings: string[] = [];
  oldMap.forEach((oldNode, path) => {
    const next = newMap.get(path);
    if (!next) {
      warnings.push(`Поле «${path}» удалено из схемы`);
    } else if (next.type !== oldNode.type) {
      warnings.push(`Тип поля «${path}» изменён: ${oldNode.type} → ${next.type}`);
    } else if (!oldNode.required && next.required) {
      warnings.push(`Поле «${path}» стало обязательным`);
    }
  });
  newMap.forEach((_node, path) => {
    if (!oldMap.has(path)) {
      warnings.push(`Добавлено поле «${path}»`);
    }
  });
  return warnings;
}

interface CompareState {
  title: string;
  before: unknown;
  after: unknown;
  beforeLabel: string;
  afterLabel: string;
}

export default function FeatureSchemaPage() {
  const { feature } = useFeatureContext();
  useDocumentTitle(`${feature.name} — схема`);
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const draftKey = queryKeys.features.draft(feature.id);
  const draftQuery = useQuery({
    queryKey: draftKey,
    queryFn: ({ signal }) => api.features.draft(feature.id, signal),
  });
  const { principal } = useAuth();
  const canEdit = principal?.role !== "user";
  const [versionsOpen, setVersionsOpen] = useState(false);
  const versionsQuery = useQuery({
    queryKey: queryKeys.features.versions(feature.id),
    queryFn: ({ signal }) => api.features.versions(feature.id, signal),
    enabled: versionsOpen,
  });

  const [fields, setFields] = useState<SchemaFieldNode[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [seeded, setSeeded] = useState(false);
  const [baseline, setBaseline] = useState("");
  const [parseWarnings, setParseWarnings] = useState<string[]>([]);
  const [actionError, setActionError] = useState<unknown>(null);
  const [conflict, setConflict] = useState<string | null>(null);
  const [remoteSchema, setRemoteSchema] = useState<JsonObject | null>(null);
  const [jsonOpen, setJsonOpen] = useState(false);
  const [issuesOpen, setIssuesOpen] = useState(false);
  const [compare, setCompare] = useState<CompareState | null>(null);
  const [versionView, setVersionView] = useState<FeatureVersion | null>(null);
  const archived = feature.status === "archived";

  useEffect(() => {
    if (seeded || !draftQuery.data) return;
    const serverSchema =
      draftQuery.data.draft?.schema ?? draftQuery.data.prod?.schema ?? null;
    const { fields: parsedFields, warnings } = serverSchema
      ? parsedSchema(serverSchema)
      : { fields: starterFields(feature.type), warnings: [] };
    const initial = serverSchema ? parsedFields : starterFields(feature.type);
    setFields(initial);
    setParseWarnings(warnings);
    setBaseline(JSON.stringify(schemaFromFields(initial, feature.name)));
    setSelectedId(initial[0]?.id ?? null);
    setSeeded(true);
  }, [draftQuery.data, seeded, feature.type, feature.name]);

  const generated = useMemo(
    () => schemaFromFields(fields, feature.name),
    [fields, feature.name],
  );
  const generatedText = useMemo(() => JSON.stringify(generated), [generated]);
  const dirty = seeded && generatedText !== baseline;
  const issues = useMemo(() => validateFields(fields), [fields]);
  const errors = issues.filter((issue) => issue.severity === "error");
  const warnings = issues.filter((issue) => issue.severity === "warning");
  const revision = draftQuery.data?.revision ?? feature.revision;
  const hasDraft = Boolean(draftQuery.data?.draft);
  const currentPublished = draftQuery.data?.prod ?? feature.prod;
  const compatibility = useMemo(
    () => compatibilityWarnings(fields, currentPublished),
    [fields, currentPublished],
  );
  const guard = useUnsavedChangesGuard(dirty);

  const saveMutation = useMutation({
    mutationFn: (schema: JsonObject) =>
      api.features.saveDraft(feature.id, revision, schema),
  });
  const publishMutation = useMutation({
    mutationFn: () => api.features.publish(feature.id, revision),
  });
  const rollbackMutation = useMutation({
    mutationFn: (version: number) => api.features.rollback(feature.id, version, "prod"),
  });
  const promoteMutation = useMutation({
    mutationFn: () => api.features.promote(feature.id, revision),
    onSuccess: (detail) => {
      queryClient.setQueryData<FeatureDraftState>(draftKey, {
        revision: detail.revision,
        draft: detail.draft,
        dev: detail.dev,
        prod: detail.prod,
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.versions(feature.id) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.detail(feature.id) });
      toast.success(detail.prod ? `Схема в prod: v${detail.prod.version}` : "Схема продвинута в prod");
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const handleConflict = async (error: unknown) => {
    if (error instanceof ApiError && error.status === 409) {
      setActionError(null);
      setConflict(error.message);
      toast.error(error.message, { description: "Ревизия изменилась. Локальные изменения сохранены в конструкторе." });
      try {
        const fresh = await api.features.draft(feature.id);
        setRemoteSchema(fresh.draft?.schema ?? fresh.prod?.schema ?? null);
      } catch {
        setRemoteSchema(null);
      }
      return true;
    }
    setActionError(error);
    return false;
  };

  const handleSave = async () => {
    if (errors.length > 0) {
      setIssuesOpen(true);
      return;
    }
    setActionError(null);
    try {
      const saved = await saveMutation.mutateAsync(generated);
      queryClient.setQueryData<FeatureDraftState>(draftKey, saved);
      setBaseline(generatedText);
      setConflict(null);
      setRemoteSchema(null);
      setParseWarnings([]);
      toast.success("Черновик схемы сохранён");
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.versions(feature.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.all,
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
    } catch (error) {
      await handleConflict(error);
    }
  };

  const publish = async () => {
    if (errors.length > 0) {
      setIssuesOpen(true);
      return;
    }
    if (dirty) {
      toast.error("Сначала сохраните черновик схемы");
      return;
    }
    if (!hasDraft) {
      toast.error("Нет черновика для публикации");
      return;
    }
    const detailLines = [
      "Публикация делает схему доступной публичному API. Черновик и записи при этом не публикуются автоматически.",
      ...compatibility.slice(0, 6),
      ...warnings.slice(0, 4).map((issue) => issue.message),
    ];
    const confirmed = await confirm({
      title: "Опубликовать схему?",
      description: (
        <div className="space-y-2">
          {detailLines.map((line, index) => (
            <p key={index}>{line}</p>
          ))}
          {compatibility.length > 6 && (
            <p>И ещё {compatibility.length - 6} изменений в структуре.</p>
          )}
          <p className="font-medium text-foreground">
            Проверьте совместимость с существующими записями — автоматическая
            миграция не выполняется.
          </p>
        </div>
      ),
      confirmLabel: "Опубликовать",
    });
    if (!confirmed) return;
    setActionError(null);
    try {
      const detail = await publishMutation.mutateAsync();
      queryClient.setQueryData<FeatureDraftState>(draftKey, {
        revision: detail.revision,
        draft: detail.draft,
        dev: detail.dev,
        prod: detail.prod,
      });
      setBaseline(generatedText);
      setConflict(null);
      setRemoteSchema(null);
      toast.success(
        detail.dev
          ? `Схема опубликована в dev: v${detail.dev.version}`
          : "Схема опубликована в dev",
      );
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.versions(feature.id),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
    } catch (error) {
      await handleConflict(error);
    }
  };

  const reloadServer = async () => {
    const confirmed = await confirm({
      title: "Загрузить серверную версию?",
      description:
        "Локальные изменения конструктора будут заменены актуальным черновиком с сервера. При необходимости сначала скопируйте JSON.",
      confirmLabel: "Загрузить",
      destructive: true,
    });
    if (!confirmed) return;
    try {
      const fresh = await api.features.draft(feature.id);
      const source = fresh.draft?.schema ?? fresh.prod?.schema ?? null;
      const parsed = source ? parsedSchema(source) : { fields: starterFields(feature.type), warnings: [] };
      const nextFields = source ? parsed.fields : starterFields(feature.type);
      setFields(nextFields);
      setBaseline(JSON.stringify(schemaFromFields(nextFields, feature.name)));
      setSelectedId(nextFields[0]?.id ?? null);
      setParseWarnings(parsed.warnings);
      setConflict(null);
      setRemoteSchema(null);
      setActionError(null);
      queryClient.setQueryData(draftKey, fresh);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      toast.info("Загружена актуальная версия схемы");
    } catch (error) {
      setActionError(error);
    }
  };

  const handleRollback = async (version: FeatureVersion) => {
    if (dirty) {
      toast.error("Сохраните или перезагрузите черновик перед откатом");
      return;
    }
    const confirmed = await confirm({
      title: `Откатиться к версии v${version.version}?`,
      description:
        "Backend переключит published-указатель на выбранную существующую версию. Новая версия не создаётся, история не переписывается; черновик сохраняется. Контент, созданный по более новой схеме, автоматически не мигрируется.",
      confirmLabel: "Откатиться",
      destructive: true,
    });
    if (!confirmed) return;
    try {
      const detail = await rollbackMutation.mutateAsync(version.version);
      queryClient.setQueryData<FeatureDraftState>(draftKey, {
        revision: detail.revision,
        draft: detail.draft,
        dev: detail.dev,
        prod: detail.prod,
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.versions(feature.id),
      });
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      const source = detail.draft?.schema ?? detail.prod?.schema ?? null;
      if (source) {
        const parsed = parsedSchema(source);
        setFields(parsed.fields);
        setBaseline(JSON.stringify(schemaFromFields(parsed.fields, feature.name)));
        setParseWarnings(parsed.warnings);
        setSelectedId(parsed.fields[0]?.id ?? null);
      }
      toast.success(`Опубликованная схема переключена на v${version.version}`);
    } catch (error) {
      await handleConflict(error);
    }
  };

  if (draftQuery.isLoading) return <LoadingPage label="Загружаем схему…" />;
  if (draftQuery.error && !draftQuery.data) {
    return (
      <ErrorState
        error={draftQuery.error}
        title="Не удалось загрузить схему"
        onRetry={() => void draftQuery.refetch()}
      />
    );
  }
  if (!seeded) return <LoadingPage label="Подготавливаем конструктор…" />;

  const saveDisabled =
    archived || !canEdit || saveMutation.isPending || errors.length > 0 || (!dirty && hasDraft);
  const publishDisabled =
    archived ||
    !canEdit ||
    publishMutation.isPending ||
    dirty ||
    errors.length > 0 ||
    !hasDraft;
  const publishReason = archived
    ? "Feature в архиве"
    : dirty
      ? "Сначала сохраните черновик"
      : errors.length > 0
        ? "Исправьте ошибки схемы"
        : !hasDraft
          ? "Нет черновика для публикации"
          : undefined;
  const saveReason = archived
    ? "Feature в архиве"
    : errors.length > 0
      ? "Исправьте ошибки схемы"
      : !dirty && hasDraft
        ? "Нет несохранённых изменений"
        : undefined;

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-lg font-semibold tracking-tight">Конструктор схемы</h2>
            <Badge variant="muted">r{revision}</Badge>
            {dirty ? (
              <Badge variant="warning">Не сохранено</Badge>
            ) : hasDraft ? (
              <Badge variant="success">Черновик сохранён</Badge>
            ) : currentPublished ? (
              <Badge variant="success">Опубликовано v{currentPublished.version}</Badge>
            ) : (
              <Badge variant="muted">Нет схемы</Badge>
            )}
          </div>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
            Поля редактируются визуально, JSON Schema генерируется автоматически.
            Сохранение создаёт черновик; публикация — отдельное действие.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => setJsonOpen(true)}>
            <Eye />
            Просмотр JSON
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setVersionsOpen(true)}>
            <History />
            История
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleSave()}
            loading={saveMutation.isPending}
            disabled={saveDisabled}
            title={saveReason}
          >
            {!saveMutation.isPending && <Save />}
            Сохранить черновик
          </Button>
          <Button
            size="sm"
            onClick={() => void publish()}
            loading={publishMutation.isPending}
            disabled={publishDisabled}
            title={publishReason}
          >
            {!publishMutation.isPending && <Send />}
            Опубликовать
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => promoteMutation.mutate()}
            loading={promoteMutation.isPending}
            disabled={archived || !canEdit || !draftQuery.data?.dev || promoteMutation.isPending}
            title={draftQuery.data?.dev ? "Продвинуть текущую dev-схему в prod" : "Сначала опубликуйте схему в dev"}
          >
            В prod
          </Button>
        </div>
      </div>

      {archived && (
        <InlineAlert tone="warning" title="Feature в архиве">
          Схему можно просматривать, но сохранение и публикация недоступны.
        </InlineAlert>
      )}

      {conflict && (
        <InlineAlert
          tone="danger"
          title="Конфликт ревизий"
          actions={
            <>
              <Button size="sm" variant="outline" onClick={() => void reloadServer()}>
                <RefreshCw />
                Загрузить серверную версию
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setCompare({
                    title: "Серверная и локальная схемы",
                    before: remoteSchema ?? currentPublished?.schema ?? {},
                    after: generated,
                    beforeLabel: "На сервере",
                    afterLabel: "Локально",
                  })
                }
              >
                <GitCompare />
                Сравнить
              </Button>
              <CopyButton value={prettyJson(generated)} label="Скопировать мой JSON" />
            </>
          }
        >
          <p>{conflict}</p>
          <p className="mt-1 text-xs">
            Введённые данные не потеряны. Загрузка серверной версии заменит
            локальные изменения, поэтому сначала при необходимости сравните или
            скопируйте JSON.
          </p>
        </InlineAlert>
      )}

      {actionError !== null && (
        <ErrorState
          error={actionError}
          title="Не удалось выполнить действие"
        />
      )}

      {parseWarnings.length > 0 && (
        <InlineAlert tone="warning" title="Схема упрощена конструктором">
          <ul className="list-disc space-y-1 pl-4">
            {parseWarnings.map((warning, index) => (
              <li key={index}>{warning}</li>
            ))}
          </ul>
        </InlineAlert>
      )}

      {(errors.length > 0 || warnings.length > 0) && (
        <div
          className={cn(
            "flex flex-col gap-2 rounded-lg border px-3.5 py-3 sm:flex-row sm:items-center sm:justify-between",
            errors.length > 0
              ? "border-destructive/30 bg-destructive/5"
              : "border-warning/30 bg-warning/5",
          )}
        >
          <div className="flex items-start gap-2 text-sm">
            {errors.length > 0 ? (
              <AlertCircle className="mt-0.5 h-4 w-4 text-destructive" />
            ) : (
              <AlertTriangle className="mt-0.5 h-4 w-4 text-warning" />
            )}
            <p>
              <span className="font-medium">
                {errors.length > 0
                  ? `Ошибки: ${errors.length}`
                  : `Предупреждения: ${warnings.length}`}
              </span>
              {errors.length > 0
                ? " — сохранение и публикация недоступны до исправления."
                : " — сохранение доступно, проверьте их перед публикацией."}
            </p>
          </div>
          <Button variant="outline" size="sm" onClick={() => setIssuesOpen(true)}>
            Показать проверку
          </Button>
        </div>
      )}

      <SchemaBuilder
        fields={fields}
        onChange={(next) => setFields(next)}
        selectedId={selectedId}
        onSelect={setSelectedId}
        issues={issues}
        disabled={archived || saveMutation.isPending || publishMutation.isPending}
      />

      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-muted/20 px-3.5 py-3 text-xs text-muted-foreground">
        <CheckCircle2 className="h-4 w-4 text-success" />
        Ревизия {revision} · черновик {hasDraft ? "создан" : "ещё не создан"} ·
        опубликовано {currentPublished ? `v${currentPublished.version}` : "нет"}.
        Публикация не изменяет записи автоматически.
      </div>

      {guard.dialog}

      <Dialog open={jsonOpen} onOpenChange={setJsonOpen}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>JSON Schema (только чтение)</DialogTitle>
            <DialogDescription>
              Схема, которая будет отправлена при сохранении черновика. Ручное
              редактирование JSON недоступно.
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-md border border-border bg-muted/40 p-4 text-xs leading-6 thin-scrollbar">
            {prettyJson(generated)}
          </pre>
          <DialogFooter>
            <CopyButton value={prettyJson(generated)} label="Копировать JSON" />
            <Button variant="outline" onClick={() => setJsonOpen(false)}>
              Закрыть
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={issuesOpen} onOpenChange={setIssuesOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Проверка схемы</DialogTitle>
            <DialogDescription>
              Ошибки блокируют сохранение. Предупреждения нужно осознанно
              подтвердить при публикации.
            </DialogDescription>
          </DialogHeader>
          {issues.length === 0 ? (
            <p className="text-sm text-muted-foreground">Проблем не найдено.</p>
          ) : (
            <ul className="max-h-[55vh] space-y-2 overflow-y-auto thin-scrollbar">
              {issues.map((issue, index) => (
                <li key={`${issue.fieldId ?? "root"}-${index}`}>
                  <button
                    type="button"
                    className="flex w-full items-start gap-2 rounded-md border border-border px-3 py-2 text-left transition-colors hover:bg-muted/40"
                    onClick={() => {
                      if (issue.fieldId) setSelectedId(issue.fieldId);
                      setIssuesOpen(false);
                    }}
                  >
                    {issue.severity === "error" ? (
                      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                    ) : (
                      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    )}
                    <span className="min-w-0">
                      <span className="block text-sm">{issue.message}</span>
                      <code className="mt-0.5 block text-2xs text-muted-foreground">
                        {issue.path || "root"}
                      </code>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          <DialogFooter>
            <Button onClick={() => setIssuesOpen(false)}>Закрыть</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={versionsOpen} onOpenChange={setVersionsOpen}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>История схемы</DialogTitle>
            <DialogDescription>
              Откат переключает published-указатель на существующую версию. Новая
              версия не создаётся, черновик не удаляется.
            </DialogDescription>
          </DialogHeader>
          {versionsQuery.isLoading ? (
            <LoadingPage label="Загружаем версии…" />
          ) : versionsQuery.error ? (
            <ErrorState
              error={versionsQuery.error}
              onRetry={() => void versionsQuery.refetch()}
            />
          ) : versionsQuery.data?.items.length === 0 ? (
            <p className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground">
              Версий пока нет. Сохраните черновик, чтобы создать первую версию.
            </p>
          ) : (
            <ul className="max-h-[55vh] divide-y divide-border overflow-y-auto rounded-md border border-border thin-scrollbar">
              {versionsQuery.data?.items.map((version) => {
                const current = currentPublished?.id === version.id;
                return (
                  <li key={version.id} className="flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium">Версия {versionLabel(version)}</span>
                        <VersionStatusBadge status={version.status} />
                        {current && <Badge variant="default">Текущая</Badge>}
                      </div>
                      <p className="mt-1 text-2xs text-muted-foreground">
                        {version.status === "published" ? "Опубликована" : "Создана"}{" "}
                        {formatDateTime(
                          version.status === "published"
                            ? version.published_at
                            : version.created_at,
                        )}
                        {version.created_by ? ` · ${version.created_by}` : ""}
                      </p>
                    </div>
                    <div className="flex shrink-0 flex-wrap gap-1.5">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setVersionView(version)}
                      >
                        <Eye />
                        JSON
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          setCompare({
                            title: `Схема ${versionLabel(version)} и текущая`,
                            before: version.schema,
                            after: generated,
                            beforeLabel: versionLabel(version),
                            afterLabel: "Текущая",
                          })
                        }
                      >
                        <GitCompare />
                        Сравнить
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={
                          version.status !== "published" ||
                          current ||
                          dirty ||
                          rollbackMutation.isPending
                        }
                        title={
                          version.status !== "published"
                            ? "Откат возможен только на опубликованную версию"
                            : dirty
                              ? "Сохраните черновик перед откатом"
                              : undefined
                        }
                        onClick={() => void handleRollback(version)}
                      >
                        <RotateCcw />
                        Откатиться
                      </Button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setVersionsOpen(false)}>
              Закрыть
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={versionView !== null} onOpenChange={(open) => !open && setVersionView(null)}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>
              JSON схемы {versionView ? versionLabel(versionView) : ""}
            </DialogTitle>
            <DialogDescription>Сохранённая версия, только чтение.</DialogDescription>
          </DialogHeader>
          <pre className="max-h-[60vh] overflow-auto rounded-md border border-border bg-muted/40 p-4 text-xs leading-6 thin-scrollbar">
            {versionView ? prettyJson(versionView.schema) : ""}
          </pre>
          <DialogFooter>
            {versionView && (
              <CopyButton value={prettyJson(versionView.schema)} label="Копировать" />
            )}
            <Button variant="outline" onClick={() => setVersionView(null)}>
              Закрыть
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={compare !== null} onOpenChange={(open) => !open && setCompare(null)}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>{compare?.title}</DialogTitle>
            <DialogDescription>
              Смысловые различия структуры. Сравнение не меняет данные.
            </DialogDescription>
          </DialogHeader>
          {compare && (
            <div className="space-y-3">
              <div className="flex items-center justify-between gap-2">
                <div className="flex gap-2 text-2xs text-muted-foreground">
                  <Badge variant="muted">{compare.beforeLabel}</Badge>
                  <span>→</span>
                  <Badge variant="muted">{compare.afterLabel}</Badge>
                </div>
                <DiffSummaryBadges entries={diffJson(compare.before, compare.after)} />
              </div>
              <div className="max-h-[55vh] overflow-y-auto thin-scrollbar">
                <DiffList entries={diffJson(compare.before, compare.after)} />
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCompare(null)}>
              Закрыть
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
