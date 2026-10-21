import { useConfirm } from "@/components/common/confirm";
import { InlineAlert } from "@/components/common/page";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea } from "@/components/ui/input";
import { Switch } from "@/components/ui/misc";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { BLOCK_TYPES, createEnumOption } from "./schema";
import { ARRAY_ITEM_TYPES, FIELD_TYPES, fieldTypeMeta } from "./field-meta";
import type {
  ArrayItemType,
  BuilderFieldType,
  BuilderIssue,
  SchemaFieldNode,
} from "./types";

function NumberInput({
  id,
  label,
  value,
  onChange,
  min = 0,
  step = 1,
  placeholder,
}: {
  id: string;
  label: string;
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  min?: number;
  step?: number | "any";
  placeholder?: string;
}) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        type="number"
        min={min}
        step={step}
        value={value ?? ""}
        placeholder={placeholder}
        onChange={(event) => {
          const raw = event.target.value;
          if (raw === "") {
            onChange(undefined);
            return;
          }
          const parsed = Number(raw);
          onChange(Number.isFinite(parsed) ? parsed : undefined);
        }}
      />
    </div>
  );
}

function ToggleRow({
  id,
  label,
  hint,
  checked,
  onCheckedChange,
}: {
  id: string;
  label: string;
  hint?: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-md border border-border px-3 py-2.5">
      <div className="min-w-0">
        <Label htmlFor={id} className="cursor-pointer text-sm">
          {label}
        </Label>
        {hint && <p className="mt-0.5 text-2xs leading-5 text-muted-foreground">{hint}</p>}
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function EnumEditor({
  field,
  onChange,
  prefix,
}: {
  field: SchemaFieldNode;
  onChange: (values: SchemaFieldNode["enumValues"]) => void;
  prefix: string;
}) {
  const update = (index: number, patch: Partial<SchemaFieldNode["enumValues"][number]>) => {
    onChange(
      field.enumValues.map((option, optionIndex) =>
        optionIndex === index ? { ...option, ...patch } : option,
      ),
    );
  };
  const move = (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= field.enumValues.length) return;
    const next = [...field.enumValues];
    const current = next[index];
    const other = next[target];
    if (!current || !other) return;
    next[index] = other;
    next[target] = current;
    onChange(next);
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label className="text-xs text-muted-foreground">Значения enum</Label>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onChange([...field.enumValues, createEnumOption()])}
        >
          <Plus />
          Значение
        </Button>
      </div>
      {field.enumValues.length === 0 && (
        <p className="text-2xs text-muted-foreground">Добавьте хотя бы одно значение.</p>
      )}
      <div className="space-y-2">
        {field.enumValues.map((option, index) => (
          <div key={option.id} className="flex items-start gap-1.5">
            <Input
              aria-label={`Значение ${prefix} ${index + 1}`}
              value={option.value}
              placeholder="value"
              className="h-8 font-mono text-xs"
              onChange={(event) => update(index, { value: event.target.value })}
            />
            <Input
              aria-label={`Подпись ${prefix} ${index + 1}`}
              value={option.label}
              placeholder="Подпись"
              className="h-8"
              onChange={(event) => update(index, { label: event.target.value })}
            />
            <div className="flex shrink-0">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => move(index, -1)}
                disabled={index === 0}
                aria-label="Выше"
              >
                <ArrowUp />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => move(index, 1)}
                disabled={index === field.enumValues.length - 1}
                aria-label="Ниже"
              >
                <ArrowDown />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => onChange(field.enumValues.filter((item) => item.id !== option.id))}
                aria-label="Удалить значение"
                className="text-destructive hover:text-destructive"
              >
                <Trash2 />
              </Button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function FieldProperties({
  field,
  issues,
  onUpdate,
  onAddChild,
  disabled,
}: {
  field: SchemaFieldNode | null;
  issues: BuilderIssue[];
  onUpdate: (field: SchemaFieldNode) => void;
  onAddChild: (parentId: string) => void;
  disabled?: boolean;
}) {
  const confirm = useConfirm();
  if (!field) {
    return (
      <div className="rounded-lg border border-dashed border-border bg-card/40 p-6">
        <h3 className="text-sm font-semibold">Свойства поля</h3>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Выберите поле в дереве слева, чтобы настроить его название, ключ, тип,
          обязательность и ограничения. Если ничего не выбрано, добавьте первое
          поле.
        </p>
        <div className="mt-4 rounded-md border border-border bg-muted/30 px-3 py-2 text-2xs leading-5 text-muted-foreground">
          Схема генерируется автоматически в JSON Schema 2020-12 с расширениями{" "}
          <code>x-ui</code>, <code>x-private</code> и <code>format: media</code>.
        </div>
      </div>
    );
  }

  const meta = fieldTypeMeta(field.type);
  const Icon = meta.icon;
  const fieldIssues = issues.filter((issue) => issue.fieldId === field.id);
  const errors = fieldIssues.filter((issue) => issue.severity === "error");
  const warnings = fieldIssues.filter((issue) => issue.severity === "warning");
  const supportsChildren =
    field.type === "object" || (field.type === "array" && field.itemType === "object");
  const idPrefix = field.id;

  const changeType = async (nextType: BuilderFieldType) => {
    const nextSupportsChildren =
      nextType === "object" ||
      (nextType === "array" && field.itemType === "object");
    if (field.children.length > 0 && !nextSupportsChildren) {
      const confirmed = await confirm({
        title: "Сменить тип поля?",
        description:
          "У текущего поля есть дочерние поля. При смене на тип без вложенности они будут удалены из конструктора.",
        confirmLabel: "Сменить тип",
        destructive: true,
      });
      if (!confirmed) return;
    }
    onUpdate({
      ...field,
      type: nextType,
      children:
        field.children.length > 0 && !nextSupportsChildren ? [] : field.children,
      enumValues:
        (nextType === "enum" ||
          (nextType === "array" && field.itemType === "enum")) &&
        field.enumValues.length === 0
          ? [createEnumOption()]
          : field.enumValues,
    });
  };

  const changeItemType = async (nextItemType: ArrayItemType) => {
    if (field.children.length > 0 && nextItemType !== "object") {
      const confirmed = await confirm({
        title: "Сменить тип элемента массива?",
        description: "Дочерние поля объекта элемента будут удалены из конструктора.",
        confirmLabel: "Сменить тип",
        destructive: true,
      });
      if (!confirmed) return;
    }
    onUpdate({
      ...field,
      itemType: nextItemType,
      children: nextItemType === "object" ? field.children : [],
      enumValues:
        nextItemType === "enum" && field.enumValues.length === 0
          ? [createEnumOption()]
          : field.enumValues,
    });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40 text-muted-foreground">
            <Icon className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <h3 className="truncate text-sm font-semibold">
              {field.label || field.key || "Новое поле"}
            </h3>
            <p className="text-2xs text-muted-foreground">
              {meta.label}
              {field.required ? " · обязательное" : ""}
            </p>
          </div>
        </div>
        <Badge variant={errors.length > 0 ? "destructive" : "muted"}>
          {errors.length > 0 ? `${errors.length} ошиб.` : "JSON Schema"}
        </Badge>
      </div>

      {errors.length > 0 && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2">
          <ul className="space-y-1 text-xs text-destructive">
            {errors.map((issue, index) => (
              <li key={`${issue.path}-${index}`}>{issue.message}</li>
            ))}
          </ul>
        </div>
      )}
      {warnings.length > 0 && (
        <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2">
          <ul className="space-y-1 text-xs text-warning">
            {warnings.map((issue, index) => (
              <li key={`${issue.path}-${index}`}>{issue.message}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-label`} className="text-xs text-muted-foreground">
          Название
        </Label>
        <Input
          id={`${idPrefix}-label`}
          value={field.label}
          disabled={disabled}
          onChange={(event) => onUpdate({ ...field, label: event.target.value })}
          placeholder="Например, Заголовок"
        />
      </div>

      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-key`} className="text-xs text-muted-foreground">
          Ключ <span className="text-destructive">*</span>
        </Label>
        <Input
          id={`${idPrefix}-key`}
          value={field.key}
          disabled={disabled}
          onChange={(event) => onUpdate({ ...field, key: event.target.value })}
          placeholder="title"
          className="font-mono text-xs"
          aria-invalid={errors.some((issue) => issue.message.includes("ключ"))}
        />
        <p className="text-2xs text-muted-foreground">
          Используется как ключ в JSON. Латиница, цифры и _.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-type`} className="text-xs text-muted-foreground">
          Тип и widget
        </Label>
        <Select
          value={field.type}
          onValueChange={(value) => void changeType(value as BuilderFieldType)}
          disabled={disabled}
        >
          <SelectTrigger id={`${idPrefix}-type`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FIELD_TYPES.map((type) => (
              <SelectItem key={type.value} value={type.value}>
                {type.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-2xs text-muted-foreground">{meta.hint}</p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-description`} className="text-xs text-muted-foreground">
          Описание
        </Label>
        <Textarea
          id={`${idPrefix}-description`}
          value={field.description}
          disabled={disabled}
          rows={2}
          onChange={(event) => onUpdate({ ...field, description: event.target.value })}
          placeholder="Подсказка для редактора контента"
        />
      </div>

      <div className="space-y-2">
        <ToggleRow
          id={`${idPrefix}-required`}
          label="Обязательное"
          hint="Добавляется в required родительского объекта"
          checked={field.required}
          onCheckedChange={(value) => onUpdate({ ...field, required: value })}
        />
        <ToggleRow
          id={`${idPrefix}-nullable`}
          label="Nullable"
          hint="Разрешает значение null в JSON Schema"
          checked={field.nullable}
          onCheckedChange={(value) => onUpdate({ ...field, nullable: value })}
        />
        <ToggleRow
          id={`${idPrefix}-private`}
          label="Приватное (x-private)"
          hint="Исключается из публичных данных backend-проекцией"
          checked={field.isPrivate}
          onCheckedChange={(value) => onUpdate({ ...field, isPrivate: value })}
        />
      </div>

      {(field.type === "string" ||
        field.type === "text" ||
        field.type === "richText" ||
        field.type === "media" ||
        field.type === "date" ||
        field.type === "dateTime") && (
        <div className="space-y-3 rounded-md border border-border p-3">
          <div className="space-y-1.5">
            <Label htmlFor={`${idPrefix}-placeholder`} className="text-xs text-muted-foreground">
              Плейсхолдер (x-ui)
            </Label>
            <Input
              id={`${idPrefix}-placeholder`}
              value={field.placeholder}
              disabled={disabled}
              onChange={(event) => onUpdate({ ...field, placeholder: event.target.value })}
              placeholder="Введите значение…"
            />
          </div>
          {(field.type === "string" ||
            field.type === "text" ||
            field.type === "richText") && (
            <div className="grid grid-cols-2 gap-2">
              <NumberInput
                id={`${idPrefix}-minlength`}
                label="Мин. длина"
                value={field.minLength}
                onChange={(value) => onUpdate({ ...field, minLength: value })}
              />
              <NumberInput
                id={`${idPrefix}-maxlength`}
                label="Макс. длина"
                value={field.maxLength}
                onChange={(value) => onUpdate({ ...field, maxLength: value })}
              />
            </div>
          )}
          <div className="space-y-1.5">
            <Label htmlFor={`${idPrefix}-default`} className="text-xs text-muted-foreground">
              Значение по умолчанию
            </Label>
            <Input
              id={`${idPrefix}-default`}
              value={field.defaultValue}
              disabled={disabled}
              onChange={(event) => onUpdate({ ...field, defaultValue: event.target.value })}
            />
          </div>
        </div>
      )}

      {(field.type === "number" || field.type === "integer") && (
        <div className="space-y-3 rounded-md border border-border p-3">
          <div className="grid grid-cols-2 gap-2">
            <NumberInput
              id={`${idPrefix}-min`}
              label="Минимум"
              value={field.min}
              onChange={(value) => onUpdate({ ...field, min: value })}
              step="any"
            />
            <NumberInput
              id={`${idPrefix}-max`}
              label="Максимум"
              value={field.max}
              onChange={(value) => onUpdate({ ...field, max: value })}
              step="any"
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <NumberInput
              id={`${idPrefix}-step`}
              label="Шаг (x-ui)"
              value={field.step}
              onChange={(value) => onUpdate({ ...field, step: value })}
              step="any"
            />
            <div className="space-y-1.5">
              <Label htmlFor={`${idPrefix}-number-default`} className="text-xs text-muted-foreground">
                По умолчанию
              </Label>
              <Input
                id={`${idPrefix}-number-default`}
                type="number"
                step={field.type === "integer" ? 1 : "any"}
                value={field.defaultValue}
                disabled={disabled}
                onChange={(event) =>
                  onUpdate({ ...field, defaultValue: event.target.value })
                }
              />
            </div>
          </div>
        </div>
      )}

      {field.type === "boolean" && (
        <div className="space-y-1.5 rounded-md border border-border p-3">
          <Label htmlFor={`${idPrefix}-bool-default`} className="text-xs text-muted-foreground">
            Значение по умолчанию
          </Label>
          <Select
            value={field.defaultValue || "unset"}
            onValueChange={(value) =>
              onUpdate({ ...field, defaultValue: value === "unset" ? "" : value })
            }
            disabled={disabled}
          >
            <SelectTrigger id={`${idPrefix}-bool-default`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="unset">Не задано</SelectItem>
              <SelectItem value="true">true</SelectItem>
              <SelectItem value="false">false</SelectItem>
            </SelectContent>
          </Select>
        </div>
      )}

      {field.type === "enum" && (
        <div className="space-y-3 rounded-md border border-border p-3">
          <EnumEditor
            field={field}
            prefix={`enum поля ${field.label || field.key}`}
            onChange={(enumValues) => onUpdate({ ...field, enumValues })}
          />
          <div className="space-y-1.5">
            <Label htmlFor={`${idPrefix}-enum-default`} className="text-xs text-muted-foreground">
              Значение по умолчанию
            </Label>
            <Select
              value={field.defaultValue || "unset"}
              onValueChange={(value) =>
                onUpdate({ ...field, defaultValue: value === "unset" ? "" : value })
              }
              disabled={disabled}
            >
              <SelectTrigger id={`${idPrefix}-enum-default`}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="unset">Не задано</SelectItem>
                {field.enumValues
                  .filter((option) => option.value.trim())
                  .map((option) => (
                    <SelectItem key={option.id} value={option.value}>
                      {option.label || option.value}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      )}

      {field.type === "array" && (
        <div className="space-y-3 rounded-md border border-border p-3">
          <div className="space-y-1.5">
            <Label htmlFor={`${idPrefix}-item-type`} className="text-xs text-muted-foreground">
              Тип элемента
            </Label>
            <Select
              value={field.itemType}
              onValueChange={(value) => void changeItemType(value as ArrayItemType)}
              disabled={disabled}
            >
              <SelectTrigger id={`${idPrefix}-item-type`}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ARRAY_ITEM_TYPES.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {(field.itemType === "number" || field.itemType === "integer") && (
            <div className="grid grid-cols-2 gap-2">
              <NumberInput
                id={`${idPrefix}-item-min`}
                label="Мин. значение"
                value={field.min}
                onChange={(value) => onUpdate({ ...field, min: value })}
                step="any"
              />
              <NumberInput
                id={`${idPrefix}-item-max`}
                label="Макс. значение"
                value={field.max}
                onChange={(value) => onUpdate({ ...field, max: value })}
                step="any"
              />
            </div>
          )}
          <div className="grid grid-cols-2 gap-2">
            <NumberInput
              id={`${idPrefix}-minItems`}
              label="Мин. элементов"
              value={field.minItems}
              onChange={(value) => onUpdate({ ...field, minItems: value })}
            />
            <NumberInput
              id={`${idPrefix}-maxItems`}
              label="Макс. элементов"
              value={field.maxItems}
              onChange={(value) => onUpdate({ ...field, maxItems: value })}
            />
          </div>
          {field.itemType === "enum" && (
            <EnumEditor
              field={field}
              prefix={`элемента массива ${field.label || field.key}`}
              onChange={(enumValues) => onUpdate({ ...field, enumValues })}
            />
          )}
        </div>
      )}

      {field.type === "blocks" && (
        <div className="space-y-3 rounded-md border border-border p-3">
          <div>
            <Label className="text-xs text-muted-foreground">Разрешённые блоки</Label>
            <div className="mt-2 grid grid-cols-2 gap-2">
              {BLOCK_TYPES.map((block) => {
                const checked = field.allowedBlocks.includes(block.value);
                return (
                  <label
                    key={block.value}
                    className="flex cursor-pointer items-center gap-2 rounded-md border border-border px-2 py-1.5 text-xs hover:bg-accent/40"
                  >
                    <input
                      type="checkbox"
                      className="h-3.5 w-3.5 rounded border-input accent-primary"
                      checked={checked}
                      disabled={disabled}
                      onChange={(event) =>
                        onUpdate({
                          ...field,
                          allowedBlocks: event.target.checked
                            ? [...field.allowedBlocks, block.value]
                            : field.allowedBlocks.filter((value) => value !== block.value),
                        })
                      }
                    />
                    {block.label}
                  </label>
                );
              })}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <NumberInput
              id={`${idPrefix}-blocks-min`}
              label="Мин. блоков"
              value={field.minItems}
              onChange={(value) => onUpdate({ ...field, minItems: value })}
            />
            <NumberInput
              id={`${idPrefix}-blocks-max`}
              label="Макс. блоков"
              value={field.maxItems}
              onChange={(value) => onUpdate({ ...field, maxItems: value })}
            />
          </div>
        </div>
      )}

      {supportsChildren && (
        <div className="space-y-2 rounded-md border border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <div>
              <p className="text-xs font-medium">
                {field.type === "object" ? "Дочерние поля" : "Поля элемента массива"}
              </p>
              <p className="mt-0.5 text-2xs text-muted-foreground">
                {field.children.length} пол.
              </p>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              onClick={() => onAddChild(field.id)}
            >
              <Plus />
              Добавить
            </Button>
          </div>
          <p className="text-2xs leading-5 text-muted-foreground">
            Дочерние поля настраиваются в дереве слева. Выберите их, чтобы изменить
            свойства.
          </p>
        </div>
      )}

      {!supportsChildren && field.type !== "object" && field.children.length > 0 && (
        <InlineAlert tone="info">
          Дочерние поля сохранены, но не используются текущим типом. Вернитесь к
          объекту или массиву объектов, чтобы снова их увидеть.
        </InlineAlert>
      )}
    </div>
  );
}
