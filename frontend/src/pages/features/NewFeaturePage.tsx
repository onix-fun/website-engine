import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
  FileText,
  Inbox,
  Info,
  Layers,
  Plus,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api";
import type { FeatureType } from "@/types";
import { queryKeys } from "@/lib/query-keys";
import {
  ErrorState,
  PageHeader,
  useDocumentTitle,
} from "@/components/common/page";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const createFeatureSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "Укажите название")
    .max(200, "Не более 200 символов"),
  key: z
    .string()
    .trim()
    .min(1, "Укажите ключ")
    .max(100, "Не более 100 символов")
    .regex(
      /^[a-z][a-z0-9_-]{0,99}$/,
      "Латинская буква в начале, далее только a-z, 0-9, _ и -",
    ),
  type: z.enum(["collection", "single", "form"]),
});

type CreateFeatureValues = z.infer<typeof createFeatureSchema>;

const typeOptions: Array<{
  value: FeatureType;
  label: string;
  description: string;
  icon: typeof Layers;
}> = [
  {
    value: "collection",
    label: "Collection",
    description: "Набор однотипных записей: статьи, проекты, товары.",
    icon: Layers,
  },
  {
    value: "single",
    label: "Single",
    description: "Одна страница или документ: главная, контакты, о нас.",
    icon: FileText,
  },
  {
    value: "form",
    label: "Form",
    description: "Публичная форма и приём заявок.",
    icon: Inbox,
  },
];

export default function NewFeaturePage() {
  useDocumentTitle("Новая Feature");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const {
    register,
    handleSubmit,
    watch,
    formState: { errors },
  } = useForm<CreateFeatureValues>({
    resolver: zodResolver(createFeatureSchema),
    defaultValues: { name: "", key: "", type: "collection" },
  });
  const selectedType = watch("type");

  const createMutation = useMutation({
    mutationFn: (values: CreateFeatureValues) => api.features.create(values),
    onSuccess: (feature) => {
      toast.success(`Feature «${feature.name}» создана`);
      void queryClient.invalidateQueries({ queryKey: queryKeys.features.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview });
      navigate(`/features/${feature.id}/schema`, { replace: true });
    },
  });

  const onSubmit = (values: CreateFeatureValues) => {
    createMutation.mutate(values);
  };

  return (
    <div className="page-grid max-w-5xl">
      <PageHeader
        backTo="/features"
        backLabel="К списку Features"
        title="Новая Feature"
        description="Feature определяет ресурс CMS. После создания откроется визуальный конструктор схемы."
      />

      {createMutation.isError && (
        <ErrorState
          error={createMutation.error}
          title="Не удалось создать Feature"
          className="mb-5"
        />
      )}

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5" noValidate>
        <Card>
          <CardContent className="space-y-5 p-5 sm:p-6">
            <div className="grid gap-5 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="feature-name">
                  Название <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="feature-name"
                  placeholder="Например, Блог"
                  autoFocus
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

              <div className="space-y-2">
                <Label htmlFor="feature-key">
                  Ключ <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="feature-key"
                  placeholder="blog"
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                  className="font-mono text-xs"
                  aria-invalid={Boolean(errors.key)}
                  aria-describedby={errors.key ? "feature-key-error" : "feature-key-hint"}
                  {...register("key")}
                />
                {errors.key ? (
                  <p id="feature-key-error" className="text-xs text-destructive">
                    {errors.key.message}
                  </p>
                ) : (
                  <p id="feature-key-hint" className="text-2xs leading-5 text-muted-foreground">
                    Используется в публичном API:{" "}
                    <code>/api/v1/content/&lt;key&gt;</code>
                  </p>
                )}
              </div>
            </div>

            <div className="rounded-md border border-border bg-muted/30 px-3 py-2.5 text-xs leading-5 text-muted-foreground">
              <Info className="mr-1.5 inline h-3.5 w-3.5" />
              Backend не поддерживает описание Feature, поэтому поле не добавлено.
              Название можно изменить позже в настройках.
            </div>

            <fieldset>
              <legend className="text-sm font-medium">Тип Feature</legend>
              <p className="mt-1 text-xs text-muted-foreground">
                Тип определяется при создании и не изменяется через API.
              </p>
              <div className="mt-3 grid gap-2 sm:grid-cols-3">
                {typeOptions.map((option) => {
                  const Icon = option.icon;
                  const checked = selectedType === option.value;
                  return (
                    <label
                      key={option.value}
                      className={cn(
                        "relative cursor-pointer rounded-lg border p-3.5 transition-colors focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background",
                        checked
                          ? "border-primary/60 bg-primary/5"
                          : "border-border bg-card hover:bg-accent/40",
                      )}
                    >
                      <input
                        type="radio"
                        value={option.value}
                        className="sr-only"
                        {...register("type")}
                      />
                      <span className="flex items-center gap-2">
                        <span
                          className={cn(
                            "flex h-8 w-8 items-center justify-center rounded-md border",
                            checked
                              ? "border-primary/30 bg-primary/10 text-primary"
                              : "border-border bg-muted/40 text-muted-foreground",
                          )}
                        >
                          <Icon className="h-4 w-4" />
                        </span>
                        <span className="text-sm font-medium">{option.label}</span>
                      </span>
                      <span className="mt-2 block text-xs leading-5 text-muted-foreground">
                        {option.description}
                      </span>
                    </label>
                  );
                })}
              </div>
              {errors.type && (
                <p className="mt-2 text-xs text-destructive">{errors.type.message}</p>
              )}
            </fieldset>
          </CardContent>
        </Card>

        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate("/features")}
            disabled={createMutation.isPending}
          >
            Отмена
          </Button>
          <Button type="submit" loading={createMutation.isPending}>
            {!createMutation.isPending && <Plus />}
            Создать Feature
          </Button>
        </div>
      </form>
    </div>
  );
}
