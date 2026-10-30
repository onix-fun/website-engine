import { lazy } from "react";
import {
  createBrowserRouter,
  Navigate,
  Outlet,
  useLocation,
} from "react-router-dom";
import { useAuth } from "@/app/auth";
import { AppShell } from "@/app/layouts/AppShell";
import { LoginPage, NoAccessScreen } from "@/app/pages/LoginPage";
import NotFoundPage, { RouteErrorPage } from "@/app/pages/NotFoundPage";
import { LoadingPage } from "@/components/common/page";

const DashboardPage = lazy(() => import("@/pages/dashboard/DashboardPage"));
const FeaturesPage = lazy(() => import("@/pages/features/FeaturesPage"));
const FeatureLayout = lazy(() => import("@/pages/features/FeatureLayout"));
const FeatureContentPage = lazy(() => import("@/pages/features/FeatureContentPage"));
const ContentEditorPage = lazy(() => import("@/pages/features/ContentEditorPage"));
const SubmissionsPage = lazy(() => import("@/pages/submissions/SubmissionsPage"));
const MediaPage = lazy(() => import("@/pages/media/MediaPage"));
const AccessLayout = lazy(() => import("@/pages/access/AccessLayout"));
const TokensPage = lazy(() => import("@/pages/access/TokensPage"));
const UsersPage = lazy(() => import("@/pages/access/UsersPage"));
const RolesPage = lazy(() => import("@/pages/access/RolesPage"));
const SettingsPage = lazy(() => import("@/pages/settings/SettingsPage"));
const SiteBuilderPage = lazy(() => import("@/pages/site/SiteBuilderPage"));
const EnvironmentSettingsPage = lazy(() => import("@/pages/settings/EnvironmentSettingsPage"));

function RequireAuth() {
  const auth = useAuth();
  const location = useLocation();

  if (auth.status === "checking") return <LoadingPage label="Проверяем сессию…" />;
  if (auth.status === "unavailable") {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-background p-4">
        <div className="w-full max-w-md rounded-lg border border-border bg-card p-6">
          <h1 className="text-lg font-semibold">API временно недоступен</h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            {auth.error ?? "Не удалось проверить сессию."}
          </p>
          <div className="mt-4 flex gap-2">
            <button
              type="button"
              className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground"
              onClick={() => void auth.retry()}
            >
              Повторить
            </button>
            <button
              type="button"
              className="rounded-md border border-border px-3 py-2 text-sm"
              onClick={() => auth.logout()}
            >
              Выйти
            </button>
          </div>
        </div>
      </div>
    );
  }
  if (auth.status === "anonymous") {
    return (
      <Navigate
        to="/login"
        replace
        state={{ from: `${location.pathname}${location.search}` }}
      />
    );
  }
  if (!auth.principal) return <LoadingPage />;
  return <Outlet />;
}

function RequireStaff({ children }: { children?: React.ReactNode }) {
  const auth = useAuth();
  if (!auth.principal) return <LoadingPage />;
  if (auth.principal.role === "user") return <NoAccessScreen />;
  // Rendering children instead of an Outlet keeps the parent route's outlet
  // context intact (useOutletContext inside children must still see it).
  if (children) return <>{children}</>;
  return <Outlet />;
}

export const router = createBrowserRouter(
  [
    {
      path: "/login",
      element: <LoginPage />,
      errorElement: <RouteErrorPage />,
    },
    {
      path: "/",
      element: <RequireAuth />,
      errorElement: <RouteErrorPage />,
      children: [
        {
          element: <AppShell />,
          children: [
            { index: true, element: <DashboardPage /> },
            { path: "features", element: <FeaturesPage /> },
            { path: "features/new", element: <Navigate to="/site" replace /> },
            {
              path: "features/:featureId",
              element: <FeatureLayout />,
              children: [
                { index: true, element: <Navigate to="content" replace /> },
                { path: "schema", element: <Navigate to="../content" replace /> },
                { path: "settings", element: <Navigate to="../content" replace /> },
                { path: "content", element: <FeatureContentPage /> },
                { path: "content/new", element: <ContentEditorPage /> },
                { path: "content/:entryId", element: <ContentEditorPage /> },
              ],
            },
            { path: "submissions", element: <SubmissionsPage /> },
            { path: "submissions/:submissionId", element: <SubmissionsPage /> },
            { path: "media", element: <MediaPage /> },
            {
              element: <RequireStaff />,
              children: [{ path: "site", element: <SiteBuilderPage /> }],
            },
            {
              element: <RequireStaff />,
              children: [{
              path: "access",
              element: <AccessLayout />,
              children: [
                { index: true, element: <Navigate to="tokens" replace /> },
                { path: "tokens", element: <TokensPage /> },
                { path: "users", element: <UsersPage /> },
                { path: "roles", element: <RolesPage /> },
              ],
            }],
            },
            {
              element: <RequireStaff />,
              children: [{ path: "settings", element: <SettingsPage /> }],
            },
            {
              element: <RequireStaff />,
              children: [{ path: "settings/environments/:envKey", element: <EnvironmentSettingsPage /> }],
            },
            { path: "*", element: <NotFoundPage /> },
          ],
        },
      ],
    },
  ],
  { basename: "/admin" },
);
