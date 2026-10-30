import {
  useEffect,
  useMemo,
  useState,
} from "react";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import {
  Archive,
  ArrowRight,
  ChevronDown,
  Layers,
  MoreHorizontal,
  Plus,
  RotateCcw,
  Search,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import type { Feature, FeatureStatus, FeatureType } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import {
  EmptyState,
  ErrorState,
  PageHeader,
  TableSkeleton,
  useDocumentTitle,
} from "@/components/common/page";
import {
  FeatureStatusBadge,
  FeatureTypeBadge,
} from "@/components/common/status-badges";
import { useConfirm } from "@/components/common/confirm";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { featureStatusLabels, featureTypeLabels, formatDateTime } from "@/lib/format";

type SortOption = "updated_desc" | "updated_asc" | "name_asc" | "name_desc";

const statusOptions: Array<{ value: FeatureStatus | "all"; label: string }> = [
  { value: "active", label: "Активные" },
  { value: "archived", label: "В архиве" },
  { value: "all", label: "Все статусы" },
];

function useFeatureFilterParams() {
  const [searchParams, setSearchParams] = useSearchParams();
  const status = (searchParams.get("status") ?? "active") as FeatureStatus | "all";
  const type = (searchParams.get("type") ?? "all") as FeatureType | "all";
  const sort = (searchParams.get("sort") ?? "updated_desc") as SortOption;
  const q = searchParams.get("q") ?? "";

  const update = (values: Record<string, string | null>, replace = false) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        Object.entries(values).forEach(([key, value]) => {
          if (
            value === null ||
            value === "" ||
            value === "all" ||
            (key === "status" && value === "active")
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

  return { status, type, sort, q, update };
}

function FeatureActions({ feature }: { feature: Feature }) {
  const queryClient = useQueryClient();
  const confirm = useConfirm();

  const archiveMutation = useMutation({
    mutationFn: () => api.features.archive(feature.id),
    onSuccess: () => {
      toast.success(`Раздел «${feature.name}» перемещён в архив`);
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "Не удалось архивировать раздел"),
  });

  const requestArchive = async () => {
    const confirmed = await confirm({
      title: "Архивировать раздел?",
      description: (
        <>
          Раздел <strong>«{feature.name}»</strong> будет перемещён в архив. Редактирование
          и публикация будут заблокированы. Вернуть её из архива через интерфейс
          нельзя.
        </>
      ),
      confirmLabel: "Архивировать",
      destructive: true,
    });
    if (confirmed) archiveMutation.mutate();
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`Действия с ${feature.name}`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        {feature.type !== "form" && (
          <DropdownMenuItem asChild>
            <Link to={`/features/${feature.id}/content`}>
              <Layers className="h-4 w-4" />
              Контент
            </Link>
          </DropdownMenuItem>
        )}
        {feature.type === "form" && (
          <DropdownMenuItem asChild>
            <Link to={`/submissions?form=${encodeURIComponent(feature.key)}`}>
              <Layers className="h-4 w-4" />
              Заявки формы
            </Link>
          </DropdownMenuItem>
        )}
        {feature.status === "active" && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onSelect={() => void requestArchive()}
              disabled={archiveMutation.isPending}
            >
              <Archive className="h-4 w-4" />
              Архивировать
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function FeatureRow({ feature }: { feature: Feature }) {
  return (
    <>
      <tr className="group">
        <td>
          <div className="min-w-0">
            <Link
              to={`/features/${feature.id}/content`}
              className="block truncate text-sm font-medium hover:text-primary"
            >
              {feature.name}
            </Link>
            <code className="mt-0.5 block truncate text-2xs text-muted-foreground">
              {feature.key}
            </code>
          </div>
        </td>
        <td>
          <FeatureTypeBadge type={feature.type} />
        </td>
        <td>
          <FeatureStatusBadge status={feature.status} />
        </td>
        <td className="text-xs text-muted-foreground">
          {formatDateTime(feature.updated_at)}
        </td>
        <td>
          <div className="flex items-center justify-end gap-1">
            <Button variant="ghost" size="sm" asChild>
              <Link to={`/features/${feature.id}/content`}>
                Открыть
                <ArrowRight />
              </Link>
            </Button>
            <FeatureActions feature={feature} />
          </div>
        </td>
      </tr>
    </>
  );
}

function FeatureCard({ feature }: { feature: Feature }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <Link
            to={`/features/${feature.id}/content`}
            className="block truncate text-sm font-semibold hover:text-primary"
          >
            {feature.name}
          </Link>
          <code className="mt-0.5 block truncate text-2xs text-muted-foreground">
            {feature.key}
          </code>
        </div>
        <FeatureActions feature={feature} />
      </div>
      <div className="mt-3 flex flex-wrap gap-1.5">
        <FeatureTypeBadge type={feature.type} />
        <FeatureStatusBadge status={feature.status} />
      </div>
      <div className="mt-3 flex items-center justify-between border-t border-border pt-3">
        <span className="text-2xs text-muted-foreground">
          {formatDateTime(feature.updated_at)}
        </span>
        <Button variant="ghost" size="sm" asChild>
          <Link to={`/features/${feature.id}/content`}>Открыть</Link>
        </Button>
      </div>
    </div>
  );
}

export default function FeaturesPage() {
  useDocumentTitle("Фичи");
  const { status, type, sort, q, update } = useFeatureFilterParams();
  const [searchInput, setSearchInput] = useState(q);
  const debouncedSearch = useDebouncedValue(searchInput, 250);

  useEffect(() => {
    setSearchInput(q);
  }, [q]);

  useEffect(() => {
    if (debouncedSearch !== q) {
      update({ q: debouncedSearch || null }, true);
    }
    // update is intentionally stable enough for this URL-sync effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const featuresQuery = useInfiniteQuery({
    queryKey: queryKeys.features.list({ status }),
    queryFn: ({ pageParam, signal }) =>
      api.features.list(
        {
          status: status === "all" ? "" : status,
          limit: 50,
          cursor: pageParam ?? undefined,
        },
        signal,
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor,
  });

  const loadedFeatures = useMemo(
    () => featuresQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [featuresQuery.data],
  );

  const features = useMemo(() => {
    const query = q.trim().toLowerCase();
    const list = loadedFeatures.filter((feature) => {
      if (type !== "all" && feature.type !== type) return false;
      if (query && !`${feature.name} ${feature.key}`.toLowerCase().includes(query)) {
        return false;
      }
      return true;
    });
    return list.sort((a, b) => {
      if (sort === "name_asc") return a.name.localeCompare(b.name, "ru");
      if (sort === "name_desc") return b.name.localeCompare(a.name, "ru");
      if (sort === "updated_asc") {
        return new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime();
      }
      return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
    });
  }, [loadedFeatures, q, sort, type]);

  const filtersActive =
    Boolean(q) || type !== "all" || sort !== "updated_desc" || status !== "active";

  const resetFilters = () => {
    setSearchInput("");
    update({ q: null, type: null, sort: null, status: null });
  };

  return (
    <div className="page-grid">
      <PageHeader
        title="Фичи"
        description="Контентные модели, зарегистрированные из feature-манифестов. Состав сайта и версии сборок настраиваются в разделе «Сайт»."
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/submissions">Заявки</Link>
            </Button>
            <Button asChild>
              <Link to="/site/features">
                <Plus />
                Сборки фич и страницы
              </Link>
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
              placeholder="Поиск по названию и ключу"
              aria-label="Поиск разделов"
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
              value={status}
              onValueChange={(value) => update({ status: value === "active" ? null : value })}
            >
              <SelectTrigger className="w-full sm:w-36" aria-label="Статус раздела">
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
            <Select value={type} onValueChange={(value) => update({ type: value })}>
              <SelectTrigger className="w-full sm:w-36" aria-label="Тип раздела">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Все типы</SelectItem>
                {(Object.keys(featureTypeLabels) as FeatureType[]).map((featureType) => (
                  <SelectItem key={featureType} value={featureType}>
                    {featureTypeLabels[featureType]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(value) => update({ sort: value })}>
              <SelectTrigger className="w-full sm:w-40" aria-label="Сортировка">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="updated_desc">Сначала новые</SelectItem>
                <SelectItem value="updated_asc">Сначала старые</SelectItem>
                <SelectItem value="name_asc">По названию А–Я</SelectItem>
                <SelectItem value="name_desc">По названию Я–А</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>
            Загружено: {loadedFeatures.length}
            {featuresQuery.hasNextPage ? " (есть ещё)" : ""} · найдено: {features.length}.
            Поиск и фильтры действуют на загруженной странице.
          </span>
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={resetFilters}>
              <RotateCcw />
              Сбросить фильтры
            </Button>
          )}
        </div>
      </div>

      {featuresQuery.isLoading ? (
        <div className="space-y-3">
          <div className="hidden overflow-hidden rounded-lg border border-border bg-card p-4 md:block">
            <TableSkeleton rows={6} />
          </div>
          <div className="grid gap-3 md:hidden">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-36" />
            ))}
          </div>
        </div>
      ) : featuresQuery.isError ? (
        <ErrorState
          error={featuresQuery.error}
          title="Не удалось загрузить разделы"
          onRetry={() => void featuresQuery.refetch()}
        />
      ) : loadedFeatures.length === 0 ? (
        <EmptyState
          icon={Layers}
          title={filtersActive ? "Разделы не найдены" : "Пока нет ни одного раздела"}
          description={
            filtersActive
              ? "По выбранным условиям ничего нет. Сбросьте фильтры или измените запрос."
              : "Контентные фичи появятся здесь после размещения в активном dev-снапшоте."
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
      ) : features.length === 0 ? (
        <EmptyState
          icon={Search}
          title="Ничего не найдено"
          description="Среди загруженных разделов нет подходящих. Очистите поиск или загрузите следующую страницу."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button variant="outline" onClick={resetFilters}>
                Сбросить фильтры
              </Button>
              {featuresQuery.hasNextPage && (
                <Button onClick={() => void featuresQuery.fetchNextPage()}>
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
                  <th scope="col">Название и ключ</th>
                  <th scope="col">Тип</th>
                  <th scope="col">Статус</th>
                  <th scope="col">Изменена</th>
                  <th scope="col" className="text-right">
                    Действия
                  </th>
                </tr>
              </thead>
              <tbody>
                {features.map((feature) => (
                  <FeatureRow key={feature.id} feature={feature} />
                ))}
              </tbody>
            </table>
          </div>

          <div className="grid gap-3 md:hidden">
            {features.map((feature) => (
              <FeatureCard key={feature.id} feature={feature} />
            ))}
          </div>

          <div className="mt-4 flex flex-col items-center justify-center gap-2">
            {featuresQuery.hasNextPage ? (
              <Button
                variant="outline"
                loading={featuresQuery.isFetchingNextPage}
                onClick={() => void featuresQuery.fetchNextPage()}
              >
                <ChevronDown />
                Загрузить ещё
              </Button>
            ) : (
              <p className="text-2xs text-muted-foreground">
                Все загруженные разделы показаны
              </p>
            )}
            {status === "archived" && (
              <Badge variant="muted">
                {featureStatusLabels.archived}: {loadedFeatures.length}
              </Badge>
            )}
          </div>
        </>
      )}
    </div>
  );
}
