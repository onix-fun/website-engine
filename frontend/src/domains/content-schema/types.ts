export type BuilderFieldType =
  | "string"
  | "text"
  | "richText"
  | "number"
  | "integer"
  | "boolean"
  | "date"
  | "dateTime"
  | "enum"
  | "object"
  | "array"
  | "media"
  | "file"
  | "blocks";

export type ArrayItemType =
  | "string"
  | "number"
  | "integer"
  | "boolean"
  | "enum"
  | "object"
  | "media"
  | "file";

export type BlockTypeName =
  | "paragraph"
  | "heading"
  | "bulletedList"
  | "numberedList"
  | "quote"
  | "code"
  | "divider"
  | "image"
  | "gallery"
  | "video";

export interface EnumOption {
  id: string;
  value: string;
  label: string;
}

export interface SchemaFieldNode {
  id: string;
  key: string;
  label: string;
  type: BuilderFieldType;
  description: string;
  required: boolean;
  nullable: boolean;
  isPrivate: boolean;
  placeholder: string;
  defaultValue: string;
  minLength?: number;
  maxLength?: number;
  min?: number;
  max?: number;
  step?: number;
  enumValues: EnumOption[];
  children: SchemaFieldNode[];
  itemType: ArrayItemType;
  minItems?: number;
  maxItems?: number;
  allowedBlocks: BlockTypeName[];
}

export type IssueSeverity = "error" | "warning";

export interface BuilderIssue {
  fieldId?: string;
  path: string;
  message: string;
  severity: IssueSeverity;
}

export interface ParsedSchema {
  fields: SchemaFieldNode[];
  warnings: string[];
}

export interface FieldTypeMeta {
  value: BuilderFieldType;
  label: string;
  group: "Базовые" | "Текст" | "Структуры" | "Медиа";
  hint: string;
}
