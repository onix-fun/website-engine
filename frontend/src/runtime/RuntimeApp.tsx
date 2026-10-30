import {
  Suspense,
  useCallback,
  createElement,
  useEffect,
  useMemo,
  useState,
  type ComponentType,
} from "react";

type RuntimePlacement = {
  id: string;
  slot: "background" | "header" | "main" | "footer";
  featureKey: string;
  version: string;
  mode: "single" | "collection" | "form";
  entryUrl: string;
  styleUrls: string[];
  arguments: Record<string, unknown>;
  content?: Record<string, unknown>;
  collectionEndpoint?: string;
  detailLink?: { routeId: string; field: string; pathTemplate: string };
};

type Composition = {
  snapshot: { id: string; sequence: number; name: string };
  documentTitle?: string;
  page: { id: string; name: string; kind: string };
  sharedStyleUrls?: string[];
  placements: RuntimePlacement[];
};

type FeatureProps = Record<string, unknown> & {
  featureKey: string;
  sdk: RuntimeSDK;
  collectionEndpoint?: string;
  detailLink?: RuntimePlacement["detailLink"];
};

type RuntimeSDK = {
  listCollection: <T>(endpoint: string, options?: {
    field?: string;
    value?: string;
    offset?: number;
    limit?: number;
  }) => Promise<{ items: Array<{ entryId: string; data: T }>; total: number; nextOffset?: number }>;
  submitForm: (featureKey: string, data: Record<string, unknown>) => Promise<{ id: string; status: string }>;
};

type LoadedFeature = { default: ComponentType<FeatureProps> };

const moduleCache = new Map<string, Promise<LoadedFeature>>();
const slotOrder = ["background", "header", "main", "footer"] as const;

const sdk: RuntimeSDK = {
  async listCollection<T>(
    endpoint: string,
    options: { field?: string; value?: string; offset?: number; limit?: number } = {},
  ) {
    const url = new URL(endpoint, window.location.origin);
    if (options.field) url.searchParams.set("field", options.field);
    if (options.value !== undefined) url.searchParams.set("value", options.value);
    if (options.offset !== undefined) url.searchParams.set("offset", String(options.offset));
    if (options.limit !== undefined) url.searchParams.set("limit", String(options.limit));
    const response = await fetch(url, { headers: { Accept: "application/json" } });
    if (!response.ok) throw new Error(`Collection request failed (${response.status})`);
    return response.json() as Promise<{ items: Array<{ entryId: string; data: T }>; total: number; nextOffset?: number }>;
  },
  async submitForm(featureKey, data) {
    const response = await fetch(`/api/v2/forms/${encodeURIComponent(featureKey)}/submissions`, {
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ data }),
    });
    if (!response.ok) {
      const body = await response.json().catch(() => null) as { error?: { message?: string }; message?: string } | null;
      throw new Error(body?.error?.message ?? body?.message ?? `Form request failed (${response.status})`);
    }
    return response.json() as Promise<{ id: string; status: string }>;
  },
};

function featureModule(url: string): Promise<LoadedFeature> {
  const cached = moduleCache.get(url);
  if (cached) return cached;
  const request = import(/* @vite-ignore */ url) as Promise<LoadedFeature>;
  moduleCache.set(url, request);
  void request.catch(() => moduleCache.delete(url));
  return request;
}

function ensureStyles(urls: string[]) {
  for (const href of urls) {
    if (document.querySelector(`link[data-feature-style="${CSS.escape(href)}"]`)) continue;
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = href;
    link.dataset.featureStyle = href;
    document.head.append(link);
  }
}

function FeatureMount({ placement }: { placement: RuntimePlacement }) {
  const [Feature, setFeature] = useState<ComponentType<FeatureProps> | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    ensureStyles(placement.styleUrls);
    void featureModule(placement.entryUrl)
      .then((module) => { if (active) setFeature(() => module.default); })
      .catch((reason: unknown) => {
        if (active) setError(reason instanceof Error ? reason.message : "Не удалось загрузить компонент");
      });
    return () => { active = false; };
  }, [placement.entryUrl, placement.styleUrls]);

  if (error) return <div className="runtime-feature-error">Не удалось загрузить раздел</div>;
  if (!Feature) return <div className="runtime-loading" aria-hidden="true" />;
  const props = {
    ...(placement.content ?? {}),
    ...placement.arguments,
    featureKey: placement.featureKey,
    collectionEndpoint: placement.collectionEndpoint,
    detailLink: placement.detailLink,
    sdk,
  } as FeatureProps;
  return <Suspense fallback={<div className="runtime-loading" aria-hidden="true" />}>{createElement(Feature, props)}</Suspense>;
}

function StatePage({ title, detail, retry }: { title: string; detail: string; retry?: () => void }) {
  return (
    <main className="runtime-state">
      <div className="runtime-state__mark" aria-hidden="true">V</div>
      <p className="runtime-state__eyebrow">VitaSil</p>
      <h1>{title}</h1>
      <p>{detail}</p>
      {retry && <button type="button" onClick={retry}>Повторить</button>}
    </main>
  );
}

export function RuntimeApp() {
  const [path, setPath] = useState(() => window.location.pathname);
  const [composition, setComposition] = useState<Composition | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing" | "error">("loading");
  const [errorMessage, setErrorMessage] = useState("Не удалось загрузить страницу.");
  const pathKey = useMemo(() => `${path}${window.location.search}`, [path]);

  useEffect(() => {
    const update = () => setPath(window.location.pathname);
    window.addEventListener("popstate", update);
    return () => window.removeEventListener("popstate", update);
  }, []);

  const load = useCallback(async () => {
    setState("loading");
    setErrorMessage("Не удалось загрузить страницу.");
    try {
      const response = await fetch(`/api/v2/runtime?path=${encodeURIComponent(pathKey.split("?")[0] || "/")}`, {
        headers: { Accept: "application/json" },
      });
      if (response.status === 404) { setState("missing"); return; }
      if (!response.ok) {
        const body = await response.json().catch(() => null) as { error?: { message?: string }; message?: string } | null;
        setErrorMessage(body?.error?.message ?? body?.message ?? `Сервер ответил с кодом ${response.status}.`);
        setState("error");
        return;
      }
      const result = await response.json() as Composition;
      ensureStyles(result.sharedStyleUrls ?? []);
      setComposition(result);
      if (result.documentTitle) document.title = result.documentTitle;
      setState("ready");
    } catch (reason) {
      setErrorMessage(reason instanceof Error ? reason.message : "Проверьте соединение и попробуйте снова.");
      setState("error");
    }
  }, [pathKey]);

  useEffect(() => { void load(); }, [load]);

  if (state === "loading") return <StatePage title="Загружаем сайт" detail="Подготавливаем страницу и её компоненты." />;
  if (state === "missing") return <StatePage title="Страница не найдена" detail="Проверьте адрес или вернитесь на главную." />;
  if (state === "error") return <StatePage title="Сайт временно недоступен" detail={errorMessage} retry={() => void load()} />;
  if (!composition) return null;

  return (
    <div className={`runtime-page runtime-page--${composition.page.kind}`} data-snapshot={composition.snapshot.id}>
      {slotOrder.flatMap((slot) => composition.placements
        .filter((placement) => placement.slot === slot)
        .map((placement) => (
          <div className={`runtime-slot runtime-slot--${slot}`} key={placement.id}>
            <FeatureMount placement={placement} />
          </div>
        )))}
    </div>
  );
}
