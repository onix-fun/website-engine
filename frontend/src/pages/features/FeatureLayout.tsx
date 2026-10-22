import { useQuery } from "@tanstack/react-query";
import {
  NavLink,
  Outlet,
  useLocation,
  useOutletContext,
  useParams,
  Link,
} from "react-router-dom";
import {
  ArrowLeft,
  Code2,
  FileText,
  Inbox,
  RefreshCw,
  Settings2,
  Layers,
} from "lucide-react";
import { api, ApiError } from "@/api";
import type { FeatureDetail } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import {
  EmptyState,
  ErrorState,
  LoadingPage,
  useDocumentTitle,
} from "@/components/common/page";
import {
  FeatureStatusBadge,
  FeatureTypeBadge,
  SchemaStateBadge,
} from "@/components/common/status-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface FeatureOutletContext {
  feature: FeatureDetail;
  refetch: () => void;
}

export function useFeatureContext() {
  const ctx = useOutletContext<FeatureOutletContext | null>();
  if (!ctx) {
    // Route misconfiguration: this page is rendered outside the FeatureLayout
    // outlet (e.g. wrapped in a route that creates its own <Outlet/>).
    throw new Error(
      "useFeatureContext: страница Feature отрендерена вне <Outlet context> FeatureLayout",
    );
  }
  return ctx;
}

function FeatureTabs({
  feature,
  activePath,
}: {
  feature: FeatureDetail;
  activePath: string;
}) {
  const tabs = [
    {
      to: `/features/${feature.id}/schema`,
      label: "Схема",
      icon: Code2,
      show: true,
    },
    {
      to: `/features/${feature.id}/content`,
      label: "Контент",
      icon: Layers,
      show: feature.type !== "form",
    },
    {
      to: `/features/${feature.id}/settings`,
      label: "Настройки",
      icon: Settings2,
      show: true,
    },
  ];
  const submissionsActive = activePath.startsWith("/submissions");

  return (
    <div className="flex items-center gap-1 overflow-x-auto border-b border-border pb-0">
      {tabs
        .filter((tab) => tab.show)
        .map((tab) => {
          const Icon = tab.icon;
          const active = activePath.startsWith(tab.to);
          return (
            <NavLink
              key={tab.to}
              to={tab.to}
              className={cn(
                "relative flex h-10 shrink-0 items-center gap-2 px-3 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
                active && "text-foreground",
              )}
            >
              <Icon className="h-4 w-4" />
              {tab.label}
              {active && (
                <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-primary" />
              )}
            </NavLink>
          );
        })}
      {feature.type === "form" && (
        <Link
          to={`/submissions?form=${encodeURIComponent(feature.key)}`}
          className={cn(
            "relative flex h-10 shrink-0 items-center gap-2 px-3 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
            submissionsActive && "text-foreground",
          )}
        >
          <Inbox className="h-4 w-4" />
          Заявки
          {submissionsActive && (
            <span className="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-primary" />
          )}
        </Link>
      )}
    </div>
  );
}

export default function FeatureLayout() {
  const { featureId } = useParams<{ featureId: string }>();
  const location = useLocation();
  const featureQuery = useQuery({
    queryKey: queryKeys.features.detail(featureId ?? ""),
    queryFn: ({ signal }) => api.features.get(featureId!, signal),
    enabled: Boolean(featureId),
  });

  useDocumentTitle(featureQuery.data?.name ?? "Feature");

  if (!featureId) {
    return (
      <div className="page-grid">
        <EmptyState title="Feature не найдена" description="Некорректный адрес раздела." />
      </div>
    );
  }

  if (featureQuery.isLoading) return <LoadingPage label="Загружаем Feature…" />;

  if (featureQuery.error) {
    if (featureQuery.error instanceof ApiError && featureQuery.error.status === 404) {
      return (
        <div className="page-grid">
          <EmptyState
            icon={FileText}
            title="Feature не найдена"
            description="Возможно, ссылка устарела или Feature была удалена."
            action={
              <Button asChild>
                <Link to="/features">К списку Features</Link>
              </Button>
            }
          />
        </div>
      );
    }
    return (
      <div className="page-grid">
        <ErrorState
          error={featureQuery.error}
          title="Не удалось загрузить Feature"
          onRetry={() => void featureQuery.refetch()}
        />
      </div>
    );
  }

  if (!featureQuery.data) return null;
  const feature = featureQuery.data;

  return (
    <div className="min-h-full">
      <div className="border-b border-border bg-card">
        <div className="mx-auto w-full max-w-[var(--content-max)] px-4 pt-4 sm:px-6 lg:px-8">
          <Link
            to="/features"
            className="mb-3 inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            Все Features
          </Link>
          <div className="flex flex-col gap-3 pb-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-xl font-semibold tracking-tight">
                  {feature.name}
                </h1>
                <FeatureTypeBadge type={feature.type} />
                <FeatureStatusBadge status={feature.status} />
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
                <code>{feature.key}</code>
                <SchemaStateBadge feature={feature} />
                <span>Ревизия r{feature.revision}</span>
                {feature.prod && (
                  <span>Опубликована схема v{feature.prod.version}</span>
                )}
                {feature.draft && (
                  <Badge variant="warning">Черновик схемы v{feature.draft.version}</Badge>
                )}
              </div>
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              {feature.type === "form" && (
                <Button variant="outline" size="sm" asChild>
                  <Link to={`/submissions?form=${encodeURIComponent(feature.key)}`}>
                    <Inbox />
                    Заявки
                  </Link>
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => void featureQuery.refetch()}
                loading={featureQuery.isFetching}
              >
                <RefreshCw />
                Обновить
              </Button>
            </div>
          </div>
          <FeatureTabs feature={feature} activePath={location.pathname} />
        </div>
      </div>

      <div className="mx-auto w-full max-w-[var(--content-max)] px-4 py-5 sm:px-6 sm:py-6 lg:px-8">
        <Outlet
          context={
            {
              feature,
              refetch: () => void featureQuery.refetch(),
            } satisfies FeatureOutletContext
          }
        />
      </div>
    </div>
  );
}
