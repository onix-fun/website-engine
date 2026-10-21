import { useMemo, useState } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  AlertCircle,
  ChevronDown,
  ChevronRight,
  Copy,
  GripVertical,
  Layers,
  MoreHorizontal,
  MoveDown,
  MoveUp,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useConfirm } from "@/components/common/confirm";
import { EmptyState } from "@/components/common/page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { FIELD_TYPES, fieldTypeMeta } from "./field-meta";
import { appendChild, insertFieldAfter, locateField, moveField, moveSibling, removeField, updateField } from "./tree";
import { cloneField, createField } from "./schema";
import type { BuilderFieldType, BuilderIssue, SchemaFieldNode } from "./types";
import { FieldProperties } from "./FieldProperties";

const GROUP_ORDER = ["Текст", "Числа и даты", "Структуры", "Медиа"] as const;

function AddFieldMenu({
  onAdd,
  disabled,
  label = "Добавить поле",
  size = "sm",
  variant = "outline",
}: {
  onAdd: (type: BuilderFieldType) => void;
  disabled?: boolean;
  label?: string;
  size?: "sm" | "default";
  variant?: "outline" | "default" | "ghost";
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" size={size} variant={variant} disabled={disabled}>
          <Plus />
          {label}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        {GROUP_ORDER.map((group, groupIndex) => {
          const items = FIELD_TYPES.filter((type) => type.group === group);
          if (items.length === 0) return null;
          return (
            <div key={group}>
              {groupIndex > 0 && <DropdownMenuSeparator />}
              <DropdownMenuLabel>{group}</DropdownMenuLabel>
              <DropdownMenuGroup>
                {items.map((type) => {
                  const Icon = type.icon;
                  return (
                    <DropdownMenuItem
                      key={type.value}
                      onSelect={() => onAdd(type.value)}
                      className="gap-2.5"
                    >
                      <Icon className="h-4 w-4 text-muted-foreground" />
                      <span className="flex-1">
                        <span className="block text-sm">{type.label}</span>
                        <span className="block text-2xs text-muted-foreground">{type.hint}</span>
                      </span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            </div>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function FieldList({
  fields,
  parentId,
  depth,
  selectedId,
  collapsedIds,
  issues,
  disabled,
  onSelect,
  onToggle,
  onAdd,
  onDuplicate,
  onDelete,
  onMoveSibling,
}: {
  fields: SchemaFieldNode[];
  parentId: string | null;
  depth: number;
  selectedId: string | null;
  collapsedIds: Set<string>;
  issues: BuilderIssue[];
  disabled?: boolean;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
  onAdd: (parentId: string | null, type: BuilderFieldType) => void;
  onDuplicate: (field: SchemaFieldNode) => void;
  onDelete: (field: SchemaFieldNode) => void;
  onMoveSibling: (field: SchemaFieldNode, direction: -1 | 1) => void;
}) {
  return (
    <div className={cn(depth > 0 && "mt-2 space-y-2 border-l border-border/70 pl-3 sm:pl-4")}>
      <SortableContext items={fields.map((field) => field.id)} strategy={verticalListSortingStrategy}>
        <div className="space-y-2">
          {fields.map((field) => (
            <FieldRow
              key={field.id}
              field={field}
              depth={depth}
              selectedId={selectedId}
              collapsedIds={collapsedIds}
              collapsed={collapsedIds.has(field.id)}
              issues={issues}
              disabled={disabled}
              onSelect={onSelect}
              onToggle={onToggle}
              onAdd={onAdd}
              onDuplicate={onDuplicate}
              onDelete={onDelete}
              onMoveSibling={onMoveSibling}
            />
          ))}
        </div>
      </SortableContext>
      {!disabled && (
        <div className={cn(depth > 0 && "pt-1")}>
          <AddFieldMenu
            onAdd={(type) => onAdd(parentId, type)}
            label={depth > 0 ? "Добавить вложенное поле" : "Добавить поле"}
            variant="ghost"
          />
        </div>
      )}
    </div>
  );
}

function FieldRow({
  field,
  depth,
  selectedId,
  collapsedIds,
  collapsed,
  issues,
  disabled,
  onSelect,
  onToggle,
  onAdd,
  onDuplicate,
  onDelete,
  onMoveSibling,
}: {
  field: SchemaFieldNode;
  depth: number;
  selectedId: string | null;
  collapsedIds: Set<string>;
  collapsed: boolean;
  issues: BuilderIssue[];
  disabled?: boolean;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
  onAdd: (parentId: string | null, type: BuilderFieldType) => void;
  onDuplicate: (field: SchemaFieldNode) => void;
  onDelete: (field: SchemaFieldNode) => void;
  onMoveSibling: (field: SchemaFieldNode, direction: -1 | 1) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: field.id, disabled });
  const meta = fieldTypeMeta(field.type);
  const Icon = meta.icon;
  const supportsChildren =
    field.type === "object" || (field.type === "array" && field.itemType === "object");
  const fieldIssues = issues.filter((issue) => issue.fieldId === field.id);
  const errors = fieldIssues.filter((issue) => issue.severity === "error").length;
  const warnings = fieldIssues.filter((issue) => issue.severity === "warning").length;
  const selected = selectedId === field.id;

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(isDragging && "opacity-60")}
    >
      <div
        data-selected={selected}
        className={cn(
          "field-row group",
          selected && "border-primary/60 bg-primary/5 ring-1 ring-primary/20",
        )}
      >
        <button
          type="button"
          className="mt-0.5 cursor-grab touch-none rounded-sm p-1 text-muted-foreground/60 hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
          aria-label={`Переместить поле ${field.label || field.key || "без названия"}`}
          disabled={disabled}
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-4 w-4" />
        </button>

        {supportsChildren ? (
          <button
            type="button"
            className="mt-0.5 rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => onToggle(field.id)}
            aria-label={collapsed ? "Развернуть вложенные поля" : "Свернуть вложенные поля"}
            aria-expanded={!collapsed}
          >
            {collapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          </button>
        ) : (
          <span className="mt-0.5 w-[1.375rem]" aria-hidden="true" />
        )}

        <button
          type="button"
          onClick={() => onSelect(field.id)}
          className="flex min-w-0 flex-1 items-start gap-2.5 rounded-sm py-0.5 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-pressed={selected}
        >
          <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40 text-muted-foreground">
            <Icon className="h-3.5 w-3.5" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex flex-wrap items-center gap-1.5">
              <span className="truncate text-sm font-medium">
                {field.label || "Без названия"}
              </span>
              {field.required && <span className="text-xs text-destructive">*</span>}
              {field.isPrivate && <Badge variant="muted">private</Badge>}
              {errors > 0 && (
                <Badge variant="destructive">
                  <AlertCircle />
                  {errors}
                </Badge>
              )}
              {warnings > 0 && errors === 0 && (
                <Badge variant="warning">{warnings}</Badge>
              )}
            </span>
            <span className="mt-0.5 flex flex-wrap items-center gap-1.5 text-2xs text-muted-foreground">
              <code className={cn(errors > 0 && "text-destructive")}>
                {field.key || "ключ не задан"}
              </code>
              <span>·</span>
              <span>{meta.label}</span>
              {supportsChildren && (
                <>
                  <span>·</span>
                  <span>{field.children.length} влож.</span>
                </>
              )}
            </span>
          </span>
        </button>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Действия с полем ${field.label || field.key}`}
              className="opacity-60 group-hover:opacity-100"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuItem onSelect={() => onSelect(field.id)}>Настроить</DropdownMenuItem>
            {supportsChildren && (
              <DropdownMenuItem onSelect={() => onAdd(field.id, "string")}>
                <Plus />
                Добавить вложенное поле
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={() => onDuplicate(field)}>
              <Copy />
              Дублировать
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMoveSibling(field, -1)}>
              <MoveUp />
              Переместить выше
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMoveSibling(field, 1)}>
              <MoveDown />
              Переместить ниже
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="text-destructive focus:text-destructive"
              onSelect={() => onDelete(field)}
            >
              <Trash2 />
              Удалить поле
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {supportsChildren && !collapsed && field.children.length > 0 && (
        <FieldList
          fields={field.children}
          parentId={field.id}
          depth={depth + 1}
          selectedId={selectedId}
          collapsedIds={collapsedIds}
          issues={issues}
          disabled={disabled}
          onSelect={onSelect}
          onToggle={onToggle}
          onAdd={onAdd}
          onDuplicate={onDuplicate}
          onDelete={onDelete}
          onMoveSibling={onMoveSibling}
        />
      )}
    </div>
  );
}

export function SchemaBuilder({
  fields,
  onChange,
  selectedId,
  onSelect,
  issues,
  disabled,
}: {
  fields: SchemaFieldNode[];
  onChange: (fields: SchemaFieldNode[]) => void;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  issues: BuilderIssue[];
  disabled?: boolean;
}) {
  const confirm = useConfirm();
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set());
  const [activeId, setActiveId] = useState<string | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const selectedField = useMemo(
    () => (selectedId ? locateField(fields, selectedId)?.field ?? null : null),
    [fields, selectedId],
  );

  const addField = (parentId: string | null, type: BuilderFieldType) => {
    const field = createField(type);
    onChange(appendChild(fields, parentId, field));
    if (parentId) {
      setCollapsedIds((current) => {
        const next = new Set(current);
        next.delete(parentId);
        return next;
      });
    }
    onSelect(field.id);
  };

  const duplicateField = (field: SchemaFieldNode) => {
    const location = locateField(fields, field.id);
    if (!location) return;
    const copy = cloneField(field);
    const baseKey = field.key.trim() || "field";
    let candidate = `${baseKey}_copy`;
    let counter = 2;
    while (location.siblings.some((sibling) => sibling.key.trim() === candidate)) {
      candidate = `${baseKey}_copy_${counter}`;
      counter += 1;
    }
    copy.key = candidate;
    copy.label = field.label ? `${field.label} (копия)` : "Копия поля";
    onChange(insertFieldAfter(fields, field.id, copy));
    onSelect(copy.id);
    toast.success("Поле дублировано");
  };

  const deleteField = async (field: SchemaFieldNode) => {
    const confirmed = await confirm({
      title: "Удалить поле?",
      description: (
        <>
          Поле <strong>«{field.label || field.key}»</strong>
          {field.children.length > 0
            ? ` и ${field.children.length} вложенных полей будут удалены из черновика схемы.`
            : " будет удалено из черновика схемы."}
        </>
      ),
      confirmLabel: "Удалить",
      destructive: true,
    });
    if (!confirmed) return;
    onChange(removeField(fields, field.id));
    if (selectedId === field.id) onSelect(null);
    toast.success("Поле удалено");
  };

  const handleMoveSibling = (field: SchemaFieldNode, direction: -1 | 1) => {
    onChange(moveSibling(fields, field.id, direction));
  };

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(String(event.active.id));
  };

  const handleDragEnd = (event: DragEndEvent) => {
    setActiveId(null);
    if (!event.over || event.active.id === event.over.id) return;
    const active = String(event.active.id);
    const over = String(event.over.id);
    const activeLocation = locateField(fields, active);
    const overLocation = locateField(fields, over);
    const mode =
      activeLocation &&
      overLocation &&
      activeLocation.parentId === overLocation.parentId &&
      activeLocation.index < overLocation.index
        ? "after"
        : "before";
    onChange(moveField(fields, active, over, mode));
  };

  const activeField = activeId ? locateField(fields, activeId)?.field : undefined;

  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_23rem]">
      <section aria-label="Дерево полей схемы" className="min-w-0">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <h2 className="text-base font-semibold">Поля схемы</h2>
            <Badge variant="muted">{fields.length}</Badge>
            {issues.filter((issue) => issue.severity === "error").length > 0 ? (
              <Badge variant="destructive">
                {issues.filter((issue) => issue.severity === "error").length} ошиб.
              </Badge>
            ) : (
              <Badge variant="success">Проверка пройдена</Badge>
            )}
          </div>
          {!disabled && (
            <AddFieldMenu onAdd={(type) => addField(null, type)} label="Добавить поле" variant="default" />
          )}
        </div>

        {fields.length === 0 ? (
          <EmptyState
            icon={Layers}
            title="В схеме пока нет полей"
            description="Добавьте первое поле: строку, число, объект, медиа или массив. Конструктор сгенерирует JSON Schema автоматически."
            action={
              !disabled ? (
                <AddFieldMenu onAdd={(type) => addField(null, type)} label="Добавить первое поле" variant="default" />
              ) : undefined
            }
          />
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
            onDragCancel={() => setActiveId(null)}
          >
            <FieldList
              fields={fields}
              parentId={null}
              depth={0}
              selectedId={selectedId}
              collapsedIds={collapsedIds}
              issues={issues}
              disabled={disabled}
              onSelect={onSelect}
              onToggle={(id) =>
                setCollapsedIds((current) => {
                  const next = new Set(current);
                  if (next.has(id)) next.delete(id);
                  else next.add(id);
                  return next;
                })
              }
              onAdd={addField}
              onDuplicate={duplicateField}
              onDelete={(field) => void deleteField(field)}
              onMoveSibling={handleMoveSibling}
            />
            <DragOverlay>
              {activeField ? (
                <div className="rounded-md border border-primary/40 bg-card px-3 py-2 text-sm shadow-md">
                  {activeField.label || activeField.key || "Поле"}
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>
        )}

        <p className="mt-3 text-2xs leading-5 text-muted-foreground">
          Порядок полей можно менять перетаскиванием или кнопками в меню. Вложенность
          задаётся через объект или массив объектов; на мобильных доступны те же
          действия без drag-and-drop.
        </p>
      </section>

      <aside className="self-start rounded-lg border border-border bg-card p-4 xl:sticky xl:top-[calc(var(--header-height)+1.25rem)]">
        <FieldProperties
          field={selectedField}
          issues={issues}
          onUpdate={(updated) => {
            if (!selectedField) return;
            onChange(updateField(fields, selectedField.id, () => updated));
          }}
          onAddChild={(parentId) => addField(parentId, "string")}
          disabled={disabled}
        />
      </aside>
    </div>
  );
}
