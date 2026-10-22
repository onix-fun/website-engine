import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Package } from "lucide-react";
import { api } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/misc";
import { formatDateTime } from "@/lib/format";
import type { Environment, SiteRelease } from "@/types";

const envLabels: Record<Environment, string> = {
  dev: "Разработка",
  prod: "Продакшен",
};

/** Releases + environments summary for the dashboard. */
export function SiteStatusCard() {
  const environments = useQuery({
    queryKey: ["site-environments"],
    queryFn: ({ signal }) => api.releases.environments(signal),
  });
  const releases = useQuery({
    queryKey: ["releases"],
    queryFn: ({ signal }) => api.releases.list(null, 100, signal),
  });

  const findRelease = (id?: string | null): SiteRelease | undefined =>
    releases.data?.items.find((item) => item.id === id);

  const loading = environments.isLoading || releases.isLoading;

  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Package className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <div>
              <h2 className="text-sm font-semibold">Сайт и релизы</h2>
              <p className="mt-0.5 text-xs text-muted-foreground">
                Активные версии в каждой среде
              </p>
            </div>
          </div>
          <Link
            to="/releases"
            className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            Все релизы
            <ArrowRight className="h-3.5 w-3.5" />
          </Link>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          {loading ? (
            <>
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </>
          ) : environments.error || releases.error ? (
            <p className="text-sm text-destructive sm:col-span-2">
              Не удалось загрузить статус сред. Попробуйте открыть раздел «Сайт».
            </p>
          ) : (
            (environments.data?.items ?? []).map((env) => {
              const release = findRelease(env.active_release_id);
              return (
                <div
                  key={env.key}
                  className="rounded-md border border-border px-3 py-2.5"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-medium">
                      {envLabels[env.key] ?? env.key}
                    </span>
                    <Badge variant={env.key === "prod" ? "success" : "secondary"}>
                      {env.key}
                    </Badge>
                  </div>
                  {release ? (
                    <p className="mt-1 truncate text-xs text-muted-foreground">
                      v{release.version}
                      {release.tag ? ` · ${release.tag}` : ""} ·{" "}
                      {formatDateTime(release.created_at)}
                    </p>
                  ) : (
                    <p className="mt-1 text-xs text-muted-foreground">
                      Активный релиз не выбран
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
