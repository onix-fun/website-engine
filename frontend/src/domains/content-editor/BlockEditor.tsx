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
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowDown,
  ArrowUp,
  Code2,
  Copy,
  Film,
  GalleryHorizontal,
  GripVertical,
  Heading,
  Image as ImageIcon,
  ImagePlus,
  List,
  ListOrdered,
  Minus,
  MoreHorizontal,
  Pilcrow,
  Plus,
  Quote,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import type { Media } from "@/types";
import { publicMediaPath } from "@/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input, Label, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/common/page";
import { MediaPickerDialog } from "@/domains/media/MediaPickerDialog";
import {
  BLOCK_OPTIONS,
  blockPreviewText,
  createBlock,
  duplicateBlock,
  TEXT_BLOCK_TYPES,
} from "./blocks";
import type { BlockType, ContentBlock } from "./types";

const blockIcons = {
  paragraph: Pilcrow,
  heading: Heading,
  bulletedList: List,
  numberedList: ListOrdered,
  quote: Quote,
  code: Code2,
  divider: Minus,
  image: ImageIcon,
  gallery: GalleryHorizontal,
  video: Film,
} as const;

function blockLabel(type: BlockType): string {
  return BLOCK_OPTIONS.find((option) => option.type === type)?.label ?? type;
}

function AddBlockMenu({
  onAdd,
  types,
  disabled,
  compact = false,
}: {
  onAdd: (type: BlockType) => void;
  types: BlockType[];
  disabled?: boolean;
  compact?: boolean;
}) {
  const options = BLOCK_OPTIONS.filter((option) => types.includes(option.type));
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size={compact ? "icon-sm" : "sm"}
          disabled={disabled}
          aria-label="Добавить блок"
          className={compact ? "text-muted-foreground" : undefined}
        >
          <Plus />
          {!compact && "Добавить блок"}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        <DropdownMenuLabel>Блоки</DropdownMenuLabel>
        {options.map((option) => {
          const Icon = blockIcons[option.type];
          return (
            <DropdownMenuItem key={option.type} onSelect={() => onAdd(option.type)}>
              <Icon className="h-4 w-4 text-muted-foreground" />
              <span>
                <span className="block">{option.label}</span>
                <span className="block text-2xs text-muted-foreground">{option.description}</span>
              </span>
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function InlineAdd({
  onAdd,
  types,
  disabled,
}: {
  onAdd: (type: BlockType) => void;
  types: BlockType[];
  disabled?: boolean;
}) {
  return (
    <div className="group flex h-8 items-center gap-2">
      <div className="h-px flex-1 bg-border/0 transition-colors group-hover:bg-border" />
      <AddBlockMenu onAdd={onAdd} types={types} disabled={disabled} compact />
      <div className="h-px flex-1 bg-border/0 transition-colors group-hover:bg-border" />
    </div>
  );
}

function MediaField({
  mediaId,
  onPick,
  onClear,
  label,
  accept = "image",
  kind = "image",
  disabled,
}: {
  mediaId: string;
  onPick: () => void;
  onClear: () => void;
  label: string;
  accept?: "image" | "video";
  kind?: "image" | "video";
  disabled?: boolean;
}) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      {mediaId ? (
        <div className="overflow-hidden rounded-md border border-border">
          {kind === "image" ? (
            <img
              src={publicMediaPath(mediaId)}
              alt=""
              className="max-h-64 w-full object-cover"
            />
          ) : (
            <video
              src={publicMediaPath(mediaId)}
              controls
              preload="metadata"
              className="max-h-64 w-full bg-black"
            />
          )}
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border bg-muted/30 px-2 py-1.5">
            <code className="truncate text-2xs text-muted-foreground">{mediaId}</code>
            <div className="flex gap-1">
              <Button type="button" variant="ghost" size="sm" onClick={onPick} disabled={disabled}>
                <RefreshCw />
                Заменить
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-destructive hover:text-destructive"
                onClick={onClear}
                disabled={disabled}
              >
                <Trash2 />
                Убрать
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <button
          type="button"
          onClick={onPick}
          disabled={disabled}
          className="flex w-full items-center justify-center gap-2 rounded-md border border-dashed border-border px-4 py-8 text-sm text-muted-foreground transition-colors hover:border-primary/40 hover:bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
        >
          <ImagePlus className="h-5 w-5" />
          Выбрать {accept === "video" ? "видео" : "изображение"} из медиатеки
        </button>
      )}
    </div>
  );
}

function BlockContent({
  block,
  disabled,
  update,
  openMedia,
}: {
  block: ContentBlock;
  disabled?: boolean;
  update: (block: ContentBlock, coalesce?: boolean) => void;
  openMedia: (block: ContentBlock) => void;
}) {
  switch (block.type) {
    case "paragraph":
      return (
        <Textarea
          value={block.data.text}
          disabled={disabled}
          rows={3}
          placeholder="Введите текст параграфа…"
          aria-label="Текст параграфа"
          onChange={(event) =>
            update({ ...block, data: { text: event.target.value } }, true)
          }
          className="resize-y border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
        />
      );
    case "heading":
      return (
        <div className="flex flex-col gap-2 sm:flex-row">
          <Select
            value={String(block.data.level)}
            onValueChange={(value) =>
              update({
                ...block,
                data: { ...block.data, level: Number(value) as 1 | 2 | 3 | 4 },
              })
            }
            disabled={disabled}
          >
            <SelectTrigger className="h-8 w-24 shrink-0" aria-label="Уровень заголовка">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[1, 2, 3, 4].map((level) => (
                <SelectItem key={level} value={String(level)}>
                  H{level}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            value={block.data.text}
            disabled={disabled}
            placeholder="Заголовок"
            aria-label="Текст заголовка"
            onChange={(event) =>
              update({ ...block, data: { ...block.data, text: event.target.value } }, true)
            }
            className={cn(
              "border-0 bg-transparent px-0 font-semibold shadow-none focus-visible:ring-0",
              block.data.level === 1 ? "text-xl" : block.data.level === 2 ? "text-lg" : "text-base",
            )}
          />
        </div>
      );
    case "bulletedList":
    case "numberedList":
      return (
        <div className="space-y-2">
          {block.data.items.map((item, index) => (
            <div key={index} className="flex items-center gap-2">
              <span className="w-5 shrink-0 text-right text-xs text-muted-foreground">
                {block.type === "numberedList" ? `${index + 1}.` : "•"}
              </span>
              <Input
                value={item}
                disabled={disabled}
                aria-label={`Пункт ${index + 1}`}
                placeholder="Пункт списка"
                onChange={(event) => {
                  const items = [...block.data.items];
                  items[index] = event.target.value;
                  update({ ...block, data: { items } }, true);
                }}
                className="h-8"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={disabled || block.data.items.length === 1}
                onClick={() =>
                  update({
                    ...block,
                    data: { items: block.data.items.filter((_, itemIndex) => itemIndex !== index) },
                  })
                }
                aria-label="Удалить пункт"
              >
                <X />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() => update({ ...block, data: { items: [...block.data.items, ""] } })}
          >
            <Plus />
            Пункт
          </Button>
        </div>
      );
    case "quote":
      return (
        <div className="space-y-2 border-l-2 border-primary/40 pl-3">
          <Textarea
            value={block.data.text}
            disabled={disabled}
            rows={2}
            placeholder="Текст цитаты"
            aria-label="Текст цитаты"
            onChange={(event) =>
              update({ ...block, data: { ...block.data, text: event.target.value } }, true)
            }
            className="border-0 bg-transparent px-0 italic shadow-none focus-visible:ring-0"
          />
          <Input
            value={block.data.attribution ?? ""}
            disabled={disabled}
            placeholder="Автор (необязательно)"
            aria-label="Автор цитаты"
            onChange={(event) =>
              update(
                { ...block, data: { ...block.data, attribution: event.target.value } },
                true,
              )
            }
            className="h-8 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0"
          />
        </div>
      );
    case "code":
      return (
        <div className="space-y-2">
          <Input
            value={block.data.language ?? ""}
            disabled={disabled}
            placeholder="Язык, например ts"
            aria-label="Язык кода"
            onChange={(event) =>
              update(
                { ...block, data: { ...block.data, language: event.target.value } },
                true,
              )
            }
            className="h-8 w-48 font-mono text-xs"
          />
          <Textarea
            value={block.data.code}
            disabled={disabled}
            rows={6}
            spellCheck={false}
            placeholder="Код"
            aria-label="Код"
            onChange={(event) =>
              update({ ...block, data: { ...block.data, code: event.target.value } }, true)
            }
            className="bg-muted/40 font-mono text-xs"
          />
        </div>
      );
    case "divider":
      return (
        <div className="flex items-center gap-3 py-3 text-2xs text-muted-foreground">
          <span className="h-px flex-1 bg-border" />
          Разделитель
          <span className="h-px flex-1 bg-border" />
        </div>
      );
    case "image":
      return (
        <div className="space-y-3">
          <MediaField
            mediaId={block.data.mediaId}
            onPick={() => openMedia(block)}
            onClear={() => update({ ...block, data: { ...block.data, mediaId: "" } })}
            label="Изображение"
            accept="image"
            kind="image"
            disabled={disabled}
          />
          <div className="grid gap-2 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor={`${block.id}-alt`} className="text-xs text-muted-foreground">
                Alt-текст
              </Label>
              <Input
                id={`${block.id}-alt`}
                value={block.data.alt}
                disabled={disabled}
                onChange={(event) =>
                  update({ ...block, data: { ...block.data, alt: event.target.value } }, true)
                }
                placeholder="Описание изображения"
                className="h-8"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={`${block.id}-caption`} className="text-xs text-muted-foreground">
                Подпись
              </Label>
              <Input
                id={`${block.id}-caption`}
                value={block.data.caption ?? ""}
                disabled={disabled}
                onChange={(event) =>
                  update(
                    { ...block, data: { ...block.data, caption: event.target.value } },
                    true,
                  )
                }
                placeholder="Необязательно"
                className="h-8"
              />
            </div>
          </div>
        </div>
      );
    case "gallery":
      return (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <Label className="text-xs text-muted-foreground">
              Галерея · {block.data.mediaIds.length} файл(ов)
            </Label>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => openMedia(block)}
              disabled={disabled}
            >
              <ImagePlus />
              Добавить изображения
            </Button>
          </div>
          {block.data.mediaIds.length === 0 ? (
            <p className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
              Изображения не выбраны
            </p>
          ) : (
            <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
              {block.data.mediaIds.map((id) => (
                <div key={id} className="group relative overflow-hidden rounded-md border border-border">
                  <img src={publicMediaPath(id)} alt="" className="aspect-square w-full object-cover" />
                  <button
                    type="button"
                    className="absolute right-1 top-1 rounded-sm bg-background/90 p-1 text-destructive opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                    onClick={() =>
                      update({
                        ...block,
                        data: {
                          ...block.data,
                          mediaIds: block.data.mediaIds.filter((mediaId) => mediaId !== id),
                        },
                      })
                    }
                    disabled={disabled}
                    aria-label="Удалить изображение из галереи"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </div>
          )}
          <Input
            value={block.data.caption ?? ""}
            disabled={disabled}
            placeholder="Подпись галереи (необязательно)"
            aria-label="Подпись галереи"
            onChange={(event) =>
              update({ ...block, data: { ...block.data, caption: event.target.value } }, true)
            }
            className="h-8"
          />
        </div>
      );
    case "video":
      return (
        <div className="space-y-3">
          <MediaField
            mediaId={block.data.mediaId}
            onPick={() => openMedia(block)}
            onClear={() => update({ ...block, data: { ...block.data, mediaId: "" } })}
            label="Видео (MP4 из медиатеки)"
            accept="video"
            kind="video"
            disabled={disabled}
          />
          <Input
            value={block.data.caption ?? ""}
            disabled={disabled}
            placeholder="Подпись (необязательно)"
            aria-label="Подпись видео"
            onChange={(event) =>
              update({ ...block, data: { ...block.data, caption: event.target.value } }, true)
            }
            className="h-8"
          />
        </div>
      );
    default:
      return null;
  }
}

function textOf(block: ContentBlock): string {
  if (
    block.type === "paragraph" ||
    block.type === "heading" ||
    block.type === "quote"
  ) {
    return block.data.text;
  }
  if (block.type === "code") return block.data.code;
  return "";
}

function BlockCard({
  block,
  index,
  total,
  disabled,
  onUpdate,
  onDuplicate,
  onDelete,
  onMove,
  onConvert,
  openMedia,
}: {
  block: ContentBlock;
  index: number;
  total: number;
  disabled?: boolean;
  onUpdate: (block: ContentBlock, coalesce?: boolean) => void;
  onDuplicate: () => void;
  onDelete: () => void;
  onMove: (direction: -1 | 1) => void;
  onConvert: (type: BlockType) => void;
  openMedia: (block: ContentBlock) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: block.id, disabled });
  const Icon = blockIcons[block.type];
  const canConvertText = TEXT_BLOCK_TYPES.includes(block.type);

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn("block-shell", isDragging && "opacity-60")}
      data-dragging={isDragging}
    >
      <div className="flex items-center gap-1.5 border-b border-border/70 px-2 py-1.5">
        <button
          type="button"
          className="cursor-grab touch-none rounded-sm p-1 text-muted-foreground/60 hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
          aria-label={`Переместить блок ${blockLabel(block.type)}`}
          disabled={disabled}
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-4 w-4" />
        </button>
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-xs font-medium text-muted-foreground">
          {blockLabel(block.type)}
        </span>
        <span className="hidden truncate text-2xs text-muted-foreground/70 sm:inline">
          {blockPreviewText(block) || "пусто"}
        </span>
        <div className="ml-auto flex items-center gap-0.5">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => onMove(-1)}
            disabled={disabled || index === 0}
            aria-label="Переместить блок выше"
          >
            <ArrowUp />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => onMove(1)}
            disabled={disabled || index === total - 1}
            aria-label="Переместить блок ниже"
          >
            <ArrowDown />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Действия с блоком"
                disabled={disabled}
              >
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuItem onSelect={onDuplicate}>
                <Copy />
                Дублировать
              </DropdownMenuItem>
              {canConvertText && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel>Преобразовать</DropdownMenuLabel>
                  {block.type !== "paragraph" && (
                    <DropdownMenuItem onSelect={() => onConvert("paragraph")}>
                      В параграф
                    </DropdownMenuItem>
                  )}
                  {block.type !== "heading" && (
                    <DropdownMenuItem onSelect={() => onConvert("heading")}>
                      В заголовок
                    </DropdownMenuItem>
                  )}
                  {block.type !== "quote" && (
                    <DropdownMenuItem onSelect={() => onConvert("quote")}>
                      В цитату
                    </DropdownMenuItem>
                  )}
                </>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="text-destructive focus:text-destructive"
                onSelect={onDelete}
              >
                <Trash2 />
                Удалить блок
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      <div className="p-3 sm:p-4">
        <BlockContent block={block} disabled={disabled} update={onUpdate} openMedia={openMedia} />
      </div>
    </div>
  );
}

export function BlockEditor({
  blocks,
  onChange,
  allowedTypes,
  disabled,
}: {
  blocks: ContentBlock[];
  onChange: (blocks: ContentBlock[], coalesce?: boolean) => void;
  allowedTypes: BlockType[];
  disabled?: boolean;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const [activeId, setActiveId] = useState<string | null>(null);
  const [picker, setPicker] = useState<{
    blockId: string;
    accept: "image" | "video";
    multiple: boolean;
    title: string;
    initialSelected: string[];
  } | null>(null);

  const availableTypes = useMemo(
    () => (allowedTypes.length > 0 ? allowedTypes : BLOCK_OPTIONS.map((option) => option.type)),
    [allowedTypes],
  );

  const addBlock = (type: BlockType, index: number) => {
    const block = createBlock(type);
    const next = [...blocks];
    next.splice(index, 0, block);
    onChange(next);
  };

  const updateBlock = (id: string, block: ContentBlock, coalesce = false) => {
    onChange(blocks.map((item) => (item.id === id ? block : item)), coalesce);
  };

  const moveBlock = (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= blocks.length) return;
    onChange(arrayMove(blocks, index, target));
  };

  const duplicate = (index: number) => {
    const block = blocks[index];
    if (!block) return;
    const next = [...blocks];
    next.splice(index + 1, 0, duplicateBlock(block));
    onChange(next);
  };

  const remove = (block: ContentBlock) => {
    onChange(blocks.filter((item) => item.id !== block.id));
  };

  const convert = (block: ContentBlock, type: BlockType) => {
    if (!TEXT_BLOCK_TYPES.includes(block.type)) return;
    const text = textOf(block);
    let next: ContentBlock;
    if (type === "heading") {
      next = { id: block.id, type: "heading", data: { text, level: 2 } };
    } else if (type === "quote") {
      next = { id: block.id, type: "quote", data: { text, attribution: "" } };
    } else {
      next = { id: block.id, type: "paragraph", data: { text } };
    }
    updateBlock(block.id, next);
  };

  const openMedia = (block: ContentBlock) => {
    if (block.type === "image") {
      setPicker({
        blockId: block.id,
        accept: "image",
        multiple: false,
        title: "Выбрать изображение",
        initialSelected: block.data.mediaId ? [block.data.mediaId] : [],
      });
    } else if (block.type === "gallery") {
      setPicker({
        blockId: block.id,
        accept: "image",
        multiple: true,
        title: "Выбрать изображения для галереи",
        initialSelected: block.data.mediaIds,
      });
    } else if (block.type === "video") {
      setPicker({
        blockId: block.id,
        accept: "video",
        multiple: false,
        title: "Выбрать видео",
        initialSelected: block.data.mediaId ? [block.data.mediaId] : [],
      });
    }
  };

  const handleMediaSelected = (media: Media[]) => {
    if (!picker) return;
    const ids = media.map((item) => item.id);
    const block = blocks.find((item) => item.id === picker.blockId);
    if (!block) return;
    if (block.type === "image") {
      const first = ids[0] ?? "";
      updateBlock(block.id, {
        ...block,
        data: { ...block.data, mediaId: first, alt: block.data.alt || media[0]?.original_filename || "" },
      });
    } else if (block.type === "gallery") {
      updateBlock(block.id, {
        ...block,
        data: { ...block.data, mediaIds: Array.from(new Set([...block.data.mediaIds, ...ids])) },
      });
    } else if (block.type === "video") {
      updateBlock(block.id, {
        ...block,
        data: { ...block.data, mediaId: ids[0] ?? "" },
      });
    }
    setPicker(null);
  };

  const handleDragStart = (event: DragStartEvent) => setActiveId(String(event.active.id));
  const handleDragEnd = (event: DragEndEvent) => {
    setActiveId(null);
    if (!event.over || event.active.id === event.over.id) return;
    const oldIndex = blocks.findIndex((block) => block.id === event.active.id);
    const newIndex = blocks.findIndex((block) => block.id === event.over?.id);
    if (oldIndex < 0 || newIndex < 0) return;
    onChange(arrayMove(blocks, oldIndex, newIndex));
  };

  const activeBlock = activeId ? blocks.find((block) => block.id === activeId) : undefined;

  if (blocks.length === 0) {
    return (
      <div>
        <EmptyState
          icon={Pilcrow}
          title="В документе пока нет блоков"
          description="Добавьте параграф, заголовок, изображение или другой блок. Порядок можно менять перетаскиванием и кнопками."
          action={
            <AddBlockMenu
              onAdd={(type) => addBlock(type, 0)}
              types={availableTypes}
              disabled={disabled}
            />
          }
          className="bg-card"
        />
      </div>
    );
  }

  return (
    <div>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={() => setActiveId(null)}
      >
        <SortableContext items={blocks.map((block) => block.id)} strategy={verticalListSortingStrategy}>
          <div className="space-y-2">
            {blocks.map((block, index) => (
              <div key={block.id}>
                {index === 0 && (
                  <InlineAdd
                    onAdd={(type) => addBlock(type, 0)}
                    types={availableTypes}
                    disabled={disabled}
                  />
                )}
                <BlockCard
                  block={block}
                  index={index}
                  total={blocks.length}
                  disabled={disabled}
                  onUpdate={(updated, coalesce) => updateBlock(block.id, updated, coalesce)}
                  onDuplicate={() => duplicate(index)}
                  onDelete={() => remove(block)}
                  onMove={(direction) => moveBlock(index, direction)}
                  onConvert={(type) => convert(block, type)}
                  openMedia={openMedia}
                />
                <InlineAdd
                  onAdd={(type) => addBlock(type, index + 1)}
                  types={availableTypes}
                  disabled={disabled}
                />
              </div>
            ))}
          </div>
        </SortableContext>
        <DragOverlay>
          {activeBlock ? (
            <div className="rounded-md border border-primary/40 bg-card px-3 py-2 text-sm shadow-md">
              {blockLabel(activeBlock.type)}
            </div>
          ) : null}
        </DragOverlay>
      </DndContext>

      <div className="mt-2 flex items-center justify-between gap-2 border-t border-border pt-3">
        <p className="text-2xs text-muted-foreground">
          {blocks.length} блок(ов) · порядок меняется drag-and-drop или кнопками ↑↓
        </p>
        <AddBlockMenu
          onAdd={(type) => addBlock(type, blocks.length)}
          types={availableTypes}
          disabled={disabled}
        />
      </div>

      <MediaPickerDialog
        open={picker !== null}
        onClose={() => setPicker(null)}
        onSelect={handleMediaSelected}
        mode={picker?.multiple ? "multiple" : "single"}
        accept={picker?.accept ?? "image"}
        initialSelected={picker?.initialSelected ?? []}
        title={picker?.title ?? "Выбрать медиа"}
      />
    </div>
  );
}
