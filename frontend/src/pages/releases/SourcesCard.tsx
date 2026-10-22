import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Loader2, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { errorMessage } from "@/lib/errors";
import { formatDateTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ReleaseSource, RemoteRelease, SiteRelease } from "@/types";

const providerLabels: Record<ReleaseSource["provider"], string> = {
  manual: "Ручная загрузка",
  github: "GitHub Releases",
  gitlab: "GitLab Releases",
  gitea: "Gitea Releases",
};

export function SourcesCard({ canEdit }: { canEdit: boolean }) {
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [browsing, setBrowsing] = useState<ReleaseSource | null>(null);

  const sources = useQuery({
    queryKey: ["release-sources"],
    queryFn: ({ signal }) => api.releaseSources.list(signal),
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.releaseSources.remove(id),
    onSuccess: () => {
      toast.success("Источник удалён");
      void queryClient.invalidateQueries({ queryKey: ["release-sources"] });
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-2">
          Источники релизов
          {canEdit && (
            <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
              Добавить источник
            </Button>
          )}
        </CardTitle>
        <CardDescription>
          Git-провайдеры (GitHub/GitLab/Gitea) отдают релизы с тегами. Токен шифруется
          перед сохранением.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-2">
        {sources.isLoading ? (
          <p className="py-3 text-sm text-muted-foreground">Загружаем источники…</p>
        ) : sources.error ? (
          <p className="py-3 text-sm text-destructive">
            Не удалось загрузить источники: {errorMessage(sources.error)}
          </p>
        ) : (sources.data?.items ?? []).length === 0 ? (
          <p className="py-3 text-sm text-muted-foreground">
            Источников пока нет. Добавьте репозиторий, чтобы импортировать релизы прямо
            из него.
          </p>
        ) : (
          sources.data?.items.map((source) => (
            <div
              key={source.id}
              className="flex flex-col gap-2 rounded-md border border-border px-3 py-2 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant="muted">{providerLabels[source.provider]}</Badge>
                  <span className="truncate text-sm">{source.repo_url}</span>
                  {source.has_credential && <Badge variant="secondary">с токеном</Badge>}
                  {!source.is_enabled && <Badge variant="warning">выключен</Badge>}
                </div>
                <p className="mt-1 truncate text-xs text-muted-foreground">
                  Шаблон артефакта: {source.asset_pattern}
                </p>
              </div>
              {canEdit && (
                <div className="flex shrink-0 gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setBrowsing(source)}
                  >
                    <RefreshCw />
                    Релизы источника
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    aria-label="Удалить источник"
                    onClick={() => {
                      if (
                        window.confirm(
                          "Удалить источник? Архивы уже импортированных релизов останутся.",
                        )
                      )
                        remove.mutate(source.id);
                    }}
                    loading={remove.isPending && remove.variables === source.id}
                  >
                    <Trash2 />
                  </Button>
                </div>
              )}
            </div>
          ))
        )}
      </CardContent>

      <CreateSourceDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false);
          void queryClient.invalidateQueries({ queryKey: ["release-sources"] });
        }}
      />

      {browsing && browsing.provider !== "manual" && (
        <SourceReleasesDialog
          source={browsing}
          onClose={() => setBrowsing(null)}
          onImported={(release) => {
            void queryClient.invalidateQueries({ queryKey: ["releases"] });
            void queryClient.invalidateQueries({ queryKey: ["site-environments"] });
            toast.success(`Релиз v${release.version} импортирован`);
            setBrowsing(null);
          }}
        />
      )}
    </Card>
  );
}

function CreateSourceDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [provider, setProvider] = useState<ReleaseSource["provider"]>("github");
  const [repoUrl, setRepoUrl] = useState("");
  const [assetPattern, setAssetPattern] = useState("dist-.+\\.zip");
  const [token, setToken] = useState("");

  const create = useMutation({
    mutationFn: () =>
      api.releaseSources.create({
        provider,
        repo_url: repoUrl.trim(),
        asset_pattern: assetPattern.trim(),
        token: token.trim() || undefined,
        is_enabled: true,
      }),
    onSuccess: onCreated,
    onError: (error) => toast.error(errorMessage(error), { duration: 6000 }),
  });

  if (!open) return null;

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Новый источник релизов</DialogTitle>
          <DialogDescription>
            URL репозитория — тот же формат, что и в браузере, например
            https://github.com/owner/repo.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label className="block text-sm">
            <span className="mb-1 block text-xs text-muted-foreground">Провайдер</span>
            <Select value={provider} onValueChange={(value) => setProvider(value as ReleaseSource["provider"])}>
              <SelectTrigger aria-label="Провайдер">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="github">GitHub Releases</SelectItem>
                <SelectItem value="gitlab">GitLab Releases</SelectItem>
                <SelectItem value="gitea">Gitea Releases</SelectItem>
              </SelectContent>
            </Select>
          </label>
          <label className="block text-sm">
            <span className="mb-1 block text-xs text-muted-foreground">URL репозитория</span>
            <Input
              value={repoUrl}
              onChange={(event) => setRepoUrl(event.target.value)}
              placeholder="https://github.com/owner/repo"
              autoFocus
            />
          </label>
          <label className="block text-sm">
            <span className="mb-1 block text-xs text-muted-foreground">
              Шаблон имени артефакта (regex)
            </span>
            <Input
              value={assetPattern}
              onChange={(event) => setAssetPattern(event.target.value)}
              placeholder="dist-.+\.zip"
            />
            <span className="mt-1 block text-xs text-muted-foreground">
              По шаблону выбирается актив, который импортируется как zip-релиз.
            </span>
          </label>
          <label className="block text-sm">
            <span className="mb-1 block text-xs text-muted-foreground">
              Токен доступа (необязательно)
            </span>
            <Input
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="ghp_…"
              autoComplete="new-password"
            />
            <span className="mt-1 block text-xs text-muted-foreground">
              Хранится зашифрованным и никогда не возвращается в API.
            </span>
          </label>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>
              Отмена
            </Button>
            <Button
              onClick={() => create.mutate()}
              loading={create.isPending}
              disabled={!repoUrl.trim()}
            >
              Создать
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SourceReleasesDialog({
  source,
  onClose,
  onImported,
}: {
  source: ReleaseSource;
  onClose: () => void;
  onImported: (release: SiteRelease) => void;
}) {
  const releases = useQuery({
    queryKey: ["source-releases", source.id],
    queryFn: ({ signal }) => api.releaseSources.remoteReleases(source.id, signal),
  });

  const defaultAsset = (release: RemoteRelease): string | null => {
    const pattern = new RegExp(source.asset_pattern, "i");
    const match = release.assets.find((asset) => pattern.test(asset.name));
    return match?.browser_download_url ?? release.assets[0]?.browser_download_url ?? null;
  };

  const sync = useMutation({
    mutationFn: (input: { assetUrl: string; tag: string }) =>
      api.releaseSources.sync(source.id, {
        asset_url: input.assetUrl,
        version: input.tag.replace(/^[vV]/, ""),
        tag: input.tag,
      }),
    onSuccess: onImported,
    onError: (error) =>
      toast.error(
        isApiError(error)
          ? error.message
          : "Не удалось импортировать релиз из источника",
        { duration: 6000 },
      ),
  });

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Релизы {providerLabels[source.provider]}</DialogTitle>
          <DialogDescription>
            Выберите релиз и импортируйте артефакт в библиотеку движка.
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-96 space-y-2 overflow-y-auto">
          {releases.isLoading ? (
            <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              Загружаем релиз источника…
            </div>
          ) : releases.error ? (
            <p className="py-3 text-sm text-destructive">{errorMessage(releases.error)}</p>
          ) : (releases.data?.items ?? []).length === 0 ? (
            <p className="py-3 text-sm text-muted-foreground">Релизов в источнике нет.</p>
          ) : (
            releases.data?.items.map((release) => {
              const assetUrl = defaultAsset(release);
              return (
                <div
                  key={release.tag_name}
                  className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{release.tag_name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {assetUrl ? release.assets[0].name : "подходящий артефакт не найден"}
                      {release.published_at ? ` · ${formatDateTime(release.published_at)}` : ""}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    disabled={!assetUrl || sync.isPending}
                    onClick={() => sync.mutate({ assetUrl: assetUrl ?? "", tag: release.tag_name })}
                  >
                    <Download />
                    Импортировать
                  </Button>
                </div>
              );
            })
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
