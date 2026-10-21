import type {
  FeatureType,
  JsonObject,
  JsonValue,
} from "../types";

export type ParseJsonResult =
  | { ok: true; value: JsonObject }
  | { ok: false; error: string };

export function isJsonObject(value: unknown): value is JsonObject {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value)
  );
}

function syntaxError(message: string, text: string): string {
  const positionMatch = /position\s+(\d+)/i.exec(message);
  if (!positionMatch) return "Некорректный JSON";
  const position = Math.min(Number(positionMatch[1]), text.length);
  const before = text.slice(0, position);
  const line = before.split("\n").length;
  const column = position - before.lastIndexOf("\n");
  return `Некорректный JSON: строка ${line}, столбец ${column}`;
}

export function parseJsonObject(text: string): ParseJsonResult {
  if (!text.trim()) return { ok: false, error: "JSON не заполнен" };
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    return {
      ok: false,
      error: syntaxError(
        error instanceof Error ? error.message : "Некорректный JSON",
        text,
      ),
    };
  }
  if (!isJsonObject(parsed)) {
    return { ok: false, error: "Корневым значением должен быть JSON-объект" };
  }
  return { ok: true, value: parsed };
}

export function formatJsonText(text: string): { text?: string; error?: string } {
  const parsed = parseJsonObject(text);
  if (!parsed.ok) return { error: parsed.error };
  return { text: JSON.stringify(parsed.value, null, 2) };
}

export function prettyJson(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? "";
}

export function starterSchema(type: FeatureType): JsonObject {
  if (type === "form") {
    return {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      title: "Новая форма",
      type: "object",
      properties: {
        name: {
          type: "string",
          title: "Имя",
          minLength: 1,
          maxLength: 120,
        },
        email: {
          type: "string",
          title: "Email",
          format: "email",
        },
        message: {
          type: "string",
          title: "Сообщение",
          minLength: 1,
          maxLength: 4000,
          "x-ui": { label: "Сообщение", widget: "textarea" },
        },
      },
      required: ["name", "email", "message"],
      additionalProperties: false,
    } satisfies JsonObject;
  }

  if (type === "single") {
    return {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      title: "Одиночная страница",
      type: "object",
      properties: {
        title: { type: "string", title: "Заголовок", minLength: 1 },
        description: {
          type: "string",
          title: "Описание",
          "x-ui": { widget: "textarea" },
        },
      },
      required: ["title"],
      additionalProperties: false,
    } satisfies JsonObject;
  }

  return {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    title: "Коллекция",
    type: "object",
    properties: {
      title: { type: "string", title: "Заголовок", minLength: 1 },
      description: {
        type: "string",
        title: "Описание",
        "x-ui": { widget: "textarea" },
      },
      cover: {
        type: "string",
        title: "Обложка",
        format: "media",
      },
    },
    required: ["title"],
    additionalProperties: false,
  } satisfies JsonObject;
}

function defaultValueForProperty(
  property: JsonObject,
  label: string,
): JsonValue {
  if (Array.isArray(property.enum) && property.enum.length > 0) {
    return property.enum[0] as JsonValue;
  }
  if (property.const !== undefined) return property.const as JsonValue;
  const type = property.type;
  if (type === "number" || type === "integer") return 0;
  if (type === "boolean") return false;
  if (type === "array") return [];
  if (type === "object") {
    const properties = isJsonObject(property.properties)
      ? property.properties
      : {};
    const result: JsonObject = {};
    for (const [key, raw] of Object.entries(properties)) {
      if (isJsonObject(raw)) {
        result[key] = defaultValueForProperty(raw, key);
      }
    }
    return result;
  }
  if (type === "null") return null;
  if (property.format === "email") return "user@example.com";
  if (property.format === "date") return new Date().toISOString().slice(0, 10);
  if (property.format === "date-time") return new Date().toISOString();
  if (property.format === "uri" || property.format === "url") {
    return "https://example.com";
  }
  if (property.format === "media") return "";
  return label ? `Текст: ${label}` : "Текст";
}

/** Builds a friendly editable data object from a top-level JSON Schema. */
export function starterData(schema: JsonObject | null | undefined): JsonObject {
  const properties = schema && isJsonObject(schema.properties) ? schema.properties : {};
  const result: JsonObject = {};
  for (const [key, raw] of Object.entries(properties)) {
    if (!isJsonObject(raw)) {
      result[key] = "";
      continue;
    }
    result[key] = defaultValueForProperty(raw, propertyTitle(raw) ?? key);
  }
  if (Object.keys(result).length === 0) {
    result.title = "";
  }
  return result;
}

export interface SchemaField {
  path: string;
  name: string;
  label: string;
  type: string;
  format?: string;
  description?: string;
  required: boolean;
  isPrivate: boolean;
}

export function propertyTitle(property: JsonObject): string | undefined {
  if (typeof property.title === "string" && property.title.trim()) {
    return property.title.trim();
  }
  const ui = property["x-ui"];
  if (isJsonObject(ui)) {
    if (typeof ui.label === "string" && ui.label.trim()) return ui.label.trim();
    if (typeof ui.title === "string" && ui.title.trim()) return ui.title.trim();
  }
  return undefined;
}

export function collectSchemaFields(
  schema: JsonObject | null | undefined,
  limit = 80,
): SchemaField[] {
  const fields: SchemaField[] = [];
  const rootProps =
    schema && isJsonObject(schema.properties) ? schema.properties : {};
  const rootRequired = new Set(
    Array.isArray(schema?.required)
      ? (schema?.required as unknown[]).filter(
          (item): item is string => typeof item === "string",
        )
      : [],
  );

  const walk = (
    properties: Record<string, unknown>,
    required: Set<string>,
    prefix: string,
    depth: number,
  ) => {
    if (fields.length >= limit) return;
    for (const [key, raw] of Object.entries(properties)) {
      if (fields.length >= limit) return;
      if (!isJsonObject(raw)) continue;
      const path = prefix ? `${prefix}.${key}` : key;
      const type = typeof raw.type === "string" ? raw.type : "any";
      const description =
        typeof raw.description === "string" ? raw.description : undefined;
      const field: SchemaField = {
        path,
        name: key,
        label: propertyTitle(raw) ?? key,
        type,
        required: required.has(key),
        isPrivate: raw["x-private"] === true,
      };
      if (typeof raw.format === "string") field.format = raw.format;
      if (description) field.description = description;
      fields.push(field);
      if (type === "object" && isJsonObject(raw.properties) && depth < 2) {
        const childRequired = new Set(
          Array.isArray(raw.required)
            ? (raw.required as unknown[]).filter(
                (item): item is string => typeof item === "string",
              )
            : [],
        );
        walk(raw.properties, childRequired, path, depth + 1);
      }
    }
  };

  walk(rootProps, rootRequired, "", 0);
  return fields;
}

export function fieldLabelMap(
  schema: JsonObject | null | undefined,
): Map<string, SchemaField> {
  const map = new Map<string, SchemaField>();
  for (const field of collectSchemaFields(schema)) {
    map.set(field.path, field);
    if (!map.has(field.name)) map.set(field.name, field);
  }
  return map;
}

const TITLE_KEYS = [
  "title",
  "name",
  "heading",
  "label",
  "subject",
  "question",
  "заголовок",
  "название",
  "имя",
];

export function deriveEntryTitle(data: JsonObject | null | undefined): string {
  if (!data) return "Без названия";
  for (const key of TITLE_KEYS) {
    const value = data[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  for (const value of Object.values(data)) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "Без названия";
}

export function deriveEntrySubtitle(
  data: JsonObject | null | undefined,
  exclude?: string,
): string {
  if (!data) return "";
  const parts: string[] = [];
  for (const value of Object.values(data)) {
    if (exclude && value === exclude) continue;
    if (typeof value === "string" && value.trim() && !value.startsWith("http")) {
      parts.push(value.trim());
    }
    if (parts.length >= 2) break;
  }
  return parts.join(" · ");
}

export function submissionStatusFieldOptions(
  schema: JsonObject | null | undefined,
): SchemaField[] {
  return collectSchemaFields(schema).filter(
    (field) =>
      field.type === "string" ||
      field.type === "number" ||
      field.type === "integer" ||
      field.type === "boolean",
  );
}
