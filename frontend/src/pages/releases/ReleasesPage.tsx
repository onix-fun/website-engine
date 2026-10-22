import { useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RotateCcw, Settings2, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError, uploadRelease } from "@/api";
import { useAuth } from "@/app/auth";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ErrorState, LoadingPage, useDocumentTitle } from "@/components/common/page";
import { formatDateTime } from "@/lib/format";
import type { Environment } from "@/types";
import { SourcesCard } from "@/pages/releases/SourcesCard";

export default function ReleasesPage() {
  useDocumentTitle("Сайт и релизы");
  const auth = useAuth();
  const role = auth.principal?.role ?? "user";
  const canImport = role === "admin" || role === "moderator";
  const canActivateDev = role === "admin" || role === "moderator";
  const canActivateProd = role === "admin";
  const queryClient = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [version, setVersion] = useState("");
  const [tag, setTag] = useState("");
  const [progress, setProgress] = useState<number | null>(null);

  const releases = useQuery({
    queryKey: ["releases"],
    queryFn: ({ signal }) => api.releases.list(null, 100, signal),
  });
  const environments = useQuery({
    queryKey: ["site-environments"],
    queryFn: ({ signal }) => api.releases.environments(signal),
  });

  const activate = useMutation({
    mutationFn: ({ id, environment }: { id: string; environment: Environment }) =>
      api.releases.activate(id, environment),
    onSuccess: (_, input) => {
      toast.success(`Релиз активирован в ${input.environment}`);
      void queryClient.invalidateQueries({ queryKey: ["site-environments"] });
      void queryClient.invalidateQueries({ queryKey: ["releases"] });
    },
    onError: (error) => toast.error(isApiError(error) ? error.message : "Не удалось активировать релиз"),
  });

  const active = useMemo(() => {
    const map = new Map<string, string>();
    for (const env of environments.data?.items ?? []) {
      if (env.active_release_id) map.set(env.key, env.active_release_id);
    }
    return map;
  }, [environments.data]);

  // The release list is newest-first; the rollback target for an
  // environment is the release right after the currently active one.
  const previous = useMemo(() => {
    const map = new Map<string, string | null>();
    for (const [envKey, activeId] of active) {
      const index = releases.data?.items.findIndex((item) => item.id === activeId) ?? -1;
      map.set(envKey, index >= 0 ? (releases.data?.items[index + 1]?.id ?? null) : null);
    }
    return map;
  }, [active, releases.data]);

  const rollback = useMutation({
    mutationFn: ({ id, environment }: { id: string; environment: Environment }) =>
      api.releases.rollback(id, environment),
    onSuccess: (_, input) => {
      toast.success(`Откат в ${input.environment} выполнен`);
      void queryClient.invalidateQueries({ queryKey: ["site-environments"] });
      void queryClient.invalidateQueries({ queryKey: ["releases"] });
    },
    onError: (error) => toast.error(isApiError(error) ? error.message : "Не удалось откатить релиз"),
  });

  const upload = async () => {
    const file = fileRef.current?.files?.[0];
    if (!file) {
      toast.error("Выберите zip-архив релиза");
      return;
    }
    if (!version.trim()) {
      toast.error("Укажите версию релиза");
      return;
    }
    try {
      await uploadRelease(file, version.trim(), tag.trim(), setProgress);
      toast.success("Релиз импортирован");
      setVersion("");
      setTag("");
      if (fileRef.current) fileRef.current.value = "";
      void queryClient.invalidateQueries({ queryKey: ["releases"] });
    } catch (error) {
      toast.error(isApiError(error) ? error.message : "Не удалось импортировать релиз");
    } finally {
      setProgress(null);
    }
  };

  if (releases.isLoading) return <LoadingPage label="Загружаем релизы…" />;
  if (releases.error) {
    return <ErrorState error={releases.error} title="Не удалось загрузить релизы" onRetry={() => void releases.refetch()} />;
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Сайт и релизы</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Один неизменяемый артефакт используется в dev и prod. Активация переключает среду на выбранный релиз.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        {(environments.data?.items ?? []).map((env) => {
          const activeId = active.get(env.key);
          const rel = releases.data?.items.find((item) => item.id === activeId);
          return (
            <Card key={env.key}>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  {env.key}
                  <Badge variant={env.key === "prod" ? "success" : "secondary"}>{env.name}</Badge>
                </CardTitle>
                <CardDescription>{env.public_base_url || `порт ${env.http_port}`}</CardDescription>
              </CardHeader>
              <CardContent className="text-sm">
                {rel ? (
                  <p className="flex flex-wrap items-center gap-2">
                    <span>
                      Активен <strong>v{rel.version}</strong> · {formatDateTime(rel.created_at)}
                    </span>
                    {canActivateProd && previous.get(env.key) && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          rollback.mutate({
                            id: previous.get(env.key) as string,
                            environment: env.key,
                          })
                        }
                        loading={
                          rollback.isPending && rollback.variables?.environment === env.key
                        }
                      >
                        <RotateCcw />
                        Откатить
                      </Button>
                    )}
                  </p>
                ) : (
                  <p className="text-muted-foreground">Активный релиз не выбран</p>
                )}
                {canActivateProd && (
                  <Button
                    size="sm"
                    variant="ghost"
                    asChild
                    className="mt-2 px-0 text-muted-foreground"
                  >
                    <Link to={`/releases/environments/${env.key}`}>
                      <Settings2 />
                      Настроить среду
                    </Link>
                  </Button>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>

      {canImport && (
        <SourcesCard canEdit={canActivateProd} />
      )}

      {canImport && (
        <Card>
          <CardHeader>
            <CardTitle>Импорт релиза</CardTitle>
            <CardDescription>ZIP должен содержать index.html в корне.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <label className="flex-1 text-sm">
              <span className="mb-1 block text-xs text-muted-foreground">Версия</span>
              <Input value={version} onChange={(event) => setVersion(event.target.value)} placeholder="1.0.0" />
            </label>
            <label className="flex-1 text-sm">
              <span className="mb-1 block text-xs text-muted-foreground">Тег</span>
              <Input value={tag} onChange={(event) => setTag(event.target.value)} placeholder="front_v1.0.0" />
            </label>
            <label className="flex-1 text-sm">
              <span className="mb-1 block text-xs text-muted-foreground">Артефакт</span>
              <Input ref={fileRef} type="file" accept=".zip,application/zip" />
            </label>
            <Button onClick={() => void upload()} loading={progress !== null}>
              <Upload />
              {progress !== null ? `${progress}%` : "Импортировать"}
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Релизы</CardTitle>
        </CardHeader>
        <CardContent className="divide-y divide-border">
          {(releases.data?.items ?? []).length === 0 ? (
            <p className="py-4 text-sm text-muted-foreground">Релизов пока нет.</p>
          ) : (
            releases.data?.items.map((rel) => (
              <div key={rel.id} className="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">v{rel.version}</span>
                    {rel.tag && <Badge variant="muted">{rel.tag}</Badge>}
                    {active.get("dev") === rel.id && <Badge variant="secondary">dev</Badge>}
                    {active.get("prod") === rel.id && <Badge variant="success">prod</Badge>}
                  </div>
                  <p className="mt-1 truncate text-xs text-muted-foreground">
                    {formatDateTime(rel.created_at)} · {(rel.artifact_size / 1024 / 1024).toFixed(1)} MiB · {rel.artifact_sha256.slice(0, 12)}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!canActivateDev || active.get("dev") === rel.id}
                    onClick={() => activate.mutate({ id: rel.id, environment: "dev" })}
                  >
                    В dev
                  </Button>
                  <Button
                    size="sm"
                    disabled={!canActivateProd || active.get("prod") === rel.id}
                    onClick={() => activate.mutate({ id: rel.id, environment: "prod" })}
                  >
                    В prod
                  </Button>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  );
}
