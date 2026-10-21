import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Braces, ImagePlus, Lock } from "lucide-react";
import type { JsonObject, JsonValue } from "@/types";
import type { SchemaFieldNode } from "@/domains/schema-builder/types";
import { publicMediaPath } from "@/api";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/misc";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

function isoToLocalInput(value: string | undefined): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (input: number) => String(input).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function JsonFieldDialog({
  field,
  value,
  open,
  onClose,
  onApply,
}: {
  field: SchemaFieldNode;
  value: JsonValue | undefined;
  open: boolean;
  onClose: () => void;
  onApply: (value: JsonValue) => void;
}) {
  const initial = useMemo(
    () => JSON.stringify(value ?? (field.type === "array" ? [] : {}), null, 2),
    [field.type, value],
  );
  const [text, setText] = useState(initial);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setText(initial);
      setError(null);
    }
  }, [open, initial]);

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>JSON поля «{field.label || field.key}»</DialogTitle>
          <DialogDescription>
            Для {field.type === "array" ? "массива" : "объекта"} используется
            структурированный JSON. После применения локальные изменения попадут в
            документ и будут сохранены через autosave.
          </DialogDescription>
        </DialogHeader>
        <textarea
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setError(null);
          }}
          rows={14}
          spellCheck={false}
          className="w-full rounded-md border border-input bg-muted/30 p-3 font-mono text-xs leading-6 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={`JSON поля ${field.label}`}
        />
        {error && <p className="text-xs text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Отмена
          </Button>
          <Button
            onClick={() => {
              try {
                const parsed: unknown = JSON.parse(text);
                onApply(parsed as JsonValue);
                onClose();
              } catch (parseError) {
                setError(
                  parseError instanceof Error
                    ? `Некорректный JSON: ${parseError.message}`
                    : "Некорректный JSON",
                );
              }
            }}
          >
            Применить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MediaValue({
  mediaId,
  disabled,
  onPick,
  onClear,
}: {
  mediaId: string;
  disabled?: boolean;
  onPick: () => void;
  onClear: () => void;
}) {
  return (
    <div className="space-y-2">
      {mediaId ? (
        <div className="overflow-hidden rounded-md border border-border">
          <img
            src={publicMediaPath(mediaId)}
            alt=""
            className="max-h-44 w-full object-cover"
          />
          <div className="flex items-center justify-between gap-2 border-t border-border bg-muted/30 px-2 py-1.5">
            <code className="truncate text-2xs text-muted-foreground">{mediaId}</code>
            <div className="flex gap-1">
              <Button type="button" variant="ghost" size="sm" onClick={onPick} disabled={disabled}>
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
          className="flex w-full items-center justify-center gap-2 rounded-md border border-dashed border-border px-3 py-5 text-xs text-muted-foreground hover:border-primary/40 hover:bg-muted/30 disabled:opacity-50"
        >
          <ImagePlus className="h-4 w-4" />
          Выбрать из медиатеки
        </button>
      )}
    </div>
  );
}

export function MetadataEditor({
  fields,
  metadata,
  onChange,
  onPickMedia,
  disabled,
  blocksKey,
}: {
  fields: SchemaFieldNode[];
  metadata: JsonObject;
  onChange: (key: string, value: JsonValue) => void;
  onPickMedia: (field: SchemaFieldNode) => void;
  disabled?: boolean;
  blocksKey: string | null;
}) {
  const editableFields = fields.filter(
    (field) => field.type !== "blocks" && field.key !== blocksKey && field.key.trim(),
  );
  const unknownKeys = Object.keys(metadata).filter(
    (key) =>
      key !== blocksKey &&
      !fields.some((field) => field.key === key) &&
      metadata[key] !== undefined,
  );
  const [jsonField, setJsonField] = useState<SchemaFieldNode | null>(null);

  if (editableFields.length === 0) {
    return (
      <p className="rounded-md border border-dashed border-border px-3 py-4 text-xs leading-5 text-muted-foreground">
        В схеме нет дополнительных полей для формы метаданных. Основной контент
        редактируется блоками.
      </p>
    );
  }

  return (
    <div className="space-y-4">
      {editableFields.map((field) => {
        const id = `meta-${field.id}`;
        const value = metadata[field.key];
        const common = (
          <div className="flex items-center justify-between gap-2">
            <Label htmlFor={id} className="text-xs">
              {field.label || field.key}
              {field.required && <span className="ml-0.5 text-destructive">*</span>}
            </Label>
            {field.isPrivate && (
              <Badge variant="muted" title="x-private: исключается из публичных данных">
                <Lock />
                private
              </Badge>
            )}
          </div>
        );
        let control: ReactNode;
        switch (field.type) {
          case "text":
          case "richText":
            control = (
              <Textarea
                id={id}
                rows={field.type === "richText" ? 6 : 4}
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
                placeholder={field.placeholder}
              />
            );
            break;
          case "number":
          case "integer":
            control = (
              <Input
                id={id}
                type="number"
                step={field.type === "integer" ? 1 : field.step ?? "any"}
                min={field.min}
                max={field.max}
                value={typeof value === "number" ? value : ""}
                disabled={disabled}
                onChange={(event) => {
                  const raw = event.target.value;
                  if (raw === "") {
                    onChange(field.key, null);
                    return;
                  }
                  const parsed = Number(raw);
                  onChange(
                    field.key,
                    field.type === "integer" ? Math.trunc(parsed) : parsed,
                  );
                }}
              />
            );
            break;
          case "boolean":
            control = (
              <div className="flex items-center gap-2">
                <Switch
                  id={id}
                  checked={value === true}
                  disabled={disabled}
                  onCheckedChange={(checked) => onChange(field.key, checked)}
                />
                <span className="text-xs text-muted-foreground">
                  {value === true ? "true" : "false"}
                </span>
              </div>
            );
            break;
          case "date":
            control = (
              <Input
                id={id}
                type="date"
                value={typeof value === "string" ? value.slice(0, 10) : ""}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
              />
            );
            break;
          case "dateTime":
            control = (
              <Input
                id={id}
                type="datetime-local"
                value={isoToLocalInput(typeof value === "string" ? value : "")}
                disabled={disabled}
                onChange={(event) => {
                  if (!event.target.value) {
                    onChange(field.key, "");
                    return;
                  }
                  const date = new Date(event.target.value);
                  onChange(
                    field.key,
                    Number.isNaN(date.getTime()) ? event.target.value : date.toISOString(),
                  );
                }}
              />
            );
            break;
          case "enum":
            control = (
              <Select
                value={typeof value === "string" ? value : ""}
                onValueChange={(next) => onChange(field.key, next)}
                disabled={disabled}
              >
                <SelectTrigger id={id}>
                  <SelectValue placeholder="Выберите значение" />
                </SelectTrigger>
                <SelectContent>
                  {field.enumValues
                    .filter((option) => option.value.trim())
                    .map((option) => (
                      <SelectItem key={option.id} value={option.value}>
                        {option.label || option.value}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            );
            break;
          case "media":
            control = (
              <MediaValue
                mediaId={typeof value === "string" ? value : ""}
                disabled={disabled}
                onPick={() => onPickMedia(field)}
                onClear={() => onChange(field.key, "")}
              />
            );
            break;
          case "object":
          case "array":
            control = (
              <div className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/20 px-3 py-2">
                <span className="truncate font-mono text-2xs text-muted-foreground">
                  {value === undefined
                    ? "не задано"
                    : JSON.stringify(value).slice(0, 90)}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setJsonField(field)}
                  disabled={disabled}
                >
                  <Braces />
                  JSON
                </Button>
              </div>
            );
            break;
          case "string":
          default:
            control = (
              <Input
                id={id}
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                onChange={(event) => onChange(field.key, event.target.value)}
                placeholder={field.placeholder}
              />
            );
        }

        return (
          <div key={field.id} className="space-y-1.5">
            {common}
            {control}
            {field.description && (
              <p className="text-2xs leading-5 text-muted-foreground">
                {field.description}
              </p>
            )}
          </div>
        );
      })}

      {unknownKeys.length > 0 && (
        <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
          <p className="text-2xs font-medium text-muted-foreground">
            Поля вне текущей схемы сохранены в данных
          </p>
          <p className="mt-1 break-words font-mono text-2xs text-muted-foreground">
            {unknownKeys.join(", ")}
          </p>
          <p className="mt-1 text-2xs leading-5 text-muted-foreground">
            Редактор не удаляет их при сохранении. Значения можно посмотреть в JSON
            документа.
          </p>
        </div>
      )}

      {jsonField && (
        <JsonFieldDialog
          field={jsonField}
          value={metadata[jsonField.key]}
          open
          onClose={() => setJsonField(null)}
          onApply={(next) => onChange(jsonField.key, next)}
        />
      )}
    </div>
  );
}

