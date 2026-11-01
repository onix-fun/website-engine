import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { Outlet, useLocation } from "react-router-dom";
import { toast } from "sonner";
import { api, isApiError } from "@/api";
import { ErrorState, useDocumentTitle, useUnsavedChangesGuard } from "@/components/common/page";
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
  const documentTitle = {
    "/site/pages": "Страницы и маршруты",
    "/site/router": "Страницы и маршруты",
    "/site/features": "Реестр фичей",
    "/site/snapshots": "Снапшоты",
    "/site/design-system": "Дизайн-система",
  }[location.pathname] ?? "Сайт";
  useDocumentTitle(documentTitle);
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
  const guard = useUnsavedChangesGuard(dirty);
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
  if (error || !config) return <ErrorState error={error} title="Не удалось загрузить черновик сайта" onRetry={() => void reload()} />;

  const value: Workspace = { config, revision, dirty, saving, conflict, update, save, reload };
  return (
    <WorkspaceContext.Provider value={value}>
      {guard.dialog}
      <div className={canvasRoute ? "h-full min-h-0 overflow-hidden" : "min-w-0"}>
        {children ?? <Outlet />}
      </div>
    </WorkspaceContext.Provider>
  );
}
