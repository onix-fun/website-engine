import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, RefreshCw, Search, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import type { CreatedToken, Role, Token } from "@/types";
import { useAuth } from "@/app/auth";
import { queryKeys } from "@/lib/query-keys";
import { errorMessage } from "@/lib/errors";
import {
  formatDate,
  formatDateTime,
  roleLabels,
} from "@/lib/format";
import {
  EmptyState,
  ErrorState,
  InlineAlert,
  TableSkeleton,
} from "@/components/common/page";
import { useConfirm } from "@/components/common/confirm";
import { RoleBadge, TokenStatusBadge } from "@/components/common/status-badges";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type StatusFilter = "all" | "active" | "revoked" | "expired";

function tokenStatus(token: Token): Exclude<StatusFilter, "all"> {
  if (token.revoked_at) return "revoked";
  if (token.expires_at && new Date(token.expires_at).getTime() <= Date.now()) {
    return "expired";
  }
  return "active";
}

function CreateTokenDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (token: CreatedToken) => void;
}) {
  const { principal } = useAuth();
  const isModerator = principal?.role === "moderator";
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("user");
  const [expires, setExpires] = useState("");

  useEffect(() => {
    if (open) {
      setName("");
      setRole("user");
      setExpires("");
    }
  }, [open]);

  const createMutation = useMutation({
    mutationFn: () =>
      api.tokens.create({
        name: name.trim(),
        role,
        expires_at: expires
          ? new Date(`${expires}T23:59:59`).toISOString()
          : undefined,
      }),
    onSuccess: (created) => {
      onCreated(created);
      onClose();
    },
    onError: (error) => toast.error(errorMessage(error), { duration: 6000 }),
  });

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Новый токен доступа</DialogTitle>
          <DialogDescription>
            Токен задаёт доступ и роль учётной записи. Секрет показывается только
            один раз сразу после создания.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div>
            <label htmlFor="token-name" className="mb-1 block text-xs font-medium">
              Название
            </label>
            <Input
              id="token-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Например, Редактор контента"
              maxLength={80}
              autoFocus
            />
            <p className="mt-1 text-2xs text-muted-foreground">
              Имя видно в списке и в журналах. Не используйте секреты в названии.
            </p>
          </div>

          <div>
            <label htmlFor="token-role" className="mb-1 block text-xs font-medium">
              Роль
            </label>
            <Select
              value={role}
              onValueChange={(value) => setRole(value as Role)}
              disabled={isModerator}
            >
              <SelectTrigger id="token-role" aria-label="Роль токена">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="user">{roleLabels.user}</SelectItem>
                <SelectItem value="staff">{roleLabels.staff}</SelectItem>
                <SelectItem value="moderator">{roleLabels.moderator}</SelectItem>
                <SelectItem value="admin">{roleLabels.admin}</SelectItem>
              </SelectContent>
            </Select>
            {isModerator && (
              <p className="mt-1 text-2xs text-muted-foreground">
                Модератор может выпускать только токены пользователей.
              </p>
            )}
          </div>

          <div>
            <label htmlFor="token-expires" className="mb-1 block text-xs font-medium">
              Действует до
            </label>
            <Input
              id="token-expires"
              type="date"
              value={expires}
              onChange={(event) => setExpires(event.target.value)}
            />
            <p className="mt-1 text-2xs text-muted-foreground">
              Необязательно. Пустое поле — токен без срока действия.
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Отмена
          </Button>
          <Button
            onClick={() => createMutation.mutate()}
            disabled={!name.trim()}
            loading={createMutation.isPending}
          >
            <KeyRound />
            Создать
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CreatedTokenDialog({
  token,
  open,
  onClose,
}: {
  token: CreatedToken | null;
  open: boolean;
  onClose: () => void;
}) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    setVisible(false);
  }, [token]);

  if (!token) return null;

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Токен создан</DialogTitle>
          <DialogDescription>
            Скопируйте секрет сейчас. После закрытия окна его невозможно получить
            снова — только отозвать токен и выпустить новый.
          </DialogDescription>
        </DialogHeader>

        <InlineAlert tone="warning" title="Секрет показывается один раз">
          Передайте токен владельцу по защищённому каналу и не сохраняйте его в
          репозитории или чатах.
        </InlineAlert>

        <div>
          <p className="mb-1 text-xs font-medium">Токен</p>
          <div className="rounded-md border border-border bg-muted/30 p-2">
            <code className="block break-all text-xs">
              {visible ? token.token : "•".repeat(Math.min(token.token.length, 48))}
            </code>
          </div>
          <div className="mt-2 flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setVisible((current) => !current)}
            >
              {visible ? "Скрыть" : "Показать"}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                void navigator.clipboard?.writeText(token.token).then(
                  () => toast.success("Токен скопирован"),
                  () => toast.error("Не удалось скопировать"),
                );
              }}
            >
              Копировать
            </Button>
          </div>
        </div>

        <dl className="grid grid-cols-2 gap-2 text-xs">
          <div>
            <dt className="text-muted-foreground">Название</dt>
            <dd className="mt-0.5 font-medium">{token.name}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Роль</dt>
            <dd className="mt-0.5">
              <RoleBadge role={token.role} />
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Создан</dt>
            <dd className="mt-0.5">{formatDateTime(token.created_at)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Действует до</dt>
            <dd className="mt-0.5">
              {token.expires_at ? formatDate(token.expires_at) : "Без срока"}
            </dd>
          </div>
        </dl>

        <DialogFooter>
          <Button onClick={onClose}>Готово</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function TokensPage() {
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const { principal } = useAuth();
  const [searchParams, setSearchParams] = useSearchParams();
  const urlQuery = searchParams.get("q") ?? "";
  const [search, setSearch] = useState(urlQuery);
  const [status, setStatus] = useState<StatusFilter>(
    (searchParams.get("status") as StatusFilter | null) ?? "all",
  );

  useEffect(() => {
    setSearch(urlQuery);
  }, [urlQuery]);

  const updateParams = (values: Record<string, string | null>) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        Object.entries(values).forEach(([key, value]) => {
          if (value === null || value === "" || value === "all") {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        });
        return next;
      },
      { replace: true },
    );
  };
  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<CreatedToken | null>(null);
  const [createdOpen, setCreatedOpen] = useState(false);

  const tokensQuery = useQuery({
    queryKey: queryKeys.tokens.list(),
    queryFn: ({ signal }) => api.tokens.list(signal),
  });

  const revokeMutation = useMutation({
    mutationFn: (id: string) => api.tokens.revoke(id),
    onSuccess: () => {
      toast.success("Токен отозван");
      void queryClient.invalidateQueries({ queryKey: queryKeys.tokens.all });
    },
    onError: (error) => toast.error(errorMessage(error)),
  });

  const tokens = useMemo(() => {
    const query = search.trim().toLowerCase();
    return (tokensQuery.data?.items ?? []).filter((token) => {
      if (status !== "all" && tokenStatus(token) !== status) return false;
      if (
        query &&
        !`${token.name} ${roleLabels[token.role]} ${token.id}`
          .toLowerCase()
          .includes(query)
      ) {
        return false;
      }
      return true;
    });
  }, [tokensQuery.data, search, status]);

  const all = tokensQuery.data?.items ?? [];
  const activeCount = all.filter((token) => tokenStatus(token) === "active").length;
  const revokedCount = all.filter((token) => tokenStatus(token) === "revoked").length;

  const requestRevoke = async (token: Token) => {
    const self = principal?.id === token.id;
    const confirmed = await confirm({
      title: "Отозвать токен?",
      description: (
        <div className="space-y-2">
          <p>
            Токен <strong>«{token.name}»</strong> перестанет работать сразу.
          </p>
          {self && (
            <p className="font-medium text-destructive">
              Это токен текущей сессии — после отзыва вы выйдете из панели.
            </p>
          )}
          <p className="text-muted-foreground">
            Выпустить его заново нельзя, можно создать новый токен.
          </p>
        </div>
      ),
      confirmLabel: "Отозвать",
      destructive: true,
    });
    if (confirmed) revokeMutation.mutate(token.id);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Токены доступа</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {principal?.role === "moderator"
              ? "Модератор видит и отзывает только выпущенные им токены."
              : "Токен задаёт роль учётной записи. Отзыв сразу прекращает доступ."}
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => void tokensQuery.refetch()}
            loading={tokensQuery.isFetching}
          >
            <RefreshCw />
            Обновить
          </Button>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus />
            Создать токен
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-2">
        <div className="rounded-lg border border-border bg-card p-3">
          <p className="text-2xs text-muted-foreground">Всего</p>
          <p className="mt-0.5 text-lg font-semibold">{all.length}</p>
        </div>
        <div className="rounded-lg border border-border bg-card p-3">
          <p className="text-2xs text-muted-foreground">Активных</p>
          <p className="mt-0.5 text-lg font-semibold">{activeCount}</p>
        </div>
        <div className="rounded-lg border border-border bg-card p-3">
          <p className="text-2xs text-muted-foreground">Отозвано</p>
          <p className="mt-0.5 text-lg font-semibold">{revokedCount}</p>
        </div>
      </div>

      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              updateParams({ q: event.target.value || null });
            }}
            placeholder="Поиск по названию, роли или ID"
            aria-label="Поиск токенов"
            className="pl-9 pr-9"
          />
          {search && (
            <button
              type="button"
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-1 text-muted-foreground hover:bg-accent"
              onClick={() => {
                setSearch("");
                updateParams({ q: null });
              }}
              aria-label="Очистить поиск"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value as StatusFilter);
            updateParams({ status: value });
          }}
        >
          <SelectTrigger className="w-full sm:w-44" aria-label="Статус токена">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все статусы</SelectItem>
            <SelectItem value="active">Активные</SelectItem>
            <SelectItem value="revoked">Отозванные</SelectItem>
            <SelectItem value="expired">Истёкшие</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {tokensQuery.isLoading ? (
        <TableSkeleton rows={5} />
      ) : tokensQuery.error ? (
        <ErrorState
          error={tokensQuery.error}
          onRetry={() => void tokensQuery.refetch()}
        />
      ) : tokens.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={search || status !== "all" ? "Токены не найдены" : "Токенов пока нет"}
          description={
            search || status !== "all"
              ? "Измените запрос или фильтр статуса."
              : "Создайте первый токен, чтобы выдать доступ редактору или модератору."
          }
          action={
            <Button onClick={() => setCreateOpen(true)}>
              <Plus />
              Создать токен
            </Button>
          }
        />
      ) : (
        <>
        <div className="table-wrap hidden md:block">
          <table className="data-table min-w-[760px]">
            <thead>
              <tr>
                <th>Название</th>
                <th>Роль</th>
                <th>Статус</th>
                <th>Создан</th>
                <th>Действует до</th>
                <th>Последнее использование</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => (
                <tr key={token.id} className="group">
                  <td>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{token.name}</p>
                      <code className="block truncate text-2xs text-muted-foreground">
                        {token.id}
                      </code>
                    </div>
                  </td>
                  <td>
                    <RoleBadge role={token.role} />
                  </td>
                  <td>
                    <TokenStatusBadge token={token} />
                  </td>
                  <td className="text-xs text-muted-foreground">
                    {formatDateTime(token.created_at)}
                  </td>
                  <td className="text-xs text-muted-foreground">
                    {token.expires_at ? formatDate(token.expires_at) : "Без срока"}
                  </td>
                  <td className="text-xs text-muted-foreground">
                    {token.last_used_at
                      ? formatDateTime(token.last_used_at)
                      : "Не использовался"}
                  </td>
                  <td>
                    <div className="flex justify-end">
                      {token.revoked_at ? (
                        <Badge variant="muted">Отозван</Badge>
                      ) : (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-destructive hover:text-destructive"
                          onClick={() => void requestRevoke(token)}
                          disabled={revokeMutation.isPending}
                        >
                          <Trash2 />
                          Отозвать
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="space-y-3 md:hidden">
          {tokens.map((token) => (
            <div
              key={token.id}
              className="rounded-lg border border-border bg-card p-3"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{token.name}</p>
                  <code className="block truncate text-2xs text-muted-foreground">
                    {token.id}
                  </code>
                </div>
                <RoleBadge role={token.role} />
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <TokenStatusBadge token={token} />
                <span>Создан {formatDateTime(token.created_at)}</span>
                <span>
                  До {token.expires_at ? formatDate(token.expires_at) : "бессрочно"}
                </span>
              </div>
              <div className="mt-3 flex justify-end">
                {token.revoked_at ? (
                  <Badge variant="muted">Отозван</Badge>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => void requestRevoke(token)}
                    disabled={revokeMutation.isPending}
                  >
                    <Trash2 />
                    Отозвать
                  </Button>
                )}
              </div>
            </div>
          ))}
        </div>
        </>
      )}

      <InlineAlert tone="info" title="Безопасность">
        Токены передаются в заголовке <code>Authorization: Bearer …</code>.
        Панель хранит только хеш секрета, поэтому восстановить забытый токен
        невозможно — его нужно отозвать и выпустить новый.
      </InlineAlert>

      <CreateTokenDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(token) => {
          setCreated(token);
          setCreatedOpen(true);
          void queryClient.invalidateQueries({ queryKey: queryKeys.tokens.all });
        }}
      />
      <CreatedTokenDialog
        token={created}
        open={createdOpen}
        onClose={() => setCreatedOpen(false)}
      />
    </div>
  );
}
