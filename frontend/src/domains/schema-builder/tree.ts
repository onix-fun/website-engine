import type { SchemaFieldNode } from "./types";

export interface FieldLocation {
  field: SchemaFieldNode;
  siblings: SchemaFieldNode[];
  index: number;
  parentId: string | null;
  parentPath: string;
}

export function locateField(
  fields: SchemaFieldNode[],
  id: string,
  parentId: string | null = null,
  parentPath = "",
): FieldLocation | null {
  for (let index = 0; index < fields.length; index += 1) {
    const field = fields[index];
    if (!field) continue;
    if (field.id === id) {
      return { field, siblings: fields, index, parentId, parentPath };
    }
    const nested = locateField(
      field.children,
      id,
      field.id,
      parentPath ? `${parentPath}.${field.key}` : field.key,
    );
    if (nested) return nested;
  }
  return null;
}

export function updateField(
  fields: SchemaFieldNode[],
  id: string,
  updater: (field: SchemaFieldNode) => SchemaFieldNode,
): SchemaFieldNode[] {
  return fields.map((field) => {
    if (field.id === id) return updater(field);
    if (field.children.length > 0) {
      return { ...field, children: updateField(field.children, id, updater) };
    }
    return field;
  });
}

export function removeField(fields: SchemaFieldNode[], id: string): SchemaFieldNode[] {
  return fields
    .filter((field) => field.id !== id)
    .map((field) =>
      field.children.length > 0
        ? { ...field, children: removeField(field.children, id) }
        : field,
    );
}

export function appendChild(
  root: SchemaFieldNode[],
  parentId: string | null,
  child: SchemaFieldNode,
): SchemaFieldNode[] {
  if (parentId === null) return [...root, child];
  return root.map((field) => {
    if (field.id === parentId) {
      return { ...field, children: [...field.children, child] };
    }
    if (field.children.length > 0) {
      return { ...field, children: appendChild(field.children, parentId, child) };
    }
    return field;
  });
}

export function isDescendant(
  fields: SchemaFieldNode[],
  ancestorId: string,
  candidateId: string,
): boolean {
  const ancestors = locateField(fields, ancestorId);
  if (!ancestors) return false;
  const stack = [...ancestors.field.children];
  while (stack.length > 0) {
    const node = stack.pop();
    if (!node) continue;
    if (node.id === candidateId) return true;
    stack.push(...node.children);
  }
  return false;
}

export function findParent(fields: SchemaFieldNode[], id: string): SchemaFieldNode | null {
  for (const field of fields) {
    if (field.children.some((child) => child.id === id)) return field;
    const nested = findParent(field.children, id);
    if (nested) return nested;
  }
  return null;
}

/**
 * Moves a field next to another field. Moving across levels is allowed when the
 * target itself lives in a nested object or object-array, which makes nesting
 * reachable by drag-and-drop; move buttons remain the deterministic fallback.
 */
export function moveField(
  fields: SchemaFieldNode[],
  activeId: string,
  targetId: string,
  mode: "before" | "after" = "before",
): SchemaFieldNode[] {
  if (activeId === targetId) return fields;
  if (isDescendant(fields, activeId, targetId)) return fields;

  const activeLocation = locateField(fields, activeId);
  if (!activeLocation) return fields;

  const without = removeField(fields, activeId);
  const targetLocation = locateField(without, targetId);
  if (!targetLocation) return fields;

  const insertAt =
    targetLocation.index + (mode === "after" ? 1 : 0);
  if (targetLocation.parentId === null) {
    const next = [...without];
    next.splice(insertAt, 0, activeLocation.field);
    return next;
  }
  return insertIntoParent(
    without,
    targetLocation.parentId,
    activeLocation.field,
    insertAt,
  );
}

function insertIntoParent(
  fields: SchemaFieldNode[],
  parentId: string,
  child: SchemaFieldNode,
  index: number,
): SchemaFieldNode[] {
  return fields.map((field) => {
    if (field.id === parentId) {
      const children = [...field.children];
      children.splice(index, 0, child);
      return { ...field, children };
    }
    if (field.children.length > 0) {
      return {
        ...field,
        children: insertIntoParent(field.children, parentId, child, index),
      };
    }
    return field;
  });
}

export function moveSibling(
  fields: SchemaFieldNode[],
  id: string,
  direction: -1 | 1,
): SchemaFieldNode[] {
  const location = locateField(fields, id);
  if (!location) return fields;
  const target = location.index + direction;
  if (target < 0 || target >= location.siblings.length) return fields;
  const active = location.siblings[location.index];
  const other = location.siblings[target];
  if (!active || !other) return fields;
  return moveField(
    fields,
    active.id,
    other.id,
    direction > 0 ? "after" : "before",
  );
}

export function containsDuplicateKey(
  siblings: SchemaFieldNode[],
  key: string,
  exceptId: string,
): boolean {
  return siblings.some(
    (field) => field.id !== exceptId && field.key.trim() === key.trim(),
  );
}

export function insertFieldAfter(
  fields: SchemaFieldNode[],
  afterId: string,
  inserted: SchemaFieldNode,
): SchemaFieldNode[] {
  for (let index = 0; index < fields.length; index += 1) {
    const field = fields[index];
    if (!field) continue;
    if (field.id === afterId) {
      const next = [...fields];
      next.splice(index + 1, 0, inserted);
      return next;
    }
    if (field.children.length > 0) {
      const children = insertFieldAfter(field.children, afterId, inserted);
      if (children !== field.children) {
        return fields.map((item, itemIndex) =>
          itemIndex === index ? { ...field, children } : item,
        );
      }
    }
  }
  return fields;
}
