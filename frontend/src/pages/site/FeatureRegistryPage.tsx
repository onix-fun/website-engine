import { useEffect, useState, type ChangeEvent } from "react";
import { Boxes, CloudUpload, FileCode2, Fingerprint } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/page";
import type { FeatureBuild } from "@/types";

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "Неизвестная ошибка";
}

export default function FeatureRegistryPage() {
  const [items, setItems] = useState<FeatureBuild[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const refresh = async () => {
    const result = await api.siteBuilder.featureBuilds();
    setItems(result.items);
  };
  useEffect(() => {
    let active = true;
    api.siteBuilder.featureBuilds().then((result) => { if (active) setItems(result.items); })
      .catch((reason: unknown) => { if (active) setError(reason); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".zip")) { toast.error("Выберите ZIP-архив сборки фичи."); return; }
    setUploading(true);
    try {
      const build = await api.siteBuilder.importFeatureBuild(file);
      await refresh();
      toast.success(`${build.key}@${build.version} добавлена в реестр`);
    } catch (reason) { toast.error(errorMessage(reason)); }
    finally { setUploading(false); }
  };

  if (loading) return <p className="p-6 text-sm text-muted-foreground">Загружаем реестр фичей…</p>;
  if (error) return <ErrorState error={error} title="Не удалось загрузить реестр фичей" />;

  const sorted = [...items].sort((a, b) => a.key.localeCompare(b.key) || b.version.localeCompare(a.version));
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3"><div><h2 className="text-xl font-semibold tracking-tight">Реестр фичей</h2><p className="mt-1 text-sm text-muted-foreground">Неизменяемые версии компонентов, доступные редактору страниц.</p></div>
        <label className="inline-flex cursor-pointer items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
          <CloudUpload className="size-4" />{uploading ? "Загружаем сборку…" : "Загрузить ZIP"}
          <input className="sr-only" type="file" accept=".zip,application/zip" disabled={uploading} onChange={upload} />
        </label>
      </div>
      <Card><CardHeader><CardTitle className="text-base">Сборки фич</CardTitle><CardDescription>ZIP включает manifest.json, ESM entry, стили и ресурсы. После загрузки сборка сохраняется по digest и не редактируется.</CardDescription></CardHeader>
        <CardContent>
          {sorted.length === 0 ? <div className="rounded-md border border-dashed border-border px-4 py-12 text-center"><Boxes className="mx-auto size-8 text-muted-foreground" /><p className="mt-3 text-sm font-medium">Реестр пока пуст</p><p className="mt-1 text-sm text-muted-foreground">Загрузите первую ZIP-сборку, чтобы добавить её на страницу.</p><label className="mt-4 inline-flex cursor-pointer items-center gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm hover:bg-accent"><CloudUpload className="size-4" />Выбрать ZIP<input className="sr-only" type="file" accept=".zip,application/zip" disabled={uploading} onChange={upload} /></label></div> : (
            <div className="divide-y divide-border">
              {sorted.map((build) => <details className="group py-3 first:pt-0 last:pb-0" key={build.id}>
                <summary className="flex cursor-pointer list-none flex-wrap items-center justify-between gap-3 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  <span className="flex min-w-0 items-center gap-3"><span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted"><Boxes className="size-4" /></span><span className="min-w-0"><span className="block truncate text-sm font-medium">{build.name}</span><span className="mt-0.5 block truncate text-xs text-muted-foreground">{build.key}@{build.version} · {build.manifest.content.mode}</span></span></span>
                  <span className="flex items-center gap-2 text-xs text-muted-foreground"><Fingerprint className="size-3.5" />{build.digest.slice(0, 16)}</span>
                </summary>
                <div className="mt-3 grid gap-4 rounded-md bg-muted/30 p-4 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,.8fr)]">
                  <div className="space-y-3 text-sm"><div className="flex items-center gap-2 font-medium"><FileCode2 className="size-4" />Манифест сборки</div><dl className="grid grid-cols-[8rem_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs"><dt className="text-muted-foreground">Режим контента</dt><dd>{build.manifest.content.mode}</dd><dt className="text-muted-foreground">Совместимость</dt><dd>{build.manifest.engineApi}</dd><dt className="text-muted-foreground">Точки размещения</dt><dd>{build.manifest.slots.join(", ")}</dd><dt className="text-muted-foreground">Аргументы</dt><dd>{Object.keys((build.manifest.arguments.properties as Record<string, unknown> | undefined) ?? {}).join(", ") || "Нет"}</dd><dt className="text-muted-foreground">Загружена</dt><dd>{new Date(build.createdAt).toLocaleString("ru-RU")}</dd><dt className="text-muted-foreground">SHA-256</dt><dd className="break-all font-mono">{build.digest}</dd></dl></div>
                  <div className="min-w-0"><p className="mb-2 text-xs font-medium text-muted-foreground">Исходный manifest.json</p><pre className="max-h-64 overflow-auto rounded-md border border-border bg-background p-3 text-[11px] leading-5">{JSON.stringify(build.manifest, null, 2)}</pre></div>
                </div>
              </details>)}
            </div>
          )}
        </CardContent>
      </Card>
      <p className="text-xs text-muted-foreground">Сборки, используемые опубликованными снапшотами, не удаляются из реестра.</p>
    </div>
  );
}
