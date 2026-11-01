import { useCallback, useEffect, useMemo, useState } from "react";
import { Check, Image, RotateCcw, Save, Type } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { ContextHelp, PageHeader, useUnsavedChangesGuard } from "@/components/common/page";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input, Label } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { MediaPickerDialog } from "@/domains/media/MediaPickerDialog";
import { cssColorValue, parseCssColor } from "@/lib/css-color";
import type { DesignFont, DesignSystem, DesignSystemState, DesignTokenDefinition, Media } from "@/types";

const BRAND_TAB = "__brand";
const units = ["", "rem", "px", "em", "ch", "vw", "svh", "%"];
const aspectRatios = ["1 / 1", "4 / 3", "3 / 2", "16 / 9"];
const weightValues = [300, 400, 500, 600, 700, 800, 900];
const roleLabels = { body: "Основной текст", display: "Заголовки", header: "Шапка", hero: "Первый экран" } as const;
const groupDescriptions: Record<string, string> = {
  Цвета: "Палитра, текст и поверхности сайта.",
  Типографика: "Размеры, интервалы и начертания текста.",
  "Размеры и сетка": "Отступы, размеры блоков и адаптивная геометрия.",
  "Радиусы и границы": "Скругления углов и толщина линий.",
  Эффекты: "Тени, затемнения и фоновые заливки.",
};

function HelpTip({ children }: { children: string }) {
  return <ContextHelp>{children}</ContextHelp>;
}

type SavedState = Pick<DesignSystemState, "revision" | "draft">;

function parseScalar(raw: string): { value: number; unit: string } | null {
  const match = raw.trim().match(/^(-?\d+(?:\.\d+)?)(rem|px|vw|svh|em|ch|%)?$/);
  return match ? { value: Number(match[1]), unit: match[2] ?? "" } : null;
}

function normalizeFont(font: DesignFont): DesignFont {
  return {
    family: font.family,
    mediaId: font.mediaId,
    weight: font.weight || "100 900",
    style: font.style || "normal",
    ...(font.unicodeRange ? { unicodeRange: font.unicodeRange } : {}),
  };
}

function TokenEditor({
  definition,
  value,
  onChange,
  onReset,
}: {
  definition: DesignTokenDefinition;
  value: string;
  onChange: (value: string) => void;
  onReset: () => void;
}) {
  const color = parseCssColor(value) ?? { hex: "#ffffff", alpha: 100 };
  const scalar = parseScalar(value);
  const options = definition.key === "--product-visual-ratio"
    ? aspectRatios
    : definition.key.startsWith("--weight-")
      ? weightValues.map(String)
      : null;
  const isKnownOption = options?.includes(value) ?? false;
  const inputId = `token-${definition.key.replace(/[^a-z0-9-]/gi, "-")}-${definition.scope ?? "base"}`;

  return (
    <article className="min-w-0 border-b border-border/70 py-4 last:border-0 last:pb-0">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Label htmlFor={inputId} className="text-sm font-medium">{definition.label}</Label>
          <p className="mt-0.5 break-all font-mono text-[11px] leading-4 text-muted-foreground">{definition.key}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {definition.scope === "mobile" && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">Мобильный</span>}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`Сбросить «${definition.label}»`}
                disabled={value === definition.default}
                onClick={onReset}
              >
                <RotateCcw />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Вернуть исходное значение</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {definition.kind === "color" ? (
        <div className="mt-3 space-y-2">
          <div className="flex items-center gap-2">
            <input
              type="color"
              aria-label={`Цвет: ${definition.label}`}
              className="size-10 shrink-0 cursor-pointer rounded border border-input bg-background p-1"
              value={color.hex}
              onChange={(event) => onChange(cssColorValue(event.currentTarget.value, color.alpha))}
            />
            <Input id={inputId} aria-label={`CSS-значение: ${definition.label}`} className="font-mono text-xs" value={value} onChange={(event) => onChange(event.currentTarget.value)} />
            <span className="size-8 shrink-0 rounded border border-border" style={{ backgroundColor: value }} aria-label="Образец цвета" />
          </div>
          <div className="flex items-center gap-3">
            <Label htmlFor={`${inputId}-opacity`} className="shrink-0 text-xs text-muted-foreground">Прозрачность</Label>
            <input id={`${inputId}-opacity`} className="min-w-0 flex-1 accent-primary" type="range" min="0" max="100" value={color.alpha} onChange={(event) => onChange(cssColorValue(color.hex, Number(event.currentTarget.value)))} />
            <span className="w-10 text-right text-xs tabular-nums text-muted-foreground">{color.alpha}%</span>
          </div>
        </div>
      ) : options ? (
        <div className="mt-3 space-y-2">
          <select id={inputId} className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={isKnownOption ? value : "__custom"} onChange={(event) => { if (event.currentTarget.value !== "__custom") onChange(event.currentTarget.value); }}>
            {options.map((option) => <option key={option} value={option}>{option}</option>)}
            {!isKnownOption && <option value="__custom">Своё значение</option>}
          </select>
          {!isKnownOption && <Input aria-label={`Своё значение: ${definition.label}`} className="font-mono text-xs" value={value} onChange={(event) => onChange(event.currentTarget.value)} />}
        </div>
      ) : scalar ? (
        <div className="mt-3 flex gap-2">
          <Input
            id={inputId}
            aria-label={definition.label}
            type="number"
            inputMode="decimal"
            step="any"
            min={definition.min}
            max={definition.max}
            value={scalar.value}
            onChange={(event) => onChange(`${event.currentTarget.value || "0"}${scalar.unit}`)}
          />
          <select
            aria-label={`Единица измерения: ${definition.label}`}
            className="h-9 max-w-32 rounded-md border border-input bg-background px-2 text-sm"
            value={scalar.unit}
            onChange={(event) => onChange(`${scalar.value}${event.currentTarget.value}`)}
          >
            {[...new Set([...units, ...(scalar.unit && !units.includes(scalar.unit) ? [scalar.unit] : [])])].map((unit) => <option key={unit || "unitless"} value={unit}>{unit || "без единицы"}</option>)}
          </select>
        </div>
      ) : (
        <div className="mt-3 space-y-1.5">
          <Label htmlFor={inputId} className="text-xs text-muted-foreground">Значение CSS</Label>
          <Input id={inputId} className="font-mono text-xs" value={value} onChange={(event) => onChange(event.currentTarget.value)} />
          {/^(clamp|min|max)\(/.test(definition.default) && <p className="text-xs text-muted-foreground">Сохраняйте выражение целиком, например clamp(2rem, 4vw, 5rem).</p>}
        </div>
      )}
    </article>
  );
}

export default function DesignSystemPage() {
  const [saved, setSaved] = useState<SavedState | null>(null);
  const [draft, setDraft] = useState<DesignSystem | null>(null);
  const [definitions, setDefinitions] = useState<DesignTokenDefinition[]>([]);
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
  const [fontError, setFontError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    try {
      const theme = await api.siteBuilder.designSystem();
      setSaved(theme.state);
      setDraft(theme.state.draft);
      setDefinitions(theme.definitions);
      setConflict(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Не удалось загрузить дизайн-систему";
      setLoadError(message);
      toast.error(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const dirty = Boolean(saved && draft && JSON.stringify(saved.draft) !== JSON.stringify(draft));
  const guard = useUnsavedChangesGuard(dirty);
  const groups = useMemo(() => [...new Set(definitions.map((token) => token.group))], [definitions]);
  const [activeTab, setActiveTab] = useState("");
  const currentTab = activeTab === BRAND_TAB || groups.includes(activeTab) ? activeTab : groups[0] ?? BRAND_TAB;
  const currentTokens = definitions.filter((token) => token.group === currentTab);

  const patchDraft = (patch: Partial<DesignSystem>) => setDraft((current) => current ? { ...current, ...patch } : current);
  const setToken = (definition: DesignTokenDefinition, value: string) => setDraft((current) => {
    if (!current) return current;
    return definition.scope === "mobile"
      ? { ...current, mobileTokens: { ...current.mobileTokens, [definition.key]: value } }
      : { ...current, tokens: { ...current.tokens, [definition.key]: value } };
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
      toast.success("Дизайн-система сохранена. Она попадёт в новый снапшот.");
    } catch (error) {
      if (isApiError(error) && error.status === 409) setConflict(true);
      toast.error(isApiError(error) ? error.message : "Не удалось сохранить дизайн-систему");
    } finally {
      setSaving(false);
    }
  };

  const selectFont = (media: Media) => {
    setFontEditor(media);
    setFontError("");
    const name = media.original_filename.replace(/\.woff2$/i, "");
    const subset = name.match(/alumni-sans-sc-(cyrillic-ext|cyrillic|vietnamese|latin-ext|latin)-wght-normal/i)?.[1];
    const ranges: Record<string, string> = {
      "cyrillic-ext": "U+0460-052F,U+1C80-1C8A,U+20B4,U+2DE0-2DFF,U+A640-A69F,U+FE2E-FE2F",
      cyrillic: "U+0301,U+0400-045F,U+0490-0491,U+04B0-04B1,U+2116",
      vietnamese: "U+0102-0103,U+0110-0111,U+0128-0129,U+0168-0169,U+01A0-01A1,U+01AF-01B0,U+0300-0301,U+0303-0304,U+0308-0309,U+0323,U+0329,U+1EA0-1EF9,U+20AB",
      "latin-ext": "U+0100-02BA,U+02BD-02C5,U+02C7-02CC,U+02CE-02D7,U+02DD-02FF,U+0304,U+0308,U+0329,U+1D00-1DBF,U+1E00-1E9F,U+1EF2-1EFF,U+2020,U+20A0-20AB,U+20AD-20C0,U+2113,U+2C60-2C7F,U+A720-A7FF",
      latin: "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD",
    };
    setFontFamily(subset ? "Alumni Sans SC" : name.replace(/[-_]+/g, " "));
    setFontWeight("100 900");
    setFontUnicodeRange(subset ? ranges[subset] : "");
  };

  const addFont = () => {
    if (!fontEditor || !draft) return;
    if (!fontFamily.trim()) { setFontError("Введите название семейства шрифта."); return; }
    if (!/^(100|200|300|400|500|600|700|800|900)( (100|200|300|400|500|600|700|800|900))?$/.test(fontWeight.trim())) {
      setFontError("Укажите вес 100–900 или диапазон, например 100 900.");
      return;
    }
    if (fontUnicodeRange && !/^U\+[0-9A-F?]{1,6}(?:-[0-9A-F?]{1,6})?(?:\s*,\s*U\+[0-9A-F?]{1,6}(?:-[0-9A-F?]{1,6})?)*$/i.test(fontUnicodeRange.trim())) {
      setFontError("Проверьте формат диапазона Unicode: U+0400-045F,U+2116.");
      return;
    }
    const font = normalizeFont({
      family: fontFamily.trim(),
      mediaId: fontEditor.id,
      weight: fontWeight.trim(),
      style: "normal",
      unicodeRange: fontUnicodeRange.trim() || undefined,
    });
    patchDraft({ fonts: [...draft.fonts.filter((item) => !(item.mediaId === font.mediaId && item.unicodeRange === font.unicodeRange)), font] });
    setFontEditor(null);
  };

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Загружаем дизайн-систему…</div>;
  if (!draft || !saved) return <div className="space-y-3 p-8"><p className="text-sm text-destructive">{loadError || "Не удалось загрузить дизайн-систему."}</p><Button variant="outline" onClick={() => void load()}><RotateCcw />Повторить</Button></div>;

  const fontFamilies = [...new Set(draft.fonts.map((font) => font.family))];

  return (
    <div className="page-grid">
      <PageHeader
        title="Дизайн-система"
        description="Настройте токены; сохранения войдут в снапшот."
        actions={(
          <>
            <span role="status" aria-live="polite" className={`text-xs ${dirty ? "text-warning" : "text-success"}`}>
              {dirty ? "Есть несохранённые изменения" : `Сохранено · ревизия ${saved.revision}`}
            </span>
            {conflict && <Button type="button" variant="outline" size="sm" onClick={() => void load()}><RotateCcw />Загрузить актуальную</Button>}
            {dirty && <Button type="button" variant="outline" size="sm" onClick={() => { setDraft(saved.draft); toast.success("Изменения отменены"); }} disabled={saving}><RotateCcw />Отменить</Button>}
            <Button type="button" onClick={() => void save()} disabled={!dirty || saving} loading={saving}>
              {dirty ? <Save /> : <Check />}{saving ? "Сохраняем…" : "Сохранить"}
            </Button>
          </>
        )}
      />

      <Tabs value={currentTab} onValueChange={setActiveTab}>
        <TabsList className="h-auto w-full flex-wrap justify-start gap-1 rounded-md bg-muted/70 p-1.5">
          {groups.map((group) => (
            <TabsTrigger key={group} value={group} className="shrink-0 px-3 py-2">
              {group}<span className="ml-1 rounded bg-muted px-1.5 text-[10px] text-muted-foreground">{definitions.filter((token) => token.group === group).length}</span>
            </TabsTrigger>
          ))}
          <TabsTrigger value={BRAND_TAB} className="shrink-0 px-3 py-2">Шрифты и бренд</TabsTrigger>
        </TabsList>

        {groups.map((group) => (
          <TabsContent key={group} value={group} className="mt-4">
            <Card>
              <CardHeader className="flex-row items-center justify-between pb-2">
                <CardTitle className="text-base">{group}</CardTitle>
                <HelpTip>
                  {groupDescriptions[group] ?? "Параметры визуального оформления сайта."}
                </HelpTip>
              </CardHeader>
              <CardContent className="grid gap-x-8 gap-y-0 md:grid-cols-2">
                {currentTab === group && currentTokens.map((definition) => {
                  const value = (definition.scope === "mobile" ? draft.mobileTokens[definition.key] : draft.tokens[definition.key]) ?? definition.default;
                  return <TokenEditor
                    key={`${definition.scope ?? "base"}:${definition.key}`}
                    definition={definition}
                    value={value}
                    onChange={(next) => setToken(definition, next)}
                    onReset={() => setToken(definition, definition.default)}
                  />;
                })}
              </CardContent>
            </Card>
          </TabsContent>
        ))}

        <TabsContent value={BRAND_TAB} className="mt-4">
          <div className="space-y-4">
            <Card>
              <CardHeader className="flex-row items-center justify-between pb-2"><CardTitle className="text-base">Логотип сайта</CardTitle><HelpTip>Логотип отображается в шапке публичного сайта.</HelpTip></CardHeader>
              <CardContent>
                <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="grid size-12 shrink-0 place-items-center overflow-hidden rounded-lg bg-muted">
                      {draft.brandLogoMediaId ? <img className="max-h-full max-w-full object-contain" src={`/api/v2/system-assets/${draft.brandLogoMediaId}`} alt="Текущий логотип" /> : <Image className="size-5 text-muted-foreground" />}
                    </div>
                    <div className="flex items-center gap-1.5"><p className="text-sm font-medium">{draft.brandLogoMediaId ? "Логотип выбран" : "Логотип не выбран"}</p>{!draft.brandLogoMediaId && <HelpTip>Выберите изображение из медиатеки.</HelpTip>}</div>
                  </div>
                  <div className="flex gap-2">
                    {draft.brandLogoMediaId && <Button type="button" variant="ghost" size="sm" onClick={() => patchDraft({ brandLogoMediaId: undefined })}>Убрать</Button>}
                    <Button type="button" variant="outline" size="sm" onClick={() => setLogoPicker(true)}>{draft.brandLogoMediaId ? "Заменить" : "Выбрать файл"}</Button>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex items-center gap-1.5"><CardTitle className="text-base">Шрифты WOFF2</CardTitle><HelpTip>Добавьте файлы шрифтов и назначьте семейства ролям текста.</HelpTip></div>
                  <Button type="button" variant="outline" size="sm" onClick={() => setFontPicker(true)}><Type />Добавить шрифт</Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-5">
                {draft.fonts.length === 0 ? (
                <p className="border-y border-border py-3 text-sm text-muted-foreground">Файлы шрифтов ещё не добавлены. Пока можно использовать системные семейства.</p>
                ) : (
                  <div className="divide-y divide-border">
                    {draft.fonts.map((font, index) => (
                      <div key={`${font.mediaId}-${font.unicodeRange ?? index}`} className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <div className="min-w-0"><p className="text-sm font-medium">{font.family}</p><p className="mt-0.5 break-all text-xs text-muted-foreground">{font.weight} · {font.style} · {font.mediaId.slice(0, 12)}{font.unicodeRange ? ` · ${font.unicodeRange}` : ""}</p></div>
                        <Button type="button" size="sm" variant="ghost" className="text-destructive" onClick={() => {
                          const fonts = draft.fonts.filter((_, fontIndex) => fontIndex !== index);
                          const familyStillAvailable = fonts.some((item) => item.family === font.family);
                          const fontRoles = familyStillAvailable ? draft.fontRoles : Object.fromEntries(Object.entries(draft.fontRoles).map(([role, family]) => [role, family === font.family ? "sans-serif" : family])) as DesignSystem["fontRoles"];
                          patchDraft({ fonts, fontRoles });
                        }}>Удалить</Button>
                      </div>
                    ))}
                  </div>
                )}
                <div className="grid gap-3 sm:grid-cols-2">
                  {(Object.keys(roleLabels) as Array<keyof typeof roleLabels>).map((role) => (
                    <label key={role} className="space-y-1.5">
                      <span className="text-sm font-medium">{roleLabels[role]}</span>
                      <select className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm" value={draft.fontRoles[role] ?? "sans-serif"} onChange={(event) => patchDraft({ fontRoles: { ...draft.fontRoles, [role]: event.currentTarget.value } })}>
                        <option value="sans-serif">Системный sans-serif</option>
                        <option value="system-ui">System UI</option>
                        <option value="serif">Системный serif</option>
                        <option value="Alumni Sans SC">Alumni Sans SC</option>
                        {fontFamilies.filter((family) => family !== "Alumni Sans SC").map((family) => <option key={family} value={family}>{family}</option>)}
                      </select>
                    </label>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>
      </Tabs>

      <MediaPickerDialog open={logoPicker} onClose={() => setLogoPicker(false)} accept="image" onSelect={(items) => { const selected = items[0]; if (selected) patchDraft({ brandLogoMediaId: selected.id }); }} initialSelected={draft.brandLogoMediaId ? [draft.brandLogoMediaId] : []} title="Логотип сайта" />
      <MediaPickerDialog open={fontPicker} onClose={() => setFontPicker(false)} accept="font" onSelect={(items) => { const selected = items[0]; if (selected) selectFont(selected); }} title="Файлы шрифтов" />

      <Dialog open={fontEditor !== null} onOpenChange={(open) => { if (!open) setFontEditor(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Добавить шрифт</DialogTitle>
            <DialogDescription>{fontEditor?.original_filename}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-1.5"><Label htmlFor="font-family">Название семейства</Label><Input id="font-family" autoFocus value={fontFamily} onChange={(event) => setFontFamily(event.currentTarget.value)} placeholder="Например, Inter" /></div>
            <div className="space-y-1.5"><Label htmlFor="font-weight">Вес или диапазон variable</Label><Input id="font-weight" value={fontWeight} onChange={(event) => setFontWeight(event.currentTarget.value)} placeholder="400 или 100 900" /><p className="text-xs text-muted-foreground">Для variable-шрифта укажите диапазон, например 100 900.</p></div>
            <div className="space-y-1.5"><Label htmlFor="font-unicode-range">Набор символов <span className="font-normal text-muted-foreground">(если файл — подмножество)</span></Label><Input id="font-unicode-range" value={fontUnicodeRange} onChange={(event) => setFontUnicodeRange(event.currentTarget.value)} placeholder="U+0400-045F,U+2116" /></div>
            {fontError && <p role="alert" className="text-sm text-destructive">{fontError}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setFontEditor(null)}>Отмена</Button>
            <Button type="button" onClick={addFont}>Добавить шрифт</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {guard.dialog}
    </div>
  );
}
