package sitebuilder

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	formsFeature "websiteengine/features/forms"
	"websiteengine/features/sitebuilder"
	"websiteengine/pkg/environment"
	"websiteengine/pkg/principal"
	"websiteengine/pkg/response"
)

type SnapshotsHandler struct {
	service *sitebuilder.SnapshotService
	forms   *formsFeature.Service
	log     *slog.Logger
	maxJSON int64
}

func NewSnapshotsHandler(service *sitebuilder.SnapshotService, forms *formsFeature.Service, log *slog.Logger, maxJSON int64) *SnapshotsHandler {
	return &SnapshotsHandler{service: service, forms: forms, log: log, maxJSON: maxJSON}
}

func (h *SnapshotsHandler) Draft(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.GetDraft(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, state)
}

type saveDraftRequest struct {
	Revision int64           `json:"revision"`
	Config   json.RawMessage `json:"config"`
}

func (h *SnapshotsHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	actor, _ := principal.FromContext(r.Context())
	var req saveDraftRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	if len(req.Config) == 0 {
		response.Error(w, r, http.StatusBadRequest, "invalid_config", "Config is required")
		return
	}
	state, err := h.service.SaveDraft(r.Context(), actor.TokenID, req.Revision, req.Config)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, state)
}

type publishRequest struct {
	Name string `json:"name"`
}

func (h *SnapshotsHandler) Publish(w http.ResponseWriter, r *http.Request) {
	actor, _ := principal.FromContext(r.Context())
	var req publishRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	record, err := h.service.Publish(r.Context(), actor.TokenID, strings.TrimSpace(req.Name))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, record)
}

func (h *SnapshotsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, err := response.ParseLimit(r.URL.Query().Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items, err := h.service.List(r.Context(), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	active := map[string]string{"dev": "", "prod": ""}
	for _, env := range []environment.Environment{environment.Dev, environment.Prod} {
		if record, _, activeErr := h.service.Active(r.Context(), env); activeErr == nil {
			active[env.String()] = record.ID.String()
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "active": active})
}

func (h *SnapshotsHandler) Promote(w http.ResponseWriter, r *http.Request) {
	actor, _ := principal.FromContext(r.Context())
	id, ok := snapshotID(w, r)
	if !ok {
		return
	}
	record, err := h.service.Promote(r.Context(), actor.TokenID, id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, record)
}

type rollbackRequest struct {
	Environment string `json:"environment"`
}

func (h *SnapshotsHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	actor, _ := principal.FromContext(r.Context())
	id, ok := snapshotID(w, r)
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
	record, err := h.service.Rollback(r.Context(), actor.TokenID, id, env)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, record)
}

func (h *SnapshotsHandler) Runtime(w http.ResponseWriter, r *http.Request) {
	requestPath := r.URL.Query().Get("path")
	if requestPath == "" {
		requestPath = "/"
	}
	composition, err := h.service.Runtime(r.Context(), environment.FromContext(r.Context()), requestPath)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, composition)
}

func (h *SnapshotsHandler) ContentCatalog(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ContentCatalog(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *SnapshotsHandler) Collection(w http.ResponseWriter, r *http.Request) {
	offset, err := parseNonnegative(r.URL.Query().Get("offset"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_offset", "offset must be a non-negative integer")
		return
	}
	limit, err := parseNonnegative(r.URL.Query().Get("limit"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer")
		return
	}
	page, err := h.service.Collection(r.Context(), environment.FromContext(r.Context()), r.PathValue("key"), r.URL.Query().Get("field"), r.URL.Query().Get("value"), offset, limit)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, page)
}

type formRequest struct {
	Data json.RawMessage `json:"data"`
}

func (h *SnapshotsHandler) SubmitForm(w http.ResponseWriter, r *http.Request) {
	var req formRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	key := r.PathValue("key")
	env := environment.FromContext(r.Context())
	schemaID, schema, err := h.service.FormSchema(r.Context(), env, key)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	submission, err := h.forms.SubmitWithSchema(r.Context(), env, key, schemaID, schema, req.Data)
	if err != nil {
		h.writePublicError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]string{"id": submission.ID.String(), "status": submission.Status})
}

func (h *SnapshotsHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "site_builder_error"
	switch {
	case errors.Is(err, sitebuilder.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_site_config"
	case errors.Is(err, sitebuilder.ErrConflict):
		status, code = http.StatusConflict, "site_builder_conflict"
	case errors.Is(err, sitebuilder.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	}
	if status == http.StatusInternalServerError && h.log != nil {
		h.log.Error("site builder request failed", "request_id", response.RequestID(r.Context()), "error", err)
	}
	response.Error(w, r, status, code, err.Error())
}

func (h *SnapshotsHandler) writePublicError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusInternalServerError, "runtime_error"
	switch {
	case errors.Is(err, sitebuilder.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, sitebuilder.ErrSnapshotUnavailable):
		status, code = http.StatusServiceUnavailable, "snapshot_unavailable"
	case errors.Is(err, sitebuilder.ErrConflict):
		status, code = http.StatusConflict, "ambiguous_content"
	case errors.Is(err, sitebuilder.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_query"
	case errors.Is(err, formsFeature.ErrValidation):
		status, code = http.StatusBadRequest, "invalid_form"
	case errors.Is(err, formsFeature.ErrNotFound):
		status, code = http.StatusNotFound, "form_not_found"
	}
	if status == http.StatusInternalServerError && h.log != nil {
		h.log.Error("public runtime request failed", "request_id", response.RequestID(r.Context()), "error", err)
	}
	response.Error(w, r, status, code, err.Error())
}

func snapshotID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_id", "Некорректный идентификатор снапшота")
		return uuid.Nil, false
	}
	return id, true
}

func parseNonnegative(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return n, nil
}
