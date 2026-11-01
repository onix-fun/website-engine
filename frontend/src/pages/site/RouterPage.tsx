import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ButtonHTMLAttributes, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type WheelEvent as ReactWheelEvent } from "react";
import { AlertTriangle, ArrowDown, ArrowUp, Check, FilePlus2, GitBranch, Home, Link2, Maximize2, Plus, Save, Trash2, ZoomIn, ZoomOut } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ContextHelp } from "@/components/common/page";
import type { RouteTreeNode, SitePage } from "@/types";
import { useSiteWorkspace } from "./SiteWorkspace";
import PageEditorPage from "./PageEditorPage";
import { addRouteNode, createPageFromRoute, describeRoutes, moveRoutePriority, removeRouteBranch, reparentRoute, updateRouteNode } from "./route-tree";

const NODE_WIDTH = 286;
const NODE_HEIGHT = 106;
const COLUMN_GAP = 370;
const ROW_GAP = 158;

type RoutePosition = { node: RouteTreeNode; path: string; captures: string[]; x: number; y: number; width: number; height: number; parentId?: string };
type OrphanPosition = { page: SitePage; x: number; y: number; width: number; height: number };
type RouteDialogState = { mode: "root" | "child" | "edit"; parentId?: string; routeId?: string; existingPageId?: string; chooseExisting?: boolean };
type ContextState = { x: number; y: number; routeId?: string; pageId?: string };
type PanState = { pointerId: number; clientX: number; clientY: number; originX: number; originY: number };
type ConnectionState = { pointerId: number; sourceId: string; clientX: number; clientY: number };

function MenuAction({ className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button role="menuitem" type="button" className={`flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm outline-none hover:bg-accent focus:bg-accent disabled:pointer-events-none disabled:opacity-40 ${className}`} {...props} />;
}

function collectPositions(tree: RouteTreeNode): RoutePosition[] {
  const positions: RoutePosition[] = [];
  const descriptions = new Map(describeRoutes(tree).map((route) => [route.id, route]));
  let row = 0;
  const visit = (node: RouteTreeNode, depth: number, parentId: string | undefined): number => {
    const description = descriptions.get(node.id);
    const children = node.children ?? [];
    const childRows = children.map((child) => visit(child, depth + 1, node.id));
    const nodeRow = childRows.length ? childRows.reduce((sum, value) => sum + value, 0) / childRows.length : row++;
    positions.push({
      node,
      path: description?.path ?? "/",
      captures: description?.captures ?? [],
      x: 40 + depth * COLUMN_GAP,
      y: 48 + nodeRow * ROW_GAP,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      parentId,
    });
    return nodeRow;
  };
  const roots = tree.type === "site" ? tree.children ?? [] : [tree];
  roots.forEach((root) => visit(root, 0, undefined));
  return positions;
}

function currentNode(tree: RouteTreeNode, id?: string): RouteTreeNode | undefined {
  if (!id) return undefined;
  return describeRoutes(tree).find((route) => route.id === id)?.node;
}

function pageName(pages: SitePage[], id?: string) {
  return pages.find((page) => page.id === id)?.name;
}

function displayNodeType(node: RouteTreeNode) {
  switch (node.type) {
    case "path": return "Корневой адрес";
    case "index": return "Главная";
    case "static": return "Статический сегмент";
    case "param": return "Параметр пути";
    case "regex": return "Regex-сегмент";
    case "catchAll": return "Catch-all";
    case "site": return "Сайт";
  }
}

function readRouteSegment(node: RouteTreeNode): string {
  if (node.type === "path") return node.path ?? "/";
  if (node.type === "index") return "/";
  if (node.type === "static") return node.value ?? "";
  if (node.type === "param") return `:${node.name ?? "slug"}`;
  if (node.type === "regex") return `~${node.pattern ?? ""}`;
  if (node.type === "catchAll") return `*${node.name ?? "path"}`;
  return "";
}

function readRootPath(node: RouteTreeNode): string {
  if (node.type === "path") return node.path ?? "/";
  if (node.type === "index") return "/";
  if (node.type === "static") return `/${node.value ?? ""}`;
  if (node.type === "param") return `/:${node.name ?? "slug"}`;
  if (node.type === "regex") return `/~${node.pattern ?? ""}`;
  if (node.type === "catchAll") return `/*${node.name ?? "path"}`;
  return "/";
}

function parseAbsolutePath(value: string): { path?: string; error?: string } {
  const path = value.trim();
  if (path === "/") return { path };
  if (!path.startsWith("/") || path.endsWith("/") || path.length > 500 || path.includes("#")) {
    return { error: "Введите абсолютный путь без завершающего слеша, query и fragment." };
  }
  const parts = path.slice(1).split("/");
  if (parts.some((part) => !part)) return { error: "Путь не может содержать пустые сегменты." };
  for (const [index, part] of parts.entries()) {
    if (part.includes("?") && !part.startsWith("~")) return { error: "Символ ? допустим только внутри regex-сегмента." };
    if ((part === "." || part === "..") && !part.startsWith("~")) return { error: "Сегменты . и .. нельзя использовать в маршруте." };
    if (part.startsWith(":")) {
      if (!/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(part.slice(1))) return { error: `Некорректный параметр «${part}».` };
    } else if (part.startsWith("*")) {
      if (index !== parts.length - 1 || !/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(part.slice(1))) return { error: "Catch-all должен быть последним сегментом и иметь имя: *path." };
    } else if (part.startsWith("~")) {
      if (!part.slice(1)) return { error: "Regex-сегмент не может быть пустым." };
    } else if (part.includes("/")) return { error: "Статический сегмент не может содержать /." };
  }
  return { path };
}

function childNodeFromSegment(input: string, id: string, pageId?: string, children: RouteTreeNode[] = []): { node?: RouteTreeNode; error?: string } {
  const segment = input.trim();
  if (!segment || segment.includes("/") || segment.includes("#") || (segment.includes("?") && !segment.startsWith("~"))) return { error: "Введите ровно один URL-сегмент." };
  if (segment === "." || segment === "..") return { error: "Сегменты . и .. нельзя использовать в маршруте." };
  const common = { id, ...(pageId ? { pageId } : {}), children };
  if (segment.startsWith(":")) {
    const name = segment.slice(1);
    return /^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(name) ? { node: { ...common, type: "param", name } } : { error: "Имя параметра должно начинаться с буквы и содержать только буквы, цифры и _." };
  }
  if (segment.startsWith("*")) {
    const name = segment.slice(1);
    if (!/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(name)) return { error: "Catch-all должен иметь имя, например *path." };
    if (children.length) return { error: "Catch-all не может содержать дочерние маршруты." };
    return { node: { ...common, type: "catchAll", name } };
  }
  if (segment.startsWith("~")) return segment.length > 1 ? { node: { ...common, type: "regex", pattern: segment.slice(1) } } : { error: "Regex-сегмент не может быть пустым." };
  return { node: { ...common, type: "static", value: segment } };
}

function suggestedName(path: string) {
  if (path === "/") return "Главная";
  const part = path.split("/").filter(Boolean).at(-1) ?? "Новая страница";
  const value = part.replace(/^[:*~]/, "").replace(/\(\?<[^>]+>/g, "").replace(/[()?+*]/g, "").replaceAll("[", "").replaceAll("]", "");
  return value ? value.charAt(0).toLocaleUpperCase("ru-RU") + value.slice(1) : "Новая страница";
}

function comparableRootPath(path: string) {
  if (path === "/") return "/";
  return path.split("/").map((segment) => {
    if (segment.startsWith(":")) return `{${segment.slice(1)}}`;
    if (segment.startsWith("*")) return `{${segment.slice(1)}}`;
    if (segment.startsWith("~")) return `{${Array.from(segment.matchAll(/\(\?<([A-Za-z][A-Za-z0-9_]*)>/g), (match) => match[1])[0] ?? "regex"}}`;
    return segment;
  }).join("/");
}

function defaultRootPath(tree: RouteTreeNode) {
  const existing = new Set(describeRoutes(tree).map((route) => route.path));
  let suffix = 1;
  let candidate = "/new-page";
  while (existing.has(candidate)) candidate = `/new-page-${++suffix}`;
  return candidate;
}

function RouteDialog({
  state,
  pages,
  tree,
  onClose,
  onApply,
}: {
  state: RouteDialogState | null;
  pages: SitePage[];
  tree: RouteTreeNode;
  onClose: () => void;
  onApply: (route: RouteTreeNode, parentId?: string | null, page?: SitePage) => void;
}) {
  const node = state?.mode === "edit" ? currentNode(tree, state.routeId) : undefined;
  const descriptor = state?.mode === "edit" ? describeRoutes(tree).find((route) => route.id === state.routeId) : undefined;
  const isRoot = state?.mode === "root" || (state?.mode === "edit" && descriptor?.parentId === undefined);
  const [value, setValue] = useState("");
  const [pageTarget, setPageTarget] = useState("__new");
  const [name, setName] = useState("");
  const [kind, setKind] = useState<SitePage["kind"]>("page");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!state) return;
    const rootPath = defaultRootPath(tree);
    setValue(state.mode === "edit" && node ? (isRoot ? readRootPath(node) : readRouteSegment(node)) : state.mode === "root" ? rootPath : "new");
    setPageTarget(state.mode === "edit" && node?.pageId ? node.pageId : state.existingPageId ?? (state.chooseExisting ? "__select" : "__new"));
    setName(state.mode === "edit" && node?.pageId ? pageName(pages, node.pageId) ?? "Новая страница" : suggestedName(state.mode === "root" ? rootPath : "new"));
    setKind("page");
    setError("");
  }, [state, node, descriptor?.path, isRoot, pages, tree]);

  const submit = () => {
    if (!state) return;
    const id = node?.id ?? `route-${crypto.randomUUID().slice(0, 8)}`;
    const selectedPage = pageTarget !== "__new" && pageTarget !== "__none" ? pages.find((page) => page.id === pageTarget) : undefined;
    const page = pageTarget === "__new" ? { id: `page-${crypto.randomUUID().slice(0, 8)}`, name: name.trim() || suggestedName(value), kind, placements: [] } satisfies SitePage : undefined;
    const pageId = selectedPage?.id ?? page?.id ?? (pageTarget === "__none" ? undefined : node?.pageId);
    let nextNode: RouteTreeNode | undefined;
    let validationError: string | undefined;

    if (isRoot) {
      const parsed = parseAbsolutePath(value);
      validationError = parsed.error;
      if (parsed.path) nextNode = { id, type: "path", path: parsed.path, ...(pageId ? { pageId } : {}), children: node?.children ?? [] };
    } else {
      const parent = state.parentId ? currentNode(tree, state.parentId) : undefined;
      if (parent?.type === "catchAll" || (parent?.type === "path" && parent.path?.split("/").filter(Boolean).at(-1)?.startsWith("*"))) {
        setError("Catch-all маршрут не может содержать дочерние узлы.");
        return;
      }
      const parsed = childNodeFromSegment(value, id, pageId, node?.children ?? []);
      nextNode = parsed.node;
      validationError = parsed.error;
    }
    if (!validationError && !nextNode) validationError = "Не удалось собрать маршрут.";
    if (!validationError && pageTarget === "__none" && !(node?.children?.length)) validationError = "Листовой маршрут должен быть связан со страницей.";
    if (validationError) { setError(validationError); return; }
    if (!nextNode) { setError("Не удалось собрать маршрут."); return; }

    if (state.mode !== "edit" && !pageId) { setError("Выберите существующую страницу или создайте новую."); return; }
    if (state.mode === "edit" && pageTarget === "__new" && !node?.pageId && !page) { setError("Укажите название новой страницы."); return; }
    const current = describeRoutes(tree).filter((route) => route.id !== node?.id);
    const candidate = nextNode;
    if (isRoot && candidate.type === "path") {
      const equivalent = comparableRootPath(candidate.path ?? "/");
      if (current.some((route) => route.path === equivalent)) { setError("Такой URL уже настроен. Измените путь или привяжите нужную страницу к существующему узлу."); return; }
    }
    const parentId = state.mode === "child" ? state.parentId ?? null : undefined;
    if (state.mode === "edit") onApply(nextNode, undefined, page);
    else onApply(nextNode, parentId ?? null, page);
  };

  return <Dialog open={Boolean(state)} onOpenChange={(open) => { if (!open) onClose(); }}>
    <DialogContent>
      <DialogHeader><DialogTitle>{state?.mode === "edit" ? "Настроить маршрут" : state?.mode === "child" ? "Добавить дочерний маршрут" : "Добавить самостоятельный маршрут"}</DialogTitle><DialogDescription>{isRoot ? "Корень задаёт полный URL. Дочерние узлы продолжат его следующими сегментами." : "Укажите один сегмент, который добавится к пути родителя."}</DialogDescription></DialogHeader>
      <div className="space-y-4">
        <div className="space-y-1.5"><Label htmlFor="route-value">{isRoot ? "Полный URL" : "Сегмент"}</Label><Input id="route-value" autoFocus value={value} onChange={(event) => { setValue(event.currentTarget.value); if (pageTarget === "__new") setName(suggestedName(event.currentTarget.value)); setError(""); }} placeholder={isRoot ? "/about" : "products, :slug, ~(?<id>[0-9]+), *path"} /><p className="text-xs leading-5 text-muted-foreground">Статический текст · :параметр · ~regex-сегмент · *catch-all. Catch-all может быть только последним.</p></div>
        <div className="space-y-1.5"><Label htmlFor="route-page">Страница</Label><select id="route-page" className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={pageTarget} onChange={(event) => { setPageTarget(event.currentTarget.value); setError(""); }}><option value="__new">Создать страницу</option>{pageTarget === "__select" && <option value="__select" disabled>Выберите страницу</option>}{state?.mode === "edit" && Boolean(node?.children?.length) && <option value="__none">Оставить узел без страницы</option>}{pages.map((page) => <option key={page.id} value={page.id}>{page.name}</option>)}</select></div>
        {pageTarget === "__new" && <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]"><div className="space-y-1.5"><Label htmlFor="route-page-name">Название страницы</Label><Input id="route-page-name" value={name} onChange={(event) => setName(event.currentTarget.value)} /></div><label className="space-y-1.5 text-sm"><span className="font-medium">Тип страницы</span><select className="h-9 w-full rounded-md border border-input bg-background px-3" value={kind} onChange={(event) => setKind(event.currentTarget.value as SitePage["kind"])}><option value="page">Обычная страница</option><option value="collection-list">Список коллекции</option><option value="detail">Карточка записи</option></select></label></div>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter><Button type="button" variant="outline" onClick={onClose}>Отмена</Button><Button type="button" onClick={submit}>{state?.mode === "edit" ? "Сохранить маршрут" : "Создать"}</Button></DialogFooter>
    </DialogContent>
  </Dialog>;
}

export default function RouterPage() {
  const { config, update, dirty, saving, conflict, revision, save, reload } = useSiteWorkspace();
  const [editingPageId, setEditingPageId] = useState<string | null>(null);
  const [selectedRouteId, setSelectedRouteId] = useState<string | null>(null);
  const [dialog, setDialog] = useState<RouteDialogState | null>(null);
  const [menu, setMenu] = useState<ContextState | null>(null);
  const [scale, setScale] = useState(1);
  const [pan, setPan] = useState({ x: 32, y: 40 });
  const [panGesture, setPanGesture] = useState<PanState | null>(null);
  const [connection, setConnection] = useState<ConnectionState | null>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  const hasFit = useRef(false);
  const root = config.routeTree;
  const positions = useMemo(() => collectPositions(root), [root]);
  const routeIds = useMemo(() => new Set(positions.map((position) => position.node.pageId).filter((id): id is string => Boolean(id))), [positions]);
  const orphanPages = useMemo(() => config.pages.filter((page) => !routeIds.has(page.id)), [config.pages, routeIds]);
  const maxRouteY = positions.reduce((max, position) => Math.max(max, position.y + position.height), 120);
  const orphanPositions: OrphanPosition[] = orphanPages.map((page, index) => ({ page, x: 40 + index * (NODE_WIDTH + 28), y: maxRouteY + 92, width: NODE_WIDTH, height: NODE_HEIGHT }));
  const canvasWidth = Math.max(760, ...positions.map((position) => position.x + position.width + 120), ...orphanPositions.map((position) => position.x + position.width + 80));
  const canvasHeight = Math.max(540, maxRouteY + (orphanPages.length ? 260 : 120));
  const positionById = useMemo(() => new Map(positions.map((position) => [position.node.id, position])), [positions]);
  const descriptors = useMemo(() => describeRoutes(root), [root]);
  const selectedNode = currentNode(root, menu?.routeId);

  const fitCanvas = useCallback(() => {
    const viewport = canvasRef.current;
    if (!viewport) return;
    const bounds = viewport.getBoundingClientRect();
    const availableWidth = Math.max(200, bounds.width - 100);
    const availableHeight = Math.max(200, bounds.height - 150);
    const nextScale = Math.max(0.22, Math.min(1.1, availableWidth / canvasWidth, availableHeight / canvasHeight));
    setScale(nextScale);
    setPan({
      x: Math.max(24, (bounds.width - canvasWidth * nextScale) / 2),
      y: Math.max(86, 90 + (availableHeight - canvasHeight * nextScale) / 2),
    });
  }, [canvasWidth, canvasHeight]);

  useLayoutEffect(() => {
    if (!hasFit.current && positions.length) {
      fitCanvas();
      hasFit.current = true;
    }
  }, [fitCanvas, positions.length]);

  useEffect(() => {
    if (!menu) return;
    const close = (event: Event) => {
      if (event.type === "keydown" && (event as KeyboardEvent).key === "Escape") setMenu(null);
      if (event.type === "pointerdown" && !(event.target as HTMLElement).closest("[data-route-context-menu]")) setMenu(null);
    };
    document.addEventListener("pointerdown", close);
    document.addEventListener("keydown", close);
    return () => { document.removeEventListener("pointerdown", close); document.removeEventListener("keydown", close); };
  }, [menu]);

  const applyTree = (tree: RouteTreeNode) => update((current) => ({ ...current, routeTree: tree, formatVersion: 3 }));
  const makeNewRoute = (node: RouteTreeNode, parentId: string | null, page?: SitePage) => {
    if (page) update((current) => ({ ...current, pages: [...current.pages, page], routeTree: addRouteNode(current.routeTree, parentId, node), formatVersion: 3 }));
    else applyTree(addRouteNode(root, parentId, node));
    setSelectedRouteId(node.id);
    if (page) setEditingPageId(page.id);
    setDialog(null);
  };

  const applyRouteEdit = (node: RouteTreeNode, _parentId: null, page?: SitePage) => {
    update((current) => ({
      ...current,
      pages: page ? [...current.pages, page] : current.pages,
      routeTree: updateRouteNode(current.routeTree, node.id, () => node),
      formatVersion: 3,
    }));
    setDialog(null);
    setMenu(null);
    if (page) setEditingPageId(page.id);
  };

  const openPageForRoute = (node: RouteTreeNode) => {
    let pageId = node.pageId;
    if (!pageId) {
      const descriptor = descriptors.find((route) => route.id === node.id);
      const page = createPageFromRoute(node, descriptor?.path ?? "/");
      pageId = page.id;
      update((current) => ({
        ...current,
        pages: [...current.pages, page],
        routeTree: updateRouteNode(current.routeTree, node.id, (currentNode) => ({ ...currentNode, pageId: page.id })),
        formatVersion: 3,
      }));
    }
    setSelectedRouteId(node.id);
    setMenu(null);
    setEditingPageId(pageId);
  };

  const contextMenu = (event: ReactMouseEvent, routeId?: string, pageId?: string) => {
    event.preventDefault();
    event.stopPropagation();
    const bounds = canvasRef.current?.getBoundingClientRect();
    if (!bounds) return;
    setMenu({ x: Math.max(8, Math.min(event.clientX - bounds.left, bounds.width - 250)), y: Math.max(8, Math.min(event.clientY - bounds.top, bounds.height - 250)), routeId, pageId });
  };

  const openChildDialog = (parentId: string) => { setDialog({ mode: "child", parentId }); setMenu(null); };
  const openRootDialog = (existingPageId?: string, chooseExisting = false) => { setDialog({ mode: "root", existingPageId, chooseExisting }); setMenu(null); };
  const editRouteDialog = (node: RouteTreeNode) => { setDialog({ mode: "edit", routeId: node.id }); setMenu(null); };

  const deleteBranch = (node: RouteTreeNode) => {
    const descendants = positions.filter((position) => position.node.id === node.id || isDescendant(root, node.id, position.node.id)).length;
    const removedIds = new Set(positions.filter((position) => position.node.id === node.id || isDescendant(root, node.id, position.node.id)).map((position) => position.node.id));
    const remainsRouted = positions.some((position) => !removedIds.has(position.node.id) && Boolean(position.node.pageId));
    if (!remainsRouted) { toast.error("Нельзя удалить последний маршрут сайта."); return; }
    if (!window.confirm(descendants > 1 ? `Удалить маршрут «${descriptors.find((route) => route.id === node.id)?.path}» и ${descendants - 1} дочерних маршрутов? Страницы останутся в черновиках без маршрута.` : `Удалить маршрут «${descriptors.find((route) => route.id === node.id)?.path}»? Страница останется в черновиках без маршрута.`)) return;
    applyTree(removeRouteBranch(root, node.id));
    setSelectedRouteId(null);
    setMenu(null);
  };

  const changeParent = (sourceId: string, targetId: string | null) => {
    const result = reparentRoute(root, sourceId, targetId);
    if (!result.tree) { toast.error(result.error ?? "Не удалось соединить маршруты"); return; }
    applyTree(result.tree);
    setMenu(null);
  };

  const openPageForOrphan = (pageId: string) => { setEditingPageId(pageId); setMenu(null); };

  const onCanvasPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    if ((event.target as HTMLElement).closest("button, [data-route-id], input, [role=menu]")) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    setPanGesture({ pointerId: event.pointerId, clientX: event.clientX, clientY: event.clientY, originX: pan.x, originY: pan.y });
    setMenu(null);
  };
  const onCanvasPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (panGesture?.pointerId === event.pointerId) setPan({ x: panGesture.originX + event.clientX - panGesture.clientX, y: panGesture.originY + event.clientY - panGesture.clientY });
    if (connection?.pointerId === event.pointerId) setConnection({ ...connection, clientX: event.clientX, clientY: event.clientY });
  };
  const onCanvasPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (panGesture?.pointerId === event.pointerId) setPanGesture(null);
    if (connection?.pointerId === event.pointerId) {
      const hit = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLElement>("[data-route-id]");
      const targetId = hit?.dataset.routeId ?? null;
      if (targetId === connection.sourceId) setConnection(null);
      else { setConnection(null); changeParent(connection.sourceId, targetId); }
    }
  };
  const onCanvasWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    event.preventDefault();
    const direction = event.deltaY < 0 ? 1 : -1;
    setScale((current) => Math.max(0.22, Math.min(1.5, current + direction * 0.06)));
  };

  if (editingPageId) return <PageEditorPage pageId={editingPageId} onBack={() => setEditingPageId(null)} />;

  const source = connection ? positionById.get(connection.sourceId) : undefined;
  const connectionPoint = connection && canvasRef.current ? (() => {
    const bounds = canvasRef.current!.getBoundingClientRect();
    return { x: (connection.clientX - bounds.left - pan.x) / scale, y: (connection.clientY - bounds.top - pan.y) / scale };
  })() : null;
  const pathRootCount = root.children?.length ?? 0;

  return <div className="relative h-full min-h-0 overflow-hidden bg-background" onContextMenu={(event) => { if (!(event.target as HTMLElement).closest("[data-route-id], [data-route-context-menu], button, input, [role=dialog]")) contextMenu(event); }}>
    <div className="absolute inset-0" data-route-canvas ref={canvasRef} onPointerDown={onCanvasPointerDown} onPointerMove={onCanvasPointerMove} onPointerUp={onCanvasPointerUp} onPointerCancel={onCanvasPointerUp} onWheel={onCanvasWheel} style={{ cursor: panGesture ? "grabbing" : connection ? "crosshair" : "grab", touchAction: "none" }}>
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 opacity-65" style={{ backgroundImage: "radial-gradient(hsl(var(--border)) .75px, transparent .75px)", backgroundSize: "24px 24px" }} />
      <div className="absolute left-3 right-3 top-3 z-20 flex flex-wrap items-center justify-between gap-2 sm:left-5 sm:right-5 sm:top-5" onContextMenu={(event) => event.stopPropagation()}>
        <div className="flex min-w-0 items-center gap-2 rounded-lg border border-border bg-card/95 px-3 py-2 shadow-sm backdrop-blur-sm">
          <GitBranch className="size-4 shrink-0 text-primary" />
          <div className="min-w-0"><h1 className="truncate text-sm font-semibold">Страницы и маршруты</h1><p className="hidden text-xs text-muted-foreground sm:block">{pathRootCount} корневых маршрута · {positions.length} узлов · ревизия {revision}</p></div>
          <span className={`ml-1 flex shrink-0 items-center gap-1 rounded-full px-2 py-1 text-xs ${dirty ? "bg-warning/10 text-warning" : "bg-success/10 text-success"}`}><span className={`size-1.5 rounded-full ${dirty ? "bg-warning" : "bg-success"}`} />{dirty ? "Изменено" : "Сохранено"}</span>
          {conflict && <span className="hidden items-center gap-1 text-xs text-destructive md:flex"><AlertTriangle className="size-3.5" />Черновик изменился</span>}
        </div>
        <div className="flex items-center gap-1 rounded-lg border border-border bg-card/95 p-1 shadow-sm backdrop-blur-sm">
          <Button type="button" size="sm" variant="ghost" className="px-2 sm:px-3" onClick={() => openRootDialog()} aria-label="Добавить корневой маршрут"><Plus /><span className="hidden sm:inline">Корневой маршрут</span></Button>
          <span className="mx-1 h-5 border-l border-border" />
          <Button type="button" size="icon-sm" variant="ghost" aria-label="Уменьшить масштаб" onClick={() => setScale((current) => Math.max(0.22, current - 0.1))}><ZoomOut /></Button>
          <span className="w-10 text-center text-xs tabular-nums text-muted-foreground">{Math.round(scale * 100)}%</span>
          <Button type="button" size="icon-sm" variant="ghost" aria-label="Увеличить масштаб" onClick={() => setScale((current) => Math.min(1.5, current + 0.1))}><ZoomIn /></Button>
          <Button type="button" size="icon-sm" variant="ghost" aria-label="Вписать маршруты в окно" onClick={fitCanvas}><Maximize2 /></Button>
          <Button type="button" size="sm" className="px-2 sm:px-3" aria-label={saving ? "Сохраняем черновик" : dirty ? "Сохранить черновик" : "Черновик сохранён"} title={saving ? "Сохраняем черновик" : dirty ? "Сохранить черновик" : "Черновик сохранён"} onClick={() => void save()} disabled={!dirty || saving}>{dirty ? <Save /> : <Check />}<span className="hidden sm:inline">{saving ? "Сохраняем…" : dirty ? "Сохранить" : "Сохранено"}</span></Button>
          {conflict && <Button type="button" size="icon-sm" variant="outline" aria-label="Загрузить актуальный черновик" disabled={saving} onClick={() => void reload()}><ArrowUp /></Button>}
        </div>
      </div>

      <div className="absolute bottom-4 left-4 z-10 sm:bottom-5 sm:left-5">
        <ContextHelp
          label="Подсказка по маршрутам"
          side="top"
          buttonClassName="size-10 rounded-xl border border-input bg-card/95 text-foreground shadow-sm backdrop-blur-sm hover:bg-accent"
        >
          ПКМ — действия маршрута. Перетащите кружок на узел, чтобы вложить маршрут, или в пустое место, чтобы отделить.
        </ContextHelp>
      </div>

      <div className="absolute left-0 top-0 origin-top-left" style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${scale})`, width: canvasWidth, height: canvasHeight }}>
        <svg className="pointer-events-none absolute inset-0 overflow-visible" width={canvasWidth} height={canvasHeight} aria-hidden="true">
          <defs><marker id="route-arrow" markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto"><path d="M 0 0 L 9 4.5 L 0 9 z" fill="hsl(var(--primary))" /></marker></defs>
          {positions.flatMap((parent) => (parent.node.children ?? []).map((child) => {
            const target = positionById.get(child.id);
            if (!target) return null;
            const startX = parent.x + parent.width;
            const startY = parent.y + parent.height / 2;
            const endX = target.x;
            const endY = target.y + target.height / 2;
            const control = Math.max(44, (endX - startX) * 0.45);
            return <path key={`${parent.node.id}-${child.id}`} d={`M ${startX} ${startY} C ${startX + control} ${startY}, ${endX - control} ${endY}, ${endX} ${endY}`} fill="none" stroke="hsl(var(--primary) / .48)" strokeWidth="2" markerEnd="url(#route-arrow)" />;
          }))}
          {connection && source && connectionPoint && <path d={`M ${source.x + source.width} ${source.y + source.height / 2} C ${source.x + source.width + 70} ${source.y + source.height / 2}, ${connectionPoint.x - 60} ${connectionPoint.y}, ${connectionPoint.x} ${connectionPoint.y}`} fill="none" stroke="hsl(var(--primary))" strokeWidth="2.5" strokeDasharray="7 6" />}
        </svg>

        {positions.map((position) => {
          const node = position.node;
          const label = pageName(config.pages, node.pageId);
          const childCount = node.children?.length ?? 0;
          return <div key={node.id} data-route-id={node.id} onContextMenu={(event) => contextMenu(event, node.id)} className="absolute" style={{ left: position.x, top: position.y, width: position.width, height: position.height }}>
            <button type="button" onClick={() => openPageForRoute(node)} aria-label={`Открыть страницу ${label ?? position.path}`} className={`group flex h-full w-full flex-col justify-between rounded-lg border bg-card px-3.5 py-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selectedRouteId === node.id ? "border-primary ring-2 ring-primary/20" : "border-border hover:border-primary/60"}`}>
              <span className="flex min-w-0 items-center gap-2">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">{position.path === "/" ? <Home className="size-4" /> : <GitBranch className="size-4" />}</span>
                <code className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">{position.path}</code>
                <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{displayNodeType(node)}</span>
              </span>
              <span className="flex items-end justify-between gap-2 pl-9">
                <span className="min-w-0 truncate text-sm text-muted-foreground">{label ?? "Страница не назначена"}</span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{config.pages.find((page) => page.id === node.pageId)?.placements.length ?? 0} фич</span>
              </span>
              {childCount > 0 && <span className="absolute -bottom-5 left-9 text-[10px] text-muted-foreground">{childCount} дочерн. {childCount === 1 ? "маршрут" : "маршрута"}</span>}
            </button>
            <button type="button" aria-label={`Соединить ${position.path} с дочерним маршрутом`} title="Перетащите, чтобы вложить маршрут" onPointerDown={(event) => { if (event.button !== 0) return; event.stopPropagation(); const canvas = event.currentTarget.closest("[data-route-canvas]"); if (canvas instanceof HTMLDivElement) canvas.setPointerCapture(event.pointerId); setConnection({ pointerId: event.pointerId, sourceId: node.id, clientX: event.clientX, clientY: event.clientY }); }} className="absolute -right-2.5 top-1/2 z-10 flex size-5 -translate-y-1/2 items-center justify-center rounded-full border-2 border-card bg-primary text-primary-foreground shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><span className="size-1.5 rounded-full bg-current" /></button>
            <button type="button" aria-label={`ПКМ: действия маршрута ${position.path}`} onContextMenu={(event) => contextMenu(event, node.id)} className="sr-only">Действия</button>
          </div>;
        })}

        {orphanPositions.map(({ page, x, y, width, height }) => <div key={`page-${page.id}`} className="absolute" style={{ left: x, top: y, width, height }} onContextMenu={(event) => contextMenu(event, undefined, page.id)}>
          <button type="button" onClick={() => openPageForOrphan(page.id)} className="flex h-full w-full flex-col justify-between rounded-lg border border-dashed border-warning/50 bg-card/90 px-3.5 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <span className="flex items-center gap-2"><span className="flex size-7 items-center justify-center rounded-md bg-warning/10 text-warning"><FilePlus2 className="size-4" /></span><span className="text-xs text-warning">Без маршрута</span></span>
            <span className="truncate pl-9 text-sm font-medium">{page.name}</span>
          </button>
          <button type="button" aria-label={`Открыть действия страницы ${page.name}`} onContextMenu={(event) => contextMenu(event, undefined, page.id)} className="sr-only">Действия</button>
        </div>)}
      </div>
    </div>

    {menu && <div data-route-context-menu role="menu" className="absolute z-40 min-w-56 rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-lg" style={{ left: menu.x, top: menu.y }}>
      {menu.routeId ? <>
        <MenuAction onClick={() => selectedNode && openPageForRoute(selectedNode)}>Открыть страницу</MenuAction>
        <MenuAction onClick={() => openChildDialog(menu.routeId!)} disabled={selectedNode?.type === "catchAll" || (selectedNode?.type === "path" && selectedNode.path?.split("/").filter(Boolean).at(-1)?.startsWith("*"))}><Plus className="size-4" />Добавить дочерний</MenuAction>
        <MenuAction onClick={() => selectedNode && editRouteDialog(selectedNode)}>Изменить URL и страницу</MenuAction>
        {descriptors.find((route) => route.id === menu.routeId)?.parentId && <MenuAction onClick={() => changeParent(menu.routeId!, null)}><Link2 className="size-4" />Отсоединить в корень</MenuAction>}
        <div className="my-1 border-t border-border" />
        {(() => {
          const parentId = descriptors.find((route) => route.id === menu.routeId)?.parentId;
          const siblingIds = (parentId ? currentNode(root, parentId)?.children : root.children)?.map((node) => node.id) ?? [];
          const index = siblingIds.indexOf(menu.routeId!);
          return <>
            <MenuAction disabled={index <= 0} onClick={() => { applyTree(moveRoutePriority(root, menu.routeId!, -1)); setMenu(null); }}><ArrowUp className="size-4" />Выше приоритетом</MenuAction>
            <MenuAction disabled={index < 0 || index === siblingIds.length - 1} onClick={() => { applyTree(moveRoutePriority(root, menu.routeId!, 1)); setMenu(null); }}><ArrowDown className="size-4" />Ниже приоритетом</MenuAction>
          </>;
        })()}
        <MenuAction className="text-destructive" onClick={() => selectedNode && deleteBranch(selectedNode)}><Trash2 className="size-4" />Удалить ветвь</MenuAction>
      </> : menu.pageId ? <>
        <MenuAction onClick={() => openPageForOrphan(menu.pageId!)}>Редактировать страницу</MenuAction>
        <MenuAction onClick={() => openRootDialog(menu.pageId)}><Plus className="size-4" />Создать для неё маршрут</MenuAction>
      </> : <>
        <MenuAction onClick={() => openRootDialog()}><Plus className="size-4" />Новая самостоятельная страница</MenuAction>
        <MenuAction onClick={() => openRootDialog(undefined, true)}><Link2 className="size-4" />Добавить маршрут к существующей</MenuAction>
      </>}
    </div>}

    <RouteDialog key={`${dialog?.mode ?? "closed"}-${dialog?.routeId ?? dialog?.parentId ?? "root"}`} state={dialog} pages={config.pages} tree={root} onClose={() => setDialog(null)} onApply={(route, parentId, page) => {
      if (dialog?.mode === "edit") applyRouteEdit(route, null, page);
      else makeNewRoute(route, parentId ?? null, page);
    }} />
  </div>;
}

function isDescendant(tree: RouteTreeNode, ancestorId: string, id: string): boolean {
  const ancestor = currentNode(tree, ancestorId);
  const contains = (node: RouteTreeNode): boolean => node.id === id || (node.children ?? []).some(contains);
  return Boolean(ancestor?.children?.some(contains));
}
