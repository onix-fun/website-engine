// Package overview exposes the operational summary used by the admin home page.
package overview

import (
	"log/slog"
	"net/http"

	"websiteengine/presentation/http/response"
)

type Handler struct {
	repo *Repository
	log  *slog.Logger
}

func NewHandler(repo *Repository, log *slog.Logger) *Handler {
	return &Handler{repo: repo, log: log}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	data, err := h.repo.Load(r.Context())
	if err != nil {
		h.log.Error("overview failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Не удалось загрузить сводку")
		return
	}
	response.JSON(w, http.StatusOK, data)
}
