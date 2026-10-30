import { useEffect, useMemo, useState } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  Archive,
  ArrowLeft,
  ChevronDown,
  Download,
  Inbox,
  RotateCcw,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import type {
  JsonObject,
  JsonValue,
  SubmissionFilters,
  SubmissionStatus,
} from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { errorMessage } from "@/lib/errors";
import { downloadBlob, formatDateTime, dateInputToRfc3339 } from "@/lib/format";
import {
  EmptyState,
  ErrorState,
  InlineAlert,
  PageHeader,
  TableSkeleton,
  useDocumentTitle,
} from "@/components/common/page";
import { SubmissionStatusBadge } from "@/components/common/status-badges";
import { useConfirm } from "@/components/common/confirm";
import { parsedSchema } from "@/domains/content-schema/schema";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/misc";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const statusOptions: Array<{ value: SubmissionStatus | "all"; label: string }> = [
  { value: "all", label: "Все статусы" },
  { value: "new", label: "Новые" },
  { value: "in_progress", label: "В работе" },
  { value: "processed", label: "Обработанные" },
  { value: "rejected", label: "Отклонённые" },
];

const nextStatuses: Record<SubmissionStatus, SubmissionStatus[]> = {
  new: ["in_progress", "processed", "rejected"],
  in_progress: ["processed", "rejected"],
  processed: ["in_progress"],
  rejected: ["in_progress", "processed"],
};

function useSubmissionFilterParams() {
  const [searchParams, setSearchParams] = useSearchParams();
  const form = searchParams.get("form") ?? "";
  const status = (searchParams.get("status") ?? "all") as SubmissionStatus | "all";
  const environment = (searchParams.get("environment") ?? "all") as "dev" | "prod" | "all";
  const field = searchParams.get("field") ?? "";
  const from = searchParams.get("from") ?? "";
  const to = searchParams.get("to") ?? "";
  const sort = searchParams.get("sort") ?? "desc";
  const archived = searchParams.get("archived") === "1";
  const q = searchParams.get("q") ?? "";

  const update = (values: Record<string, string | null>, replace = false) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        Object.entries(values).forEach(([key, value]) => {
          if (value === null || value === "" || value === "all") {
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

  return { form, environment, status, field, from, to, sort, archived, q, update };
}

function summarize(data: JsonObject): string {
  const parts: string[] = [];
  for (const value of Object.values(data)) {
    if (typeof value === "string" && value.trim()) {
      parts.push(value.trim());
    } else if (typeof value === "number" || typeof value === "boolean") {
      parts.push(String(value));
    }
    if (parts.length >= 3) break;
  }
  return parts.join(" · ");
}

function valueToText(value: JsonValue | undefined): string {
  if (value === undefined || value === null) return "—";
  if (typeof value === "string") return value.trim() || "—";
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value, null, 2);
}

export default function SubmissionsPage() {
  const { submissionId } = useParams<{ submissionId: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { form, environment, status, field, from, to, sort, archived, q, update } =
    useSubmissionFilterParams();
  const [searchInput, setSearchInput] = useState(q);
  const debouncedSearch = useDebouncedValue(searchInput, 300);

  useDocumentTitle(submissionId ? "Заявка" : "Заявки");

  useEffect(() => {
    setSearchInput(q);
  }, [q]);

  useEffect(() => {
    if (debouncedSearch !== q) {
      update({ q: debouncedSearch || null }, true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const formsQuery = useQuery({
    queryKey: queryKeys.features.list({ type: "form" }),
    queryFn: ({ signal }) =>
      api.features.list({ type: "form", status: "", limit: 200 }, signal),
  });
  const forms = useMemo(
    () => formsQuery.data?.items.filter((item) => item.type === "form") ?? [],
    [formsQuery.data],
  );

  const activeForm = forms.find((item) => item.key === form) ?? null;

  const formSchemaQuery = useQuery({
    queryKey: queryKeys.features.draft(activeForm?.id ?? ""),
    queryFn: ({ signal }) => api.features.draft(activeForm!.id, signal),
    enabled: Boolean(activeForm),
  });
  const formFields = useMemo(
    () =>
      parsedSchema(
        formSchemaQuery.data?.prod?.schema ?? formSchemaQuery.data?.draft?.schema,
      ).fields,
    [formSchemaQuery.data],
  );

  const filters = useMemo<SubmissionFilters>(
    () => ({
      environment: environment === "all" ? "" : environment,
      status: status === "all" ? "" : status,
      created_from: dateInputToRfc3339(from, "start"),
      created_to: dateInputToRfc3339(to, "end"),
      q: q || undefined,
      field: field || undefined,
      include_archived: archived,
      sort: sort as "asc" | "desc",
    }),
    [environment, status, from, to, q, field, archived, sort],
  );

  const submissionsQuery = useInfiniteQuery({
    queryKey: queryKeys.submissions.list(form, {
      status,
      field,
      from,
      to,
      sort,
      archived,
      q,
    }),
    queryFn: ({ pageParam, signal }) =>
      api.forms.submissions(
        form,
        filters,
        pageParam ?? undefined,
        50,
        signal,
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
    enabled: Boolean(form),
  });

  const loaded = useMemo(
    () => submissionsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [submissionsQuery.data],
  );

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.submissions.all });
    void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
  };

  const exportMutation = useMutation({
    mutationFn: () => api.forms.exportCsv(form, filters),
    onSuccess: ({ blob, filename }) => {
      downloadBlob(blob, filename);
      toast.success("Экспорт заявок готов");
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  if (submissionId) {
    return (
      <SubmissionDetail
        submissionId={submissionId}
        onBack={() => navigate(`/submissions${form ? `?form=${encodeURIComponent(form)}` : ""}`)}
        onChanged={invalidate}
      />
    );
  }

  const filtersActive =
    Boolean(q) || environment !== "all" || status !== "all" || Boolean(field) || Boolean(from) || Boolean(to) || archived;

  const resetFilters = () => {
    setSearchInput("");
    update({
      q: null,
      environment: null,
      status: null,
      field: null,
      from: null,
      to: null,
      archived: null,
    });
  };

  return (
    <div className="page-grid">
      <PageHeader
        title="Заявки"
        description="Отправки публичных форм. Данные показываются по той версии схемы, которая была опубликована на момент отправки."
        actions={
          <>
            {form && (
              <Button
                variant="outline"
                loading={exportMutation.isPending}
                onClick={() => exportMutation.mutate()}
              >
                <Download />
                Экспорт CSV
              </Button>
            )}
          </>
        }
      />

      <div className="space-y-3">
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <div className="space-y-1.5">
            <Label htmlFor="filter-form">Форма</Label>
            <Select
              value={form || "none"}
              onValueChange={(value) => update({ form: value === "none" ? null : value })}
            >
              <SelectTrigger id="filter-form" className="w-full">
                <SelectValue placeholder="Выберите форму" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">Все формы</SelectItem>
                {forms.map((item) => (
                  <SelectItem key={item.id} value={item.key}>
                    {item.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <div className="space-y-1.5">
              <Label htmlFor="filter-environment">Среда</Label>
              <Select
                value={environment}
                onValueChange={(value) => update({ environment: value })}
              >
                <SelectTrigger id="filter-environment" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Все среды</SelectItem>
                  <SelectItem value="dev">dev</SelectItem>
                  <SelectItem value="prod">prod</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Label htmlFor="filter-status">Статус</Label>
            <Select
              value={status}
              onValueChange={(value) => update({ status: value })}
            >
              <SelectTrigger id="filter-status" className="w-full">
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
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="filter-field">Поле поиска</Label>
            <Select
              value={field || "all"}
              onValueChange={(value) => update({ field: value === "all" ? null : value })}
              disabled={!form}
            >
              <SelectTrigger id="filter-field" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Любое поле</SelectItem>
                {formFields.map((item) => (
                  <SelectItem key={item.id} value={item.key}>
                    {item.label || item.key}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="filter-sort">Сортировка</Label>
            <Select
              value={sort}
              onValueChange={(value) => update({ sort: value })}
            >
              <SelectTrigger id="filter-sort" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="desc">Сначала новые</SelectItem>
                <SelectItem value="asc">Сначала старые</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="Поиск по содержимому заявки"
              aria-label="Поиск заявок"
              className="pl-9 pr-9"
              disabled={!form}
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
            <div className="space-y-1.5">
              <Label htmlFor="filter-from" className="text-2xs text-muted-foreground">
                С
              </Label>
              <Input
                id="filter-from"
                type="date"
                value={from}
                max={to || undefined}
                onChange={(event) =>
                  update({
                    from: event.target.value || null,
                  })
                }
                disabled={!form}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="filter-to" className="text-2xs text-muted-foreground">
                По
              </Label>
              <Input
                id="filter-to"
                type="date"
                value={to}
                min={from || undefined}
                onChange={(event) =>
                  update({
                    to: event.target.value || null,
                  })
                }
                disabled={!form}
              />
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>
            Загружено: {loaded.length}
            {submissionsQuery.hasNextPage ? " (есть ещё)" : ""}
          </span>
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={resetFilters}>
              <RotateCcw />
              Сбросить фильтры
            </Button>
          )}
        </div>
      </div>

      {!form ? (
        <EmptyState
          icon={Inbox}
          title="Выберите форму"
          description="Заявки доступны для form-фич, загруженных из feature-сборок. Выберите форму в фильтре, чтобы увидеть отправки."
          action={
            formsQuery.isLoading ? (
              <Skeleton className="h-9 w-40" />
            ) : forms.length === 0 ? (
              <Button asChild variant="outline">
                <Link to="/site">Загрузить form-сборку</Link>
              </Button>
            ) : undefined
          }
        />
      ) : submissionsQuery.isLoading ? (
        <div className="hidden overflow-hidden rounded-lg border border-border bg-card p-4 md:block">
          <TableSkeleton rows={6} />
        </div>
      ) : submissionsQuery.isError ? (
        <ErrorState
          error={submissionsQuery.error}
          title="Не удалось загрузить заявки"
          onRetry={() => void submissionsQuery.refetch()}
        />
      ) : loaded.length === 0 ? (
        <EmptyState
          icon={Inbox}
          title={filtersActive ? "Заявки не найдены" : "Заявок пока нет"}
          description={
            filtersActive
              ? "По заданным условиям ничего не найдено. Сбросьте фильтры или измените запрос."
              : "Как только форму заполнят, отправки появятся здесь."
          }
          action={
            filtersActive ? (
              <Button variant="outline" onClick={resetFilters}>
                <RotateCcw />
                Сбросить фильтры
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          <div className="hidden overflow-hidden rounded-lg border border-border bg-card md:block">
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col">Получена</th>
                  <th scope="col">Форма</th>
                  <th scope="col">Содержание</th>
                  <th scope="col">Статус</th>
                  <th scope="col" className="text-right">
                    Действия
                  </th>
                </tr>
              </thead>
              <tbody>
                {loaded.map((submission) => (
                  <tr key={submission.id}>
                    <td className="whitespace-nowrap text-xs text-muted-foreground">
                      {formatDateTime(submission.created_at)}
                    </td>
                    <td className="text-xs">
                      <code className="text-2xs">{submission.feature_key}</code>
                      <Badge variant={submission.environment === "prod" ? "success" : "secondary"} className="ml-1">
                        {submission.environment}
                      </Badge>
                    </td>
                    <td className="max-w-md">
                      <p className="truncate text-sm">
                        {summarize(submission.data) || "—"}
                      </p>
                    </td>
                    <td>
                      <SubmissionStatusBadge status={submission.status} />
                    </td>
                    <td>
                      <div className="flex justify-end">
                        <Button variant="ghost" size="sm" asChild>
                          <Link
                            to={`/submissions/${submission.id}${
                              form ? `?form=${encodeURIComponent(form)}` : ""
                            }`}
                          >
                            Открыть
                          </Link>
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="grid gap-3 md:hidden">
            {loaded.map((submission) => (
              <Link
                key={submission.id}
                to={`/submissions/${submission.id}${
                  form ? `?form=${encodeURIComponent(form)}` : ""
                }`}
                className="rounded-lg border border-border bg-card p-4"
              >
                <div className="flex items-start justify-between gap-2">
                  <p className="line-clamp-2 text-sm font-medium">
                    {summarize(submission.data) || "Без данных"}
                  </p>
                  <SubmissionStatusBadge status={submission.status} />
                </div>
                <p className="mt-2 text-2xs text-muted-foreground">
                  {submission.feature_key} · {submission.environment} · {formatDateTime(submission.created_at)}
                </p>
              </Link>
            ))}
          </div>

          <div className="flex flex-col items-center justify-center gap-2">
            {submissionsQuery.hasNextPage ? (
              <Button
                variant="outline"
                loading={submissionsQuery.isFetchingNextPage}
                onClick={() => void submissionsQuery.fetchNextPage()}
              >
                <ChevronDown />
                Загрузить ещё
              </Button>
            ) : (
              <p className="text-2xs text-muted-foreground">
                Все загруженные заявки показаны
              </p>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function SubmissionDetail({
  submissionId,
  onBack,
  onChanged,
}: {
  submissionId: string;
  onBack: () => void;
  onChanged: () => void;
}) {
  const queryClient = useQueryClient();
  const confirm = useConfirm();

  const submissionQuery = useQuery({
    queryKey: queryKeys.submissions.detail(submissionId),
    queryFn: ({ signal }) => api.forms.get(submissionId, signal),
  });
  const submission = submissionQuery.data;

  const versionsQuery = useQuery({
    queryKey: queryKeys.features.versions(submission?.feature_id ?? ""),
    queryFn: ({ signal }) => api.features.versions(submission!.feature_id, signal),
    enabled: Boolean(submission),
  });

  const schemaVersion = useMemo(
    () =>
      versionsQuery.data?.items.find(
        (version) => version.id === submission?.schema_version_id,
      ) ?? null,
    [versionsQuery.data, submission],
  );

  const fields = useMemo(
    () => parsedSchema(schemaVersion?.schema).fields,
    [schemaVersion],
  );

  const statusMutation = useMutation({
    mutationFn: (next: SubmissionStatus) =>
      api.forms.updateStatus(submissionId, next),
    onSuccess: (updated) => {
      queryClient.setQueryData(
        queryKeys.submissions.detail(updated.id),
        updated,
      );
      onChanged();
      toast.success(`Статус изменён: ${statusLabel(updated.status)}`);
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const archiveMutation = useMutation({
    mutationFn: () => api.forms.archive(submissionId),
    onSuccess: () => {
      onChanged();
      toast.success("Заявка отправлена в архив");
      onBack();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const removeMutation = useMutation({
    mutationFn: () => api.forms.remove(submissionId),
    onSuccess: () => {
      onChanged();
      toast.success("Заявка удалена безвозвратно");
      onBack();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  if (submissionQuery.isLoading) {
    return <Skeleton className="h-64 w-full" />;
  }

  if (submissionQuery.error) {
    return (
      <div className="space-y-4">
        <ErrorState
          error={submissionQuery.error}
          title="Не удалось загрузить заявку"
          onRetry={() => void submissionQuery.refetch()}
        />
        <Button variant="outline" onClick={onBack}>
          <ArrowLeft />
          К списку заявок
        </Button>
      </div>
    );
  }

  if (!submission) return null;

  const requestArchive = async () => {
    const confirmed = await confirm({
      title: "Отправить заявку в архив?",
      description:
        "Заявка исчезнет из списка и из экспорта, но останется в базе. Данные не удаляются.",
      confirmLabel: "В архив",
      destructive: true,
    });
    if (confirmed) archiveMutation.mutate();
  };

  const requestRemove = async () => {
    const confirmed = await confirm({
      title: "Удалить заявку безвозвратно?",
      description:
        "Данные заявки будут удалены из базы без возможности восстановления. Отменить это действие нельзя.",
      confirmLabel: "Удалить",
      destructive: true,
    });
    if (confirmed) removeMutation.mutate();
  };

  return (
    <div className="page-grid">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <Button variant="ghost" size="sm" className="-ml-2" onClick={onBack}>
            <ArrowLeft />
            К списку заявок
          </Button>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <h1 className="text-lg font-semibold tracking-tight">Заявка</h1>
            <SubmissionStatusBadge status={submission.status} />
            {submission.archived_at && <Badge variant="muted">В архиве</Badge>}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            Получена {formatDateTime(submission.created_at)} · форма{" "}
            <code>{submission.feature_key}</code>
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {nextStatuses[submission.status].map((next) => (
            <Button
              key={next}
              variant={next === "processed" ? "default" : "outline"}
              size="sm"
              loading={statusMutation.isPending}
              onClick={() => statusMutation.mutate(next)}
            >
              {statusLabel(next)}
            </Button>
          ))}
          <Button
            variant="outline"
            size="sm"
            loading={archiveMutation.isPending}
            onClick={() => void requestArchive()}
            disabled={Boolean(submission.archived_at)}
          >
            <Archive />
            В архив
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="text-destructive hover:text-destructive"
            loading={removeMutation.isPending}
            onClick={() => void requestRemove()}
          >
            <Trash2 />
            Удалить
          </Button>
        </div>
      </div>

      {schemaVersion ? (
        <InlineAlert tone="info" title="Данные по версии схемы на момент отправки">
          Показаны поля версии схемы v{schemaVersion.version}. Если схема формы с тех
          пор изменилась, структура заявки может отличаться от текущей.
        </InlineAlert>
      ) : (
        <InlineAlert tone="warning" title="Версия схемы не найдена">
          Не удалось сопоставить заявку с версией схемы. Данные показаны в исходном
          виде.
        </InlineAlert>
      )}

      <div className="rounded-lg border border-border bg-card">
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold">Данные заявки</h2>
        </div>
        {fields.length > 0 ? (
          <dl className="divide-y divide-border">
            {fields.map((fieldNode) => (
              <div
                key={fieldNode.id}
                className="grid gap-1 px-4 py-3 sm:grid-cols-[14rem_minmax(0,1fr)] sm:gap-4"
              >
                <dt className="text-xs font-medium text-muted-foreground">
                  {fieldNode.label || fieldNode.key}
                </dt>
                <dd className="min-w-0 text-sm">
                  <FieldValue value={submission.data[fieldNode.key]} />
                </dd>
              </div>
            ))}
            {Object.entries(submission.data)
              .filter(
                ([key]) => !fields.some((fieldNode) => fieldNode.key === key),
              )
              .map(([key, value]) => (
                <div
                  key={key}
                  className="grid gap-1 px-4 py-3 sm:grid-cols-[14rem_minmax(0,1fr)] sm:gap-4"
                >
                  <dt className="text-xs font-medium text-muted-foreground">
                    {key}
                  </dt>
                  <dd className="min-w-0 text-sm">
                    <FieldValue value={value} />
                  </dd>
                </div>
              ))}
          </dl>
        ) : (
          <pre className="overflow-x-auto px-4 py-3 text-xs leading-6">
            {JSON.stringify(submission.data, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
}

function FieldValue({ value }: { value: JsonValue | undefined }) {
  const text = valueToText(value);
  if (text.includes("\n")) {
    return (
      <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md bg-muted/40 px-2.5 py-2 text-2xs leading-5">
        {text}
      </pre>
    );
  }
  return <span className="break-words">{text}</span>;
}

function statusLabel(status: SubmissionStatus): string {
  return (
    {
      new: "Новая",
      in_progress: "В работе",
      processed: "Обработана",
      rejected: "Отклонена",
    }[status]
  );
}
