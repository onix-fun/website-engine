import { useEffect, useMemo, useState } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import {
  Archive,
  ArrowRight,
  ChevronDown,
  Eye,
  EyeOff,
  GitCompareArrows,
  Layers,
  MoreHorizontal,
  Pencil,
  Plus,
  RotateCcw,
  Search,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import type { EntryDetail, JsonObject } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { errorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { deriveEntrySubtitle, deriveEntryTitle } from "@/lib/json";
import {
  EmptyState,
  ErrorState,
  InlineAlert,
  TableSkeleton,
  useDocumentTitle,
} from "@/components/common/page";
import { useConfirm } from "@/components/common/confirm";
import { EntryPublishStateBadge, entryPublishState } from "@/domains/content-editor/entry-status";
import { ContentPreview } from "@/domains/content-editor/ContentPreview";
import {
  blocksKeyFromFields,
  documentMetadata,
  parseBlocks,
} from "@/domains/content-editor/blocks";
import { DiffList, DiffSummaryBadges } from "@/domains/versioning/DiffView";
import { diffJson } from "@/domains/versioning/diff";
import { parsedSchema } from "@/domains/schema-builder/schema";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox, Input } from "@/components/ui/input";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useFeatureContext } from "@/pages/features/FeatureLayout";

type StateFilter = "all" | "published" | "draft" | "unpublished" | "archived";
type SortOption = "updated_desc" | "updated_asc" | "created_desc" | "created_asc" | "title_asc";

const stateOptions: Array<{ value: StateFilter; label: string }> = [
  { value: "all", label: "Все записи" },
  { value: "published", label: "Опубликованные" },
  { value: "draft", label: "С черновиком" },
  { value: "unpublished", label: "Снятые с публикации" },
  { value: "archived", label: "В архиве" },
];

const sortOptions: Array<{ value: SortOption; label: string }> = [
  { value: "updated_desc", label: "Сначала изменённые" },
  { value: "updated_asc", label: "Сначала давно изменённые" },
  { value: "created_desc", label: "Сначала новые" },
  { value: "created_asc", label: "Сначала старые" },
  { value: "title_asc", label: "По названию А–Я" },
];

function entryData(entry: EntryDetail): JsonObject | null {
  return entry.draft?.data ?? entry.prod?.data ?? null;
}

function useEntryFilterParams() {
  const [searchParams, setSearchParams] = useSearchParams();
  const state = (searchParams.get("state") ?? "all") as StateFilter;
  const sort = (searchParams.get("sort") ?? "updated_desc") as SortOption;
  const q = searchParams.get("q") ?? "";
  const archived = searchParams.get("archived") === "1";

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

  return { state, sort, q, archived, update };
}

export default function FeatureContentPage() {
  const { feature } = useFeatureContext();
  const { state, sort, q, archived, update } = useEntryFilterParams();
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [searchInput, setSearchInput] = useState(q);
  const debouncedSearch = useDebouncedValue(searchInput, 300);
  const [selected, setSelected] = useState<string[]>([]);
  const [previewEntry, setPreviewEntry] = useState<EntryDetail | null>(null);
  const [compareEntry, setCompareEntry] = useState<EntryDetail | null>(null);

  useDocumentTitle(`Контент — ${feature.name}`);

  useEffect(() => {
    setSearchInput(q);
  }, [q]);

  useEffect(() => {
    if (debouncedSearch !== q) {
      update({ q: debouncedSearch || null }, true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const schemaQuery = useQuery({
    queryKey: queryKeys.features.draft(feature.id),
    queryFn: ({ signal }) => api.features.draft(feature.id, signal),
  });
  const fields = useMemo(
    () => parsedSchema(schemaQuery.data?.draft?.schema ?? schemaQuery.data?.prod?.schema).fields,
    [schemaQuery.data],
  );
  const blocksKey = useMemo(() => blocksKeyFromFields(fields), [fields]);

  const publishedPreview = useMemo(() => {
    if (!previewEntry?.prod) return null;
    const data = previewEntry.prod.data;
    return {
      metadata: documentMetadata(data, blocksKey),
      blocks: parseBlocks(
        data[blocksKey ?? ""] as JsonObject[string] | undefined,
      ).blocks,
    };
  }, [previewEntry, blocksKey]);

  const entriesQuery = useInfiniteQuery({
    queryKey: queryKeys.entries.list(feature.id, {
      state,
      sort,
      q,
      archived,
    }),
    queryFn: ({ pageParam, signal }) =>
      api.entries.list(
        feature.id,
        {
          q,
          include_archived:
            archived || state === "archived" || feature.type === "single",
          limit: 50,
          cursor: pageParam ?? undefined,
        },
        signal,
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  });

  const loadedEntries = useMemo(
    () => entriesQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [entriesQuery.data],
  );

  const entries = useMemo(() => {
    const list = loadedEntries.filter((entry) => {
      const entryState = entryPublishState(entry);
      if (state === "published") {
        if (entryState !== "published" && entryState !== "published_with_draft") {
          return false;
        }
      }
      if (state === "draft" && entryState !== "draft" && entryState !== "published_with_draft") {
        return false;
      }
      if (state === "unpublished" && entryState !== "unpublished") return false;
      if (state === "archived" && entryState !== "archived") return false;
      return true;
    });
    return [...list].sort((a, b) => {
      if (sort === "updated_asc") {
        return new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime();
      }
      if (sort === "created_desc") {
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
      }
      if (sort === "created_asc") {
        return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
      }
      if (sort === "title_asc") {
        return deriveEntryTitle(entryData(a)).localeCompare(
          deriveEntryTitle(entryData(b)),
          "ru",
        );
      }
      return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
    });
  }, [loadedEntries, sort, state]);

  const selectedEntries = useMemo(
    () => loadedEntries.filter((entry) => selected.includes(entry.id)),
    [loadedEntries, selected],
  );

  const toggleSelected = (id: string) => {
    setSelected((current) =>
      current.includes(id)
        ? current.filter((item) => item !== id)
        : [...current, id],
    );
  };

  const allSelected =
    entries.length > 0 && entries.every((entry) => selected.includes(entry.id));

  const invalidate = () => {
    void queryClient.invalidateQueries({
      queryKey: queryKeys.entries.list(feature.id),
    });
    void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
  };

  const unpublishMutation = useMutation({
    mutationFn: (id: string) => api.entries.unpublish(id, "prod"),
    onSuccess: () => {
      toast.success("Запись снята с публикации");
      invalidate();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const archiveMutation = useMutation({
    mutationFn: (id: string) => api.entries.archive(id),
    onSuccess: () => {
      toast.success("Запись отправлена в архив");
      invalidate();
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const requestUnpublish = async (entry: EntryDetail) => {
    const title = deriveEntryTitle(entryData(entry));
    const confirmed = await confirm({
      title: "Снять запись с публикации?",
      description: (
        <>
          Запись <strong>«{title}»</strong> перестанет отдаваться публичным API.
          Черновик и все версии сохранятся, запись останется доступна для
          редактирования и повторной публикации.
        </>
      ),
      confirmLabel: "Снять с публикации",
      destructive: true,
    });
    if (confirmed) unpublishMutation.mutate(entry.id);
  };

  const requestArchive = async (entry: EntryDetail) => {
    const title = deriveEntryTitle(entryData(entry));
    const confirmed = await confirm({
      title: "Отправить запись в архив?",
      description: (
        <>
          Запись <strong>«{title}»</strong> станет архивной: она исчезнет из списка
          и из публичного API, а редактирование и публикация будут заблокированы.
          Данные не удаляются, но вернуть запись из архива через интерфейс нельзя.
        </>
      ),
      confirmLabel: "В архив",
      destructive: true,
    });
    if (confirmed) archiveMutation.mutate(entry.id);
  };

  const requestBulkUnpublish = async () => {
    const confirmed = await confirm({
      title: `Снять с публикации ${selectedEntries.length} записей?`,
      description: (
        <>
          Выбранные записи перестанут отдаваться публичным API. Черновики и версии
          сохранятся — записи останутся доступны для редактирования.
        </>
      ),
      confirmLabel: "Снять с публикации",
      destructive: true,
    });
    if (!confirmed) return;
    const results = await Promise.allSettled(
      selectedEntries.map((entry) => api.entries.unpublish(entry.id, "prod")),
    );
    const failed = results.filter((result) => result.status === "rejected").length;
    if (failed > 0) {
      toast.error(`Не удалось снять с публикации: ${failed} из ${results.length}`);
    } else {
      toast.success(`Снято с публикации: ${results.length}`);
    }
    setSelected([]);
    invalidate();
  };

  const requestBulkArchive = async () => {
    const confirmed = await confirm({
      title: `Отправить в архив ${selectedEntries.length} записей?`,
      description: (
        <>
          Выбранные записи станут архивными: они исчезнут из списка и публичного
          API, редактирование и публикация будут заблокированы. Данные не
          удаляются, но вернуть записи из архива через интерфейс нельзя.
        </>
      ),
      confirmLabel: "В архив",
      destructive: true,
    });
    if (!confirmed) return;
    const results = await Promise.allSettled(
      selectedEntries.map((entry) => api.entries.archive(entry.id)),
    );
    const failed = results.filter((result) => result.status === "rejected").length;
    if (failed > 0) {
      toast.error(`Не удалось архивировать: ${failed} из ${results.length}`);
    } else {
      toast.success(`Архивировано: ${results.length}`);
    }
    setSelected([]);
    invalidate();
  };

  const filtersActive = Boolean(q) || state !== "all" || sort !== "updated_desc" || archived;

  const resetFilters = () => {
    setSearchInput("");
    update({ q: null, state: null, sort: null, archived: null });
  };

  // A single feature owns exactly one record, so the table is replaced by the
  // editor itself.
  if (feature.type === "single") {
    if (entriesQuery.isLoading) {
      return <Skeleton className="h-40 w-full" />;
    }
    const single = loadedEntries[0];
    if (single) {
      return (
        <Navigate
          to={`/features/${feature.id}/content/${single.id}`}
          replace
        />
      );
    }
    return (
      <EmptyState
        icon={Layers}
        title="Запись ещё не создана"
        description="Feature типа single содержит одну запись. Создайте её, чтобы открыть блочный редактор."
        action={
          <Button asChild>
            <Link to={`/features/${feature.id}/content/new`}>
              <Plus />
              Создать запись
            </Link>
          </Button>
        }
      />
    );
  }

  return (
    <div className="page-grid">
      <div className="flex flex-col gap-4 pb-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-lg font-semibold tracking-tight">Контент</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Записи Feature «{feature.name}». Поиск выполняется на сервере, сортировка
            и фильтр по статусу — по загруженной странице.
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {feature.type !== "form" && (
            <Button asChild>
              <Link to={`/features/${feature.id}/content/new`}>
                <Plus />
                Создать запись
              </Link>
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-3">
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="Поиск по содержимому записи"
              aria-label="Поиск записей"
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
            <Select
              value={state}
              onValueChange={(value) => update({ state: value })}
            >
              <SelectTrigger className="w-full sm:w-52" aria-label="Статус публикации">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {stateOptions.map((option) => (
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
              <SelectTrigger className="w-full sm:w-56" aria-label="Сортировка">
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

        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>
            Загружено: {loadedEntries.length}
            {entriesQuery.hasNextPage ? " (есть ещё)" : ""} · показано: {entries.length}
          </span>
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={resetFilters}>
              <RotateCcw />
              Сбросить фильтры
            </Button>
          )}
        </div>
      </div>

      {selected.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2">
          <p className="text-sm">
            Выбрано записей: <strong>{selected.length}</strong>
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => void requestBulkUnpublish()}
            >
              <EyeOff />
              Снять с публикации
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void requestBulkArchive()}
            >
              <Archive />
              В архив
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setSelected([])}
            >
              Снять выделение
            </Button>
          </div>
        </div>
      )}

      {entriesQuery.isLoading ? (
        <div className="space-y-3">
          <div className="hidden overflow-hidden rounded-lg border border-border bg-card p-4 md:block">
            <TableSkeleton rows={6} />
          </div>
          <div className="grid gap-3 md:hidden">
            {Array.from({ length: 3 }).map((_, index) => (
              <Skeleton key={index} className="h-32" />
            ))}
          </div>
        </div>
      ) : entriesQuery.isError ? (
        <ErrorState
          error={entriesQuery.error}
          title="Не удалось загрузить записи"
          onRetry={() => void entriesQuery.refetch()}
        />
      ) : loadedEntries.length === 0 ? (
        <EmptyState
          icon={Layers}
          title={filtersActive ? "Записи не найдены" : "В Feature пока нет записей"}
          description={
            filtersActive
              ? "По заданным условиям ничего не найдено. Сбросьте фильтры или измените запрос."
              : "Создайте первую запись — откроется блочный редактор с автосохранением черновика."
          }
          action={
            filtersActive ? (
              <Button variant="outline" onClick={resetFilters}>
                <RotateCcw />
                Сбросить фильтры
              </Button>
            ) : (
              <Button asChild>
                <Link to={`/features/${feature.id}/content/new`}>
                  <Plus />
                  Создать запись
                </Link>
              </Button>
            )
          }
        />
      ) : entries.length === 0 ? (
        <EmptyState
          icon={Search}
          title="Нет записей по выбранному фильтру"
          description="Измените фильтр статуса или сбросьте его, чтобы увидеть все загруженные записи."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" onClick={resetFilters}>
                Сбросить фильтры
              </Button>
              {entriesQuery.hasNextPage && (
                <Button onClick={() => void entriesQuery.fetchNextPage()}>
                  Загрузить ещё
                </Button>
              )}
            </div>
          }
        />
      ) : (
        <>
          <div className="hidden overflow-hidden rounded-lg border border-border bg-card md:block">
            <table className="data-table">
              <thead>
                <tr>
                  <th scope="col" className="w-10">
                    <Checkbox
                      checked={allSelected}
                      onCheckedChange={(checked) =>
                        setSelected(
                          checked
                            ? entries.map((entry) => entry.id)
                            : [],
                        )
                      }
                      aria-label="Выбрать все записи"
                    />
                  </th>
                  <th scope="col">Запись</th>
                  <th scope="col">Публикация</th>
                  <th scope="col">Изменена</th>
                  <th scope="col">Автор</th>
                  <th scope="col" className="text-right">
                    Действия
                  </th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => {
                  const data = entryData(entry);
                  const title = deriveEntryTitle(data);
                  const subtitle = deriveEntrySubtitle(data, title);
                  const entryState = entryPublishState(entry);
                  return (
                    <tr key={entry.id}>
                      <td>
                        <Checkbox
                          checked={selected.includes(entry.id)}
                          onCheckedChange={() => toggleSelected(entry.id)}
                          aria-label={`Выбрать запись ${title}`}
                        />
                      </td>
                      <td>
                        <div className="min-w-0">
                          <Link
                            to={`/features/${feature.id}/content/${entry.id}`}
                            className="block truncate text-sm font-medium hover:text-primary"
                          >
                            {title}
                          </Link>
                          {subtitle && (
                            <p className="mt-0.5 truncate text-2xs text-muted-foreground">
                              {subtitle}
                            </p>
                          )}
                        </div>
                      </td>
                      <td>
                        <div className="flex flex-col items-start gap-1">
                          <EntryPublishStateBadge state={entryState} />
                          {entry.prod && entry.draft && (
                            <span className="text-2xs text-muted-foreground">
                              v{entry.prod.version} → v{entry.draft.version}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="text-xs text-muted-foreground">
                        {formatDateTime(entry.updated_at)}
                      </td>
                      <td className="text-xs text-muted-foreground">
                        {entry.created_by ? (
                          <span title={entry.created_by}>
                            Токен {entry.created_by.slice(0, 8)}
                          </span>
                        ) : (
                          "—"
                        )}
                      </td>
                      <td>
                        <div className="flex items-center justify-end gap-1">
                          <Button variant="ghost" size="sm" asChild>
                            <Link to={`/features/${feature.id}/content/${entry.id}`}>
                              <Pencil />
                              Изменить
                            </Link>
                          </Button>
                          <EntryActions
                            entry={entry}
                            onPreview={() => setPreviewEntry(entry)}
                            onCompare={() => setCompareEntry(entry)}
                            onUnpublish={() => void requestUnpublish(entry)}
                            onArchive={() => void requestArchive(entry)}
                          />
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <div className="grid gap-3 md:hidden">
            {entries.map((entry) => {
              const data = entryData(entry);
              const title = deriveEntryTitle(data);
              const subtitle = deriveEntrySubtitle(data, title);
              const entryState = entryPublishState(entry);
              return (
                <div key={entry.id} className="rounded-lg border border-border bg-card p-4">
                  <div className="flex items-start gap-3">
                    <Checkbox
                      checked={selected.includes(entry.id)}
                      onCheckedChange={() => toggleSelected(entry.id)}
                      aria-label={`Выбрать запись ${title}`}
                      className="mt-1"
                    />
                    <div className="min-w-0 flex-1">
                      <Link
                        to={`/features/${feature.id}/content/${entry.id}`}
                        className="block truncate text-sm font-semibold hover:text-primary"
                      >
                        {title}
                      </Link>
                      {subtitle && (
                        <p className="mt-0.5 truncate text-2xs text-muted-foreground">
                          {subtitle}
                        </p>
                      )}
                    </div>
                    <EntryActions
                      entry={entry}
                      onPreview={() => setPreviewEntry(entry)}
                      onCompare={() => setCompareEntry(entry)}
                      onUnpublish={() => void requestUnpublish(entry)}
                      onArchive={() => void requestArchive(entry)}
                    />
                  </div>
                  <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
                    <EntryPublishStateBadge state={entryState} />
                    <span className="text-2xs text-muted-foreground">
                      {formatDateTime(entry.updated_at)}
                      {entry.created_by
                        ? ` · токен ${entry.created_by.slice(0, 8)}`
                        : ""}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>

          <div className="flex flex-col items-center justify-center gap-2">
            {entriesQuery.hasNextPage ? (
              <Button
                variant="outline"
                loading={entriesQuery.isFetchingNextPage}
                onClick={() => void entriesQuery.fetchNextPage()}
              >
                <ChevronDown />
                Загрузить ещё
              </Button>
            ) : (
              <p className="text-2xs text-muted-foreground">
                Все загруженные записи показаны
              </p>
            )}
          </div>
        </>
      )}

      {feature.type === "form" && (
        <InlineAlert tone="info" title="Для формы используется раздел «Заявки»">
          Записи Feature типа form создаются публичной формой. Отправки доступны в
          разделе «Заявки».
        </InlineAlert>
      )}

      <Dialog
        open={previewEntry !== null}
        onOpenChange={(open) => !open && setPreviewEntry(null)}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>
              Опубликованная версия
              {previewEntry?.prod ? ` · v${previewEntry.prod.version}` : ""}
            </DialogTitle>
            <DialogDescription>
              Так содержимое видят посетители сайта. Черновик в эту версию не
              включён.
            </DialogDescription>
          </DialogHeader>
          {previewEntry?.prod && publishedPreview ? (
            <>
              <div className="rounded-md border border-border bg-background p-4">
                <ContentPreview
                  metadata={publishedPreview.metadata}
                  blocks={publishedPreview.blocks}
                  fields={fields}
                  width="desktop"
                />
              </div>
              <p className="text-2xs text-muted-foreground">
                Опубликовано {formatDateTime(previewEntry.prod.published_at)}
              </p>
            </>
          ) : (
            <EmptyState
              title="Запись не опубликована"
              description="У записи нет опубликованной версии, поэтому посетители её не видят."
            />
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={compareEntry !== null}
        onOpenChange={(open) => !open && setCompareEntry(null)}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>Сравнение версий</DialogTitle>
            <DialogDescription>
              Опубликованная версия и текущий черновик. Различия показаны по полям
              и блокам.
            </DialogDescription>
          </DialogHeader>
          {compareEntry ? (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="success">
                  Опубликовано: v{compareEntry.prod?.version ?? "—"}
                </Badge>
                <Badge variant="warning">
                  Черновик: v{compareEntry.draft?.version ?? "—"}
                </Badge>
                <DiffSummaryBadges
                  entries={diffJson(
                    compareEntry.prod?.data ?? null,
                    compareEntry.draft?.data ?? null,
                  )}
                />
              </div>
              <DiffList
                entries={diffJson(
                  compareEntry.prod?.data ?? null,
                  compareEntry.draft?.data ?? null,
                )}
                emptyText="Черновик совпадает с опубликованной версией"
              />
              {compareEntry.draft && (
                <Button variant="outline" asChild>
                  <Link to={`/features/${feature.id}/content/${compareEntry.id}`}>
                    <ArrowRight />
                    Вернуться к редактированию
                  </Link>
                </Button>
              )}
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function EntryActions({
  entry,
  onPreview,
  onCompare,
  onUnpublish,
  onArchive,
}: {
  entry: EntryDetail;
  onPreview: () => void;
  onCompare: () => void;
  onUnpublish: () => void;
  onArchive: () => void;
}) {
  const state = entryPublishState(entry);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Действия с записью ${deriveEntryTitle(entryData(entry))}`}
        >
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuItem onSelect={onPreview} disabled={!entry.prod}>
          <Eye className="h-4 w-4" />
          Опубликованная версия
        </DropdownMenuItem>
        <DropdownMenuItem
          onSelect={onCompare}
          disabled={!entry.draft || !entry.prod}
        >
          <GitCompareArrows className="h-4 w-4" />
          Сравнить с черновиком
        </DropdownMenuItem>
        {state !== "archived" && (
          <>
            <DropdownMenuSeparator />
            {entry.prod_version_id && (
              <DropdownMenuItem
                className="text-destructive focus:text-destructive"
                onSelect={onUnpublish}
              >
                <EyeOff className="h-4 w-4" />
                Снять с публикации
              </DropdownMenuItem>
            )}
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onSelect={onArchive}
            >
              <Archive className="h-4 w-4" />
              В архив
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
