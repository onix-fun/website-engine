import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { AlertCircle, Check, RotateCcw, Save } from "lucide-react";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { Button } from "@/components/ui/button";
import { ErrorState, PageHeader } from "@/components/common/page";
import type { SiteConfig } from "@/types";

type Workspace = {
  config: SiteConfig;
  revision: number;
  dirty: boolean;
  saving: boolean;
  conflict: boolean;
  update: (next: SiteConfig | ((current: SiteConfig) => SiteConfig)) => void;
  save: () => Promise<boolean>;
  reload: () => Promise<void>;
};

const WorkspaceContext = createContext<Workspace | null>(null);

export function useSiteWorkspace() {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error("useSiteWorkspace must be used inside SiteWorkspace");
  return value;
}

export function SiteWorkspace({ children }: { children?: ReactNode }) {
  const location = useLocation();
  const canvasRoute = location.pathname === "/site/pages" || location.pathname === "/site/router";
  const [config, setConfig] = useState<SiteConfig | null>(null);
  const [savedConfig, setSavedConfig] = useState<SiteConfig | null>(null);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const draft = await api.siteBuilder.draft();
      setConfig(draft.config);
      setSavedConfig(draft.config);
      setRevision(draft.revision);
      setConflict(false);
    } catch (reason) {
      setError(reason);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  const dirty = useMemo(
    () => config !== null && savedConfig !== null && JSON.stringify(config) !== JSON.stringify(savedConfig),
    [config, savedConfig],
  );
  const update: Workspace["update"] = (next) => {
    setConfig((current) => current ? (typeof next === "function" ? next(current) : next) : current);
  };
  const save = useCallback(async () => {
    if (!config || !dirty) return true;
    setSaving(true);
    setConflict(false);
    try {
      const draft = await api.siteBuilder.saveDraft(revision, config);
      setConfig((current) => JSON.stringify(current) === JSON.stringify(config) ? draft.config : current);
      setSavedConfig(draft.config);
      setRevision(draft.revision);
      toast.success("Черновик страниц сохранён");
      return true;
    } catch (reason) {
      if (isApiError(reason) && reason.status === 409) setConflict(true);
      toast.error(isApiError(reason) ? reason.message : "Не удалось сохранить черновик");
      return false;
    } finally {
      setSaving(false);
    }
  }, [config, dirty, revision]);

  if (loading) return <div className="p-8 text-sm text-muted-foreground">Загружаем черновик сайта…</div>;
  if (error || !config) return <ErrorState error={error} title="Не удалось загрузить черновик сайта" />;

  const value: Workspace = { config, revision, dirty, saving, conflict, update, save, reload };
  return (
    <WorkspaceContext.Provider value={value}>
      <div className={canvasRoute ? "h-full min-h-0 overflow-hidden" : "space-y-5"}>
        {!canvasRoute && <>
        <PageHeader
          title="Настройки сайта"
          description="Маршруты и страницы используют общий версионируемый черновик. Снапшот публикует только сохранённую версию."
          actions={(
            <Button type="button" onClick={() => void save()} loading={saving} disabled={!dirty}>
              {dirty ? <Save /> : <Check />}{dirty ? "Сохранить черновик" : "Черновик сохранён"}
            </Button>
          )}
        />
        <div className="flex flex-wrap items-center justify-between gap-3 border-y border-border py-2.5 text-sm">
          <div className="flex items-center gap-2">
            <span className={`size-2 rounded-full ${dirty ? "bg-amber-500" : "bg-emerald-500"}`} aria-hidden="true" />
            <span>{dirty ? "Есть несохранённые изменения" : `Сохранённая версия · ревизия ${revision}`}</span>
          </div>
          {dirty && <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={() => void reload()}><RotateCcw />Загрузить сохранённую</Button>}
        </div>
        {conflict && (
          <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
            <span className="flex items-center gap-2"><AlertCircle className="size-4 shrink-0" />Черновик изменился в другой вкладке. Сначала загрузите актуальную ревизию.</span>
            <Button type="button" size="sm" variant="outline" disabled={saving} onClick={() => void reload()}>Обновить</Button>
          </div>
        )}
        <div className="flex flex-wrap gap-2 text-sm">
          <Link className="rounded-md border border-border px-3 py-1.5 hover:bg-muted" to="/site/pages">Страницы и маршруты</Link>
          <Link className="rounded-md border border-border px-3 py-1.5 hover:bg-muted" to="/site/features">Реестр фичей</Link>
          <Link className="rounded-md border border-border px-3 py-1.5 hover:bg-muted" to="/site/snapshots">Снапшоты</Link>
        </div>
        </>}
        {children ?? <Outlet />}
      </div>
    </WorkspaceContext.Provider>
  );
}
