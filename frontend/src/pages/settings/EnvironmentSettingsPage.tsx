import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, Save } from "lucide-react";
import { api, isApiError } from "@/api";
import { useDocumentTitle } from "@/components/common/page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/misc";
import type { Environment, SiteEnvironment } from "@/types";

const envLabels: Record<Environment, string> = {
  dev: "Разработка (dev)",
  prod: "Продакшен (prod)",
};

export default function EnvironmentSettingsPage() {
  useDocumentTitle("Настройки среды");
  const params = useParams<{ envKey: string }>();
  const envKey = (params.envKey === "prod" ? "prod" : "dev") as Environment;
  const queryClient = useQueryClient();

  const environments = useQuery({
    queryKey: ["site-environments"],
    queryFn: ({ signal }) => api.environments.list(signal),
  });
  const env = environments.data?.items.find((item) => item.key === envKey);

  if (environments.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (environments.error) {
    return (
      <div className="text-sm text-destructive">
        Не удалось загрузить среды. Попробуйте обновить страницу.
      </div>
    );
  }
  if (!env) {
    return <div className="text-sm text-muted-foreground">Среда не найдена.</div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="sm" asChild>
          <Link to="/settings">
            <ArrowLeft />
            К настройкам
          </Link>
        </Button>
      </div>
      <div>
        <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
          {envLabels[envKey]}
          <Badge variant={envKey === "prod" ? "success" : "secondary"}>{envKey}</Badge>
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Домены используются TLS-роутером, CORS-origins — публичным API этой среды.
        </p>
      </div>
      <EnvironmentForm
        key={env.updated_at}
        env={env}
        onSaved={() => {
          void queryClient.invalidateQueries({ queryKey: ["site-environments"] });
          toast.success("Настройки среды сохранены");
        }}
      />
    </div>
  );
}

function EnvironmentForm({ env, onSaved }: { env: SiteEnvironment; onSaved: () => void }) {
  const [name, setName] = useState(env.name);
  const [baseUrl, setBaseUrl] = useState(env.public_base_url);
  const [domains, setDomains] = useState(env.domains.join("\n"));
  const [origins, setOrigins] = useState(env.cors_origins.join("\n"));
  const [tls, setTls] = useState(env.tls_enabled);

  // A concurrent change bumps the revision: offer the freshest state instead
  // of asking the operator to re-type everything after a conflict.
  const [acknowledgedRevision, setAcknowledgedRevision] = useState(env.revision);
  useEffect(() => {
    if (env.revision !== acknowledgedRevision) setTls(env.tls_enabled);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [env.revision, env.updated_at]);

  const splitLines = (raw: string) =>
    raw
      .split(/[\n,]+/)
      .map((line) => line.trim())
      .filter(Boolean);

  const save = useMutation({
    mutationFn: () =>
      api.environments.update(env.key, {
        revision: env.revision,
        name: name.trim() || undefined,
        public_base_url: baseUrl.trim(),
        domains: splitLines(domains),
        cors_origins: splitLines(origins),
        tls_enabled: tls,
      }),
    onSuccess: (updated) => {
      setAcknowledgedRevision(updated.revision);
      onSaved();
    },
    onError: (error) => {
      if (isApiError(error) && error.status === 409) {
        toast.error(
          "Настройки среды изменены другим пользователем. Перечитайте форму и повторите.",
        );
        return;
      }
      toast.error(isApiError(error) ? error.message : "Не удалось сохранить настройки");
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Параметры среды</CardTitle>
        <CardDescription>
          Ревизия формы: {env.revision}. Изменённая другими пользователем среда потребует перечитать
          значения.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <label className="block max-w-md text-sm">
          <span className="mb-1 block text-xs text-muted-foreground">Название</span>
          <Input value={name} onChange={(event) => setName(event.target.value)} maxLength={200} />
        </label>
        <label className="block max-w-md text-sm">
          <span className="mb-1 block text-xs text-muted-foreground">Публичный base URL</span>
          <Input
            value={baseUrl}
            onChange={(event) => setBaseUrl(event.target.value)}
            placeholder="https://example.com"
          />
          <span className="mt-1 block text-xs text-muted-foreground">
            Используется в absolute-ссылках (медиа, письма). Оставьте пустым для относительных
            ссылок.
          </span>
        </label>
        <label className="block max-w-lg text-sm">
          <span className="mb-1 block text-xs text-muted-foreground">
            Домены TLS/SNI (по одному в строке)
          </span>
          <textarea
            className="min-h-20 w-full rounded-md border border-border bg-transparent px-3 py-2 text-sm"
            value={domains}
            onChange={(event) => setDomains(event.target.value)}
            placeholder={"example.com\nwww.example.com"}
          />
          <span className="mt-1 block text-xs text-muted-foreground">
            Домены не должны пересекаться между dev и prod. При включённом ACME для них выписываются
            сертификаты.
          </span>
        </label>
        <label className="block max-w-lg text-sm">
          <span className="mb-1 block text-xs text-muted-foreground">
            CORS-origins публичного API (по одному в строке)
          </span>
          <textarea
            className="min-h-20 w-full rounded-md border border-border bg-transparent px-3 py-2 text-sm"
            value={origins}
            onChange={(event) => setOrigins(event.target.value)}
            placeholder={"https://app.example\nhttps://editor.example"}
          />
          <span className="mt-1 block text-xs text-muted-foreground">
            Применяется без перезапуска. Пока список пуст, используются bootstrap-origins из
            конфигурации.
          </span>
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-primary"
            checked={tls}
            onChange={(event) => setTls(event.target.checked)}
          />
          HTTPS/ACME включён для этой среды
        </label>
        <div className="flex items-center gap-2">
          <Button onClick={() => save.mutate()} loading={save.isPending}>
            <Save />
            Сохранить
          </Button>
          <Button
            variant="outline"
            type="button"
            onClick={() => {
              setName(env.name);
              setBaseUrl(env.public_base_url);
              setDomains(env.domains.join("\n"));
              setOrigins(env.cors_origins.join("\n"));
              setTls(env.tls_enabled);
            }}
          >
            Сбросить
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
