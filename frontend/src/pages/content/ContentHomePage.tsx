import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Layers } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "@/api";
import { ErrorState, PageHeader, useDocumentTitle } from "@/components/common/page";
import { FeatureTypeBadge } from "@/components/common/status-badges";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/misc";
import { queryKeys } from "@/lib/query-keys";

export default function ContentHomePage() {
  useDocumentTitle("Контент");
  const catalog = useQuery({
    queryKey: queryKeys.content.catalog,
    queryFn: ({ signal }) => api.siteBuilder.contentFeatures(signal),
  });
  const items = catalog.data?.items ?? [];

  return (
    <div className="page-grid">
      <PageHeader
        title="Контент"
        description="Редактируйте тексты, изображения и записи, которые уже размещены в активном dev-снапшоте."
      />

      {catalog.isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="h-36" />)}
        </div>
      ) : catalog.isError ? (
        <ErrorState error={catalog.error} title="Не удалось загрузить контент" onRetry={() => void catalog.refetch()} />
      ) : items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border bg-card/60 px-6 py-12 text-center">
          <Layers className="mx-auto h-8 w-8 text-muted-foreground" aria-hidden="true" />
          <h2 className="mt-3 text-base font-semibold">Пока нет доступного контента</h2>
          <p className="mx-auto mt-1 max-w-lg text-sm text-muted-foreground">
            Добавьте фичу на страницу и создайте dev-снапшот.
          </p>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {items.map((item) => (
            <article key={item.id} className="flex min-h-36 flex-col rounded-xl border border-border bg-card p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="truncate text-sm font-semibold">{item.name}</h2>
                  <code className="mt-1 block truncate text-xs text-muted-foreground">{item.key}</code>
                </div>
                <FeatureTypeBadge type={item.mode} />
              </div>
              <div className="mt-auto flex items-center justify-between pt-5">
                <span className="text-xs text-muted-foreground">Сборка {item.version}</span>
                <Button size="sm" variant="ghost" asChild>
                  <Link to={`/features/${item.id}/content`}>
                    Открыть <ArrowRight />
                  </Link>
                </Button>
              </div>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
