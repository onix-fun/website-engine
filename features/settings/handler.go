// Package settings exposes a read-only, sanitized view of the runtime
// configuration. The admin panel renders it in the "Настройки" section so
// operators see the real limits and the real permission matrix instead of
// hardcoded placeholders.
//
// No secret ever leaves this package: S3 credentials, database URLs and
// bootstrap tokens are intentionally absent.
package settings

import (
	"net/http"

	"websiteengine/presentation/http/response"
)

// Limits mirrors the request and pagination limits enforced by the server.
type Limits struct {
	MaxJSONBytes            int64 `json:"max_json_bytes"`
	MaxUploadBytes          int64 `json:"max_upload_bytes"`
	FormRateLimit           int   `json:"form_rate_limit"`
	FormRateWindowSeconds   int   `json:"form_rate_window_seconds"`
	AdminPageSize           int   `json:"admin_page_size"`
	PublicPageSize          int   `json:"public_page_size"`
	SubmissionExportMaxRows int   `json:"submission_export_max_rows"`
}

// PublicAPI describes the publicly reachable API surface.
type PublicAPI struct {
	ContentPath string   `json:"content_path"`
	EntriesPath string   `json:"entries_path"`
	FormsPath   string   `json:"forms_path"`
	AdminPath   string   `json:"admin_path"`
	MediaPath   string   `json:"media_path"`
	CORSEnabled bool     `json:"cors_enabled"`
	CORSOrigins []string `json:"cors_origins"`
}

// Media describes the accepted upload formats and the generated variants.
type Media struct {
	AcceptedTypes  []string `json:"accepted_types"`
	MaxUploadBytes int64    `json:"max_upload_bytes"`
	ImageVariants  []string `json:"image_variants"`
}

// RolePermission is a single row of the permission matrix.
type RolePermission struct {
	Resource string   `json:"resource"`
	Actions  []string `json:"actions"`
}

// Role describes what a role may do. The actions listed here mirror the
// checks implemented by the HTTP middleware and the domain services.
type Role struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Summary     string           `json:"summary"`
	Permissions []RolePermission `json:"permissions"`
	Notes       []string         `json:"notes"`
}

// Settings is the payload of GET /api/v1/admin/settings.
type Settings struct {
	Limits    Limits    `json:"limits"`
	PublicAPI PublicAPI `json:"public_api"`
	Media     Media     `json:"media"`
	Roles     []Role    `json:"roles"`
}

// Config carries only the non-sensitive runtime values needed to build the
// payload. The caller decides which values to pass in.
type Config struct {
	MaxJSONBytes            int64
	MaxUploadBytes          int64
	FormRateLimit           int
	FormRateWindowSeconds   int
	PublicCORSOrigins       []string
	AdminPageSize           int
	PublicPageSize          int
	SubmissionExportMaxRows int
}

// Handler serves the sanitized settings payload.
type Handler struct {
	settings Settings
}

// NewHandler builds a handler from the sanitized runtime configuration.
func NewHandler(cfg Config) *Handler {
	return &Handler{settings: build(cfg)}
}

// Get responds with the sanitized settings payload.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, h.settings)
}

func build(cfg Config) Settings {
	origins := cfg.PublicCORSOrigins
	if origins == nil {
		origins = []string{}
	}
	return Settings{
		Limits: Limits{
			MaxJSONBytes:            cfg.MaxJSONBytes,
			MaxUploadBytes:          cfg.MaxUploadBytes,
			FormRateLimit:           cfg.FormRateLimit,
			FormRateWindowSeconds:   cfg.FormRateWindowSeconds,
			AdminPageSize:           cfg.AdminPageSize,
			PublicPageSize:          cfg.PublicPageSize,
			SubmissionExportMaxRows: cfg.SubmissionExportMaxRows,
		},
		PublicAPI: PublicAPI{
			ContentPath: "/api/v1/content",
			EntriesPath: "/api/v1/content/{key}/entries",
			FormsPath:   "/api/v1/forms/{key}/submissions",
			AdminPath:   "/api/v1/admin",
			MediaPath:   "/media/{id}",
			CORSEnabled: len(origins) > 0,
			CORSOrigins: origins,
		},
		Media: Media{
			AcceptedTypes: []string{
				"image/jpeg",
				"image/png",
				"image/gif",
				"image/webp",
				"application/pdf",
				"video/mp4",
			},
			MaxUploadBytes: cfg.MaxUploadBytes,
			ImageVariants:  []string{"original", "thumbnail", "medium", "large"},
		},
		Roles: roles(),
	}
}

// roles mirrors the access rules implemented by the server: RequireStaff and
// RequireAdmin middleware plus the per-resource checks in the domain
// services. It is a description of the current behaviour, not an aspirational
// model.
func roles() []Role {
	return []Role{
		{
			ID:      "admin",
			Label:   "Администратор",
			Summary: "Полный доступ, включая prod-релизы и управление доступами.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр", "создание", "переименование", "архивирование"}},
				{Resource: "Схемы", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev/prod"}},
				{Resource: "Контент", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev/prod", "снятие с публикации", "архивирование"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "смена статуса", "архивирование", "удаление"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование", "удаление"}},
				{Resource: "Релизы", Actions: []string{"просмотр", "импорт", "активация dev/prod", "откат dev/prod", "источники"}},
				{Resource: "Токены", Actions: []string{"просмотр всех", "создание любой роли", "отзыв"}},
				{Resource: "Настройки", Actions: []string{"просмотр"}},
			},
			Notes: []string{
				"Единственная роль, которой доступно удаление заявок и активация релизов в prod.",
				"Видит все токены, независимо от того, кто их создал.",
			},
		},
		{
			ID:      "moderator",
			Label:   "Модератор",
			Summary: "Схемы, контент dev/prod и активация релизов в dev.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр", "создание", "переименование", "архивирование"}},
				{Resource: "Схемы", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev"}},
				{Resource: "Контент", Actions: []string{"просмотр", "редактирование", "публикация в dev", "promote в prod", "откат dev", "снятие с публикации dev", "архивирование"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "смена статуса", "архивирование"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование", "удаление"}},
				{Resource: "Релизы", Actions: []string{"просмотр", "импорт", "активация dev", "источники"}},
				{Resource: "Токены", Actions: []string{"просмотр созданных", "создание роли user", "отзыв созданных"}},
				{Resource: "Настройки", Actions: []string{"просмотр"}},
			},
			Notes: []string{
				"Видит только те токены, которые создал сам.",
				"Не может создавать токены с ролями admin и moderator.",
				"Не может активировать релизы и откатывать prod.",
				"Удаление заявок недоступно.",
			},
		},
		{
			ID:      "user",
			Label:   "Пользователь",
			Summary: "Контент и dev-публикации, медиа, просмотр заявок.",
			Permissions: []RolePermission{
				{Resource: "Features", Actions: []string{"просмотр"}},
				{Resource: "Схемы", Actions: []string{"просмотр"}},
				{Resource: "Контент", Actions: []string{"просмотр", "создание", "редактирование", "публикация в dev", "снятие с публикации dev"}},
				{Resource: "Заявки", Actions: []string{"просмотр", "экспорт"}},
				{Resource: "Медиа", Actions: []string{"просмотр", "загрузка", "переименование"}},
				{Resource: "Релизы", Actions: []string{"просмотр"}},
				{Resource: "Токены", Actions: []string{}},
				{Resource: "Настройки", Actions: []string{}},
			},
			Notes: []string{
				"Не может менять схемы, настройки Feature, архивировать и удалять данные.",
				"Не может продвигать контент в prod и откатывать версии.",
			},
		},
	}
}
