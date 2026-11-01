# Архитектура frontend

Дата: 2026-11-02.

## Стек

- React 18.3, TypeScript 5.6, Vite 5.
- React Router 6 с вложенными защищёнными маршрутами и ленивой загрузкой страниц.
- TanStack Query 5 для серверного состояния; общий API-клиент и типы — `frontend/src/api.ts` и `frontend/src/types.ts`.
- React Hook Form и Zod для форм; Radix UI primitives, Tailwind CSS и Lucide для UI.
- Go сервер встраивает собранную админку в `pkg/static/dist` и отдельно обслуживает публичный runtime.

## Границы модулей

- `frontend/src/app/`: auth/theme providers, роутер и общий shell.
- `frontend/src/api.ts`, `types.ts`: клиент существующих и новых административных API.
- `frontend/src/domains/`: повторно используемая доменная логика и hooks.
- `frontend/src/pages/`: страницы по областям — access, content, dashboard, features, media, settings, site, submissions.
- `frontend/src/components/ui/`: общие Radix-компоненты; `components/common/`: заголовки, состояния страниц, подтверждения и общие паттерны.
- `frontend/src/lib/css-color.ts`: преобразование CSS HEX/RGB/HSL в представление цветового поля и обратно.
- `frontend/src/runtime/`: публичное приложение сайта, не связанное с администраторской навигацией и стилями.
- `presentation/http/routes.go` и Go handlers остаются источником истины для контрактов и серверной авторизации.

## Основные изменения

- Схемы контента приходят в `manifest.json` вместе с ZIP-сборкой фичи; отдельного пользовательского раздела «Модели данных» нет. Реестр сборок находится в разделе «Сайт».
- В `AppShell` оставлены одни действия выхода и смены темы; проверки ролей для manager-only экранов сохранены.
- `SiteWorkspace` управляет загрузкой и сохранением ревизии композиции страниц/маршрутов. Snapshot screen вызывает сохранение перед созданием снапшота, если черновик изменён.
- Редактор дизайн-системы теперь хранит работу по вкладкам категорий, валидирует шрифтовые настройки, защищает несохранённые изменения и не загружает runtime preview.
- Workflow снапшотов изменён на API `Create`, пагинацию и адресное `Deploy`. Backend создаёт immutable snapshot v3 с копией содержимого редакционных записей, сборок и темы. Выбранную версию можно развернуть напрямую в любую среду.
- Старые `promote` и `rollback` маршруты сохранены; формат payload v2 продолжает работать без восстановления закреплённого контента.

Таким образом, текущая работа затронула и frontend, и backend API/хранение снапшотов: это не только стилистический рефакторинг. Миграции `0005_snapshot_workflow.sql` и `0006_snapshot_content_deploy.sql` снимают уникальность digest для unlimited snapshots и разрешают независимо развёртывать редакционные pointer-версии по средам.

## Сборка и локальная разработка

Из `frontend/` команда `pnpm build` запускает оба TypeScript typecheck, затем собирает админку и публичный runtime. Admin bundle встраивается в `/admin/`. Vite proxy по умолчанию направляет `/api`, `/media`, `/health` на `localhost:8080`; `ADMIN_API_PROXY_TARGET` позволяет задать изолированный API во время локальной проверки.

Дополнительные проверки выполняются из `frontend/`: `pnpm lint`, `pnpm test`, `pnpm typecheck`. Картину проверок конкретного изменения см. в `docs/QA_REPORT.md`.
