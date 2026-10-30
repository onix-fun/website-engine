import {
  Suspense,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
} from "react-router-dom";
import {
  ChevronRight,
  Home,
  Image,
  Inbox,
  KeyRound,
  Layers,
  LayoutDashboard,
  LogOut,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  Settings,
  Settings2,
  Blocks,
  FileText,
  GitBranch,
  History,
  ShieldCheck,
  UserRound,
} from "lucide-react";
import { useAuth } from "@/app/auth";
import { ThemeToggle } from "@/components/common/theme-toggle";
import { RoleBadge } from "@/components/common/status-badges";
import { LoadingPage } from "@/components/common/page";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { roleLabels } from "@/lib/format";

const SIDEBAR_KEY = "we_admin_sidebar_collapsed";

interface NavItem {
  to: string;
  label: string;
  icon: typeof Home;
  end?: boolean;
  match?: (pathname: string) => boolean;
}

const homeNav: NavItem[] = [
  { to: "/", label: "Главная", icon: LayoutDashboard, end: true },
];

const siteSettingsNav: NavItem[] = [
  { to: "/site/router", label: "Роутер", icon: GitBranch, match: (p) => p === "/site/router" },
  { to: "/site/features", label: "Реестр фичей", icon: Blocks, match: (p) => p === "/site/features" },
  { to: "/site/pages", label: "Редактор страниц", icon: FileText, match: (p) => p === "/site/pages" },
  { to: "/site/snapshots", label: "Снапшоты", icon: History, match: (p) => p === "/site/snapshots" },
];

const administrationNav: NavItem[] = [
  { to: "/access/tokens", label: "Доступы", icon: ShieldCheck, match: (p) => p.startsWith("/access") },
  { to: "/settings", label: "Настройки", icon: Settings, match: (p) => p.startsWith("/settings") },
];

const contentNav: NavItem[] = [
  { to: "/content", label: "Контент", icon: Layers, match: (p) => p.startsWith("/content") || /^\/features\/[^/]+/.test(p) },
  { to: "/site-metadata", label: "Основные настройки сайта", icon: Settings2, match: (p) => p.startsWith("/site-metadata") },
  { to: "/media", label: "Файлы", icon: Image, match: (p) => p.startsWith("/media") },
  { to: "/submissions", label: "Заявки", icon: Inbox, match: (p) => p.startsWith("/submissions") },
];

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link
      to="/"
      className="flex min-w-0 items-center gap-2.5 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      aria-label="Website Engine — на главную"
    >
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">
        W
      </span>
      {!compact && (
        <span className="min-w-0 leading-tight">
          <span className="block truncate text-sm font-semibold">Website Engine</span>
          <span className="block truncate text-2xs text-muted-foreground">
            Управление контентом
          </span>
        </span>
      )}
    </Link>
  );
}

function UserMenu({
  align = "end",
  children,
}: {
  align?: "start" | "end";
  children: ReactNode;
}) {
  const { principal, logout } = useAuth();
  if (!principal) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="w-60">
        <DropdownMenuLabel className="font-normal">
          <p className="truncate text-sm font-medium text-foreground">{principal.name}</p>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">Текущий токен</p>
        </DropdownMenuLabel>
        <div className="px-2 pb-2">
          <RoleBadge role={principal.role} />
        </div>
        <DropdownMenuSeparator />
        {(principal.role === "admin" || principal.role === "moderator") && (
          <DropdownMenuItem asChild>
            <Link to="/access/tokens">
              <KeyRound className="h-4 w-4" />
              Токены доступа
            </Link>
          </DropdownMenuItem>
        )}
        {(principal.role === "admin" || principal.role === "moderator") && (
          <DropdownMenuItem asChild>
            <Link to="/settings">
              <Settings className="h-4 w-4" />
              Настройки
            </Link>
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="text-destructive focus:text-destructive"
          onSelect={() => logout()}
        >
          <LogOut className="h-4 w-4" />
          Выйти
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NavList({
  collapsed,
  onNavigate,
  items,
}: {
  collapsed: boolean;
  onNavigate?: () => void;
  items: NavItem[];
}) {
  const location = useLocation();
  return (
    <ul className="space-y-1">
      {items.map((item) => {
        const Icon = item.icon;
        const active = item.match
          ? item.match(location.pathname)
          : item.end
            ? location.pathname === item.to
            : location.pathname.startsWith(item.to);
        const link = (
          <NavLink
            to={item.to}
            end={item.end}
            onClick={onNavigate}
            data-active={active}
            className={cn(
              "sidebar-link",
              collapsed && "justify-center px-0",
            )}
            aria-current={active ? "page" : undefined}
          >
            <Icon className="h-4 w-4 shrink-0" aria-hidden="true" />
            {!collapsed && <span className="truncate">{item.label}</span>}
          </NavLink>
        );

        return (
          <li key={item.to}>
            {collapsed ? (
              <Tooltip>
                <TooltipTrigger asChild>{link}</TooltipTrigger>
                <TooltipContent side="right">{item.label}</TooltipContent>
              </Tooltip>
            ) : (
              link
            )}
          </li>
        );
      })}
    </ul>
  );
}

function SidebarContent({
  collapsed,
  showCollapse,
  onToggleCollapse,
  onNavigate,
}: {
  collapsed: boolean;
  showCollapse: boolean;
  onToggleCollapse?: () => void;
  onNavigate?: () => void;
}) {
  const { principal } = useAuth();
  const siteManager = principal?.role === "admin" || principal?.role === "moderator";
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        className={cn(
          "flex h-14 shrink-0 items-center border-b border-sidebar-border px-3",
          collapsed ? "justify-center" : "justify-between",
        )}
      >
        <Brand compact={collapsed} />
        {showCollapse && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={onToggleCollapse}
                className="text-muted-foreground"
                aria-label={collapsed ? "Развернуть меню" : "Свернуть меню"}
              >
                {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">
              {collapsed ? "Развернуть меню" : "Свернуть меню"}
            </TooltipContent>
          </Tooltip>
        )}
      </div>

      <nav
        className={cn("flex-1 overflow-y-auto py-3 thin-scrollbar", collapsed ? "px-2" : "px-3")}
        aria-label="Основная навигация"
      >
        <NavList collapsed={collapsed} items={homeNav} onNavigate={onNavigate} />
        {siteManager && (
          <>
            <div className="my-4 border-t border-sidebar-border" />
            {!collapsed && <p className="mb-2 px-2 text-2xs font-medium uppercase tracking-wider text-sidebar-muted">Настройки сайта</p>}
            <NavList collapsed={collapsed} items={siteSettingsNav} onNavigate={onNavigate} />
            <div className="my-4 border-t border-sidebar-border" />
            {!collapsed && <p className="mb-2 px-2 text-2xs font-medium uppercase tracking-wider text-sidebar-muted">Настройки и доступы</p>}
            <NavList collapsed={collapsed} items={administrationNav} onNavigate={onNavigate} />
          </>
        )}
        <div className="my-4 border-t border-sidebar-border" />
        {!collapsed && <p className="mb-2 px-2 text-2xs font-medium uppercase tracking-wider text-sidebar-muted">Контент</p>}
        <NavList collapsed={collapsed} items={contentNav} onNavigate={onNavigate} />
      </nav>

      <div className={cn("shrink-0 border-t border-sidebar-border p-3", collapsed && "px-2")}>
        {principal && (
          <div
            className={cn(
              "flex items-center gap-2.5",
              collapsed && "justify-center",
            )}
          >
            <UserMenu>
              <button
                type="button"
                className="flex min-w-0 flex-1 items-center gap-2.5 rounded-md p-1.5 text-left transition-colors hover:bg-sidebar-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label="Меню пользователя"
              >
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-sidebar-border bg-sidebar-accent text-xs font-semibold uppercase">
                  {Array.from(principal.name)[0] ?? <UserRound />}
                </span>
                {!collapsed && (
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs font-medium text-sidebar-foreground">
                      {principal.name}
                    </span>
                    <span className="block truncate text-2xs text-sidebar-muted">
                      {roleLabels[principal.role]}
                    </span>
                  </span>
                )}
              </button>
            </UserMenu>
          </div>
        )}
        {!collapsed && (
          <p className="mt-2 px-1.5 text-2xs text-sidebar-muted">
            Веб-CMS · администрирование
          </p>
        )}
      </div>
    </div>
  );
}

const segmentLabels: Record<string, string> = {
  features: "Разделы",
  new: "Создание",
  schema: "Схема",
  content: "Контент",
  settings: "Настройки",
  submissions: "Заявки",
  media: "Медиа",
  access: "Доступы",
  tokens: "Токены",
  users: "Пользователи",
  roles: "Роли",
};

function Breadcrumbs() {
  const location = useLocation();
  const segments = location.pathname.split("/").filter(Boolean);
  const crumbs: Array<{ label: string; to?: string }> = [
    { label: "Главная", to: "/" },
  ];
  let accumulated = "";
  segments.forEach((segment, index) => {
    accumulated += `/${segment}`;
    const isLast = index === segments.length - 1;
    const parentSegment = segments[index - 1];
    const label =
      segmentLabels[segment] ??
      (parentSegment === "features"
        ? "Раздел"
        : parentSegment === "submissions"
          ? "Заявка"
          : parentSegment === "content"
            ? "Запись"
            : segment.length > 18
              ? "Объект"
              : segment);
    crumbs.push({ label, to: isLast ? undefined : accumulated });
  });

  return (
    <nav aria-label="Хлебные крошки" className="min-w-0">
      <ol className="flex min-w-0 items-center gap-1.5 text-xs">
        {crumbs.map((crumb, index) => {
          const last = index === crumbs.length - 1;
          return (
            <li key={`${crumb.label}-${index}`} className="flex min-w-0 items-center gap-1.5">
              {index > 0 && (
                <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground/60" aria-hidden="true" />
              )}
              {index === 0 ? (
                <Link
                  to={crumb.to ?? "/"}
                  className="flex shrink-0 items-center text-muted-foreground transition-colors hover:text-foreground"
                  aria-label="Главная"
                >
                  <Home className="h-3.5 w-3.5" />
                </Link>
              ) : crumb.to ? (
                <Link
                  to={crumb.to}
                  className="truncate text-muted-foreground transition-colors hover:text-foreground"
                >
                  {crumb.label}
                </Link>
              ) : (
                <span
                  className={cn(
                    "truncate",
                    last ? "font-medium text-foreground" : "text-muted-foreground",
                  )}
                  aria-current={last ? "page" : undefined}
                >
                  {crumb.label}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

export function AppShell() {
  const { principal } = useAuth();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem(SIDEBAR_KEY) === "true";
    } catch {
      return false;
    }
  });
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

  useEffect(() => {
    try {
      window.localStorage.setItem(SIDEBAR_KEY, String(collapsed));
    } catch {
      // ignore persistence failures
    }
  }, [collapsed]);

  return (
    <div className="flex min-h-dvh bg-background">
      <a
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground focus:not-sr-only focus:fixed focus:left-3 focus:top-3"
      >
        Перейти к содержимому
      </a>

      <aside
        className={cn(
          "sticky top-0 hidden h-dvh shrink-0 border-r border-sidebar-border bg-sidebar text-sidebar-foreground lg:flex lg:flex-col",
          collapsed ? "w-[var(--sidebar-collapsed)]" : "w-[var(--sidebar-width)]",
        )}
      >
        <SidebarContent
          collapsed={collapsed}
          showCollapse
          onToggleCollapse={() => setCollapsed((value) => !value)}
        />
      </aside>

      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent
          side="left"
          hideClose
          className="w-[85%] max-w-[19rem] border-sidebar-border bg-sidebar p-0 text-sidebar-foreground"
        >
          <SheetHeader className="sr-only">
            <SheetTitle>Меню</SheetTitle>
          </SheetHeader>
          <SidebarContent
            collapsed={false}
            showCollapse={false}
            onNavigate={() => setMobileOpen(false)}
          />
        </SheetContent>
      </Sheet>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-[var(--header-height)] shrink-0 items-center justify-between gap-3 border-b border-border bg-background px-3 sm:px-5">
          <div className="flex min-w-0 items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden"
              onClick={() => setMobileOpen(true)}
              aria-label="Открыть меню"
            >
              <Menu />
            </Button>
            <Breadcrumbs />
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <ThemeToggle compact />
            {principal && (
              <div className="hidden items-center gap-1.5 sm:flex">
                <Badge variant="muted" className="max-w-48 truncate">
                  {principal.name}
                </Badge>
              </div>
            )}
            <UserMenu>
              <Button
                variant="ghost"
                size="icon"
                aria-label="Меню пользователя"
                className="rounded-full"
              >
                <span className="flex h-7 w-7 items-center justify-center rounded-full border border-border bg-muted text-xs font-semibold uppercase">
                  {principal ? Array.from(principal.name)[0] : <UserRound className="h-3.5 w-3.5" />}
                </span>
              </Button>
            </UserMenu>
          </div>
        </header>

        <main
          id="main-content"
          tabIndex={-1}
          className="min-w-0 flex-1 focus-visible:outline-none"
        >
          <Suspense fallback={<LoadingPage />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  );
}
