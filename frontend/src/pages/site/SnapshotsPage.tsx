import { useEffect, useState } from "react";
import { History, Rocket, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorState } from "@/components/common/page";
import type { SiteSnapshot, SiteSnapshotList } from "@/types";
import { useSiteWorkspace } from "./SiteWorkspace";

const emptyList: SiteSnapshotList = { items: [], active: { dev: "", prod: "" } };
const message = (error: unknown) => error instanceof Error ? error.message : "Неизвестная ошибка";

export default function SnapshotsPage() {
  const { dirty } = useSiteWorkspace();
  const [snapshots, setSnapshots] = useState(emptyList);
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const refresh = async () => setSnapshots(await api.siteBuilder.snapshots());
  useEffect(() => {
    let active = true;
    api.siteBuilder.snapshots().then((result) => { if (active) setSnapshots(result); })
      .catch((reason: unknown) => { if (active) setError(reason); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);

  const publish = async () => {
    setBusy(true);
    try {
      const result = await api.siteBuilder.publish(name.trim());
      setName("");
      await refresh();
      toast.success(`Снапшот #${result.sequence} опубликован в dev`);
    } catch (reason) { toast.error(message(reason)); }
    finally { setBusy(false); }
  };
  const promote = async (snapshot: SiteSnapshot) => {
    setBusy(true);
    try { await api.siteBuilder.promote(snapshot.id); await refresh(); toast.success(`Снапшот #${snapshot.sequence} продвинут в prod`); }
    catch (reason) { toast.error(message(reason)); }
    finally { setBusy(false); }
  };
  const rollback = async (snapshot: SiteSnapshot, environment: "dev" | "prod") => {
    if (!window.confirm(`Переключить ${environment} на снапшот #${snapshot.sequence}?`)) return;
    setBusy(true);
    try { await api.siteBuilder.rollback(snapshot.id, environment); await refresh(); toast.success(`${environment}: активирован снапшот #${snapshot.sequence}`); }
    catch (reason) { toast.error(message(reason)); }
    finally { setBusy(false); }
  };

  if (loading) return <p className="p-6 text-sm text-muted-foreground">Загружаем снапшоты…</p>;
  if (error) return <ErrorState error={error} title="Не удалось загрузить снапшоты" />;
  const activeName = (id: string) => snapshots.items.find((item) => item.id === id)?.name ?? "Нет опубликованной версии";

  return (
    <div className="space-y-4">
      <div><h2 className="text-xl font-semibold tracking-tight">Снапшоты</h2><p className="mt-1 text-sm text-muted-foreground">Снапшот фиксирует сохранённый черновик, версии фичей и опубликованный контент.</p></div>
      <div className="grid gap-3 sm:grid-cols-2"><Card><CardContent className="p-4"><p className="text-xs text-muted-foreground">Активен в dev</p><p className="mt-1 truncate text-sm font-medium">{activeName(snapshots.active.dev)}</p></CardContent></Card><Card><CardContent className="p-4"><p className="text-xs text-muted-foreground">Активен в prod</p><p className="mt-1 truncate text-sm font-medium">{activeName(snapshots.active.prod)}</p></CardContent></Card></div>
      <Card><CardHeader><CardTitle className="text-base">Опубликовать в dev</CardTitle><CardDescription>Снапшот создаётся строго из последней сохранённой ревизии.</CardDescription></CardHeader><CardContent className="flex flex-wrap items-end gap-3"><div className="min-w-[16rem] flex-1 space-y-1.5"><Label htmlFor="snapshot-name">Название снапшота</Label><Input id="snapshot-name" value={name} onChange={(event) => setName(event.currentTarget.value)} placeholder="Например, каталог с новыми карточками" /></div><Button type="button" onClick={() => void publish()} loading={busy} disabled={dirty}><Rocket />Опубликовать в dev</Button>{dirty && <p className="basis-full text-xs text-amber-700 dark:text-amber-300">Сначала сохраните изменения в редакторе страниц или роутере.</p>}</CardContent></Card>
      <Card><CardHeader><CardTitle className="text-base">История публикаций</CardTitle><CardDescription>Указатели dev и prod переключаются только после успешной проверки состава.</CardDescription></CardHeader><CardContent>
        {snapshots.items.length === 0 ? <div className="rounded-md border border-dashed border-border px-4 py-10 text-center"><History className="mx-auto size-7 text-muted-foreground" /><p className="mt-3 text-sm font-medium">Снапшотов пока нет</p><p className="mt-1 text-sm text-muted-foreground">Сохраните страницы и опубликуйте первую версию в dev.</p></div> : <div className="divide-y divide-border">{snapshots.items.map((snapshot) => <div className="flex flex-wrap items-center justify-between gap-4 py-3 first:pt-0 last:pb-0" key={snapshot.id}>
          <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><p className="text-sm font-medium">#{snapshot.sequence} · {snapshot.name}</p>{snapshot.id === snapshots.active.dev && <span className="rounded bg-blue-500/10 px-2 py-0.5 text-[11px] text-blue-700 dark:text-blue-300">dev</span>}{snapshot.id === snapshots.active.prod && <span className="rounded bg-emerald-500/10 px-2 py-0.5 text-[11px] text-emerald-700 dark:text-emerald-300">prod</span>}</div><p className="mt-1 truncate text-xs text-muted-foreground">{new Date(snapshot.createdAt).toLocaleString("ru-RU")} · sha256 {snapshot.digest}</p></div>
          <div className="flex flex-wrap gap-2">{snapshot.id === snapshots.active.dev && snapshot.id !== snapshots.active.prod && <Button type="button" size="sm" onClick={() => void promote(snapshot)} disabled={busy}><Rocket />В prod</Button>}{snapshot.id !== snapshots.active.dev && <Button type="button" size="sm" variant="outline" onClick={() => void rollback(snapshot, "dev")} disabled={busy}><RotateCcw />Откатить dev</Button>}{snapshot.id !== snapshots.active.prod && <Button type="button" size="sm" variant="outline" onClick={() => void rollback(snapshot, "prod")} disabled={busy}><RotateCcw />Откатить prod</Button>}</div>
        </div>)}</div>}
      </CardContent></Card>
    </div>
  );
}
