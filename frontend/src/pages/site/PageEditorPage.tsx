import { useEffect, useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/page";
import type { FeatureBuild, FeatureArgumentBinding, JsonValue, RouteTreeNode, SiteConfig, SitePage, SitePlacement } from "@/types";
import { useSiteWorkspace } from "./SiteWorkspace";

type SchemaField = { type?: string; title?: string; description?: string; default?: JsonValue; enum?: JsonValue[]; items?: SchemaField; properties?: Record<string, SchemaField> };
type DetailRoute = { id: string; path: string; captures: string[]; pageId: string };

function object(value: unknown): Record<string, unknown> { return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {}; }
function properties(schema: unknown): Record<string, SchemaField> { return object(object(schema).properties) as Record<string, SchemaField>; }
function initialValue(schema: SchemaField): JsonValue {
  if (schema.default !== undefined) return schema.default;
  if (schema.type === "boolean") return false;
  if (schema.type === "integer" || schema.type === "number") return 0;
  if (schema.type === "array") return [];
  return "";
}
function coerceInput(type: string | undefined, value: string): JsonValue {
  if (type === "integer") return value === "" ? "" : Number.parseInt(value, 10);
  if (type === "number") return value === "" ? "" : Number(value);
  return value;
}
function routeCaptures(schema: RouteTreeNode, pageId: string, inherited: string[] = [], path = ""): DetailRoute[] {
	const regexNames = schema.type === "regex" ? Array.from((schema.pattern ?? "").matchAll(/\(\?<([A-Za-z][A-Za-z0-9_]*)>/g), (m) => m[1]) : [];
	const segment = schema.type === "static" ? `/${schema.value}` : schema.type === "param" ? `/{${schema.name}}` : schema.type === "regex" ? `/{${regexNames[0] ?? "segment"}}` : schema.type === "catchAll" ? `/{${schema.name}}` : "";
  const captures = [...inherited];
  if (schema.type === "param" || schema.type === "catchAll") if (schema.name) captures.push(schema.name);
	if (schema.type === "regex") captures.push(...regexNames);
  const currentPath = schema.type === "index" ? "/" : `${path}${segment}`;
  const result = schema.pageId === pageId ? [{ id: schema.id, path: currentPath || "/", captures, pageId }] : [];
  return [...result, ...(schema.children ?? []).flatMap((child) => routeCaptures(child, pageId, captures, currentPath === "/" ? "" : currentPath))];
}
function allDetailRoutes(tree: RouteTreeNode, pages: SitePage[]): DetailRoute[] {
  return pages.filter((page) => page.kind === "detail").flatMap((page) => routeCaptures(tree, page.id));
}
function mapTree(root: RouteTreeNode, pageId: string): RouteTreeNode {
  return { ...root, ...(root.pageId === pageId ? { pageId: undefined } : {}), children: (root.children ?? []).map((node) => mapTree(node, pageId)) };
}
function buildFor(builds: FeatureBuild[], placement: SitePlacement) { return builds.find((item) => item.key === placement.build.featureKey && item.version === placement.build.version); }

function ArgumentField({ schema, binding, captures, onChange }: { schema: SchemaField; binding?: FeatureArgumentBinding; captures: string[]; onChange: (binding?: FeatureArgumentBinding) => void }) {
  const name = schema.title ?? "Аргумент";
  const source = binding?.source ?? "literal";
  const value = binding?.source === "literal" ? binding.value : schema.default ?? initialValue(schema);
  const isPathScalar = ["string", "integer", "number", "boolean"].includes(schema.type ?? "") && captures.length > 0;
  return <div className="space-y-2 rounded-md border border-border p-3">
    <div className="flex flex-wrap items-center justify-between gap-2"><div><Label>{name}</Label>{schema.description && <p className="mt-1 text-xs text-muted-foreground">{schema.description}</p>}</div><select aria-label={`Источник аргумента ${name}`} className="h-8 rounded-md border border-input bg-background px-2 text-xs" value={source} onChange={(event) => {
      const next = event.currentTarget.value;
      if (next === "path") onChange({ source: "path", pathArg: captures[0] });
      else if (next === "query") onChange({ source: "query", queryKey: name } as FeatureArgumentBinding);
      else onChange({ source: "literal", value: schema.default ?? initialValue(schema) });
    }}><option value="literal">Постоянное</option>{isPathScalar && <option value="path">Из пути</option>}<option value="query">Из query</option></select></div>
    {source === "path" && <select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={binding?.source === "path" ? binding.pathArg : captures[0]} onChange={(event) => onChange({ source: "path", pathArg: event.currentTarget.value })}>{captures.map((capture) => <option key={capture} value={capture}>{capture}</option>)}</select>}
    {source === "query" && <div className="space-y-1"><Label className="text-xs text-muted-foreground">Ключ query-строки</Label><Input value={binding?.source === "query" ? binding.queryKey : name} onChange={(event) => onChange({ source: "query", queryKey: event.currentTarget.value })} placeholder="page" /></div>}
    {source === "literal" && (schema.enum ? <select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={String(value ?? "")} onChange={(event) => onChange({ source: "literal", value: event.currentTarget.value })}>{schema.enum.map((option) => <option key={String(option)} value={String(option)}>{String(option)}</option>)}</select> : schema.type === "boolean" ? <select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={String(value)} onChange={(event) => onChange({ source: "literal", value: event.currentTarget.value === "true" })}><option value="true">Да</option><option value="false">Нет</option></select> : schema.type === "array" ? <Input value={Array.isArray(value) ? value.map(String).join(", ") : ""} onChange={(event) => onChange({ source: "literal", value: event.currentTarget.value.split(",").map((part) => coerceInput(schema.items?.type, part.trim())) })} placeholder="Значения через запятую" /> : <Input type={schema.type === "integer" || schema.type === "number" ? "number" : "text"} value={typeof value === "string" || typeof value === "number" ? String(value) : ""} onChange={(event) => onChange({ source: "literal", value: coerceInput(schema.type, event.currentTarget.value) })} />)}
    {schema.type === "object" && <p className="text-xs text-amber-700 dark:text-amber-300">Для объектного аргумента задайте поля как простые значения в манифесте фичи.</p>}
  </div>;
}

function PlacementEditor({ page, placement, builds, config, onChange, onRemove }: { page: SitePage; placement: SitePlacement; builds: FeatureBuild[]; config: SiteConfig; onChange: (value: SitePlacement) => void; onRemove: () => void }) {
  const build = buildFor(builds, placement);
  const manifest = build?.manifest;
  const fields = properties(manifest?.arguments);
  const captures = routeCaptures(config.routeTree, page.id).flatMap((route) => route.captures);
  const uniqueCaptures = [...new Set(captures)];
  const collectionBuilds = builds.filter((item) => item.manifest.content.mode === "collection");
  const sourceBuild = collectionBuilds.find((item) => item.key === placement.collectionSource?.featureKey && item.version === placement.collectionSource.version);
  const sourceFields = sourceBuild ? Object.keys(properties(sourceBuild.manifest.content.schema)) : [];
  const contentFields = Object.keys(properties(manifest?.content.schema));
  const detailRoutes = allDetailRoutes(config.routeTree, config.pages);
  const patchBinding = (key: string, binding?: FeatureArgumentBinding) => { const next = { ...(placement.arguments ?? {}) }; if (binding) next[key] = binding; else delete next[key]; onChange({ ...placement, arguments: next }); };

  return <section className="space-y-4 rounded-md border border-border bg-muted/15 p-4">
    <div className="flex items-start justify-between gap-3"><div><p className="text-sm font-medium">{manifest?.name ?? "Фича страницы"}</p><p className="mt-1 text-xs text-muted-foreground">{placement.build.featureKey}@{placement.build.version}</p></div><Button variant="ghost" size="icon-sm" aria-label="Удалить фичу со страницы" onClick={onRemove}><Trash2 /></Button></div>
    <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]"><label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Сборка фичи</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={`${placement.build.featureKey}@${placement.build.version}`} onChange={(event) => { const selected = builds.find((item) => `${item.key}@${item.version}` === event.currentTarget.value); if (selected) onChange({ id: placement.id, build: { featureKey: selected.key, version: selected.version }, slot: selected.manifest.slots[0] ?? "main", arguments: {} }); }}>{builds.map((item) => <option key={item.id} value={`${item.key}@${item.version}`}>{item.name} · {item.version}</option>)}</select></label>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Место на странице</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.slot} onChange={(event) => onChange({ ...placement, slot: event.currentTarget.value })}>{(manifest?.slots ?? ["main"]).map((slot) => <option key={slot} value={slot}>{slot}</option>)}</select></label></div>
    {Object.entries(fields).length > 0 && <fieldset className="space-y-2 border-t border-border pt-3"><legend className="px-1 text-sm font-medium">Аргументы размещения</legend>{Object.entries(fields).map(([key, schema]) => <ArgumentField key={key} schema={schema} binding={placement.arguments?.[key]} captures={uniqueCaptures} onChange={(binding) => patchBinding(key, binding)} />)}</fieldset>}
    {page.kind === "detail" && manifest?.content.mode !== "collection" && <fieldset className="grid gap-3 border-t border-border pt-3 sm:grid-cols-2"><legend className="px-1 text-sm font-medium">Источник записи</legend>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Коллекция</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.collectionSource ? `${placement.collectionSource.featureKey}@${placement.collectionSource.version}` : ""} onChange={(event) => { const selected = collectionBuilds.find((item) => `${item.key}@${item.version}` === event.currentTarget.value); onChange({ ...placement, collectionSource: selected ? { featureKey: selected.key, version: selected.version, field: Object.keys(properties(selected.manifest.content.schema))[0] ?? "", argument: Object.keys(fields)[0] ?? "" } : undefined }); }}><option value="">Выберите коллекцию</option>{collectionBuilds.map((item) => <option key={item.id} value={`${item.key}@${item.version}`}>{item.name} · {item.version}</option>)}</select></label>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Поле для поиска</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.collectionSource?.field ?? ""} onChange={(event) => onChange({ ...placement, collectionSource: placement.collectionSource ? { ...placement.collectionSource, field: event.currentTarget.value } : undefined })}><option value="">Поле</option>{sourceFields.map((field) => <option key={field}>{field}</option>)}</select></label>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Аргумент detail-фичи</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.collectionSource?.argument ?? ""} onChange={(event) => onChange({ ...placement, collectionSource: placement.collectionSource ? { ...placement.collectionSource, argument: event.currentTarget.value } : undefined })}><option value="">Аргумент</option>{Object.entries(fields).filter(([, schema]) => schema.type === "string").map(([key, schema]) => <option key={key} value={key}>{schema.title ?? key}</option>)}</select></label>
      {!uniqueCaptures.length && <p className="self-end text-xs text-destructive">У страницы должна быть ветвь URL с параметром пути.</p>}
    </fieldset>}
    {manifest?.content.mode === "collection" && <fieldset className="grid gap-3 border-t border-border pt-3 sm:grid-cols-2"><legend className="px-1 text-sm font-medium">Ссылка на detail-страницу</legend>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Маршрут</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.detailLink?.routeId ?? ""} onChange={(event) => { const route = detailRoutes.find((item) => item.id === event.currentTarget.value); onChange({ ...placement, detailLink: route ? { routeId: route.id, field: placement.detailLink?.field ?? contentFields[0] ?? "", pathTemplate: route.path } : undefined }); }}><option value="">Не добавлять ссылку</option>{detailRoutes.map((route) => <option key={route.id} value={route.id}>{route.path}</option>)}</select></label>
      <label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Поле записи</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={placement.detailLink?.field ?? ""} onChange={(event) => onChange({ ...placement, detailLink: placement.detailLink ? { ...placement.detailLink, field: event.currentTarget.value } : undefined })}><option value="">Выберите поле</option>{contentFields.map((field) => <option key={field}>{field}</option>)}</select></label>
      <label className="space-y-1.5 text-sm sm:col-span-2"><span className="text-muted-foreground">Шаблон ссылки</span><Input value={placement.detailLink?.pathTemplate ?? ""} onChange={(event) => onChange({ ...placement, detailLink: placement.detailLink ? { ...placement.detailLink, pathTemplate: event.currentTarget.value } : undefined })} /></label>
    </fieldset>}
  </section>;
}

export default function PageEditorPage() {
  const { config, update } = useSiteWorkspace();
  const [builds, setBuilds] = useState<FeatureBuild[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [selectedPageId, setSelectedPageId] = useState(config.pages[0]?.id ?? "");
  useEffect(() => {
    let active = true;
    api.siteBuilder.featureBuilds().then((result) => { if (active) setBuilds(result.items); }).catch((reason: unknown) => { if (active) setError(reason); });
    return () => { active = false; };
  }, []);
  const page = config.pages.find((item) => item.id === selectedPageId) ?? config.pages[0];
  const detailRoutes = useMemo(() => allDetailRoutes(config.routeTree, config.pages), [config.routeTree, config.pages]);

  const addPage = () => {
    const id = `page-${crypto.randomUUID().slice(0, 8)}`;
    const next: SitePage = { id, name: "Новая страница", kind: "page", placements: [] };
    update((current) => ({ ...current, pages: [...current.pages, next] }));
    setSelectedPageId(id);
  };
  const changePage = (id: string, patch: Partial<SitePage>) => update((current) => ({ ...current, pages: current.pages.map((item) => item.id === id ? { ...item, ...patch } : item) }));
  const removePage = (id: string) => {
    if (config.pages.length <= 1) { toast.error("В конфигурации должна остаться хотя бы одна страница."); return; }
    update((current) => ({ ...current, pages: current.pages.filter((item) => item.id !== id), routeTree: mapTree(current.routeTree, id) }));
    setSelectedPageId(config.pages.find((item) => item.id !== id)?.id ?? "");
  };
  const addPlacement = () => {
    if (!page) return;
    const build = builds[0];
    if (!build) { toast.error("Сначала добавьте сборку в реестр фичей."); return; }
    const next: SitePlacement = { id: `placement-${crypto.randomUUID().slice(0, 8)}`, build: { featureKey: build.key, version: build.version }, slot: build.manifest.slots[0] ?? "main", arguments: {} };
    changePage(page.id, { placements: [...page.placements, next] });
  };

  if (error) return <ErrorState error={error} title="Не удалось загрузить сборки фич" />;
  return <div className="space-y-4">
    <div className="flex flex-wrap items-end justify-between gap-3"><div><h2 className="text-xl font-semibold tracking-tight">Редактор страниц</h2><p className="mt-1 text-sm text-muted-foreground">Страница состоит из размещений версий фич. Введите аргументы без редактирования конфигурационного JSON.</p></div><Button type="button" variant="outline" onClick={addPage}><Plus />Новая страница</Button></div>
    <div className="grid gap-4 xl:grid-cols-[250px_minmax(0,1fr)]">
      <Card className="h-fit"><CardHeader className="pb-3"><CardTitle className="text-base">Страницы</CardTitle><CardDescription>{config.pages.length} в текущем черновике</CardDescription></CardHeader><CardContent className="space-y-1 px-2 pb-2">{config.pages.map((item) => <button key={item.id} type="button" onClick={() => setSelectedPageId(item.id)} className={`flex w-full items-center justify-between gap-2 rounded-md px-3 py-2 text-left text-sm ${page?.id === item.id ? "bg-primary/10 font-medium text-primary" : "hover:bg-muted"}`}><span className="min-w-0 truncate">{item.name}</span><span className="text-[10px] text-muted-foreground">{item.placements.length}</span></button>)}</CardContent></Card>
      {!page ? <Card><CardContent className="py-12 text-center"><p className="text-sm text-muted-foreground">Создайте страницу, затем назначьте её узлу в роутере.</p></CardContent></Card> : <div className="space-y-4">
        <Card><CardHeader><CardTitle className="text-base">Параметры страницы</CardTitle><CardDescription>Маршруты назначаются отдельно в дереве роутера.</CardDescription></CardHeader><CardContent className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_13rem_auto]"><div className="space-y-1.5"><Label htmlFor="page-name">Название страницы</Label><Input id="page-name" value={page.name} onChange={(event) => changePage(page.id, { name: event.currentTarget.value })} /></div><label className="space-y-1.5 text-sm"><span className="text-muted-foreground">Тип страницы</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={page.kind} onChange={(event) => changePage(page.id, { kind: event.currentTarget.value as SitePage["kind"] })}><option value="page">Обычная страница</option><option value="collection-list">Список коллекции</option><option value="detail">Карточка записи</option></select></label><Button className="self-end" type="button" variant="ghost" size="icon" aria-label="Удалить страницу" onClick={() => removePage(page.id)}><Trash2 /></Button></CardContent></Card>
        <Card><CardHeader className="flex-row items-center justify-between gap-3"><div><CardTitle className="text-base">Компоненты страницы</CardTitle><CardDescription>Версии и аргументы берутся из реестра сборок фич.</CardDescription></div><Button type="button" size="sm" variant="outline" onClick={addPlacement}><Plus />Добавить фичу</Button></CardHeader><CardContent className="space-y-3">{page.placements.length === 0 ? <div className="rounded-md border border-dashed border-border px-4 py-9 text-center text-sm text-muted-foreground">Добавьте первую фичу из реестра.</div> : page.placements.map((placement, index) => <div key={placement.id} className="space-y-2"><div className="flex justify-end gap-1"><Button type="button" variant="ghost" size="icon-sm" aria-label="Поднять фичу" disabled={index === 0} onClick={() => { const placements = [...page.placements]; [placements[index - 1], placements[index]] = [placements[index], placements[index - 1]]; changePage(page.id, { placements }); }}><ArrowUp /></Button><Button type="button" variant="ghost" size="icon-sm" aria-label="Опустить фичу" disabled={index === page.placements.length - 1} onClick={() => { const placements = [...page.placements]; [placements[index], placements[index + 1]] = [placements[index + 1], placements[index]]; changePage(page.id, { placements }); }}><ArrowDown /></Button></div><PlacementEditor page={page} placement={placement} builds={builds} config={config} onChange={(next) => changePage(page.id, { placements: page.placements.map((item) => item.id === placement.id ? next : item) })} onRemove={() => changePage(page.id, { placements: page.placements.filter((item) => item.id !== placement.id) })} /></div>)}</CardContent></Card>
        <Card><CardContent className="p-4 text-xs text-muted-foreground">С этой страницей связаны маршруты: {routeCaptures(config.routeTree, page.id).map((route) => route.path).join(", ") || "маршрут ещё не задан"}. Детальные маршруты доступны фичам через path-параметры: {detailRoutes.map((route) => route.path).join(", ") || "нет"}.</CardContent></Card>
      </div>}
    </div>
  </div>;
}
