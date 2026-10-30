import { useEffect, useState, type ChangeEvent, type ReactNode } from "react";
import { ArrowDown, ArrowUp, Boxes, CloudUpload, ImagePlus, Plus, Rocket, Save, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { MediaPickerDialog } from "@/domains/media/MediaPickerDialog";
import { ErrorState, PageHeader } from "@/components/common/page";
import type {
  FeatureBuild,
  FeatureArgumentBinding,
  JsonValue,
  SiteConfig,
  SitePage,
  SitePlacement,
  SiteSnapshot,
  SiteSnapshotList,
} from "@/types";

type SchemaNode = {
  type?: string;
  title?: string;
  description?: string;
  default?: JsonValue;
  enum?: JsonValue[];
  format?: string;
  "x-ui"?: { widget?: string };
  items?: SchemaNode;
  properties?: Record<string, SchemaNode>;
};

const emptyConfig = (): SiteConfig => ({
  formatVersion: 1,
  documentTitle: "VitaSil",
  pages: [{ id: "home", name: "Главная", kind: "page", placements: [] }],
  routes: [{ id: "home", pattern: "^/$", pageId: "home" }],
});

function asObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function schemaProperties(value: unknown): Record<string, SchemaNode> {
  return asObject(asObject(value).properties) as Record<string, SchemaNode>;
}

function defaultValue(schema: SchemaNode): JsonValue {
  if (schema.default !== undefined) return schema.default;
  switch (schema.type) {
    case "boolean": return false;
    case "integer":
    case "number": return 0;
    case "object": return {};
    case "array": return [];
    default: return "";
  }
}

function routeCaptures(pattern: string): string[] {
  return Array.from(pattern.matchAll(/\(\?<([A-Za-z][A-Za-z0-9_]*)>/g), (match) => match[1]);
}

function buildFor(builds: FeatureBuild[], placement: SitePlacement): FeatureBuild | undefined {
  return builds.find((item) => item.key === placement.build.featureKey && item.version === placement.build.version);
}

function formatError(error: unknown): string {
  return isApiError(error) ? error.message : error instanceof Error ? error.message : "Неизвестная ошибка";
}

function Panel({ title, description, children, action }: {
  title: string;
  description?: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div>
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription className="mt-1">{description}</CardDescription>}
        </div>
        {action}
      </CardHeader>
      <CardContent className="space-y-4">{children}</CardContent>
    </Card>
  );
}

function ScalarField({ schema, value, onChange }: {
  schema: SchemaNode;
  value: JsonValue | undefined;
  onChange: (value: JsonValue) => void;
}) {
  const [mediaPickerOpen, setMediaPickerOpen] = useState(false);
  const type = schema.type ?? "string";
  if (type === "boolean") {
    return (
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={Boolean(value ?? schema.default)} onChange={(event) => onChange(event.currentTarget.checked)} />
        {schema.title ?? "Включено"}
      </label>
    );
  }
  if (schema.enum) {
    return (
      <select
        className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
        value={value === undefined ? "" : JSON.stringify(value)}
        onChange={(event) => onChange(JSON.parse(event.currentTarget.value) as JsonValue)}
      >
        <option value="">Выберите значение</option>
        {schema.enum.map((option) => <option key={JSON.stringify(option)} value={JSON.stringify(option)}>{String(option)}</option>)}
      </select>
    );
  }
  if (type === "object") {
    const objectValue = asObject(value) as Record<string, JsonValue>;
    return (
      <div className="grid gap-3 rounded-md border border-border p-3 sm:grid-cols-2">
        {Object.entries(schema.properties ?? {}).map(([key, property]) => (
          <div className="space-y-1.5" key={key}>
            <Label>{property.title ?? key}</Label>
            <ScalarField
              schema={property}
              value={objectValue[key]}
              onChange={(next) => onChange({ ...objectValue, [key]: next })}
            />
          </div>
        ))}
      </div>
    );
  }
  if (type === "array") {
    const items = Array.isArray(value) ? value : [];
    const itemSchema = schema.items ?? { type: "string" };
    return (
      <div className="space-y-2">
        {items.map((item, index) => (
          <div className="flex items-start gap-2" key={index}>
            <div className="min-w-0 flex-1">
              <ScalarField
                schema={itemSchema}
                value={item}
                onChange={(next) => onChange(items.map((current, itemIndex) => itemIndex === index ? next : current))}
              />
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Удалить элемент ${index + 1}`}
              onClick={() => onChange(items.filter((_, itemIndex) => itemIndex !== index))}
            >
              <Trash2 />
            </Button>
          </div>
        ))}
        <Button type="button" variant="outline" size="sm" onClick={() => onChange([...items, defaultValue(itemSchema)])}>
          <Plus /> Добавить элемент
        </Button>
      </div>
    );
  }
  if (schema.format === "media" || schema["x-ui"]?.widget === "media") {
    const mediaId = typeof value === "string" ? value : "";
    return (
      <>
        <div className="flex gap-2">
          <Input value={mediaId} readOnly placeholder="Выберите изображение" />
          <Button type="button" variant="outline" onClick={() => setMediaPickerOpen(true)}>
            <ImagePlus />
            Выбрать
          </Button>
        </div>
        <MediaPickerDialog
          open={mediaPickerOpen}
          onClose={() => setMediaPickerOpen(false)}
          onSelect={(media) => onChange(media[0]?.id ?? "")}
          accept="image"
          initialSelected={mediaId ? [mediaId] : []}
          title={schema.title ?? "Выберите изображение"}
        />
      </>
    );
  }
  const shown = value ?? schema.default;
  return (
    <Input
      type={type === "integer" || type === "number" ? "number" : "text"}
      value={shown === undefined || shown === null ? "" : String(shown)}
      placeholder={schema.description}
      onChange={(event) => {
        const next = event.currentTarget.value;
        onChange(type === "integer" ? (next === "" ? "" : Number.parseInt(next, 10)) : type === "number" ? (next === "" ? "" : Number(next)) : next);
      }}
    />
  );
}

function PlacementEditor({
  page,
  placement,
  builds,
  config,
  onChange,
  onRemove,
}: {
  page: SitePage;
  placement: SitePlacement;
  builds: FeatureBuild[];
  config: SiteConfig;
  onChange: (next: SitePlacement) => void;
  onRemove: () => void;
}) {
  const build = buildFor(builds, placement);
  const manifest = build?.manifest;
  const argumentProperties = schemaProperties(manifest?.arguments);
  const bindings = placement.arguments ?? {};
  const pageRoutes = config.routes.filter((route) => route.pageId === page.id);
  const captures = Array.from(new Set(pageRoutes.flatMap((route) => routeCaptures(route.pattern))));
  const collectionBuilds = builds.filter((item) => item.manifest.content.mode === "collection");
  const detailRoutes = config.routes.filter((route) => config.pages.some((candidate) => candidate.id === route.pageId && candidate.kind === "detail"));

  const updateBinding = (name: string, binding?: FeatureArgumentBinding) => {
    const next = { ...bindings };
    if (binding) next[name] = binding;
    else delete next[name];
    onChange({ ...placement, arguments: next });
  };

  const contentFields = manifest ? Object.keys(schemaProperties(manifest.content.schema)) : [];
  const sourceBuild = collectionBuilds.find((item) => item.key === placement.collectionSource?.featureKey && item.version === placement.collectionSource.version);
  const sourceFields = sourceBuild ? Object.keys(schemaProperties(sourceBuild.manifest.content.schema)) : [];

  return (
    <div className="space-y-4 rounded-lg border border-border bg-muted/20 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{manifest?.name ?? "Компонент страницы"}</p>
          <p className="mt-1 text-xs text-muted-foreground">{placement.build.featureKey}@{placement.build.version}</p>
        </div>
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Удалить фичу со страницы" onClick={onRemove}><Trash2 /></Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]">
        <div className="space-y-1.5">
          <Label>Сборка фичи</Label>
          <select
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            value={`${placement.build.featureKey}@${placement.build.version}`}
            onChange={(event) => {
              const selected = builds.find((item) => `${item.key}@${item.version}` === event.currentTarget.value);
              if (!selected) return;
              onChange({ id: placement.id, build: { featureKey: selected.key, version: selected.version }, slot: selected.manifest.slots[0], arguments: {} });
            }}
          >
            {builds.map((item) => <option key={item.id} value={`${item.key}@${item.version}`}>{item.name} · {item.version}</option>)}
          </select>
        </div>
        <div className="space-y-1.5">
          <Label>Место</Label>
          <select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={placement.slot} onChange={(event) => onChange({ ...placement, slot: event.currentTarget.value })}>
            {(manifest?.slots ?? ["main"]).map((slot) => <option value={slot} key={slot}>{slot}</option>)}
          </select>
        </div>
      </div>

      {Object.keys(argumentProperties).length > 0 && (
        <fieldset className="space-y-3 border-t border-border pt-3">
          <legend className="px-1 text-sm font-medium">Аргументы компонента</legend>
          {Object.entries(argumentProperties).map(([name, schema]) => {
            const binding = bindings[name];
            const source = binding?.source ?? "literal";
            const isRouteBindable = ["string", "integer", "number", "boolean"].includes(schema.type ?? "") && captures.length > 0;
            const defaultValue = binding?.source === "literal" ? binding.value : schema.default;
            return (
              <div className="space-y-2" key={name}>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Label>{schema.title ?? name}</Label>
                  {isRouteBindable && (
                    <select
                      className="h-8 rounded-md border border-input bg-background px-2 text-xs"
                      value={source}
                      onChange={(event) => {
                        const nextSource = event.currentTarget.value;
                        updateBinding(name, nextSource === "route"
                          ? { source: "route", routeArg: captures[0] }
                          : { source: "literal", value: schema.default ?? "" });
                      }}
                    >
                      <option value="literal">Постоянное значение</option>
                      <option value="route">Из адреса страницы</option>
                    </select>
                  )}
                </div>
                {source === "route" && isRouteBindable ? (
                  <select
                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                    value={binding?.routeArg ?? captures[0]}
                    onChange={(event) => updateBinding(name, { source: "route", routeArg: event.currentTarget.value })}
                  >
                    {captures.map((capture) => <option key={capture} value={capture}>{capture}</option>)}
                  </select>
                ) : (
                  <ScalarField schema={schema} value={defaultValue} onChange={(value) => updateBinding(name, { source: "literal", value })} />
                )}
              </div>
            );
          })}
        </fieldset>
      )}

      {page.kind === "detail" && manifest?.content.mode !== "collection" && (
        <fieldset className="grid gap-3 border-t border-border pt-3 sm:grid-cols-3">
          <legend className="px-1 text-sm font-medium">Источник записи для detail-страницы</legend>
          <label className="space-y-1.5 text-sm">
            <span className="text-muted-foreground">Коллекция</span>
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-3"
              value={placement.collectionSource ? `${placement.collectionSource.featureKey}@${placement.collectionSource.version}` : ""}
              onChange={(event) => {
                const selected = collectionBuilds.find((item) => `${item.key}@${item.version}` === event.currentTarget.value);
                onChange({ ...placement, collectionSource: selected ? { featureKey: selected.key, version: selected.version, field: Object.keys(schemaProperties(selected.manifest.content.schema))[0] ?? "", argument: Object.keys(argumentProperties)[0] ?? "" } : undefined });
              }}
            >
              <option value="">Выберите коллекцию</option>
              {collectionBuilds.map((item) => <option key={item.id} value={`${item.key}@${item.version}`}>{item.name} · {item.version}</option>)}
            </select>
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="text-muted-foreground">Поле поиска</span>
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-3"
              value={placement.collectionSource?.field ?? ""}
              onChange={(event) => onChange({ ...placement, collectionSource: placement.collectionSource ? { ...placement.collectionSource, field: event.currentTarget.value } : undefined })}
            >
              <option value="">Выберите поле</option>
              {sourceFields.map((field) => <option key={field} value={field}>{field}</option>)}
            </select>
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="text-muted-foreground">Аргумент detail-фичи</span>
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-3"
              value={placement.collectionSource?.argument ?? ""}
              onChange={(event) => onChange({ ...placement, collectionSource: placement.collectionSource ? { ...placement.collectionSource, argument: event.currentTarget.value } : undefined })}
            >
              <option value="">Выберите аргумент</option>
              {Object.entries(argumentProperties).filter(([, schema]) => schema.type === "string").map(([name, schema]) => <option key={name} value={name}>{schema.title ?? name}</option>)}
            </select>
          </label>
          {!captures.length && <p className="text-xs text-destructive sm:col-span-3">Для detail-страницы добавьте маршрут с именованной группой, например <code>(?&lt;slug&gt;[^/]+)</code>.</p>}
          {contentFields.length === 0 && <p className="text-xs text-muted-foreground sm:col-span-3">В схеме коллекции пока нет полей.</p>}
        </fieldset>
      )}

      {manifest?.content.mode === "collection" && (
        <fieldset className="grid gap-3 border-t border-border pt-3 sm:grid-cols-3">
          <legend className="px-1 text-sm font-medium">Ссылка на detail-страницу</legend>
          <label className="space-y-1.5 text-sm">
            <span className="text-muted-foreground">Маршрут</span>
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-3"
              value={placement.detailLink?.routeId ?? ""}
              onChange={(event) => {
                const route = detailRoutes.find((item) => item.id === event.currentTarget.value);
                onChange({ ...placement, detailLink: route ? { routeId: route.id, field: placement.detailLink?.field ?? contentFields[0] ?? "", pathTemplate: placement.detailLink?.pathTemplate ?? "" } : undefined });
              }}
            >
              <option value="">Без ссылки</option>
              {detailRoutes.map((route) => <option key={route.id} value={route.id}>{route.pattern}</option>)}
            </select>
          </label>
          <label className="space-y-1.5 text-sm">
            <span className="text-muted-foreground">Поле записи</span>
            <select
              className="h-9 w-full rounded-md border border-input bg-background px-3"
              value={placement.detailLink?.field ?? ""}
              onChange={(event) => onChange({ ...placement, detailLink: placement.detailLink ? { ...placement.detailLink, field: event.currentTarget.value } : undefined })}
            >
              <option value="">Выберите поле</option>
              {contentFields.map((field) => <option key={field} value={field}>{field}</option>)}
            </select>
          </label>
          <label className="space-y-1.5 text-sm sm:col-span-1">
            <span className="text-muted-foreground">Шаблон ссылки, например /catalog/{"{slug}"}</span>
            <Input value={placement.detailLink?.pathTemplate ?? ""} onChange={(event) => onChange({ ...placement, detailLink: placement.detailLink ? { ...placement.detailLink, pathTemplate: event.currentTarget.value } : undefined })} />
          </label>
          {detailRoutes.length === 0 && <p className="text-xs text-muted-foreground sm:col-span-3">Добавьте detail-страницу и её маршрут, чтобы связать товары с карточкой.</p>}
        </fieldset>
      )}
    </div>
  );
}

export default function SiteBuilderPage() {
  const [config, setConfig] = useState<SiteConfig>(emptyConfig);
  const [revision, setRevision] = useState(1);
  const [builds, setBuilds] = useState<FeatureBuild[]>([]);
  const [snapshots, setSnapshots] = useState<SiteSnapshotList>({ items: [], active: { dev: "", prod: "" } });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    let active = true;
    Promise.all([api.siteBuilder.draft(), api.siteBuilder.featureBuilds(), api.siteBuilder.snapshots()])
      .then(([draft, buildList, snapshotList]) => {
        if (!active) return;
        setRevision(draft.revision);
        setConfig(draft.config.pages.length ? draft.config : emptyConfig());
        setBuilds(buildList.items);
        setSnapshots(snapshotList);
      })
      .catch((reason: unknown) => { if (active) setError(reason); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);

  const updateConfig = (update: (current: SiteConfig) => SiteConfig) => setConfig(update(config));

  const refreshSnapshots = async () => setSnapshots(await api.siteBuilder.snapshots());

  const uploadBuild = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".zip")) { toast.error("Выберите ZIP-архив фичи."); return; }
    setUploading(true);
    try {
      const created = await api.siteBuilder.importFeatureBuild(file);
      const result = await api.siteBuilder.featureBuilds();
      setBuilds(result.items);
      toast.success(`${created.key}@${created.version} загружена`);
    } catch (reason) {
      toast.error(formatError(reason));
    } finally {
      setUploading(false);
    }
  };

  const save = async () => {
    setSaving(true);
    try {
      const result = await api.siteBuilder.saveDraft(revision, config);
      setRevision(result.revision);
      setConfig(result.config);
      toast.success("Черновик сохранён");
    } catch (reason) {
      toast.error(formatError(reason));
    } finally {
      setSaving(false);
    }
  };

  const publish = async () => {
    setPublishing(true);
    try {
      const draft = await api.siteBuilder.saveDraft(revision, config);
      setRevision(draft.revision);
      setConfig(draft.config);
      const snapshot = await api.siteBuilder.publish(`Снапшот ${new Date().toLocaleString("ru-RU")}`);
      await refreshSnapshots();
      toast.success(`Снапшот #${snapshot.sequence} опубликован в dev`);
    } catch (reason) {
      toast.error(formatError(reason));
    } finally {
      setPublishing(false);
    }
  };

  const addPage = () => {
    const id = `page-${Date.now().toString(36)}`;
    const page: SitePage = { id, name: "Новая страница", kind: "page", placements: [] };
    updateConfig((current) => ({
      ...current,
      pages: [...current.pages, page],
      routes: [...current.routes, { id: `route-${id}`, pattern: `^/${id}$`, pageId: id }],
    }));
  };

  const addRoute = (pageId: string) => updateConfig((current) => ({
    ...current,
    routes: [...current.routes, { id: `route-${Date.now().toString(36)}`, pattern: "^/new-path$", pageId }],
  }));

  const addPlacement = (pageId: string) => {
    const build = builds[0];
    if (!build) { toast.error("Сначала загрузите ZIP-сборку фичи."); return; }
    const placement: SitePlacement = {
      id: `placement-${Date.now().toString(36)}`,
      build: { featureKey: build.key, version: build.version },
      slot: build.manifest.slots[0] ?? "main",
      arguments: {},
    };
    updateConfig((current) => ({ ...current, pages: current.pages.map((page) => page.id === pageId ? { ...page, placements: [...page.placements, placement] } : page) }));
  };

  const promote = async (snapshot: SiteSnapshot) => {
    try {
      await api.siteBuilder.promote(snapshot.id);
      await refreshSnapshots();
      toast.success(`Снапшот #${snapshot.sequence} отправлен в prod`);
    } catch (reason) { toast.error(formatError(reason)); }
  };

  const rollback = async (snapshot: SiteSnapshot, environment: "dev" | "prod") => {
    if (!window.confirm(`Переключить ${environment} на снапшот #${snapshot.sequence}?`)) return;
    try {
      await api.siteBuilder.rollback(snapshot.id, environment);
      await refreshSnapshots();
      toast.success(`Окружение ${environment} переключено на снапшот #${snapshot.sequence}`);
    } catch (reason) { toast.error(formatError(reason)); }
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Загружаем конфигурацию сайта…</div>;
  if (error) return <ErrorState error={error} title="Не удалось загрузить редактор сайта" />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Сборка сайта"
        description="Загрузите независимые сборки фич, настройте страницы и маршруты, затем опубликуйте неизменяемый снапшот."
        actions={(
          <>
            <Button type="button" variant="outline" onClick={() => void save()} loading={saving}><Save />Сохранить черновик</Button>
            <Button type="button" onClick={() => void publish()} loading={publishing}><Rocket />Опубликовать в dev</Button>
          </>
        )}
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_minmax(20rem,.9fr)]">
        <div className="space-y-6">
          <Panel title="Сборки фич" description="В движок загружаются готовые ESM ZIP-архивы с manifest.json. Версии и digest после загрузки неизменяемы." action={(
            <label className="inline-flex cursor-pointer items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
              <CloudUpload className="size-4" />{uploading ? "Загрузка…" : "Загрузить ZIP"}
              <input className="sr-only" type="file" accept=".zip,application/zip" disabled={uploading} onChange={uploadBuild} />
            </label>
          )}>
            {builds.length === 0 ? (
              <div className="rounded-md border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">Пока нет сборок. Загрузите ZIP одного из каталогов features.</div>
            ) : (
              <div className="grid gap-2 sm:grid-cols-2">
                {builds.map((build) => (
                  <div className="flex min-w-0 items-center gap-3 rounded-md border border-border p-3" key={build.id}>
                    <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted"><Boxes className="size-4" /></span>
                    <div className="min-w-0"><p className="truncate text-sm font-medium">{build.name}</p><p className="truncate text-xs text-muted-foreground">{build.key}@{build.version} · {build.manifest.content.mode} · {build.digest.slice(0, 10)}</p></div>
                  </div>
                ))}
              </div>
            )}
          </Panel>

          <Panel title="Общие настройки" description="Документный заголовок применяется ко всем маршрутам.">
            <div className="max-w-md space-y-1.5"><Label htmlFor="document-title">Название сайта</Label><Input id="document-title" value={config.documentTitle ?? ""} onChange={(event) => updateConfig((current) => ({ ...current, documentTitle: event.currentTarget.value }))} /></div>
          </Panel>

          <Panel title="Страницы" description="Каждая страница содержит упорядоченные размещения зарегистрированных фич. Порядок маршрутов определяет приоритет совпадений." action={<Button type="button" variant="outline" size="sm" onClick={addPage}><Plus />Добавить страницу</Button>}>
            {config.pages.length === 0 && <p className="text-sm text-muted-foreground">Добавьте первую страницу.</p>}
            <div className="space-y-4">
              {config.pages.map((page) => (
                <section className="space-y-4 rounded-lg border border-border p-4" key={page.id}>
                  <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem_auto]">
                    <div className="space-y-1.5"><Label>Название</Label><Input value={page.name} onChange={(event) => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...item, name: event.currentTarget.value } : item) }))} /></div>
                    <div className="space-y-1.5"><Label>Тип страницы</Label><select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={page.kind} onChange={(event) => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...item, kind: event.currentTarget.value as SitePage["kind"] } : item) }))}><option value="page">Страница</option><option value="collection-list">Список коллекции</option><option value="detail">Карточка записи</option></select></div>
                    <Button className="self-end" type="button" variant="ghost" size="icon" aria-label="Удалить страницу" onClick={() => updateConfig((current) => ({ ...current, pages: current.pages.filter((item) => item.id !== page.id), routes: current.routes.filter((route) => route.pageId !== page.id) }))}><Trash2 /></Button>
                  </div>

                  <div className="space-y-2 border-t border-border pt-3">
                    <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-medium">Маршруты</h3><Button type="button" variant="outline" size="sm" onClick={() => addRoute(page.id)}><Plus />Добавить маршрут</Button></div>
                    {config.routes.map((route) => {
                      if (route.pageId !== page.id) return null;
                      const routeIndex = config.routes.findIndex((item) => item.id === route.id);
                      return (
                        <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto_auto_auto_auto]" key={route.id}>
                          <Input aria-label={`Регулярное выражение маршрута для ${page.name}`} value={route.pattern} onChange={(event) => updateConfig((current) => ({ ...current, routes: current.routes.map((item) => item.id === route.id ? { ...item, pattern: event.currentTarget.value } : item) }))} />
                          <Button type="button" variant="ghost" size="icon-sm" aria-label="Поднять маршрут выше" disabled={routeIndex <= 0} onClick={() => updateConfig((current) => {
                            const routes = [...current.routes];
                            [routes[routeIndex - 1], routes[routeIndex]] = [routes[routeIndex], routes[routeIndex - 1]];
                            return { ...current, routes };
                          })}><ArrowUp /></Button>
                          <Button type="button" variant="ghost" size="icon-sm" aria-label="Опустить маршрут ниже" disabled={routeIndex === config.routes.length - 1} onClick={() => updateConfig((current) => {
                            const routes = [...current.routes];
                            [routes[routeIndex], routes[routeIndex + 1]] = [routes[routeIndex + 1], routes[routeIndex]];
                            return { ...current, routes };
                          })}><ArrowDown /></Button>
                          <Button type="button" variant="ghost" size="icon-sm" aria-label="Удалить маршрут" onClick={() => updateConfig((current) => ({ ...current, routes: current.routes.filter((item) => item.id !== route.id) }))}><Trash2 /></Button>
                          <span className="self-center text-xs text-muted-foreground">#{routeIndex + 1} {routeCaptures(route.pattern).map((capture) => `:${capture}`).join(" ")}</span>
                        </div>
                      );
                    })}
                    <p className="text-xs text-muted-foreground">PCRE-подобный regex с именованными группами: <code>^/catalog/(?&lt;slug&gt;[^/]+)$</code>. Группы можно передать в аргументы фич.</p>
                  </div>

                  <div className="space-y-3 border-t border-border pt-3">
                    <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-medium">Компоненты</h3><Button type="button" variant="outline" size="sm" onClick={() => addPlacement(page.id)}><Plus />Добавить фичу</Button></div>
                    {page.placements.length === 0 && <p className="text-sm text-muted-foreground">Добавьте сборку в слот страницы.</p>}
                    {page.placements.map((placement, placementIndex) => (
                      <div key={placement.id} className="space-y-2">
                        <div className="flex justify-end gap-1">
                          <Button type="button" variant="ghost" size="icon-sm" disabled={placementIndex === 0} aria-label="Переместить компонент выше" onClick={() => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => {
                            const placements = [...item.placements];
                            [placements[placementIndex - 1], placements[placementIndex]] = [placements[placementIndex], placements[placementIndex - 1]];
                            return { ...item, placements };
                          }) }))}><ArrowUp /></Button>
                          <Button type="button" variant="ghost" size="icon-sm" disabled={placementIndex === page.placements.length - 1} aria-label="Переместить компонент ниже" onClick={() => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => {
                            if (item.id !== page.id) return item;
                            const placements = [...item.placements];
                            [placements[placementIndex], placements[placementIndex + 1]] = [placements[placementIndex + 1], placements[placementIndex]];
                            return { ...item, placements };
                          }) }))}><ArrowDown /></Button>
                        </div>
                        <PlacementEditor
                          page={page}
                          placement={placement}
                          builds={builds}
                          config={config}
                          onChange={(next) => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...item, placements: item.placements.map((candidate) => candidate.id === placement.id ? next : candidate) } : item) }))}
                          onRemove={() => updateConfig((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...item, placements: item.placements.filter((candidate) => candidate.id !== placement.id) } : item) }))}
                        />
                      </div>
                    ))}
                  </div>
                </section>
              ))}
            </div>
          </Panel>
        </div>

        <aside className="space-y-6 xl:sticky xl:top-4 xl:self-start">
          <Panel title="Публикации" description="Снапшот фиксирует версии фич и опубликованный контент.">
            <div className="grid grid-cols-2 gap-2 text-sm">
              <div className="rounded-md border border-border p-3"><p className="text-xs text-muted-foreground">Активен в dev</p><p className="mt-1 font-medium">{snapshots.items.find((item) => item.id === snapshots.active.dev)?.name ?? "—"}</p></div>
              <div className="rounded-md border border-border p-3"><p className="text-xs text-muted-foreground">Активен в prod</p><p className="mt-1 font-medium">{snapshots.items.find((item) => item.id === snapshots.active.prod)?.name ?? "—"}</p></div>
            </div>
            {snapshots.items.length === 0 ? <p className="text-sm text-muted-foreground">Опубликованных снапшотов пока нет.</p> : (
              <div className="max-h-[70vh] space-y-2 overflow-auto pr-1">
                {snapshots.items.map((snapshot) => (
                  <div className="space-y-2 rounded-md border border-border p-3" key={snapshot.id}>
                    <div className="flex items-start justify-between gap-2"><div className="min-w-0"><p className="truncate text-sm font-medium">#{snapshot.sequence} · {snapshot.name}</p><p className="mt-1 text-xs text-muted-foreground">{new Date(snapshot.createdAt).toLocaleString("ru-RU")} · {snapshot.digest.slice(0, 12)}</p></div><div className="flex gap-1">{snapshot.id === snapshots.active.dev && <span className="rounded bg-blue-500/10 px-2 py-1 text-[11px] text-blue-700">dev</span>}{snapshot.id === snapshots.active.prod && <span className="rounded bg-green-500/10 px-2 py-1 text-[11px] text-green-700">prod</span>}</div></div>
                    <div className="flex flex-wrap gap-2">
                      {snapshot.id === snapshots.active.dev && snapshot.id !== snapshots.active.prod && <Button type="button" size="sm" onClick={() => void promote(snapshot)}>В prod</Button>}
                      {snapshot.id !== snapshots.active.dev && <Button type="button" size="sm" variant="outline" onClick={() => void rollback(snapshot, "dev")}>Откатить dev</Button>}
                      {snapshot.id !== snapshots.active.prod && <Button type="button" size="sm" variant="outline" onClick={() => void rollback(snapshot, "prod")}>Откатить prod</Button>}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Panel>
          <Panel title="Состав черновика" description="Фичи, используемые в страницах.">
            <div className="space-y-2">
              {Array.from(new Set(config.pages.flatMap((page) => page.placements.map((placement) => `${placement.build.featureKey}@${placement.build.version}`)))).map((ref) => <div className="flex items-center justify-between rounded-md bg-muted/50 px-3 py-2 text-sm" key={ref}><span>{ref}</span><span className="text-xs text-muted-foreground">{config.pages.filter((page) => page.placements.some((placement) => `${placement.build.featureKey}@${placement.build.version}` === ref)).length} стр.</span></div>)}
              {config.pages.every((page) => page.placements.length === 0) && <p className="text-sm text-muted-foreground">Страницы пока пустые.</p>}
            </div>
          </Panel>
          <p className="px-1 text-xs leading-5 text-muted-foreground">Публикация проверит маршруты, аргументы, состав коллекций и уникальность ключей detail-страниц. Ошибки будут показаны до переключения активного снапшота.</p>
        </aside>
      </div>
    </div>
  );
}
