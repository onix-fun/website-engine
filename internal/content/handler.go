package content

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"websiteengine/internal/auth"
	"websiteengine/internal/environment"
	"websiteengine/internal/features"
	"websiteengine/internal/transport/http/response"
)

type Handler struct {
	service *Service
	log     *slog.Logger
	maxJSON int64
}

func NewHandler(service *Service, log *slog.Logger, maxJSON int64) *Handler {
	return &Handler{service: service, log: log, maxJSON: maxJSON}
}

func (h *Handler) ListEntries(w http.ResponseWriter, r *http.Request) {
	featureID, ok := uuidParam(w, r, "id", "Feature не найдена")
	if !ok {
		return
	}
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	items, next, err := h.service.List(r.Context(), featureID, includeArchived, r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullable(next)})
}

type dataRequest struct {
	Data json.RawMessage `json:"data"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	featureID, ok := uuidParam(w, r, "id", "Feature не найдена")
	if !ok {
		return
	}
	var req dataRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	detail, err := h.service.Create(r.Context(), actor, featureID, req.Data)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, detail)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	detail, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type draftRequest struct {
	Revision int64           `json:"revision"`
	Data     json.RawMessage `json:"data"`
}

func (h *Handler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	var req draftRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	detail, err := h.service.SaveDraft(r.Context(), actor, id, req.Revision, req.Data)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type revisionRequest struct {
	Revision int64 `json:"revision"`
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	var req revisionRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	detail, err := h.service.Publish(r.Context(), actor, id, req.Revision)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type promoteRequest struct {
	Revision int64 `json:"revision"`
}

func (h *Handler) Promote(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	var req promoteRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	detail, err := h.service.Promote(r.Context(), actor, id, req.Revision)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type rollbackRequest struct {
	Version     int    `json:"version"`
	Environment string `json:"environment"`
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	var req rollbackRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	env, err := environment.Parse(req.Environment)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_environment", err.Error())
		return
	}
	if !auth.CanRollbackContent(actor.Role, env) {
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для отката в этой среде")
		return
	}
	detail, err := h.service.Rollback(r.Context(), actor, id, req.Version, env)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type environmentRequest struct {
	Environment string `json:"environment"`
}

func (h *Handler) Unpublish(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	var req environmentRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	env, err := environment.Parse(req.Environment)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_environment", err.Error())
		return
	}
	if !auth.CanUnpublishContent(actor.Role, env) {
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для снятия с публикации в этой среде")
		return
	}
	detail, err := h.service.Unpublish(r.Context(), actor, id, env)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

// DiscardDraft drops the draft pointer and keeps the published version.
func (h *Handler) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	detail, err := h.service.DiscardDraft(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	detail, err := h.service.Archive(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id", "Запись не найдена")
	if !ok {
		return
	}
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	versions, err := h.service.Versions(r.Context(), id, limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": versions})
}

// PublicContent handles GET /api/v1/content?keys=a,b.
func (h *Handler) PublicContent(w http.ResponseWriter, r *http.Request) {
	rawKeys := strings.TrimSpace(r.URL.Query().Get("keys"))
	if rawKeys == "" {
		response.Error(w, r, http.StatusBadRequest, "keys_required", "Параметр keys обязателен")
		return
	}
	fields := strings.Split(rawKeys, ",")
	keys := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	for _, key := range fields {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		if len(key) > 100 {
			response.Error(w, r, http.StatusBadRequest, "invalid_key", "Некорректный ключ Feature")
			return
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 || len(keys) > 20 {
		response.Error(w, r, http.StatusBadRequest, "invalid_keys", "Запросите от 1 до 20 ключей Feature")
		return
	}
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 20, 50)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	data, err := h.service.PublicContent(r.Context(), environment.FromContext(r.Context()), keys, limit)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Handler) PublicFeature(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 20, 50)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	data, err := h.service.PublicFeature(r.Context(), environment.FromContext(r.Context()), key, limit)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Handler) PublicEntries(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 20, 50)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	data, next, err := h.service.PublicEntries(r.Context(), environment.FromContext(r.Context()), key, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"data": data, "next_cursor": nullable(next)})
}

func uuidParam(w http.ResponseWriter, r *http.Request, name, notFound string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", notFound)
		return uuid.Nil, false
	}
	return id, true
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *ValidationError
	var conflictErr *ConflictError
	switch {
	case errors.As(err, &validationErr):
		details := make([]response.ErrorDetail, 0, len(validationErr.Details))
		for _, d := range validationErr.Details {
			details = append(details, response.ErrorDetail{Path: d.Path, Code: d.Code, Message: d.Message})
		}
		if len(details) > 0 {
			response.Error(w, r, http.StatusUnprocessableEntity, "schema_validation_failed", validationErr.Message, details...)
			return
		}
		response.Error(w, r, http.StatusBadRequest, "validation_failed", validationErr.Message)
	case errors.As(err, &conflictErr):
		response.Error(w, r, http.StatusConflict, "conflict", conflictErr.Message)
	case errors.Is(err, features.ErrNotFound), errors.Is(err, ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Запись или Feature не найдены")
	case errors.Is(err, ErrConflict):
		response.Error(w, r, http.StatusConflict, "conflict", "Конфликт состояния записи")
	case errors.Is(err, ErrValidation):
		response.Error(w, r, http.StatusBadRequest, "validation_failed", "Некорректные данные")
	default:
		h.log.Error("content operation failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}

func (h *Handler) writePublicError(w http.ResponseWriter, r *http.Request, err error) {
	var conflictErr *ConflictError
	switch {
	case errors.As(err, &conflictErr):
		response.Error(w, r, http.StatusConflict, "conflict", conflictErr.Message)
	case errors.Is(err, ErrNotFound), errors.Is(err, features.ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Опубликованный контент не найден")
	case errors.Is(err, ErrValidation):
		response.Error(w, r, http.StatusBadRequest, "validation_failed", "Некорректный запрос")
	default:
		h.log.Error("public content failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}
