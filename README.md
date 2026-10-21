# Website Engine

Движок для контентных сайтов: Go-сервис с PostgreSQL, JSON-схемами, версионируемым
контентом, формами, медиахранилищем в S3 и встроенной React-админкой.

Один процесс отдаёт:

- React-админку по `/admin/`;
- REST API по `/api/v1/`;
- публичные редиректы медиа по `/media/{id}`;
- служебные проверки `/health/live` и `/health/ready`.

## Быстрый старт

```bash
cp .env.example .env
docker compose up --build
```

- Админка: <http://localhost:8080/admin/>
- API: <http://localhost:8080/api/v1/content?keys=services>
- MinIO console: <http://localhost:9001> (`minioadmin` / `minioadmin`)
- Токен из локального `.env.example`: `dev-admin-token-change-me-please`

Токен администратора создаётся один раз при первом запуске, если активного admin-токена
ещё нет. Смените `BOOTSTRAP_ADMIN_TOKEN` и пароли перед любым публичным развёртыванием.
После первого запуска значение переменной больше не перезаписывает существующий токен.

Остановка: `docker compose down`. Удаление данных: `docker compose down -v`.

## Возможности

- PostgreSQL 16, миграции применяются автоматически и идемпотентно.
- API-токены с ролями `admin`, `moderator`, `user`; в БД хранится только SHA-256 хеш,
  секрет показывается один раз.
- Политика модератора: управляет контентом и заявками, создаёт только `user`-токены и
  видит только созданные им токены; не удаляет заявки физически.
- Features трёх типов: `collection`, `single`, `form`.
- Неизменяемые опубликованные версии JSON Schema и контента; отдельный редактируемый
  черновик; публикация и откат.
- JSON Schema draft 2020-12 с расширениями `x-ui`, `x-private`, `x-public`,
  `format: "media"`.
- Оптимистическая блокировка через `revision` для черновиков схемы и записей.
- Публичный BFF для нескольких Feature, публичные коллекции с курсором.
- Формы с серверной валидацией по опубликованной схеме, статусами заявок, фильтрами,
  поиском по разрешённым полям, CSV-экспортом и rate limit.
- Медиа в S3: загрузка, варианты изображений, публичный `302`-редирект, безопасное
  построение URL, повторная обработка и мягкое удаление.
- Встроенная React/TypeScript-админка (без отдельного сервера).
- Структурированные JSON-логи, request_id, единый формат ошибок, security headers.

## Архитектура

```text
cmd/server                 сборка приложения, конфигурация, graceful shutdown
internal/auth              токены, роли, bootstrap
internal/features          Feature, версии JSON Schema, публикация/rollback
internal/content           контентные записи, версии, публичное чтение
internal/forms             отправки форм, фильтры, CSV
internal/media             метаданные S3, обработка изображений, 302
internal/storage/s3        AWS SDK v2, S3-совместимое хранилище
internal/database          pgxpool и мигратор
internal/platform/validate JSON Schema и публичная проекция данных
internal/transport/http    маршруты, middleware, JSON-ответы
internal/transport/static  встроенная сборка React-админки
migrations                 PostgreSQL-миграции
frontend                   React + TypeScript + Vite
docs/API.md                контракт API
```

### Модель данных

| Таблица | Назначение |
| --- | --- |
| `access_tokens` | хеши токенов, роли, срок действия, отзыв |
| `features` | идентичность Feature, тип, статус, указатели версий |
| `feature_versions` | неизменяемые версии JSON Schema |
| `feature_entries` | идентичность контентных записей, revision |
| `feature_entry_versions` | черновики и опубликованные версии контента |
| `form_submissions` | заявки, статус обработки, мягкое удаление |
| `media_objects` | метаданные S3 и варианты файлов |

Помимо ограничений и индексов, миграция создаёт PL/pgSQL-триггеры, которые:

- запрещают изменение/удаление опубликованных версий схемы и контента;
- не дают записи ссылаться на схему чужой Feature;
- требуют опубликованную схему для публикуемой версии контента и заявки;
- следят за тем, чтобы у `single` была ровно одна активная запись;
- запрещают контентные записи для `form` и смену типа Feature при наличии записей;
- проверяют согласованность указателей `draft_version_id`/`published_version_id`.

Для `single` уникальность действует только среди неархивных записей: после архивации можно
создать замену. Схемы и данные хранятся в `JSONB`, а идентификаторы, статусы и временные
метки — в обычных колонках.

### Публикация

Сохранение черновика и публикация выполняются в транзакции с `SELECT ... FOR UPDATE`.
Клиент передаёт `revision`, полученный при чтении; при расхождении возвращается `409`.
Публикация схемы сначала атомарно помечает версию опубликованной и переключает
`features.published_version_id`, затем освобождает указатель черновика. Старые версии
остаются в истории.

Публикация записи проверяет данные по актуальной опубликованной схеме Feature и
фиксирует `schema_version_id`. Отправка формы также захватывает Feature на время чтения
схемы и вставки заявки, поэтому форма не может быть провалидирована по одной версии,
а сохранена с другой.

## Конфигурация

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `DATABASE_URL` | — (обязательна) | PostgreSQL DSN |
| `MIGRATE_ON_START` | `true` | применять миграции при старте |
| `BOOTSTRAP_ADMIN_TOKEN` | — | создать admin-токен, если активных admin нет; минимум 24 символа |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `MAX_JSON_BYTES` | `1048576` | максимальный размер JSON-тела |
| `MAX_UPLOAD_BYTES` | `20971520` | максимальный размер медиафайла |
| `FORM_RATE_LIMIT` | `20` | публичных отправок формы на IP в минуту |
| `TRUST_PROXY_HEADERS` | `false` | доверять `X-Forwarded-For`/`X-Real-IP` |
| `PUBLIC_CORS_ORIGINS` | — | список origin через запятую для публичного API (`https://site.example`); `*` разрешает все |
| `S3_ENDPOINT` | `http://localhost:9000` | endpoint S3 |
| `S3_REGION` | `us-east-1` | регион |
| `S3_BUCKET` | `media` | bucket |
| `S3_ACCESS_KEY` | `minioadmin` | access key |
| `S3_SECRET_KEY` | `minioadmin` | secret key |
| `S3_PUBLIC_BASE_URL` | `http://localhost:9000` | публичный базовый URL хранилища |
| `S3_FORCE_PATH_STYLE` | `true` | path-style для MinIO/совместимых S3 |
| `S3_ENSURE_BUCKET` | `false` | создать bucket и public-read policy при старте (в compose — `true`) |
| `TZ` | — | API принудительно отдаёт время в UTC |

## Локальная разработка

### Backend

```bash
docker compose up -d db minio
export DATABASE_URL='postgres://engine:engine@localhost:5432/engine?sslmode=disable'
export BOOTSTRAP_ADMIN_TOKEN='dev-admin-token-change-me-please'
export S3_ENDPOINT='http://localhost:9000'
export S3_PUBLIC_BASE_URL='http://localhost:9000'
export S3_ENSURE_BUCKET=true
go run ./cmd/server
```

Сервер сам применит миграции. Админка будет доступна после сборки frontend (см. ниже).

### Frontend

```bash
pnpm --dir frontend install
pnpm --dir frontend dev
```

Vite dev server: <http://localhost:5173/admin/>, прокси `/api` и `/media` на `:8080`.
Production-сборка встраивается в Go-бинарь:

```bash
pnpm --dir frontend build
go run ./cmd/server
```

Сборка пишется в `internal/transport/static/dist/` и встраивается через `go:embed`.

### Тесты

```bash
go test ./...
go vet ./...
```

Интеграционные тесты требуют отдельную disposable PostgreSQL-базу:

```bash
docker run -d --name engine-test-db \
  -e POSTGRES_USER=engine -e POSTGRES_PASSWORD=engine -e POSTGRES_DB=engine_test \
  -p 55432:5432 postgres:16-alpine

TEST_DATABASE_URL='postgres://engine:engine@localhost:55432/engine_test?sslmode=disable' \
  go test -v ./internal/integration
```

Тесты создают схему миграциями, очищают таблицы, используют in-memory fake S3 и проверяют
полный жизненный цикл Features, контента, форм, токенов, медиа, CSV, конкурентную
публикацию и ограничения БД. **Не направляйте `TEST_DATABASE_URL` на рабочую базу: тесты
очищают таблицы.**

## API и примеры

Подробный контракт: [docs/API.md](docs/API.md).

```bash
TOKEN='dev-admin-token-change-me-please'
API='http://localhost:8080/api/v1'

# создать Feature
curl -s -X POST "$API/admin/features" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"key":"services","name":"Услуги","type":"collection"}'

# сохранить черновик схемы (revision — из ответа предыдущей операции)
curl -s -X PUT "$API/admin/features/<id>/draft" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"revision":1,"schema":{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}}'

# опубликовать
curl -s -X POST "$API/admin/features/<id>/publish" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"revision":2}'

# создать и опубликовать запись
curl -s -X POST "$API/admin/features/<id>/entries" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"data":{"title":"Разработка сайта"}}'

# публичное чтение
curl -s "$API/content?keys=services"
```

Поля с `x-private: true` не попадают в `/content`, даже если они сохранены в данных.

## Принятые решения и ограничения

- В стартовую модель добавлены `revision` для `features` и `feature_entries` — иначе
  невозможно реализовать заявленную оптимистическую блокировку.
- `form_submissions` получила `archived_at`; `deleted_at` используется для мягкого
  удаления администратором.
- Уникальность `single` ограничена активными записями, чтобы архивированную запись можно
  было заменить.
- Черновик схемы/контента — одна редактируемая версия; каждая публикация создаёт
  неизменяемую версию. Номер версии монотонно растёт.
- Админка — собственная реализация на React/CSS; API и модель от UI не зависят.
- Публичный S3 bucket обязателен для модели `302`-редиректа. Приватные файлы требуют
  другой политики и подписанных URL.
- Варианты изображений: 320/960/1600, WebP/PNG/GIF перекодируются в PNG, JPEG — в JPEG.
  Видео не транскодируется.
- `format: "media"` проверяет UUID, но не существование медиаобъекта при публикации.
- Не реализованы: журнал аудита, идемпотентность публичных форм (`Idempotency-Key`),
  инвалидация внешнего кеша, автоматическая очистка неиспользуемых медиа, фоновая очередь
  обработки видео, ограничение токенов по отдельным Feature.
- Rate limit публичных форм хранится в памяти процесса. При нескольких экземплярах нужен
  общий limiter.
- Публичный API по умолчанию доступен только same-origin. Для внешнего frontend задайте
  `PUBLIC_CORS_ORIGINS`; CORS применяется только к публичному чтению и отправке форм.
- Внешние `$ref` в JSON Schema запрещены, чтобы администратор не мог инициировать
  сетевые запросы с сервера.
- Для production рекомендуется использовать managed PostgreSQL/S3, TLS на прокси,
  секреты вне `.env`, `TRUST_PROXY_HEADERS=true` только за доверенным прокси,
  резервное копирование БД и отдельную роль PostgreSQL без `SUPERUSER`/`BYPASSRLS` —
  иначе триггеры неизменяемости можно обойти правами суперпользователя.
