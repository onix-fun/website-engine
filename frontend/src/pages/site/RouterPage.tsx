import { useMemo, useState, type PointerEvent, type WheelEvent } from "react";
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowDown, ArrowUp, Code2, GripVertical, Minus, Plus, Route as RouteIcon, Trash2, ZoomIn, ZoomOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import type { RouteTreeNode } from "@/types";
import { useSiteWorkspace } from "./SiteWorkspace";

type Position = { x: number; y: number; width: number; height: number; node: RouteTreeNode; path: string };
type NewNodeType = Exclude<RouteTreeNode["type"], "index">;

function childSegment(node: RouteTreeNode) {
  if (node.type === "static") return `/${node.value ?? "segment"}`;
  if (node.type === "param") return `/:${node.name ?? "param"}`;
  if (node.type === "regex") return `/{${node.pattern ?? "regex"}}`;
  if (node.type === "catchAll") return `/*${node.name ?? "path"}`;
  return "";
}

function collectPositions(root: RouteTreeNode): Position[] {
  const nodes: Position[] = [];
  let row = 0;
  const visit = (node: RouteTreeNode, depth: number, parentPath: string): number => {
    const path = node.type === "index" ? "/" : `${parentPath === "/" ? "" : parentPath}${childSegment(node)}` || "/";
    const children = node.children ?? [];
    const childRows = children.map((child) => visit(child, depth + 1, path));
    const y = childRows.length ? childRows.reduce((sum, value) => sum + value, 0) / childRows.length : row++ * 116;
    nodes.push({ x: depth * 258 + 28, y: y * 116 + 32, width: 206, height: 76, node, path });
    return y;
  };
  visit(root, 0, "");
  return nodes;
}

function updateNode(root: RouteTreeNode, id: string, update: (node: RouteTreeNode) => RouteTreeNode): RouteTreeNode {
  if (root.id === id) return update(root);
  return { ...root, children: (root.children ?? []).map((child) => updateNode(child, id, update)) };
}

function removeNode(root: RouteTreeNode, id: string): RouteTreeNode {
  return { ...root, children: (root.children ?? []).filter((child) => child.id !== id).map((child) => removeNode(child, id)) };
}

function findNode(root: RouteTreeNode, id: string): RouteTreeNode | undefined {
  if (root.id === id) return root;
  for (const child of root.children ?? []) {
    const found = findNode(child, id);
    if (found) return found;
  }
  return undefined;
}

function routeTypeName(type: RouteTreeNode["type"]) {
  return ({ index: "Корень сайта", static: "Статический сегмент", param: "Параметр пути", regex: "Regex-сегмент", catchAll: "Catch-all" })[type];
}

function SortableBranch({ node, pageName, onSelect, selected }: { node: RouteTreeNode; pageName?: string; onSelect: () => void; selected: boolean }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: node.id });
  return (
    <div ref={setNodeRef} style={{ transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.5 : 1 }} className="flex items-center gap-1">
      <button type="button" className="cursor-grab rounded p-1 text-muted-foreground hover:bg-muted" aria-label="Перетащить ветвь" {...attributes} {...listeners}><GripVertical className="size-4" /></button>
      <button type="button" onClick={onSelect} className={`min-w-0 flex-1 truncate rounded border px-2 py-1.5 text-left text-xs ${selected ? "border-primary bg-primary/5" : "border-border hover:bg-muted"}`}>
        <span className="font-medium">{childSegment(node)}</span>{pageName && <span className="ml-1 text-muted-foreground">→ {pageName}</span>}
      </button>
    </div>
  );
}

export default function RouterPage() {
  const { config, update } = useSiteWorkspace();
  const [selectedId, setSelectedId] = useState(config.routeTree.id);
  const [newType, setNewType] = useState<NewNodeType>("static");
  const [scale, setScale] = useState(1);
  const [pan, setPan] = useState({ x: 24, y: 24 });
  const [dragging, setDragging] = useState<{ pointer: number; x: number; y: number; originX: number; originY: number } | null>(null);
  const root = config.routeTree;
  const selected = findNode(root, selectedId) ?? root;
  const positions = useMemo(() => collectPositions(root), [root]);
  const byId = new Map(positions.map((item) => [item.node.id, item]));
  const pages = config.pages;
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const selectedParent = positions.find((item) => item.node.children?.some((child) => child.id === selected.id))?.node;

  const change = (id: string, values: Partial<RouteTreeNode>) => update((current) => ({
    ...current,
    routeTree: updateNode(current.routeTree, id, (node) => ({ ...node, ...values })),
  }));

  const addBranch = () => {
    if (selected.type === "catchAll") return;
    const id = `route-${crypto.randomUUID().slice(0, 8)}`;
    const node: RouteTreeNode = {
      id,
      type: newType,
      ...(newType === "static" ? { value: "new" } : {}),
      ...(newType === "param" ? { name: "slug" } : {}),
      ...(newType === "regex" ? { pattern: "(?<slug>[a-z0-9-]+)" } : {}),
      ...(newType === "catchAll" ? { name: "path" } : {}),
      children: [],
    };
    change(selected.id, { ...(selected.type === "index" ? {} : { pageId: undefined }), children: [...(selected.children ?? []), node] });
    setSelectedId(id);
  };

  const reorderBranches = (event: DragEndEvent) => {
    if (!event.over || event.active.id === event.over.id || !selectedParent) return;
    const children = [...(selectedParent.children ?? [])];
    const from = children.findIndex((child) => child.id === event.active.id);
    const to = children.findIndex((child) => child.id === event.over?.id);
    if (from < 0 || to < 0) return;
    update((current) => ({ ...current, routeTree: updateNode(current.routeTree, selectedParent.id, (node) => ({ ...node, children: arrayMove(children, from, to) })) }));
  };

  const canvasPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if ((event.target as HTMLElement).closest("button")) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    setDragging({ pointer: event.pointerId, x: event.clientX, y: event.clientY, originX: pan.x, originY: pan.y });
  };
  const canvasPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!dragging || dragging.pointer !== event.pointerId) return;
    setPan({ x: dragging.originX + event.clientX - dragging.x, y: dragging.originY + event.clientY - dragging.y });
  };
  const canvasPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    if (dragging?.pointer === event.pointerId) setDragging(null);
  };
  const canvasWheel = (event: WheelEvent<HTMLDivElement>) => {
    if (!event.ctrlKey && !event.metaKey) return;
    event.preventDefault();
    setScale((current) => Math.max(0.55, Math.min(1.5, current + (event.deltaY < 0 ? 0.08 : -0.08))));
  };

  const pathFor = (node: RouteTreeNode) => byId.get(node.id)?.path ?? "/";
  const pageName = (pageId?: string) => pages.find((page) => page.id === pageId)?.name;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div><h2 className="text-xl font-semibold tracking-tight">Роутер</h2><p className="mt-1 text-sm text-muted-foreground">Стройте URL как дерево. Порядок соседних ветвей задаёт приоритет совпадения.</p></div>
        <div className="flex items-center gap-2 rounded-md border border-border bg-card p-1">
          <Button type="button" variant="ghost" size="icon-sm" aria-label="Уменьшить" onClick={() => setScale((v) => Math.max(0.55, v - 0.1))}><ZoomOut /></Button>
          <span className="w-12 text-center text-xs tabular-nums">{Math.round(scale * 100)}%</span>
          <Button type="button" variant="ghost" size="icon-sm" aria-label="Увеличить" onClick={() => setScale((v) => Math.min(1.5, v + 0.1))}><ZoomIn /></Button>
          <Button type="button" variant="ghost" size="icon-sm" aria-label="Сбросить масштаб и позицию" onClick={() => { setScale(1); setPan({ x: 24, y: 24 }); }}><RouteIcon /></Button>
        </div>
      </div>

      <div className="grid min-h-[570px] overflow-hidden rounded-lg border border-border bg-card xl:grid-cols-[minmax(0,1fr)_330px]">
        <div className="relative min-h-[570px] overflow-hidden bg-muted/15" onPointerDown={canvasPointerDown} onPointerMove={canvasPointerMove} onPointerUp={canvasPointerUp} onPointerCancel={canvasPointerUp} onWheel={canvasWheel} style={{ cursor: dragging ? "grabbing" : "grab", touchAction: "none" }}>
          <div className="absolute inset-0 opacity-60" style={{ backgroundImage: "radial-gradient(hsl(var(--border)) 0.8px, transparent 0.8px)", backgroundSize: "22px 22px" }} />
          <div className="absolute left-4 top-4 z-10 flex items-center gap-2 rounded-md border border-border bg-background/90 px-2.5 py-1.5 text-xs text-muted-foreground"><Code2 className="size-3.5" />Перетаскивайте пустое поле · Ctrl/⌘ + колесо — масштаб</div>
          <div className="absolute left-0 top-0 origin-top-left" style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${scale})` }}>
            <svg className="pointer-events-none absolute left-0 top-0 overflow-visible" width={Math.max(760, ...positions.map((item) => item.x + item.width + 100))} height={Math.max(620, ...positions.map((item) => item.y + item.height + 80))} aria-hidden="true">
              {positions.flatMap((parent) => (parent.node.children ?? []).map((child) => {
                const target = byId.get(child.id);
                if (!target) return null;
                const startX = parent.x + parent.width;
                const startY = parent.y + parent.height / 2;
                const endX = target.x;
                const endY = target.y + target.height / 2;
                const middle = (startX + endX) / 2;
                return <path key={child.id} d={`M ${startX} ${startY} C ${middle} ${startY}, ${middle} ${endY}, ${endX} ${endY}`} fill="none" stroke="hsl(var(--border))" strokeWidth="2" />;
              }))}
            </svg>
            {positions.map(({ node, path, ...position }) => (
              <button type="button" key={node.id} onClick={() => setSelectedId(node.id)} style={{ left: position.x, top: position.y, width: position.width, height: position.height }} className={`absolute rounded-md border bg-card px-3 py-2 text-left shadow-sm transition-colors ${selectedId === node.id ? "border-primary ring-2 ring-primary/15" : "border-border hover:border-primary/50"}`}>
                <span className="flex items-center justify-between gap-2"><span className="truncate text-sm font-medium">{node.type === "index" ? "/" : childSegment(node)}</span><span className="shrink-0 text-[10px] text-muted-foreground">{node.type}</span></span>
                <span className="mt-1 block truncate text-xs text-muted-foreground">{pageName(node.pageId) ?? path}</span>
              </button>
            ))}
          </div>
        </div>

        <aside className="flex min-w-0 flex-col border-t border-border xl:border-l xl:border-t-0">
          <div className="border-b border-border px-4 py-3"><h3 className="font-medium">Свойства узла</h3><p className="mt-1 truncate text-xs text-muted-foreground">{pathFor(selected)}</p></div>
          <div className="flex-1 space-y-4 overflow-y-auto p-4">
            <div><p className="text-sm font-medium">{routeTypeName(selected.type)}</p><p className="mt-1 text-xs text-muted-foreground">ID: {selected.id}</p></div>
            {selected.type === "static" && <div className="space-y-1.5"><Label>Сегмент URL</Label><Input value={selected.value ?? ""} onChange={(event) => change(selected.id, { value: event.currentTarget.value })} /></div>}
            {(selected.type === "param" || selected.type === "catchAll") && <div className="space-y-1.5"><Label>Имя параметра</Label><Input value={selected.name ?? ""} onChange={(event) => change(selected.id, { name: event.currentTarget.value })} placeholder={selected.type === "catchAll" ? "path" : "slug"} /><p className="text-xs text-muted-foreground">Фича сможет получить это значение как аргумент из пути.</p></div>}
            {selected.type === "regex" && <div className="space-y-1.5"><Label>Regex одного сегмента</Label><Input value={selected.pattern ?? ""} onChange={(event) => change(selected.id, { pattern: event.currentTarget.value })} placeholder="(?&lt;id&gt;[0-9]+)" /><p className="text-xs text-muted-foreground">Именованные группы, например <code>(?&lt;id&gt;[0-9]+)</code>, становятся path-параметрами. Сегмент не может включать /.</p></div>}
            {(selected.type === "index" || (selected.children ?? []).length === 0) ? <div className="space-y-1.5"><Label>{selected.type === "index" ? "Страница корня" : "Страница маршрута"}</Label><select className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" value={selected.pageId ?? ""} onChange={(event) => change(selected.id, { pageId: event.currentTarget.value || undefined })}><option value="">Без страницы</option>{pages.map((page) => <option key={page.id} value={page.id}>{page.name}</option>)}</select></div> : <p className="rounded-md bg-muted/50 p-2 text-xs text-muted-foreground">Промежуточная ветвь. Страницу назначьте одному из её листьев.</p>}
            {selected.type !== "catchAll" && <div className="space-y-2 border-t border-border pt-3">
              <div className="flex items-center justify-between gap-2"><div><h4 className="text-sm font-medium">Следующие сегменты</h4><p className="text-xs text-muted-foreground">Верхняя ветвь проверяется первой.</p></div></div>
              <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={reorderBranches}>
                <SortableContext items={(selected.children ?? []).map((child) => child.id)} strategy={verticalListSortingStrategy}>
                  <div className="space-y-1">{(selected.children ?? []).map((child) => <SortableBranch key={child.id} node={child} pageName={pageName(child.pageId)} selected={selectedId === child.id} onSelect={() => setSelectedId(child.id)} />)}</div>
                </SortableContext>
              </DndContext>
              <div className="flex gap-2"><select className="h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-sm" value={newType} onChange={(event) => setNewType(event.currentTarget.value as NewNodeType)}><option value="static">Статический</option><option value="param">Параметр</option><option value="regex">Regex</option><option value="catchAll">Catch-all</option></select><Button type="button" size="sm" variant="outline" onClick={addBranch}><Plus />Добавить</Button></div>
            </div>}
            {selected.type !== "index" && <Button type="button" variant="outline" size="sm" className="w-full text-destructive" onClick={() => { update((current) => ({ ...current, routeTree: removeNode(current.routeTree, selected.id) })); setSelectedId(selectedParent?.id ?? root.id); }}><Trash2 />Удалить ветвь</Button>}
            {selectedParent && selectedParent.children && selectedParent.children.length > 1 && <div className="flex gap-2"><Button type="button" variant="outline" size="sm" className="flex-1" disabled={selectedParent.children[0]?.id === selected.id} onClick={() => { const children = [...selectedParent.children!]; const at = children.findIndex((item) => item.id === selected.id); if (at > 0) update((current) => ({ ...current, routeTree: updateNode(current.routeTree, selectedParent.id, (node) => ({ ...node, children: arrayMove(children, at, at - 1) })) })); }}><ArrowUp />Приоритет выше</Button><Button type="button" variant="outline" size="sm" className="flex-1" disabled={selectedParent.children.at(-1)?.id === selected.id} onClick={() => { const children = [...selectedParent.children!]; const at = children.findIndex((item) => item.id === selected.id); if (at < children.length - 1) update((current) => ({ ...current, routeTree: updateNode(current.routeTree, selectedParent.id, (node) => ({ ...node, children: arrayMove(children, at, at + 1) })) })); }}><ArrowDown />Приоритет ниже</Button></div>}
          </div>
          <div className="border-t border-border p-4 text-xs leading-5 text-muted-foreground">Маршруты одной страницы должны иметь одинаковые имена path-параметров. Regex исполняется с ограничением времени.</div>
        </aside>
      </div>
      <Card><CardHeader className="pb-3"><CardTitle className="text-base">Как читается дерево</CardTitle><CardDescription>Каждый узел описывает один сегмент URL; ветви обходятся сверху вниз.</CardDescription></CardHeader><CardContent className="flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">{["/ — корень сайта", "feed — точный сегмент", ":slug — один сегмент и path-параметр", "[regex] — проверка одного сегмента", "*path — остаток пути целиком"].map((item) => <span key={item} className="flex items-center gap-1.5"><Minus className="size-3" />{item}</span>)}</CardContent></Card>
    </div>
  );
}
