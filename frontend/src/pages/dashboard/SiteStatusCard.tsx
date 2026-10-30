import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Package } from "lucide-react";
import { api } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/misc";
import type { SiteSnapshot } from "@/types";

const envLabels = { dev: "Разработка", prod: "Продакшен" } as const;

/** Active immutable site snapshots for the dashboard. */
export function SiteStatusCard() {
  const snapshots = useQuery({
    queryKey: ["site-snapshots"],
    queryFn: ({ signal }) => api.siteBuilder.snapshots(signal),
  });

  const findSnapshot = (id?: string): SiteSnapshot | undefined =>
    snapshots.data?.items.find((item) => item.id === id);

  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Package className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <div>
              <h2 className="text-sm font-semibold">Сайт и снапшоты</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">
                Активная версия в каждом окружении
              </p>
            </div>
          </div>
          <Link
            to="/site"
            className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            Управление сайтом
            <ArrowRight className="h-3.5 w-3.5" />
          </Link>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {snapshots.isLoading ? (
            <>
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </>
          ) : snapshots.error ? (
            <p className="text-sm text-destructive sm:col-span-2">
              Не удалось загрузить статус сред. Попробуйте открыть раздел «Сайт».
            </p>
          ) : (
            (["dev", "prod"] as const).map((environment) => {
              const activeId = snapshots.data?.active[environment];
              const snapshot = activeId ? findSnapshot(activeId) : undefined;
              return (
                <div
                  key={environment}
                  className="rounded-md border border-border px-3 py-2.5"
                >
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
                </div>
              );
            })
          )}
        </div>
      </CardContent>
    </Card>
  );
}
