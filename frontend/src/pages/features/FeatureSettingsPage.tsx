import { useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Archive, Save } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import { queryKeys } from "@/lib/query-keys";
import { formatDateTime } from "@/lib/format";
import { useFeatureContext } from "@/pages/features/FeatureLayout";
import { useConfirm } from "@/components/common/confirm";
import {
  ErrorState,
  InlineAlert,
  useDocumentTitle,
} from "@/components/common/page";
import {
  FeatureStatusBadge,
  FeatureTypeBadge,
  SchemaStateBadge,
} from "@/components/common/status-badges";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const renameSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Укажите название")
    .max(200, "Не более 200 символов"),
});

type RenameValues = z.infer<typeof renameSchema>;

function MetaRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(8rem,32%)_1fr] gap-3 border-b border-border/60 py-2.5 text-sm last:border-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

export default function FeatureSettingsPage() {
  const { feature } = useFeatureContext();
  useDocumentTitle(`${feature.name} — настройки`);
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const archived = feature.status === "archived";

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<RenameValues>({
    resolver: zodResolver(renameSchema),
    defaultValues: { name: feature.name },
  });

  useEffect(() => {
    if (!isDirty) reset({ name: feature.name });
  }, [feature.name, isDirty, reset]);

  const renameMutation = useMutation({
    mutationFn: (values: RenameValues) => api.features.updateName(feature.id, values.name),
    onSuccess: () => {
      toast.success("Название Feature обновлено");
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.all });
    },
  });

  const archiveMutation = useMutation({
    mutationFn: () => api.features.archive(feature.id),
    onSuccess: () => {
      toast.success("Feature архивирована");
      void queryClient.invalidateQueries({
        queryKey: queryKeys.features.detail(feature.id),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
    },
  });

  const onSubmit = (values: RenameValues) => {
    renameMutation.mutate(values);
  };

  const requestArchive = async () => {
    const confirmed = await confirm({
      title: "Архивировать Feature?",
      description: (
        <>
          Feature <strong>«{feature.name}»</strong> станет архивной. Схему,
          контент и настройки можно будет только просматривать. Вернуть из архива
          через этот интерфейс нельзя.
        </>
      ),
      confirmLabel: "Архивировать",
      destructive: true,
    });
    if (confirmed) archiveMutation.mutate();
  };

  return (
    <div className="max-w-5xl space-y-5">
      <div>
        <h2 className="text-lg font-semibold tracking-tight">Настройки Feature</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Реальные свойства, поддерживаемые backend. Ключ, тип и идентификаторы
          доступны только для чтения.
        </p>
      </div>

      {archived && (
        <InlineAlert tone="warning" title="Feature в архиве">
          Изменения заблокированы. Доступен только просмотр существующих данных.
        </InlineAlert>
      )}

      {archiveMutation.isError && (
        <ErrorState error={archiveMutation.error} title="Не удалось архивировать Feature" />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Название</CardTitle>
          <CardDescription>
            Отображается в каталоге Features и заголовке рабочей области.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-3" noValidate>
            {renameMutation.isError && (
              <ErrorState error={renameMutation.error} title="Не удалось переименовать Feature" />
            )}
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
              <div className="flex-1 space-y-2">
                <Label htmlFor="feature-name">Название</Label>
                <Input
                  id="feature-name"
                  disabled={archived || renameMutation.isPending}
                  aria-invalid={Boolean(errors.name)}
                  aria-describedby={errors.name ? "feature-name-error" : undefined}
                  {...register("name")}
                />
                {errors.name && (
                  <p id="feature-name-error" className="text-xs text-destructive">
                    {errors.name.message}
                  </p>
                )}
              </div>
              <Button
                type="submit"
                loading={renameMutation.isPending}
                disabled={archived || !isDirty || renameMutation.isPending}
              >
                {!renameMutation.isPending && <Save />}
                Сохранить
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Свойства и метаданные</CardTitle>
          <CardDescription>
            Значения, полученные от API. Некоторые поля нельзя изменить после
            создания.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <dl>
            <MetaRow label="Название">{feature.name}</MetaRow>
            <MetaRow label="Ключ">
              <code className="text-xs">{feature.key}</code>
            </MetaRow>
            <MetaRow label="Тип">
              <FeatureTypeBadge type={feature.type} />
            </MetaRow>
            <MetaRow label="Статус">
              <FeatureStatusBadge status={feature.status} />
            </MetaRow>
            <MetaRow label="Схема">
              <SchemaStateBadge feature={feature} />
            </MetaRow>
            <MetaRow label="Ревизия">
              <span className="font-mono text-xs">r{feature.revision}</span>
            </MetaRow>
            <MetaRow label="Создана">{formatDateTime(feature.created_at)}</MetaRow>
            <MetaRow label="Обновлена">{formatDateTime(feature.updated_at)}</MetaRow>
            <MetaRow label="Создал токен">
              <code className="text-xs">{feature.created_by ?? "—"}</code>
            </MetaRow>
            <MetaRow label="ID Feature">
              <code className="break-all text-xs">{feature.id}</code>
            </MetaRow>
            <MetaRow label="Draft version ID">
              <code className="break-all text-xs">{feature.draft_version_id ?? "—"}</code>
            </MetaRow>
            <MetaRow label="Published version ID">
              <code className="break-all text-xs">{feature.prod_version_id ?? "—"}</code>
            </MetaRow>
          </dl>
        </CardContent>
      </Card>

      <Card className="border-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">Архивирование</CardTitle>
          <CardDescription>
            Backend не поддерживает удаление Feature и восстановление из архива.
            Архивирование блокирует изменения, но сохраняет данные.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <p className="max-w-2xl text-sm text-muted-foreground">
              Убедитесь, что Feature больше не используется на сайте. Действие
              необратимо через текущий интерфейс.
            </p>
            <Button
              variant="destructive"
              onClick={() => void requestArchive()}
              loading={archiveMutation.isPending}
              disabled={archived || archiveMutation.isPending}
            >
              <Archive />
              {archived ? "Уже в архиве" : "Архивировать"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
