package features

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"websiteengine/internal/auth"
	"websiteengine/internal/environment"
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items, next, err := h.service.List(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("type"), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullableString(next)})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	if !auth.CanCreateFeature(actor.Role) {
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для создания Feature")
		return
	}
	var in CreateInput
	if err := response.DecodeJSON(w, r, &in, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	detail, err := h.service.Create(r.Context(), actor, in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, detail)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := featureID(w, r)
	if !ok {
		return
	}
	detail, err := h.service.Detail(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, detail)
}

type updateFeatureRequest struct {
	Name string `json:"name"`
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := featureID(w, r)
	if !ok {
		return
	}
	var req updateFeatureRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	feature, err := h.service.UpdateName(r.Context(), id, req.Name)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, feature)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	id, ok := featureID(w, r)
	if !ok {
		return
	}
	feature, err := h.service.Archive(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, feature)
}

func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	id, ok := featureID(w, r)
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

func (h *Handler) Draft(w http.ResponseWriter, r *http.Request) {
	id, ok := featureID(w, r)
	if !ok {
		return
	}
	state, err := h.service.DraftState(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, state)
}

type saveDraftRequest struct {
	Revision int64           `json:"revision"`
	Schema   json.RawMessage `json:"schema"`
}

func (h *Handler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := featureID(w, r)
	if !ok {
		return
	}
	var req saveDraftRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	state, err := h.service.SaveDraft(r.Context(), actor, id, req.Revision, req.Schema)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, state)
}

type revisionRequest struct {
	Revision int64 `json:"revision"`
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, ok := featureID(w, r)
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
	id, ok := featureID(w, r)
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
	id, ok := featureID(w, r)
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

func featureID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Feature не найдена")
		return uuid.Nil, false
	}
	return id, true
}

func nullableString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validationErr *ValidationError
	var schemaErr *SchemaValidationError
	var conflictErr *ConflictError
	switch {
	case errors.As(err, &schemaErr):
		details := make([]response.ErrorDetail, 0, len(schemaErr.Details))
		for _, d := range schemaErr.Details {
			details = append(details, response.ErrorDetail{Path: d.Path, Code: d.Code, Message: d.Message})
		}
		response.Error(w, r, http.StatusUnprocessableEntity, "schema_validation_failed", schemaErr.Message, details...)
	case errors.As(err, &validationErr):
		response.Error(w, r, http.StatusBadRequest, "validation_failed", validationErr.Message)
	case errors.As(err, &conflictErr):
		response.Error(w, r, http.StatusConflict, "conflict", conflictErr.Message)
	case errors.Is(err, ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Feature не найдена")
	case errors.Is(err, ErrConflict):
		response.Error(w, r, http.StatusConflict, "conflict", "Конфликт состояния Feature")
	default:
		h.log.Error("feature operation failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}
