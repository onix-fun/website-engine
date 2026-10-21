import { publicMediaPath } from "@/api";
import type { JsonObject, JsonValue } from "@/types";
import type { SchemaFieldNode } from "@/domains/schema-builder/types";
import { deriveDocumentTitle } from "./blocks";
import type { ContentBlock } from "./types";

function BlockPreview({ block }: { block: ContentBlock }) {
  switch (block.type) {
    case "paragraph":
      return <p>{block.data.text || "—"}</p>;
    case "heading": {
      const Tag = `h${block.data.level}` as "h1" | "h2" | "h3" | "h4";
      return <Tag>{block.data.text || "—"}</Tag>;
    }
    case "bulletedList":
      return (
        <ul>
          {block.data.items.map((item, index) => (
            <li key={index}>{item || "—"}</li>
          ))}
        </ul>
      );
    case "numberedList":
      return (
        <ol>
          {block.data.items.map((item, index) => (
            <li key={index}>{item || "—"}</li>
          ))}
        </ol>
      );
    case "quote":
      return (
        <blockquote>
          {block.data.text || "—"}
          {block.data.attribution && (
            <footer className="mt-1 text-xs not-italic text-muted-foreground">
              — {block.data.attribution}
            </footer>
          )}
        </blockquote>
      );
    case "code":
      return (
        <pre>
          <code>{block.data.code || "—"}</code>
        </pre>
      );
    case "divider":
      return <hr />;
    case "image":
      return (
        <figure>
          {block.data.mediaId ? (
            <img src={publicMediaPath(block.data.mediaId)} alt={block.data.alt} />
          ) : (
            <div className="flex aspect-[16/9] items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground">
              Изображение не выбрано
            </div>
          )}
          {(block.data.caption || block.data.alt) && (
            <figcaption className="mt-1.5 text-xs text-muted-foreground">
              {block.data.caption || block.data.alt}
            </figcaption>
          )}
        </figure>
      );
    case "gallery":
      return (
        <figure>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {block.data.mediaIds.map((id) => (
              <img key={id} src={publicMediaPath(id)} alt="" />
            ))}
          </div>
          {block.data.caption && (
            <figcaption className="mt-1.5 text-xs text-muted-foreground">
              {block.data.caption}
            </figcaption>
          )}
        </figure>
      );
    case "video":
      return (
        <figure>
          {block.data.mediaId ? (
            <video
              src={publicMediaPath(block.data.mediaId)}
              controls
              preload="metadata"
              className="w-full rounded-md border border-border bg-black"
            />
          ) : (
            <div className="flex aspect-video items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground">
              Видео не выбрано
            </div>
          )}
          {block.data.caption && (
            <figcaption className="mt-1.5 text-xs text-muted-foreground">
              {block.data.caption}
            </figcaption>
          )}
        </figure>
      );
    default:
      return null;
  }
}

function renderMetaValue(value: JsonValue): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value, null, 2);
}

export function ContentPreview({
  metadata,
  blocks,
  fields,
  width,
}: {
  metadata: JsonObject;
  blocks: ContentBlock[];
  fields: SchemaFieldNode[];
  width: "desktop" | "tablet" | "mobile";
}) {
  const title = deriveDocumentTitle(metadata, blocks);
  const titleFieldKey = fields.find(
    (field) => ["title", "name", "heading"].includes(field.key) && field.type !== "blocks",
  )?.key;
  const metaFields = fields.filter((field) => {
    if (field.type === "blocks" || field.key === titleFieldKey) return false;
    if (!field.key.trim()) return false;
    const value = metadata[field.key];
    if (value === undefined || value === null || value === "") return false;
    if (Array.isArray(value) && value.length === 0) return false;
    return true;
  });

  const widthClass =
    width === "mobile"
      ? "max-w-sm"
      : width === "tablet"
        ? "max-w-2xl"
        : "max-w-3xl";

  return (
    <div className={`${widthClass} mx-auto w-full`}>
      <article className="cms-prose min-h-[30rem] rounded-md border border-border bg-card px-5 py-8 sm:px-10">
        <h1>{title}</h1>
        {metaFields.length > 0 && (
          <dl className="mb-6 grid gap-2 rounded-md border border-border bg-muted/30 p-3 text-xs not-italic sm:grid-cols-2">
            {metaFields.map((field) => (
              <div key={field.id}>
                <dt className="text-muted-foreground">{field.label || field.key}</dt>
                <dd className="mt-0.5 whitespace-pre-wrap break-words text-foreground">
                  {renderMetaValue(metadata[field.key])}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {blocks.length === 0 ? (
          <p className="text-muted-foreground">В документе нет блоков.</p>
        ) : (
          blocks.map((block) => <BlockPreview key={block.id} block={block} />)
        )}
      </article>
    </div>
  );
}
