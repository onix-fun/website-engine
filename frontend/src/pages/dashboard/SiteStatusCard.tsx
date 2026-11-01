import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { api } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/misc";
import type { SiteSnapshot } from "@/types";

const envLabels = { dev: "Разработка", prod: "Продакшен" } as const;

/** Active immutable site snapshots for the dashboard. */
export function SiteStatusSection() {
  const snapshots = useQuery({
    queryKey: ["site-snapshots"],
    queryFn: ({ signal }) => api.siteBuilder.snapshots(undefined, signal),
  });

  const findSnapshot = (id?: string): SiteSnapshot | undefined =>
    snapshots.data?.items.find((item) => item.id === id);

  return (
    <section aria-labelledby="site-status-title">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 id="site-status-title" className="text-sm font-semibold">Сайт и снапшоты</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Активная версия в каждом окружении
          </p>
        </div>
          <Link
            to="/site/snapshots"
            className="inline-flex min-h-11 items-center gap-1 rounded-sm text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            Управление сайтом
            <ArrowRight className="h-3.5 w-3.5" />
          </Link>
      </div>

      <ul className="mt-3 divide-y divide-border sm:grid sm:grid-cols-2 sm:divide-x sm:divide-y-0" aria-label="Окружения">
          {snapshots.isLoading ? (
            <>
              <li className="py-3 sm:pr-4"><Skeleton className="h-12 w-full" /></li>
              <li className="py-3 sm:pl-4"><Skeleton className="h-12 w-full" /></li>
            </>
          ) : snapshots.error ? (
            <li className="py-3 text-sm text-destructive sm:col-span-2">
              Не удалось загрузить статус сред. Попробуйте открыть раздел «Сайт».
            </li>
          ) : (
            (["dev", "prod"] as const).map((environment) => {
              const activeId = snapshots.data?.active[environment];
              const snapshot = snapshots.data?.activeSnapshots?.[environment]
                ?? (activeId ? findSnapshot(activeId) : undefined);
              return (
                <li key={environment} className="py-3 first:pt-3 sm:px-4 sm:first:pl-0 sm:last:pr-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-medium">
                      {envLabels[environment]}
                    </span>
                    <Badge variant={environment === "prod" ? "success" : "secondary"}>
                      {environment}
                    </Badge>
                  </div>
                  {snapshot ? (
                    <p className="mt-1 truncate text-xs text-muted-foreground">
                      #{snapshot.sequence} · {snapshot.name}
                    </p>
                  ) : (
                    <p className="mt-1 text-xs text-muted-foreground">
                      Снапшот не опубликован
                    </p>
                  )}
                </li>
              );
            })
          )}
      </ul>
    </section>
  );
}
