import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CircleDashed,
  FileText,
  Image,
  Inbox,
  Layers,
  Plus,
  Sparkles,
} from "lucide-react";
import { api, ApiError } from "@/api";
import { useAuth } from "@/app/auth";
import type { OverviewActivity, OverviewCounts } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import { formatDateTime, pluralRu } from "@/lib/format";
import {
  EmptyState,
  ErrorState,
  PageHeader,
  useDocumentTitle,
} from "@/components/common/page";
import { SubmissionStatusBadge } from "@/components/common/status-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/misc";
import { SiteStatusSection } from "@/pages/dashboard/SiteStatusCard";

const statDefinitions: Array<{
  key: keyof OverviewCounts;
  label: string;
  description: string;
  icon: typeof Layers;
  to: string;
}> = [
  {
    key: "features_active",
    label: "Активные разделы",
    description: "Доступные разделы контента",
    icon: Layers,
    to: "/content",
  },
  {
    key: "draft_features",
    label: "Черновики контента",
    description: "Контент, ожидающий публикации",
    icon: CircleDashed,
    to: "/content",
  },
  {
    key: "new_submissions",
    label: "Новые заявки",
    description: "Ожидают обработки",
    icon: Inbox,
    to: "/submissions?status=new",
  },
  {
    key: "media_ready",
    label: "Готовые медиа",
    description: "Доступны в медиатеке",
    icon: Image,
    to: "/media?status=ready",
  },
];

const activityMeta: Record<
  OverviewActivity["kind"],
  { icon: typeof Layers; label: string; to: string }
> = {
  feature: { icon: Layers, label: "Раздел", to: "/content" },
  entry: { icon: FileText, label: "Запись", to: "/content" },
  submission: { icon: Inbox, label: "Заявка", to: "/submissions" },
  media: { icon: Image, label: "Файлы", to: "/media" },
};

function activityTitle(item: OverviewActivity): string {
  if (item.kind === "feature" && item.title.startsWith("Feature:")) {
    return `Раздел:${item.title.slice("Feature:".length)}`;
  }
  return item.title;
}

function OverviewSkeleton() {
  return (
    <div className="space-y-6" aria-busy="true">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Skeleton key={index} className="h-28 w-full" />
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    </div>
  );
}

export default function DashboardPage() {
  useDocumentTitle("Главная");
  const auth = useAuth();
  const canManageSite = auth.principal?.role === "admin" || auth.principal?.role === "moderator";
  const overview = useQuery({
    queryKey: queryKeys.overview,
    queryFn: ({ signal }) => api.overview.get(signal),
    retry: false,
  });

  const unavailable =
    overview.error instanceof ApiError && overview.error.status === 404;

  const counts = overview.data?.counts;
  const attention = overview.data?.attention;
  const activity = overview.data?.recent_activity ?? [];
  const attentionTotal = attention
    ? attention.draft_features.length +
      attention.new_submissions.length +
      attention.failed_media.length
    : 0;
  const hasAnyFeature =
    counts !== undefined &&
    counts.features_active + counts.features_archived > 0;
  const availableSections = [
    ["Контент", "Фичи и записи", "/content"],
    ["Заявки", "Обработка форм", "/submissions"],
    ["Файлы", "Медиатека", "/media"],
    ...(canManageSite
      ? [
          ["Сайт", "Роутер, страницы и снапшоты", "/site/pages"],
          ["Доступы", "Пользователи и роли", "/access/tokens"],
        ]
      : []),
  ];

  return (
    <div className="page-grid">
      <PageHeader
        title="Главная"
        description="Сборки фич, контент, заявки и публикации сайта в dev и prod."
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/content">
                <Layers />
                Контент
              </Link>
            </Button>
            {canManageSite && (
              <Button asChild>
                <Link to="/site/pages">
                  <Plus />
                  Сборки и страницы
                </Link>
              </Button>
            )}
          </>
        }
      />

      {overview.isLoading ? (
        <OverviewSkeleton />
      ) : overview.error ? (
        <>
          {unavailable ? (
            <div className="space-y-6">
              <div className="rounded-lg border border-warning/30 bg-warning/5 p-5">
                <div className="flex gap-3">
                  <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning" />
                  <div>
                    <h2 className="text-sm font-semibold">Сводка недоступна</h2>
                    <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">
                      Endpoint <code className="text-xs">GET /api/v1/admin/overview</code>{" "}
                      ещё не подключён на сервере. Мы не показываем вымышленные
                      цифры и активность. Разделы CMS при этом полностью доступны.
                    </p>
                    <div className="mt-4 flex flex-wrap gap-2">
                      <Button asChild size="sm">
                        <Link to="/content">Открыть контент</Link>
                      </Button>
                      <Button asChild size="sm" variant="outline">
                        <Link to="/submissions">Открыть заявки</Link>
                      </Button>
                      <Button asChild size="sm" variant="outline">
                        <Link to="/media">Открыть медиатеку</Link>
                      </Button>
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
                <EmptyState
                  icon={Sparkles}
                  title="Работайте с контентом"
                  description="Откройте фичу из dev-снапшота, заполните её поля и опубликуйте контент."
                  action={
                    <Button asChild>
                      <Link to="/content">
                        <FileText />
                        Открыть контент
                      </Link>
                    </Button>
                  }
                  className="bg-card"
                />
                <Card>
                  <CardContent className="p-5">
                    <h2 className="text-sm font-semibold">Доступные разделы</h2>
                    <div className="mt-3 space-y-2">
                      {availableSections.map(([title, text, to]) => (
                        <Link
                          key={to}
                          to={to}
                          className="flex items-center justify-between rounded-md border border-border px-3 py-2.5 text-sm transition-colors hover:bg-muted/50"
                        >
                          <span>
                            <span className="font-medium">{title}</span>
                            <span className="ml-2 text-xs text-muted-foreground">{text}</span>
                          </span>
                          <ArrowRight className="h-4 w-4 text-muted-foreground" />
                        </Link>
                      ))}
                    </div>
                  </CardContent>
                </Card>
              </div>
            </div>
          ) : (
            <ErrorState
              error={overview.error}
              title="Не удалось загрузить сводку"
              onRetry={() => void overview.refetch()}
            />
          )}
        </>
      ) : overview.data ? (
        <div className="space-y-6">
          <section
            aria-label="Операционные показатели"
            className="grid gap-x-5 gap-y-4 border-y border-border py-4 sm:grid-cols-2 xl:grid-cols-4"
          >
            {statDefinitions.map((stat) => {
              const Icon = stat.icon;
              return (
                <Link
                  key={stat.key}
                  to={stat.to}
                  className="group rounded-sm transition-colors hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                      <Icon className="h-3.5 w-3.5" aria-hidden="true" />
                      {stat.label}
                    </div>
                    <ArrowRight className="h-4 w-4 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5" />
                  </div>
                  <p className="mt-2 text-2xl font-semibold tabular-nums">
                    {overview.data.counts[stat.key]}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">{stat.description}</p>
                </Link>
              );
            })}
          </section>

          {canManageSite && <SiteStatusSection />}

          <div className="grid gap-4 xl:grid-cols-[1.25fr_1fr]">
            <section className="rounded-lg border border-border bg-card" aria-labelledby="attention-title">
              <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3 sm:px-5">
                <div>
                  <h2 id="attention-title" className="text-sm font-semibold">
                    Требует внимания
                  </h2>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    Незавершённые или проблемные операции
                  </p>
                </div>
                <Badge variant={attentionTotal > 0 ? "warning" : "success"}>
                  {attentionTotal > 0 ? attentionTotal : "Нет задач"}
                </Badge>
              </div>
              <div className="divide-y divide-border">
                {attentionTotal === 0 && (
                  <div className="flex items-center gap-3 px-4 py-6 sm:px-5">
                    <CheckCircle2 className="h-5 w-5 text-success" />
                    <div>
                      <p className="text-sm font-medium">Всё под контролем</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        Нет черновиков без публикации, новых заявок или ошибок медиа.
                      </p>
                    </div>
                  </div>
                )}

                {attention?.draft_features.map((feature) => (
                  <Link
                    key={`feature-${feature.id}`}
                    to={`/features/${feature.id}/content`}
                    className="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/40 sm:px-5"
                  >
                    <CircleDashed className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{feature.name}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        Черновик схемы · <code>{feature.key}</code> ·{" "}
                        {formatDateTime(feature.updated_at)}
                      </p>
                    </div>
                    <div className="hidden sm:block">
                      <Badge variant="warning">Черновик схемы</Badge>
                    </div>
                  </Link>
                ))}

                {attention?.new_submissions.map((submission) => (
                  <Link
                    key={`submission-${submission.id}`}
                    to={`/submissions/${submission.id}?status=new`}
                    className="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/40 sm:px-5"
                  >
                    <Inbox className="mt-0.5 h-4 w-4 shrink-0 text-info" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">
                        {submission.data_preview || "Новая заявка"}
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        <code>{submission.feature_key}</code> ·{" "}
                        {formatDateTime(submission.created_at)}
                      </p>
                    </div>
                    <SubmissionStatusBadge status={submission.status} />
                  </Link>
                ))}

                {attention?.failed_media.map((media) => (
                  <Link
                    key={`media-${media.id}`}
                    to="/media?status=failed"
                    className="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/40 sm:px-5"
                  >
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">
                        {media.original_filename}
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        Ошибка обработки · {formatDateTime(media.created_at)}
                      </p>
                    </div>
                    <Badge variant="destructive">failed</Badge>
                  </Link>
                ))}
              </div>
            </section>

            <section className="rounded-lg border border-border bg-card" aria-labelledby="activity-title">
              <div className="border-b border-border px-4 py-3 sm:px-5">
                <h2 id="activity-title" className="text-sm font-semibold">
                  Последняя активность
                </h2>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  Реальные события из API сводки
                </p>
              </div>
              {activity.length === 0 ? (
                <p className="px-4 py-6 text-sm text-muted-foreground sm:px-5">
                  Активности пока нет.
                </p>
              ) : (
                <ul className="divide-y divide-border">
                  {activity.slice(0, 12).map((item, index) => {
                    const meta = activityMeta[item.kind];
                    const Icon = meta.icon;
                    return (
                      <li key={`${item.kind}-${item.occurred_at}-${index}`}>
                        <Link
                          to={meta.to}
                          className="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/40 sm:px-5"
                        >
                          <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted/40">
                            <Icon className="h-3.5 w-3.5 text-muted-foreground" />
                          </span>
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm">{activityTitle(item)}</span>
                            <span className="mt-0.5 block text-xs text-muted-foreground">
                              {meta.label}
                              {item.feature_key ? (
                                <>
                                  {" · "}
                                  <code>{item.feature_key}</code>
                                </>
                              ) : null}
                              {" · "}
                              {formatDateTime(item.occurred_at)}
                            </span>
                          </span>
                        </Link>
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          </div>

          {!hasAnyFeature && canManageSite && (
            <section className="border-t border-border pt-4" aria-labelledby="quick-start-title">
              <h2 id="quick-start-title" className="text-sm font-semibold">Быстрый старт</h2>
              <ol className="mt-3 grid gap-x-5 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
                {[
                  ["1", "Загрузить фичи", "Импортировать ZIP-сборки с манифестами", "/site/features"],
                  ["2", "Собрать страницы", "Настроить маршруты и размещения", "/site/pages"],
                  ["3", "Добавить контент", "Заполнить поля из манифестов", "/content"],
                  ["4", "Опубликовать снапшот", "Проверить dev и продвинуть в prod", "/site/snapshots"],
                ].map(([step, title, text, to]) => (
                  <li key={step}>
                    <Link
                      to={to}
                      className="group block h-full rounded-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      <span className="font-mono text-xs text-muted-foreground group-hover:text-primary">
                        {step}
                      </span>
                      <span className="mt-1 block text-sm font-medium group-hover:text-primary">{title}</span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">{text}</span>
                    </Link>
                  </li>
                ))}
              </ol>
            </section>
          )}
        </div>
      ) : null}

      {counts && (
        <p className="mt-6 text-2xs text-muted-foreground">
          Всего разделов: {counts.features_active + counts.features_archived} ·{" "}
          {counts.draft_entries}{" "}
          {pluralRu(counts.draft_entries, ["черновик записи", "черновика записей", "черновиков записей"])}{" "}
          · ошибок медиа: {counts.failed_media}
        </p>
      )}
    </div>
  );
}
