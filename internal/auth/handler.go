package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

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

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		response.Error(w, r, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}
	response.JSON(w, http.StatusOK, p)
}

type createTokenRequest struct {
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type createdTokenResponse struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	Role      string     `json:"role"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	var req createTokenRequest
	if err := response.DecodeJSON(w, r, &req, h.maxJSON); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	created, err := h.service.Create(r.Context(), p, req.Name, req.Role, req.ExpiresAt)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, createdTokenResponse{
		ID:        created.Token.ID,
		Name:      created.Token.Name,
		Token:     created.Secret,
		Role:      created.Token.Role,
		ExpiresAt: created.Token.ExpiresAt,
		CreatedAt: created.Token.CreatedAt,
	})
}

func (h *Handler) ListTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	items, err := h.service.List(r.Context(), p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
}

func (h *Handler) GetToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Токен не найден")
		return
	}
	token, err := h.service.Get(r.Context(), p, id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, token)
}

func (h *Handler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Токен не найден")
		return
	}
	if err := h.service.Revoke(r.Context(), p, id); err != nil {
		h.writeError(w, r, err)
		return
	}
	response.NoContent(w)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		response.Error(w, r, http.StatusForbidden, "forbidden", "Недостаточно прав")
	case errors.Is(err, ErrValidation):
		response.Error(w, r, http.StatusBadRequest, "validation_failed", "Некорректные параметры токена")
	case errors.Is(err, ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Токен не найден")
	default:
		h.log.Error("token operation failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}
