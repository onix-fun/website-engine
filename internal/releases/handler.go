package releases

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"websiteengine/internal/auth"
	"websiteengine/internal/environment"
	"websiteengine/internal/transport/http/response"
)

type Handler struct {
	service  *Service
	log      *slog.Logger
	maxBytes int64
}

func NewHandler(service *Service, log *slog.Logger, maxBytes int64) *Handler {
	return &Handler{service: service, log: log, maxBytes: maxBytes}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := response.ParseLimit(q.Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	beforeTime, beforeID, err := ParseCursor(q.Get("cursor"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	items, next, err := h.service.List(r.Context(), beforeTime, beforeID, limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullable(next)})
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_multipart", "Не удалось прочитать архив")
		return
	}
	file, header, err := r.FormFile("artifact")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "missing_artifact", "Добавьте zip-архив релиза")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		response.Error(w, r, http.StatusBadRequest, "invalid_artifact", "Артефакт должен быть zip-архивом")
		return
	}
	rel, err := h.service.ImportManual(r.Context(), actor, ImportInput{
		Version: r.FormValue("version"), Tag: r.FormValue("tag"), File: file, Size: header.Size,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, rel)
}

type releaseEnvironmentRequest struct {
	Environment string `json:"environment"`
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, false) }
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, true) }

func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, rollback bool) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Релиз не найден")
		return
	}
	var req releaseEnvironmentRequest
	if err := response.DecodeJSON(w, r, &req, 4096); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	env, err := environment.Parse(req.Environment)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_environment", err.Error())
		return
	}
	if rollback {
		if !auth.CanActivateRelease(actor.Role, env) {
			response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для отката релиза")
			return
		}
	} else if !auth.CanActivateRelease(actor.Role, env) {
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для активации релиза")
		return
	}
	var rel Release
	if rollback {
		rel, err = h.service.Rollback(r.Context(), actor, env, id)
	} else {
		rel, err = h.service.Activate(r.Context(), actor, env, id)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, rel)
}

func (h *Handler) Environments(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Environments(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Sources(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Sources(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type sourceRequest struct {
	Provider     string `json:"provider"`
	RepoURL      string `json:"repo_url"`
	AssetPattern string `json:"asset_pattern"`
	Token        string `json:"token"`
	Enabled      *bool  `json:"is_enabled"`
}

func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	var req sourceRequest
	if err := response.DecodeJSON(w, r, &req, 1<<20); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	src, err := h.service.CreateSource(r.Context(), actor, SourceInput{
		Provider: req.Provider, RepoURL: req.RepoURL, AssetPattern: req.AssetPattern,
		Token: req.Token, Enabled: enabled,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, src)
}

func (h *Handler) DeleteSource(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Источник не найден")
		return
	}
	if err := h.service.DeleteSource(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Релиз не найден")
	default:
		response.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ = json.RawMessage{}
var _ = strconv.Itoa
var _ = errors.New

func (h *Handler) SyncSource(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.PrincipalFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Источник не найден")
		return
	}
	var req struct {
		AssetURL string `json:"asset_url"`
		Tag      string `json:"tag"`
	}
	if err := response.DecodeJSON(w, r, &req, 4096); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	srcs, err := h.service.Sources(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var src *Source
	for i := range srcs {
		if srcs[i].ID == id {
			s := srcs[i]
			src = &s
			break
		}
	}
	if src == nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Источник не найден")
		return
	}
	if !auth.CanActivateRelease(actor.Role, environment.Dev) {
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав для импорта релиза")
		return
	}
	rel, err := h.service.SyncFromSource(r.Context(), actor, *src, req.AssetURL)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, rel)
}

func (h *Handler) ListReleasesForSource(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Источник не найден")
		return
	}
	srcs, err := h.service.Sources(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var src *Source
	for i := range srcs {
		if srcs[i].ID == id {
			s := srcs[i]
			src = &s
			break
		}
	}
	if src == nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Источник не найден")
		return
	}
	p := h.service.provider()
	pr, err := p.forSource(*src)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	rels, err := pr.ListReleases(r.Context(), src.RepoURL, 20)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rels})
}

func (h *Handler) provider() *Provider { return h.service.provider() }
