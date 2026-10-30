# Website Engine

Движок для сайтов, собираемых из независимо версионируемых frontend-фич. Один экземпляр обслуживает один сайт: хранит feature builds, данные и снапшоты, отдаёт встроенный публичный runtime и админку, управляет медиа, заявками, доменами, TLS и CORS.

## Быстрый старт

```bash
cp .env.example .env
# Set ENGINE_IMAGE to the engine's GitHub Container Registry package.
docker compose pull server
docker compose up -d --no-build
```

- Админка и API: <http://localhost:8080/admin/>
- Публичная dev-среда: <http://localhost:8081/>
- Публичная prod-среда: <http://localhost:8082/>
- MinIO console: <http://localhost:9001> (`minioadmin` / `minioadmin`)

`ENGINE_IMAGE` указывает на опубликованный в GHCR образ. Compose не собирает движок на этой машине и всегда проверяет registry при запуске. Для приватного пакета выполните `docker login ghcr.io` с токеном, имеющим `read:packages`. Перед запуском замените тестовые токены и пароли из `.env.example`.

## Сборка и выпуск образа

Workflow [engine-image.yml](.github/workflows/engine-image.yml) запускает проверки на pull request и push в `main`. Публикация в GHCR запускается при push SemVer-тега (`v1.2.3`) или публикации GitHub Release. Workflow публикует версии `1.2.3`, `1.2` и `latest` для стабильного релиза. Имя образа формируется из `GITHUB_REPOSITORY`: `ghcr.io/<owner>/<repository>`.

Чтобы выпустить версию, создайте SemVer-тег (`git tag v1.2.3 && git push origin v1.2.3`) либо опубликуйте GitHub Release с таким тегом. Чтобы обновить локальный движок до конкретного релиза, укажите его тег в `ENGINE_IMAGE` в `.env`, затем выполните `make compose-up`. Команда сначала получает образ из registry и запускает Compose без локальной сборки. Чтобы разрешить скачивание без входа в GHCR, после первой публикации сделайте package публичным в настройках GitHub Packages.

## Как собирается сайт

```text
репозиторий сайта                         Website Engine
┌────────────────────────┐                ┌───────────────────────────┐
│ core/                  │  feature ZIPs  │ admin + public runtime    │
│ features/<key>/        ├───────────────►│ feature build registry    │
│   manifest.json        │                │ visual page/route editor  │
│   MVC + SCSS            │                │ dev/prod snapshots        │
└────────────────────────┘                └───────────────────────────┘
```

В репозитории сайта исходниками остаются `core` и независимые `features`; в CI каждая фича собирается в отдельный ESM ZIP. Архив содержит `manifest.json`, entry-модуль, CSS и ресурсы. Движок проверяет архив и digest, хранит его неизменяемо и не компилирует загруженный код. Публичный runtime и React обслуживаются движком; страницы подгружают только нужные feature chunks.

Manifest — кодовый контракт разработчика: версия ABI, режим данных (`single`, `collection`, `form`), JSON Schema аргументов и содержимого, места размещения и UI-метаданные полей. Схемы не редактируются в админке. Администратор загружает сборки и собирает страницы/маршруты визуально; JSON-импорт контента доступен только как отдельная операция.

Сайт публикуется неизменяемыми снапшотами:

1. Черновик фиксирует страницы, маршруты, размещения, bindings аргументов и выбранные сборки.
2. Публикация атомарно фиксирует конфигурацию, digest сборок и опубликованные версии контента в dev.
3. В prod продвигается тот же снапшот; откат переключает среду на ранее созданный снапшот.

Формы валидируются схемой активного снапшота. Заявки и их статусы хранятся отдельно и переживают публикации и откаты. Коллекции сохраняют ручной порядок, а runtime запрашивает записи с серверной фильтрацией и пагинацией.

## Миграция текущего сайта

Скрипт `scripts/migrate-legacy-site.mjs` переносит feature ZIP-сборки, файлы из `backend/assets`, контент из `backend/data`, маршруты и композиции в dev-снапшот движка. Он предназначен для одноразового запуска на staging после проверки исходных данных:

```bash
ENGINE_URL=http://localhost:8080 \
ENGINE_TOKEN='...' \
LEGACY_SITE_DIR='/path/to/vitasil' \
node scripts/migrate-legacy-site.mjs
```

Перед этим соберите feature archives в репозитории сайта: `npm ci && npm run build` из `frontend/`. Скрипт публикует только в dev. Проверьте страницу и контент, затем продвиньте полученный снапшот в prod через `/admin/site`. Старый nginx-стек можно оставить развёрнутым отдельно на время staging и использовать для отката трафика.

## Архитектура движка

- `features/sitebuilder` — manifests, проверка ZIP, реестр сборок, маршруты и снапшоты.
- `features/content`, `features/forms`, `features/media` — данные, заявки и медиахранилище.
- `features/releases` — настройки сред, домены, TLS и CORS; старые таблицы релизов сохраняются для staging-перехода.
- `presentation/http` — API, middleware и публичные маршрутизаторы.
- `pkg/runtime` — встроенный публичный runtime shell.
- `frontend/src/pages/site` — визуальная сборка страниц, bindings аргументов и управление снапшотами.
- `frontend/src/domains/content-schema` — формы контента, генерируемые по manifest-backed JSON Schema.

Ограничения зависимостей: доменная логика находится в `features/<name>`, `pkg` не зависит от `features`, HTTP-слой не обращается к БД напрямую. Внешний runtime получает композицию через `/api/v2/runtime`; в него не добавлен адаптер к старой структуре статических JSON-файлов.

## Локальная разработка

```bash
docker compose up -d db minio
export DATABASE_URL='postgres://engine:engine@localhost:5432/engine?sslmode=disable'
export BOOTSTRAP_ADMIN_TOKEN='dev-admin-token-change-me-please'
export S3_ENDPOINT='http://localhost:9000'
export S3_PUBLIC_BASE_URL='http://localhost:9000'
export S3_ENSURE_BUCKET=true
```

Админка и публичный runtime встраиваются в Go-бинарь. Dockerfile собирает оба frontend bundle до сборки backend:

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend build
pnpm --dir frontend build:runtime
go run ./cmd/api
```

В репозитории сайта сборка отдельных feature ZIP-архивов проверяется так:

```bash
npm ci --prefix frontend
npm run lint --prefix frontend
npm run build --prefix frontend
```

## Проверки

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend build
go test ./...
go vet ./...
pnpm --dir frontend lint
pnpm --dir frontend typecheck
```

Интеграционные тесты требуют отдельную тестовую PostgreSQL базу. Они очищают таблицы, поэтому не направляйте на неё рабочие данные:

```bash
docker compose up -d db
pnpm --dir frontend build
TEST_DATABASE_URL='postgres://engine:engine@localhost:5432/engine?sslmode=disable' go test ./cmd/integration/ -count=1
```

Контракты и последовательность миграции описаны в [docs/FEATURE_SNAPSHOTS.md](docs/FEATURE_SNAPSHOTS.md), HTTP API — в [docs/API.md](docs/API.md).
