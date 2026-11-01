import { useEffect, useMemo, useState } from "react";
import { Check, Image, Monitor, RotateCcw, Save, Smartphone, Tablet, Type } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { MediaPickerDialog } from "@/domains/media/MediaPickerDialog";
import type { DesignFont, DesignSystem, DesignTokenDefinition, Media, SiteConfig } from "@/types";
import { describeRoutes } from "./route-tree";

type SavedState = { revision: number; draft: DesignSystem };
type Viewport = "desktop" | "tablet" | "mobile";
type RoutePreviewOption = { path: string; page: SiteConfig["pages"][number] };

function colorParts(value: string): { hex: string; alpha: number } {
  if (value.startsWith("#")) {
    let hex = value.slice(1);
    if (hex.length === 3) hex = hex.split("").map((part) => part + part).join("");
    if (hex.length === 8) return { hex: `#${hex.slice(0, 6)}`, alpha: Math.round(Number.parseInt(hex.slice(6), 16) / 255 * 100) };
    return { hex: `#${hex.slice(0, 6)}`, alpha: 100 };
  }
  const rgb = value.match(/([\d.]+)[ ,]+([\d.]+)[ ,]+([\d.]+)/);
  const alpha = value.match(/\/\s*([\d.]+)%/);
  if (!rgb) return { hex: "#ffffff", alpha: 100 };
  const hex = rgb.slice(1, 4).map((part) => Math.max(0, Math.min(255, Math.round(Number(part)))).toString(16).padStart(2, "0")).join("");
  return { hex: `#${hex}`, alpha: alpha ? Number(alpha[1]) : 100 };
}

function colorValue(hex: string, alpha: number): string {
  if (alpha >= 100) return hex;
  const { hex: normalized } = colorParts(hex);
  const rgb = [1, 3, 5].map((offset) => Number.parseInt(normalized.slice(offset, offset + 2), 16));
  return `rgb(${rgb.join(" ")} / ${alpha}%)`;
}

function numericValue(raw: string): { value: number; unit: string } {
  const match = raw.match(/(-?\d+(?:\.\d+)?)\s*(rem|px|vw|svh|em|%)?/);
  return match ? { value: Number(match[1]), unit: match[2] ?? "" } : { value: 0, unit: "" };
}

const units = ["", "rem", "px", "em", "ch", "vw", "svh", "%"];
const aspectRatios = ["1 / 1", "4 / 3", "3 / 2", "16 / 9"];
const effects: Record<string, Array<{ label: string; value: string }>> = {
  "--text-shadow-on-image": [
    { label: "Мягкая", value: "0 1px 0 var(--color-ink-muted)" },
    { label: "Без тени", value: "none" },
    { label: "Выразительная", value: "0 2px 8px var(--color-ink-muted)" },
  ],
  "--page-image-overlay": [
    { label: "Лёгкое затемнение", value: "linear-gradient(var(--color-page-wash), var(--color-page-wash))" },
    { label: "Без затемнения", value: "linear-gradient(transparent, transparent)" },
    { label: "Тёмное затемнение", value: "linear-gradient(rgb(20 48 58 / 28%), rgb(20 48 58 / 28%))" },
  ],
  "--page-fallback": [
    { label: "Фирменный градиент", value: "linear-gradient(165deg, var(--color-sky-deep) 0%, var(--color-sky) 36%, var(--color-field) 68%, var(--color-field-deep) 100%)" },
    { label: "Синее небо", value: "linear-gradient(165deg, var(--color-sky-deep), var(--color-sky))" },
    { label: "Зелёное поле", value: "linear-gradient(165deg, var(--color-field), var(--color-field-deep))" },
  ],
  "--page-background-plain": [
    { label: "Тёмное поле", value: "linear-gradient(var(--color-field-deep), var(--color-field-deep))" },
    { label: "Глубокое небо", value: "linear-gradient(var(--color-sky-deep), var(--color-sky-deep))" },
    { label: "Светлая бумага", value: "linear-gradient(var(--color-paper), var(--color-paper))" },
  ],
  "--page-background-image": [
    { label: "Однотонная заливка", value: "var(--page-background-plain)" },
    { label: "Тонкая заливка", value: "linear-gradient(rgb(20 48 58 / 8%), rgb(20 48 58 / 8%))" },
    { label: "Без слоя", value: "linear-gradient(transparent, transparent)" },
  ],
  "--button-shadow": [
    { label: "Мягкая", value: "0 0.5rem 1.75rem rgb(15 40 49 / 16%)" },
    { label: "Без тени", value: "none" },
    { label: "Выразительная", value: "0 0.75rem 2.5rem rgb(15 40 49 / 28%)" },
  ],
  "--motion-standard": [
    { label: "Быстро", value: "120ms ease" },
    { label: "Стандартно", value: "180ms ease" },
    { label: "Плавно", value: "300ms ease" },
    { label: "Плавный выход", value: "180ms ease-out" },
  ],
};

function normalizeFont(font: DesignFont): DesignFont {
  return { family: font.family, mediaId: font.mediaId, weight: font.weight || "100 900", style: font.style || "normal", ...(font.unicodeRange ? { unicodeRange: font.unicodeRange } : {}) };
}

export default function DesignSystemPage() {
  const [saved, setSaved] = useState<SavedState | null>(null);
  const [draft, setDraft] = useState<DesignSystem | null>(null);
  const [definitions, setDefinitions] = useState<DesignTokenDefinition[]>([]);
  const [config, setConfig] = useState<SiteConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [logoPicker, setLogoPicker] = useState(false);
  const [fontPicker, setFontPicker] = useState(false);
  const [fontEditor, setFontEditor] = useState<Media | null>(null);
  const [fontFamily, setFontFamily] = useState("");
  const [fontWeight, setFontWeight] = useState("100 900");
  const [fontUnicodeRange, setFontUnicodeRange] = useState("");
  const [previewToken, setPreviewToken] = useState("");
  const [previewPage, setPreviewPage] = useState("/");
  const [viewport, setViewport] = useState<Viewport>("desktop");
  const [previewError, setPreviewError] = useState("");

  const load = async () => {
    setLoading(true);
    setLoadError("");
    try {
      const [theme, siteDraft] = await Promise.all([api.siteBuilder.designSystem(), api.siteBuilder.draft()]);
      setSaved(theme.state);
      setDraft(theme.state.draft);
      setDefinitions(theme.definitions);
      setConfig(siteDraft.config);
      setConflict(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Не удалось загрузить дизайн-систему";
      setLoadError(message);
      toast.error(message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const routeOptions = useMemo(() => {
    if (!config) return [];
    return describeRoutes(config.routeTree)
      .filter((route) => Boolean(route.node.pageId))
      .map((route) => ({ path: route.path, page: config.pages.find((page) => page.id === route.node.pageId) }))
      .filter((item): item is RoutePreviewOption => Boolean(item.page));
  }, [config]);

  const homeRoute = routeOptions.find((route) => route.path === "/") ?? routeOptions.find((route) => /главн|home/i.test(route.page.name));
  const catalogRoute = routeOptions.find((route) => route.page.kind === "collection-list" || /каталог|catalog/i.test(route.page.name));
  const pageOptions = [homeRoute, catalogRoute].filter((item): item is NonNullable<typeof item> => Boolean(item));
  const dirty = Boolean(saved && draft && JSON.stringify(saved.draft) !== JSON.stringify(draft));
  const groups = useMemo(() => [...new Set(definitions.map((token) => token.group))], [definitions]);

  useEffect(() => {
    if (!draft || !homeRoute) return;
    let active = true;
    const timer = window.setTimeout(() => {
      setPreviewError("");
      void api.siteBuilder.previewDesignSystem(draft)
        .then(({ token }) => { if (active) setPreviewToken(token); })
        .catch((error: unknown) => {
          if (!active) return;
          setPreviewError(error instanceof Error ? error.message : "Предпросмотр пока недоступен");
        });
    }, 500);
    return () => { active = false; window.clearTimeout(timer); };
  }, [draft, homeRoute]);

  useEffect(() => {
    if (pageOptions.some((option) => option.path === previewPage)) return;
    setPreviewPage(homeRoute?.path ?? pageOptions[0]?.path ?? "/");
  }, [pageOptions, previewPage, homeRoute]);

  const patchDraft = (patch: Partial<DesignSystem>) => setDraft((current) => current ? { ...current, ...patch } : current);
  const setToken = (key: string, value: string, scope?: string) => setDraft((current) => {
    if (!current) return current;
    return scope === "mobile"
      ? { ...current, mobileTokens: { ...current.mobileTokens, [key]: value } }
      : { ...current, tokens: { ...current.tokens, [key]: value } };
  });

  const save = async () => {
    if (!saved || !draft || !dirty) return;
    setSaving(true);
    setConflict(false);
    try {
      const result = await api.siteBuilder.saveDesignSystem(saved.revision, draft);
      setSaved(result.state);
      setDraft(result.state.draft);
      setDefinitions(result.definitions);
      toast.success("Дизайн-система сохранена. Она попадёт в следующий снапшот.");
    } catch (error) {
      if (isApiError(error) && error.status === 409) setConflict(true);
      toast.error(isApiError(error) ? error.message : "Не удалось сохранить дизайн-систему");
    } finally {
      setSaving(false);
    }
  };

  const previewURL = useMemo(() => {
    if (!previewToken) return "";
    const url = new URL(previewPage || "/", window.location.origin);
    url.searchParams.set("__designPreview", previewToken);
    return `${url.pathname}${url.search}`;
  }, [previewPage, previewToken]);

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Загружаем дизайн-систему…</div>;
  if (!draft || !saved) return <div className="space-y-3 p-8"><p className="text-sm text-destructive">{loadError || "Не удалось загрузить дизайн-систему."}</p><Button variant="outline" onClick={() => void load()}><RotateCcw />Повторить</Button></div>;

  const sectionList = groups.map((group) => ({ group, tokens: definitions.filter((token) => token.group === group) }));
  const viewportClass = viewport === "desktop" ? "w-full" : viewport === "tablet" ? "w-[768px] max-w-full" : "w-[390px] max-w-full";

  return <div className="space-y-5 p-4 sm:p-6">
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div><p className="text-sm text-muted-foreground">Настройки сайта</p><h1 className="mt-1 text-2xl font-semibold tracking-tight">Дизайн-система</h1><p className="mt-1 max-w-2xl text-sm text-muted-foreground">Общие цвета, типографика и брендовые файлы. Сохранённые значения закрепляются за новым снапшотом.</p></div>
      <div className="flex items-center gap-3">
        <span className={`text-xs ${dirty ? "text-amber-700 dark:text-amber-300" : "text-emerald-700 dark:text-emerald-300"}`}>{dirty ? "Есть несохранённые изменения" : `Сохранено · ревизия ${saved.revision}`}</span>
        {conflict && <Button variant="outline" size="sm" onClick={() => void load()}><RotateCcw />Обновить</Button>}
        <Button size="sm" onClick={() => void save()} disabled={!dirty || saving}>{saving ? <Save /> : dirty ? <Save /> : <Check />}{saving ? "Сохраняем…" : "Сохранить"}</Button>
      </div>
    </div>

    <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(28rem,0.95fr)]">
      <div className="space-y-4">
        {sectionList.map(({ group, tokens }) => <Card key={group}>
          <CardHeader className="pb-3"><CardTitle className="text-base">{group}</CardTitle><CardDescription>{group === "Цвета" ? "Палитра и прозрачность общих поверхностей." : group === "Типографика" ? "Размеры, ритм и начертания текста." : group === "Размеры и сетка" ? "Отступы и адаптивная геометрия страницы." : group === "Радиусы и границы" ? "Скругления и толщина линий." : group === "Эффекты" ? "Предустановленные тени и слои." : "Длительность и характер переходов."}</CardDescription></CardHeader>
          <CardContent className="grid gap-3 sm:grid-cols-2">
            {tokens.map((token) => {
              const value = (token.scope === "mobile" ? draft.mobileTokens[token.key] : draft.tokens[token.key]) ?? token.default;
              return <div key={`${token.scope ?? "desktop"}:${token.key}`} className="min-w-0 rounded-md border border-border p-3">
              <Label className="text-sm font-medium">{token.label}</Label>
              {token.kind === "color" ? <div className="mt-2 flex items-center gap-3">
                <input type="color" aria-label={token.label} className="size-10 cursor-pointer rounded border border-border bg-transparent p-1" value={colorParts(value).hex} onChange={(event) => { const current = colorParts(value); setToken(token.key, colorValue(event.currentTarget.value, current.alpha), token.scope); }} />
                <div className="min-w-0 flex-1"><div className="flex justify-between text-xs text-muted-foreground"><span>Прозрачность</span><span>{Math.round(colorParts(value).alpha)}%</span></div><input className="mt-2 w-full accent-primary" type="range" min="0" max="100" value={colorParts(value).alpha} onChange={(event) => { const current = colorParts(value); setToken(token.key, colorValue(current.hex, Number(event.currentTarget.value)), token.scope); }} /></div>
              </div> : effects[token.key] ? <select className="mt-2 h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={value} onChange={(event) => setToken(token.key, event.currentTarget.value, token.scope)}>{effects[token.key].map((option) => <option key={option.label} value={option.value}>{option.label}</option>)}</select> : token.key === "--product-visual-ratio" ? <select className="mt-2 h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={value} onChange={(event) => setToken(token.key, event.currentTarget.value, token.scope)}>{aspectRatios.map((ratio) => <option key={ratio} value={ratio}>{ratio}</option>)}</select> : token.key.startsWith("--weight-") ? <select className="mt-2 h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={value} onChange={(event) => setToken(token.key, event.currentTarget.value, token.scope)}>{[300, 400, 500, 600, 700, 800, 900].map((weight) => <option key={weight} value={String(weight)}>{weight}</option>)}</select> : <div className="mt-2 flex items-center gap-2"><Input aria-label={token.label} type="number" step="0.01" value={numericValue(value).value} onChange={(event) => { const current = numericValue(value); const next = event.currentTarget.value; if (next !== "") setToken(token.key, `${next}${current.unit}`, token.scope); }} /><select aria-label={`Единица: ${token.label}`} className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={numericValue(value).unit} onChange={(event) => { const current = numericValue(value); setToken(token.key, `${current.value}${event.currentTarget.value}`, token.scope); }}>{units.map((unit) => <option key={unit || "unitless"} value={unit}>{unit || "без единицы"}</option>)}</select></div>}
              {token.kind === "value" && !effects[token.key] && !token.key.startsWith("--weight-") && /clamp\(|min\(|max\(/.test(token.default) && <p className="mt-2 text-xs text-muted-foreground">Текущее значение адаптивное. Изменение заменит его выбранным числом.</p>}
            </div>;
            })}
          </CardContent>
        </Card>)}

        <Card>
          <CardHeader className="pb-3"><CardTitle className="text-base">Шрифты и бренд</CardTitle><CardDescription>Файлы принадлежат дизайн-системе и доступны всем фичам сайта.</CardDescription></CardHeader>
          <CardContent className="space-y-5">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border p-3"><div className="flex items-center gap-3"><div className="grid size-12 place-items-center overflow-hidden rounded-md bg-muted">{draft.brandLogoMediaId ? <img className="max-h-full max-w-full object-contain" src={`/api/v2/system-assets/${draft.brandLogoMediaId}`} alt="Логотип" /> : <Image className="size-5 text-muted-foreground" />}</div><div><p className="text-sm font-medium">Общий логотип</p><p className="text-xs text-muted-foreground">Используется шапкой сайта.</p></div></div><div className="flex gap-2">{draft.brandLogoMediaId && <Button variant="ghost" size="sm" onClick={() => patchDraft({ brandLogoMediaId: undefined })}>Убрать</Button>}<Button variant="outline" size="sm" onClick={() => setLogoPicker(true)}>{draft.brandLogoMediaId ? "Заменить" : "Выбрать файл"}</Button></div></div>
            <div className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-3"><div><p className="text-sm font-medium">Файлы шрифтов WOFF2</p><p className="text-xs text-muted-foreground">Поддерживаются статические и variable начертания.</p></div><Button variant="outline" size="sm" onClick={() => setFontPicker(true)}><Type />Добавить шрифт</Button></div>
              {draft.fonts.length === 0 ? <p className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground">Шрифты пока не загружены. Для опубликованной темы можно назначить шрифт каждой роли ниже.</p> : draft.fonts.map((font, index) => <div key={`${font.mediaId}-${index}`} className="grid gap-2 rounded-md border border-border p-3 sm:grid-cols-[1fr_8rem_8rem_auto]"><div><p className="text-sm font-medium">{font.family}</p><p className="text-xs text-muted-foreground">{font.weight} · {font.style} · {font.mediaId.slice(0, 8)}{font.unicodeRange ? ` · ${font.unicodeRange}` : ""}</p></div><span className="self-center text-xs text-muted-foreground">{font.mediaId.slice(0, 12)}</span><span className="self-center text-xs text-muted-foreground">WOFF2</span><Button size="sm" variant="ghost" className="text-destructive" onClick={() => { const fonts = draft.fonts.filter((_, fontIndex) => fontIndex !== index); const familyStillAvailable = fonts.some((item) => item.family === font.family); patchDraft({ fonts, fontRoles: familyStillAvailable ? draft.fontRoles : Object.fromEntries(Object.entries(draft.fontRoles).map(([role, family]) => [role, family === font.family ? "sans-serif" : family])) as DesignSystem["fontRoles"] }); }}>Удалить</Button></div>)}
            </div>
            <div className="grid gap-3 sm:grid-cols-2">{(["body", "display", "header", "hero"] as const).map((role) => <label key={role} className="space-y-1.5"><span className="text-sm font-medium">{({ body: "Основной текст", display: "Заголовки", header: "Шапка", hero: "Первый экран" })[role]}</span><select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={draft.fontRoles[role]} onChange={(event) => patchDraft({ fontRoles: { ...draft.fontRoles, [role]: event.currentTarget.value } })}><option value="sans-serif">Системный sans-serif</option><option value="system-ui">System UI</option><option value="serif">Системный serif</option><option value="Alumni Sans SC">Alumni Sans SC</option>{[...new Set(draft.fonts.map((font) => font.family))].filter((family) => family !== "Alumni Sans SC").map((family) => <option key={family} value={family}>{family}</option>)}</select></label>)}</div>
          </CardContent>
        </Card>
      </div>

      <Card className="xl:sticky xl:top-4">
        <CardHeader className="gap-3"><div><CardTitle className="text-base">Предпросмотр</CardTitle><CardDescription>Публичная страница с несохранённой темой.</CardDescription></div>
          <div className="flex flex-wrap items-center gap-2"><select aria-label="Страница предпросмотра" className="h-9 min-w-40 flex-1 rounded-md border border-input bg-background px-3 text-sm" value={previewPage} onChange={(event) => setPreviewPage(event.currentTarget.value)}>{pageOptions.map((option) => <option key={option.path} value={option.path}>{option.page.name} · {option.path}</option>)}</select><div className="flex rounded-md border border-border p-0.5">{(["desktop", "tablet", "mobile"] as const).map((size) => { const Icon = size === "desktop" ? Monitor : size === "tablet" ? Tablet : Smartphone; return <Button key={size} variant={viewport === size ? "secondary" : "ghost"} size="icon-sm" aria-label={size} onClick={() => setViewport(size)}><Icon /></Button>; })}</div></div>
        </CardHeader>
        <CardContent>
          <div className="flex min-h-[34rem] items-start justify-center overflow-auto rounded-lg border border-border bg-muted/40 p-2 sm:p-4">
            {previewError ? <div className="m-auto max-w-sm text-center"><p className="font-medium">Предпросмотр недоступен</p><p className="mt-2 text-sm text-muted-foreground">{previewError}</p></div> : previewToken ? <iframe key={`${previewPage}:${previewToken}`} title="Предпросмотр сайта" src={previewURL} className={`${viewportClass} h-[34rem] shrink-0 rounded-md border border-border bg-background shadow-sm`} /> : <div className="m-auto text-sm text-muted-foreground">Готовим предпросмотр…</div>}
          </div>
          {!catalogRoute && <p className="mt-3 text-xs text-muted-foreground">Каталог появится в предпросмотре после добавления соответствующего маршрута.</p>}
          {!previewError && !previewToken && <p className="mt-3 text-xs text-muted-foreground">Для отображения страницы нужен активный dev-снапшот.</p>}
        </CardContent>
      </Card>
    </div>

    <MediaPickerDialog open={logoPicker} onClose={() => setLogoPicker(false)} accept="image" onSelect={(items) => { const selected = items[0]; if (selected) patchDraft({ brandLogoMediaId: selected.id }); }} initialSelected={draft.brandLogoMediaId ? [draft.brandLogoMediaId] : []} title="Логотип сайта" />
    <MediaPickerDialog open={fontPicker} onClose={() => setFontPicker(false)} accept="font" onSelect={(items) => { const selected = items[0]; if (!selected) return; setFontEditor(selected); const name = selected.original_filename.replace(/\.woff2$/i, ""); const subset = name.match(/alumni-sans-sc-(cyrillic-ext|cyrillic|vietnamese|latin-ext|latin)-wght-normal/i)?.[1]; const ranges: Record<string, string> = { "cyrillic-ext": "U+0460-052F,U+1C80-1C8A,U+20B4,U+2DE0-2DFF,U+A640-A69F,U+FE2E-FE2F", cyrillic: "U+0301,U+0400-045F,U+0490-0491,U+04B0-04B1,U+2116", vietnamese: "U+0102-0103,U+0110-0111,U+0128-0129,U+0168-0169,U+01A0-01A1,U+01AF-01B0,U+0300-0301,U+0303-0304,U+0308-0309,U+0323,U+0329,U+1EA0-1EF9,U+20AB", "latin-ext": "U+0100-02BA,U+02BD-02C5,U+02C7-02CC,U+02CE-02D7,U+02DD-02FF,U+0304,U+0308,U+0329,U+1D00-1DBF,U+1E00-1E9F,U+1EF2-1EFF,U+2020,U+20A0-20AB,U+20AD-20C0,U+2113,U+2C60-2C7F,U+A720-A7FF", latin: "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD" }; setFontFamily(subset ? "Alumni Sans SC" : name.replace(/[-_]+/g, " ")); setFontWeight("100 900"); setFontUnicodeRange(subset ? ranges[subset] : ""); }} title="Файлы шрифтов" />
    {fontEditor && <div className="fixed inset-0 z-50 grid place-items-center bg-black/40 p-4"><Card className="w-full max-w-md"><CardHeader><CardTitle className="text-base">Добавить шрифт</CardTitle><CardDescription>{fontEditor.original_filename}</CardDescription></CardHeader><CardContent className="space-y-4"><div className="space-y-1.5"><Label htmlFor="font-family">Название семейства</Label><Input id="font-family" value={fontFamily} onChange={(event) => setFontFamily(event.currentTarget.value)} /></div><div className="space-y-1.5"><Label htmlFor="font-weight">Вес или диапазон variable</Label><Input id="font-weight" value={fontWeight} onChange={(event) => setFontWeight(event.currentTarget.value)} placeholder="400 или 100 900" /></div><div className="space-y-1.5"><Label htmlFor="font-unicode-range">Набор символов (для подмножеств)</Label><Input id="font-unicode-range" value={fontUnicodeRange} onChange={(event) => setFontUnicodeRange(event.currentTarget.value)} placeholder="U+0400-045F,U+2116" /></div><div className="flex justify-end gap-2"><Button variant="outline" onClick={() => setFontEditor(null)}>Отмена</Button><Button onClick={() => { const font = normalizeFont({ family: fontFamily.trim(), mediaId: fontEditor.id, weight: fontWeight.trim(), style: "normal", unicodeRange: fontUnicodeRange.trim() || undefined }); patchDraft({ fonts: [...draft.fonts.filter((item) => !(item.mediaId === font.mediaId && item.unicodeRange === font.unicodeRange)), font] }); setFontEditor(null); }}>Добавить</Button></div></CardContent></Card></div>}
  </div>;
}
