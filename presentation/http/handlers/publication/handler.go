// Package publication exposes the content publication audit feed.
package publication

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"websiteengine/pkg/environment"
	"websiteengine/pkg/publication"
	"websiteengine/pkg/response"
)

// Handler serves the publication audit feed for the admin API.
type Handler struct {
	db  publication.Lister
	log *slog.Logger
}

// NewHandler builds the audit feed handler. db is the connection pool used
// only through the read contract of pkg/publication.
func NewHandler(db publication.Lister, log *slog.Logger) *Handler {
	return &Handler{db: db, log: log}
}

// List responds with the newest publication events, cursor paginated.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := publication.ListParams{Limit: 50}
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 || limit > 200 {
			response.Error(w, r, http.StatusBadRequest, "invalid_query", "limit должен быть 1-200")
			return
		}
		params.Limit = limit
	}
	if raw := q.Get("entity_kind"); raw != "" {
		if !publication.ListParamsValid(raw) {
			response.Error(w, r, http.StatusBadRequest, "invalid_query", "entity_kind должен быть feature или entry")
			return
		}
		params.EntityKind = raw
	}
	if raw := q.Get("environment"); raw != "" {
		env, err := environment.Parse(raw)
		if err != nil {
			response.Error(w, r, http.StatusBadRequest, "invalid_query", err.Error())
			return
		}
		params.Environment = env
	}
	if raw := q.Get("feature_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(w, r, http.StatusBadRequest, "invalid_query", "некорректный feature_id")
			return
		}
		params.FeatureID = &id
	}
	if raw := q.Get("cursor"); raw != "" {
		beforeTime, beforeID, ok := publication.DecodeEventCursor(raw)
		if !ok {
			response.Error(w, r, http.StatusBadRequest, "invalid_query", "Некорректный cursor")
			return
		}
		params.BeforeTime, params.BeforeID = beforeTime, beforeID
	}

	items, next, err := publication.ListEvents(r.Context(), h.db, params)
	if err != nil {
		h.log.Error("publication events failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Не удалось загрузить события")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next": next})
}
