# API

Базовый префикс: `/api/v1`. Все тела запросов и ответов — JSON UTF-8, кроме загрузки медиа
(`multipart/form-data`) и CSV-экспорта. Время — RFC 3339 в UTC. Идентификаторы — UUID.

Авторизация административных маршрутов:

```http
Authorization: Bearer ct_live_...
```

Единый формат ошибки:

```json
{
  "error": {
    "code": "schema_validation_failed",
    "message": "Данные не соответствуют схеме",
    "details": [
      {"path": "title", "code": "required", "message": "missing properties: 'title'"}
    ],
    "request_id": "f2f8cc9e-52bb-4c1c-a0a8-7a5146a62a44"
  }
}
```

Списки используют `limit` и курсор `cursor`. Ответ списка:

```json
{"items": [], "next_cursor": null}
```

`next_cursor` — непрозрачная строка. Ограничения страниц: 200 элементов для
административных списков, 50 для публичных коллекций. Формы: 200 для списка заявок,
10 000 строк для CSV.

Публичные маршруты чтения и отправки форм поддерживают CORS, если задан
`PUBLIC_CORS_ORIGINS`. Административные маршруты CORS не отдают.

## Служебные маршруты

| Метод | Маршрут | Доступ | Ответ |
| --- | --- | --- | --- |
| GET | `/health/live` | публичный | `{"status":"ok"}` |
| GET | `/health/ready` | публичный, без секретов | `{"status":"ready"}` или 503 |
| GET | `/media/{id}` | публичный | 302 на публичный S3 URL |
| GET | `/admin/` | публичный | React-админка |

`/health/ready` проверяет PostgreSQL и доступность S3 bucket.

## Сводка для админки

`GET /admin/overview` (staff) возвращает реальные агрегаты для главной страницы:

```json
{
  "counts": {
    "features_active": 4,
    "features_archived": 0,
    "draft_features": 1,
    "draft_entries": 2,
    "new_submissions": 3,
    "media_ready": 8,
    "failed_media": 0
  },
  "attention": {
    "draft_features": [{"id":"...","key":"services","name":"Услуги","type":"collection","revision":3,"updated_at":"..."}],
    "new_submissions": [{"id":"...","feature_key":"contacts","status":"new","created_at":"...","data_preview":"Иван"}],
    "failed_media": []
  },
  "recent_activity": [
    {"kind":"entry","title":"Запись в «Услуги»","feature_key":"services","occurred_at":"..."}
  ]
}
```

`attention` содержит не более пяти элементов каждого типа, `recent_activity` — не более десяти.

## Токены

| Метод | Маршрут | Доступ |
| --- | --- | --- |
| GET | `/admin/me` | любой действующий токен |
| GET | `/admin/tokens` | admin; moderator — только созданные им |
| POST | `/admin/tokens` | admin — любые роли; moderator — только `user` |
| GET | `/admin/tokens/{id}` | admin; moderator — только созданный им |
| DELETE | `/admin/tokens/{id}` | admin; moderator — только созданный им |

Создание токена:

```json
{"name": "CI deployment", "role": "user", "expires_at": "2027-01-01T00:00:00Z"}
```

`expires_at` можно не передавать или передать `null` для бессрочного токена.

Ответ `201 Created`:

```json
{
  "id": "e091e5a0-a274-4e67-9cc1-9bd2221a2f01",
  "name": "CI deployment",
  "token": "ct_live_<random-secret>",
  "role": "user",
  "expires_at": "2027-01-01T00:00:00Z",
  "created_at": "2026-01-01T00:00:00Z"
}
```

Секрет возвращается один раз. В БД хранится только SHA-256 хеш. `DELETE` отзывает токен
(204), физического удаления нет.

## Features и схемы

Feature:

```json
{
  "id": "uuid",
  "key": "services",
  "name": "Услуги",
  "type": "collection",
  "status": "active",
  "revision": 3,
  "draft_version_id": "uuid|null",
  "published_version_id": "uuid|null",
  "created_by": "uuid|null",
  "created_at": "...",
  "updated_at": "...",
  "draft": {"version": 2, "schema": {}, "status": "draft", "...": "..."},
  "published": {"version": 1, "schema": {}, "status": "published", "published_at": "...", "...": "..."}
}
```

`revision` — оптимистическая блокировка на уровне Feature и записи контента. Клиент обязан
передавать revision, полученный при чтении, в операции сохранения черновика и публикации.
Несовпадение — `409 Conflict`.

| Метод | Маршрут | Назначение |
| --- | --- | --- |
| GET | `/admin/features?status=&type=&q=&limit=&cursor=` | список; `q` ищет по name/key |
| POST | `/admin/features` | создание `{key,name,type}` |
| GET | `/admin/features/{id}` | детали и версии |
| PATCH | `/admin/features/{id}` | изменение `name` |
| POST | `/admin/features/{id}/archive` | архив |
| GET | `/admin/features/{id}/versions?limit=` | история схем (по умолчанию 50, максимум 200) |
| GET | `/admin/features/{id}/draft` | `{revision,draft,published}` |
| PUT | `/admin/features/{id}/draft` | сохранить `{revision,schema}` |
| POST | `/admin/features/{id}/publish` | опубликовать `{revision}` |
| POST | `/admin/features/{id}/rollback` | переключить на `{version}` |

Типы Feature: `collection`, `single`, `form`. Тип нельзя изменить, пока у Feature есть записи.

### JSON Schema и расширения

Поддерживается JSON Schema draft 2020-12. Внешние `$ref` запрещены; схема должна быть
самодостаточной. Пользовательские расширения:

| Расширение | Назначение |
| --- | --- |
| `format: "media"` | строка содержит UUID медиаобъекта |
| `x-ui` | объект с подсказками для UI (`widget`, `placeholder`, `help`, …) |
| `x-private: true` | поле не попадает в публичный ответ |
| `x-public: false` | то же, что `x-private` |

Пример:

```json
{
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "title": "Название",
      "minLength": 1,
      "maxLength": 200,
      "x-ui": {"widget": "text", "placeholder": "Название услуги"}
    },
    "cover": {"type": "string", "format": "media"},
    "internal_note": {"type": "string", "x-private": true}
  },
  "required": ["title"],
  "additionalProperties": false
}
```

При сохранении черновика схема компилируется и проверяется. Публикация атомарна:
версия получает `published`, после чего `features.published_version_id` переключается в той же
транзакции. Опубликованные версии схемы и контента неизменяемы на уровне БД.

`rollback` не удаляет историю и не трогает черновик. Он переключает указатель
`published_version_id` на ранее опубликованную версию.

## Контентные записи

Доступно для `collection` и `single`. Для `form` возвращается `409`.

| Метод | Маршрут | Назначение |
| --- | --- | --- |
| GET | `/admin/features/{id}/entries?include_archived=&q=&limit=&cursor=` | список; `q` ищет по JSON данных |
| POST | `/admin/features/{id}/entries` | создать черновик `{data}` |
| GET | `/admin/entries/{id}` | запись и обе версии |
| PATCH | `/admin/entries/{id}/draft` | `{revision,data}` |
| POST | `/admin/entries/{id}/publish` | `{revision}` |
| POST | `/admin/entries/{id}/rollback` | `{version}` |
| POST | `/admin/entries/{id}/unpublish` | снять с публикации, черновик сохраняется |
| POST | `/admin/entries/{id}/discard-draft` | отменить черновик, вернуться к опубликованной версии |
| POST | `/admin/entries/{id}/archive` | архив |
| GET | `/admin/entries/{id}/versions?limit=` | история (по умолчанию 50, максимум 200) |

Запись:

```json
{
  "id": "uuid",
  "feature_id": "uuid",
  "feature_key": "services",
  "feature_type": "collection",
  "single_slot": false,
  "revision": 2,
  "draft_version_id": null,
  "published_version_id": "uuid",
  "draft": null,
  "published": {
    "id": "uuid",
    "version": 1,
    "schema_version_id": "uuid",
    "data": {"title": "Разработка сайта"},
    "status": "published",
    "published_at": "..."
  }
}
```

Для `single` разрешена только одна активная запись. После архивации запись больше не
занимает слот. Публикация проверяет данные по актуальной опубликованной схеме Feature и
фиксирует `schema_version_id` в одной транзакции.

## Публичное чтение

| Метод | Маршрут | Ответ |
| --- | --- | --- |
| GET | `/content?keys=services,team&limit=20` | `{"data":{...}}` |
| GET | `/content/{key}` | `{"data":{...}}` |
| GET | `/content/{key}/entries?limit=&cursor=` | `{"data":{...},"next_cursor":...}` |

Пример BFF:

```json
{
  "data": {
    "services": {
      "type": "collection",
      "version": 2,
      "entries": [
        {"id": "uuid", "data": {"title": "Разработка сайта"}, "published_at": "..."}
      ]
    },
    "team": {"type": "collection", "version": 1, "entries": []},
    "contacts": {"type": "single", "version": 1, "data": {"email": "hello@example.com"}},
    "feedback": {"type": "form", "version": 1, "schema": {"type": "object", "...": "..."}}
  }
}
```

Возвращаются только активные Feature с опубликованной схемой, только опубликованные записи,
без авторов и черновиков. Поля `x-private`/`x-public:false` вырезаются. Максимум 20 ключей в
одном BFF-запросе; записи каждой коллекции ограничены `limit` (по умолчанию 20, максимум 50).

## Формы

Публичная отправка:

```http
POST /api/v1/forms/{key}/submissions
Content-Type: application/json

{"name":"Иван","email":"ivan@example.com","message":"Хочу обсудить проект"}
```

Тело — сам объект данных, без обёртки. Ответ `201`:

```json
{"id": "uuid", "status": "accepted"}
```

Сервис в одной транзакции читает опубликованную схему, валидирует данные и записывает
заявку с `schema_version_id`. Публичный API не раскрывает содержимое заявки. Действует
лимит частоты по IP (20/мин по умолчанию; настраивается `FORM_RATE_LIMIT`) и лимит размера
JSON.

Административные маршруты:

| Метод | Маршрут | Доступ |
| --- | --- | --- |
| GET | `/admin/forms/{key}/submissions` | staff |
| GET | `/admin/forms/{key}/submissions/export` | staff |
| GET | `/admin/submissions/{id}` | staff |
| PATCH | `/admin/submissions/{id}/status` | staff |
| POST | `/admin/submissions/{id}/archive` | staff |
| DELETE | `/admin/submissions/{id}` | только admin |

Параметры списка:

- `status` — `new`, `in_progress`, `processed`, `rejected`;
- `created_from`, `created_to` — RFC 3339;
- `q` — поиск, `field` — разрешённое поле из схемы формы (иначе поиск идёт по скалярным
  полям схемы, максимум 10);
- `sort` — `asc`/`desc` по дате;
- `include_archived=true`;
- `limit`, `cursor`.

Статус меняется телом `{"status":"in_progress"}`. `DELETE` — мягкое удаление; модератор
может только архивировать.

CSV-экспорт учитывает те же фильтры, значения с потенциальными формулами Excel
экранируются. Максимум 10 000 строк за экспорт.

## Настройки

`GET /admin/settings` (staff) возвращает санитизированную конфигурацию runtime без
секретов: лимиты запросов и страниц, параметры публичного API, допустимые типы файлов и
матрицу ролей. Значения берутся из реальной конфигурации процесса, поэтому раздел
«Настройки» в админке показывает фактические ограничения, а не заглушки.

```json
{
  "limits": {
    "max_json_bytes": 1048576,
    "max_upload_bytes": 20971520,
    "form_rate_limit": 20,
    "form_rate_window_seconds": 60,
    "admin_page_size": 200,
    "public_page_size": 50,
    "submission_export_max_rows": 10000
  },
  "public_api": {
    "content_path": "/api/v1/content",
    "entries_path": "/api/v1/content/{key}/entries",
    "forms_path": "/api/v1/forms/{key}/submissions",
    "admin_path": "/api/v1/admin",
    "media_path": "/media/{id}",
    "cors_enabled": true,
    "cors_origins": ["https://site.example"]
  },
  "media": {
    "accepted_types": ["image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "video/mp4"],
    "max_upload_bytes": 20971520,
    "image_variants": ["original", "thumbnail", "medium", "large"]
  },
  "roles": [
    {
      "id": "admin",
      "label": "Администратор",
      "summary": "Полный доступ ко всем административным разделам.",
      "permissions": [{"resource": "Контент", "actions": ["просмотр", "редактирование", "публикация"]}],
      "notes": ["Единственная роль, которой доступно удаление заявок."]
    }
  ]
}
```

Матрица ролей описывает текущее поведение сервера: она собрана из проверок middleware
(`RequireStaff`, `RequireAdmin`) и проверок в доменных сервисах. Секреты (S3, `DATABASE_URL`,
`BOOTSTRAP_ADMIN_TOKEN`) в ответ не попадают.

## Медиа

| Метод | Маршрут | Доступ |
| --- | --- | --- |
| POST | `/admin/media` | staff, `multipart/form-data`, поле `file` |
| GET | `/admin/media?status=&type=image|video|pdf&q=&created_from=&created_to=&min_size=&max_size=&sort=&limit=&cursor=` | staff |
| GET | `/admin/media/{id}` | staff |
| PATCH | `/admin/media/{id}` | staff, `{"filename":"new-name.png"}` — меняет только отображаемое имя |
| GET | `/admin/media/{id}/usage` | staff — записи и заявки, где используется объект |
| POST | `/admin/media/{id}/retry` | staff |
| DELETE | `/admin/media/{id}` | staff |
| GET | `/media/{id}?variant=thumbnail` | публичный, 302 |

Допустимые типы: JPEG, PNG, GIF, WebP, PDF, MP4. Лимит по умолчанию 20 MiB
(`MAX_UPLOAD_BYTES`). Изображения получают варианты `thumbnail` (320), `medium` (960),
`large` (1600), а также `original`. PDF/MP4 хранятся как оригинал. GIF/PNG/WebP variants
кодируются в PNG, JPEG — в JPEG. Видео-транскодирование не выполняется.

`GET /media/{id}` ищет объект в БД, проверяет статус `ready` и отвечает `302 Found` с
`Location` на публичный URL S3. URL строится только из ключей, сохранённых в БД,
поэтому открытый редирект невозможен. Переменная `variant` ограничена ключами
`variants.json`; по умолчанию используется `original`.

Медиа в контенте хранится как UUID (`format: "media"`), а не как URL.

Параметры списка: `status` — `pending|ready|failed`; `type` — `image|video|pdf`;
`q` ищет по отображаемому имени; `created_from`/`created_to` принимают `YYYY-MM-DD`
или RFC3339, причём `created_to` включает весь указанный день; `min_size`/`max_size`
задаются в байтах; `sort` — `new|old|name|size_desc|size_asc`. `limit` по умолчанию 50,
максимум 100. `cursor` непрозрачен и привязан к выбранной сортировке: смена `sort`
требует начать пагинацию заново.

## Коды ошибок

| HTTP | Код | Причина |
| --- | --- | --- |
| 400 | `invalid_json`, `validation_failed`, `invalid_query` | тело/параметры |
| 401 | `unauthorized` | токен отсутствует, истёк или отозван |
| 403 | `forbidden` | роль не даёт прав |
| 404 | `not_found` | объект не найден |
| 409 | `conflict` | revision, уникальность, состояние |
| 413 | `request_too_large`, `file_too_large` | лимит размера |
| 415 | `unsupported_media_type` | тип файла |
| 422 | `schema_validation_failed`, `media_processing_failed` | данные не соответствуют схеме |
| 429 | `rate_limited` | лимит публичных отправок |
| 500 | `internal_error` | необработанная ошибка |
| 503 | `auth_unavailable` | временная недоступность БД |

Внутренние SQL-ошибки, имена таблиц, пути и секреты клиенту не возвращаются.
