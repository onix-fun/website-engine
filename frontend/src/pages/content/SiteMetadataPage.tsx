import { useCallback, useEffect, useMemo, useState } from "react";
import { Globe2, ImagePlus, RotateCcw, Save, Send } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { useAuth } from "@/app/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState, PageHeader } from "@/components/common/page";
import { MediaPickerDialog } from "@/domains/media/MediaPickerDialog";
import type { SiteMetadata, SiteMetadataOverride, SiteMetadataState } from "@/types";

const errorText = (error: unknown) => error instanceof Error ? error.message : "Неизвестная ошибка";
const inherited = (override: SiteMetadataOverride | undefined, key: keyof SiteMetadataOverride, globalValue: string | undefined) => override?.[key] ?? globalValue ?? "";

function MediaField({ label, value, onChange }: { label: string; value?: string; onChange: (id?: string) => void }) {
  const [open, setOpen] = useState(false);
  return <div className="space-y-1.5"><Label>{label}</Label><div className="flex min-w-0 gap-2"><Input readOnly value={value ? `${value} · /media/${value}` : "Файл не выбран"} className="min-w-0" /><Button type="button" variant="outline" onClick={() => setOpen(true)}><ImagePlus />{value ? "Заменить" : "Выбрать"}</Button>{value && <Button type="button" variant="ghost" onClick={() => onChange(undefined)}>Убрать</Button>}</div><MediaPickerDialog open={open} onClose={() => setOpen(false)} accept="image" title={label} onSelect={(items) => onChange(items[0]?.id)} /></div>;
}

export default function SiteMetadataPage() {
  const auth = useAuth();
  const canPromote = auth.principal?.role === "admin" || auth.principal?.role === "moderator";
  const [state, setState] = useState<SiteMetadataState | null>(null);
  const [saved, setSaved] = useState<SiteMetadata | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [conflict, setConflict] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const next = await api.siteBuilder.metadata();
      setState(next);
      setSaved(next.draft);
      setConflict(false);
    } catch (reason) { setError(reason); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  const dirty = useMemo(() => Boolean(state && saved && JSON.stringify(state.draft) !== JSON.stringify(saved)), [state, saved]);
  const update = (next: SiteMetadata) => setState((current) => current ? { ...current, draft: next } : current);
  const updatePage = (pageId: string, key: keyof SiteMetadataOverride, value?: string) => {
    if (!state) return;
    const pages = { ...(state.draft.pages ?? {}) };
    const current = { ...(pages[pageId] ?? {}) };
    if (value === undefined) delete current[key]; else current[key] = value;
    if (Object.keys(current).length === 0) delete pages[pageId]; else pages[pageId] = current;
    update({ ...state.draft, pages });
  };

  const save = async () => {
    if (!state || !dirty) return true;
    setSaving(true);
    setConflict(false);
    try {
      const next = await api.siteBuilder.saveMetadata(state.revision, state.draft);
      setState(next);
      setSaved(next.draft);
      toast.success("Основные настройки сохранены");
      return true;
    } catch (reason) {
      if (isApiError(reason) && reason.status === 409) setConflict(true);
      toast.error(errorText(reason));
      return false;
    } finally { setSaving(false); }
  };

  const publishDev = async () => {
    if (dirty) { toast.error("Сначала сохраните изменения."); return; }
    setPublishing(true);
    try { const next = await api.siteBuilder.publishMetadata(); setState(next); setSaved(next.draft); toast.success("Метаданные опубликованы в dev"); }
    catch (reason) { toast.error(errorText(reason)); }
    finally { setPublishing(false); }
  };
  const promoteProd = async () => {
    setPublishing(true);
    try { const next = await api.siteBuilder.promoteMetadata(); setState(next); setSaved(next.draft); toast.success("Метаданные продвинуты в prod"); }
    catch (reason) { toast.error(errorText(reason)); }
    finally { setPublishing(false); }
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Загружаем основные настройки…</div>;
  if (error || !state) return <ErrorState error={error} title="Не удалось загрузить основные настройки сайта" />;

  const devTitle = state.dev.siteName || "Не опубликовано";
  const prodTitle = state.prod.siteName || "Не опубликовано";
  return <div className="space-y-5">
    <PageHeader title="Основные настройки сайта" description="Название, описание и изображения для поисковых систем и предпросмотра ссылок." actions={<><Button type="button" variant="outline" onClick={() => void reload()}><RotateCcw />Обновить</Button><Button type="button" onClick={() => void save()} loading={saving} disabled={!dirty}><Save />{dirty ? "Сохранить изменения" : "Сохранено"}</Button></>} />
    <div className="flex flex-wrap items-center justify-between gap-3 border-y border-border py-2.5 text-sm"><span className="flex items-center gap-2"><span className={`size-2 rounded-full ${dirty ? "bg-amber-500" : "bg-emerald-500"}`} />{dirty ? "Есть несохранённые изменения" : `Черновик сохранён · ревизия ${state.revision}`}</span><div className="flex flex-wrap gap-3 text-xs text-muted-foreground"><span>dev r{state.devRevision}: {devTitle}</span><span>prod r{state.prodRevision}: {prodTitle}</span></div></div>
    {conflict && <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm"><span>Настройки изменились в другой вкладке. Загрузите актуальную ревизию.</span><Button type="button" variant="outline" size="sm" onClick={() => void reload()}>Обновить</Button></div>}
    <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><Globe2 className="size-4" />Общие настройки</CardTitle><CardDescription>Используются для всех страниц, у которых нет переопределений.</CardDescription></CardHeader><CardContent className="grid gap-4 md:grid-cols-2">
      <div className="space-y-1.5"><Label htmlFor="site-name">Название сайта</Label><Input id="site-name" maxLength={200} value={state.draft.siteName} onChange={(event) => update({ ...state.draft, siteName: event.currentTarget.value })} /></div>
      <MediaField label="Favicon" value={state.draft.faviconMediaId} onChange={(faviconMediaId) => update({ ...state.draft, faviconMediaId })} />
      <label className="space-y-1.5 md:col-span-2"><Label htmlFor="site-description">Описание сайта</Label><Textarea id="site-description" value={state.draft.description ?? ""} onChange={(event) => update({ ...state.draft, description: event.currentTarget.value })} rows={3} maxLength={2048} /></label>
    </CardContent></Card>
    <Card><CardHeader><CardTitle className="text-base">Open Graph</CardTitle><CardDescription>Поля для карточки ссылки в мессенджерах и соцсетях.</CardDescription></CardHeader><CardContent className="grid gap-4 md:grid-cols-2">
      <div className="space-y-1.5"><Label htmlFor="og-title">Заголовок</Label><Input id="og-title" value={state.draft.ogTitle ?? ""} onChange={(event) => update({ ...state.draft, ogTitle: event.currentTarget.value })} maxLength={2048} /></div>
      <MediaField label="Изображение ссылки" value={state.draft.ogImageMediaId} onChange={(ogImageMediaId) => update({ ...state.draft, ogImageMediaId })} />
      <label className="space-y-1.5 md:col-span-2"><Label htmlFor="og-description">Описание</Label><Textarea id="og-description" value={state.draft.ogDescription ?? ""} onChange={(event) => update({ ...state.draft, ogDescription: event.currentTarget.value })} rows={3} maxLength={2048} /></label>
    </CardContent></Card>
    {state.pages.length > 0 && <Card><CardHeader><CardTitle className="text-base">Переопределения страниц</CardTitle><CardDescription>Пустые поля наследуют общие настройки. Адресные параметры не меняют SEO этой страницы.</CardDescription></CardHeader><CardContent className="space-y-3">{state.pages.map((page) => {
      const override = state.draft.pages?.[page.id];
      return <details key={page.id} className="group rounded-md border border-border"><summary className="cursor-pointer list-none px-4 py-3 text-sm font-medium">{page.name}<span className="ml-2 text-xs font-normal text-muted-foreground">{page.id}</span></summary><div className="grid gap-4 border-t border-border p-4 md:grid-cols-2">
        <div className="space-y-1.5"><Label>Название страницы</Label><Input value={inherited(override, "siteName", state.draft.siteName)} onChange={(event) => updatePage(page.id, "siteName", event.currentTarget.value || undefined)} /></div>
        <MediaField label="Favicon страницы" value={override?.faviconMediaId ?? state.draft.faviconMediaId} onChange={(faviconMediaId) => updatePage(page.id, "faviconMediaId", faviconMediaId)} />
        <div className="space-y-1.5 md:col-span-2"><Label>Описание страницы</Label><Textarea value={inherited(override, "description", state.draft.description)} onChange={(event) => updatePage(page.id, "description", event.currentTarget.value || undefined)} rows={2} /></div>
        <div className="space-y-1.5"><Label>Open Graph title</Label><Input value={inherited(override, "ogTitle", state.draft.ogTitle || state.draft.siteName)} onChange={(event) => updatePage(page.id, "ogTitle", event.currentTarget.value || undefined)} /></div>
        <MediaField label="Open Graph image страницы" value={override?.ogImageMediaId ?? state.draft.ogImageMediaId} onChange={(ogImageMediaId) => updatePage(page.id, "ogImageMediaId", ogImageMediaId)} />
        <div className="space-y-1.5 md:col-span-2"><Label>Open Graph description</Label><Textarea value={inherited(override, "ogDescription", state.draft.ogDescription || state.draft.description)} onChange={(event) => updatePage(page.id, "ogDescription", event.currentTarget.value || undefined)} rows={2} /></div>
        {override && <Button type="button" size="sm" variant="ghost" className="md:col-span-2 md:justify-self-start" onClick={() => { const pages = { ...(state.draft.pages ?? {}) }; delete pages[page.id]; update({ ...state.draft, pages }); }}>Сбросить переопределения страницы</Button>}
      </div></details>;
    })}</CardContent></Card>}
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/20 p-4"><div><p className="text-sm font-medium">Публикация метаданных</p><p className="mt-1 text-xs text-muted-foreground">Публикация обновит HTML shell маршрутов без создания нового снапшота.</p></div><div className="flex flex-wrap gap-2"><Button type="button" variant="outline" onClick={() => void publishDev()} loading={publishing} disabled={dirty}><Send />Опубликовать в dev</Button>{canPromote && <Button type="button" onClick={() => void promoteProd()} loading={publishing}><Send />Продвинуть в prod</Button>}</div></div>
  </div>;
}
