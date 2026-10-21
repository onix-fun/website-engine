package media

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"websiteengine/internal/auth"
	"websiteengine/internal/transport/http/response"
)

type Handler struct {
	service   *Service
	log       *slog.Logger
	maxUpload int64
}

func NewHandler(service *Service, log *slog.Logger, maxUpload int64) *Handler {
	return &Handler{service: service, log: log, maxUpload: maxUpload}
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload+(1<<20))
	if err := r.ParseMultipartForm(h.maxUpload + 1); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "too large") {
			response.Error(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Файл превышает допустимый размер")
			return
		}
		response.Error(w, r, http.StatusBadRequest, "invalid_multipart", "Ожидается multipart/form-data с полем file")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "file_required", "Поле file обязательно")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, h.maxUpload+1))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_file", "Не удалось прочитать файл")
		return
	}
	if int64(len(data)) > h.maxUpload {
		response.Error(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Файл превышает допустимый размер")
		return
	}
	m, err := h.service.Upload(r.Context(), currentActor(r), header.Filename, data)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, m)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := response.ParseLimit(q.Get("limit"), 50, 200)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	filters, err := parseListFilters(q)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid_filters", err.Error())
		return
	}
	items, next, err := h.service.List(r.Context(), filters, q.Get("cursor"), limit)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nullable(next)})
}

// parseListFilters reads the optional list filters from the query string.
func parseListFilters(q url.Values) (ListFilters, error) {
	f := ListFilters{
		Status: q.Get("status"),
		Search: q.Get("q"),
		Type:   q.Get("type"),
		Sort:   q.Get("sort"),
	}
	var err error
	if f.CreatedFrom, err = parseTimeParam(q.Get("created_from"), false); err != nil {
		return f, fmt.Errorf("created_from должен быть датой в формате YYYY-MM-DD или RFC 3339")
	}
	if f.CreatedTo, err = parseTimeParam(q.Get("created_to"), true); err != nil {
		return f, fmt.Errorf("created_to должен быть датой в формате YYYY-MM-DD или RFC 3339")
	}
	if f.MinSize, err = parseSizeParam(q.Get("min_size")); err != nil {
		return f, fmt.Errorf("min_size должен быть положительным числом байт")
	}
	if f.MaxSize, err = parseSizeParam(q.Get("max_size")); err != nil {
		return f, fmt.Errorf("max_size должен быть положительным числом байт")
	}
	if f.MinSize != nil && f.MaxSize != nil && *f.MinSize > *f.MaxSize {
		return f, fmt.Errorf("min_size не должен превышать max_size")
	}
	return f, nil
}

// parseTimeParam accepts a plain date or a full RFC 3339 instant. A plain
// date covers the whole day, so for the end of a range the exclusive upper
// bound is midnight of the next day.
func parseTimeParam(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		t = t.UTC()
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.UTC()
		if endOfDay {
			t = t.AddDate(0, 0, 1)
		}
		return &t, nil
	}
	return nil, errors.New("invalid date")
}

// parseSizeParam reads a byte count.
func parseSizeParam(raw string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	size, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || size < 0 {
		return nil, errors.New("invalid size")
	}
	return &size, nil
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	m, err := h.service.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, m)
}

type updateMediaRequest struct {
	Filename string `json:"filename"`
}

// Update renames the display metadata of a media object.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	var req updateMediaRequest
	if err := response.DecodeJSON(w, r, &req, h.maxUpload); err != nil {
		response.WriteDecodeError(w, r, err)
		return
	}
	m, err := h.service.Rename(r.Context(), id, req.Filename)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, m)
}

// Usage lists the content records that reference the media object.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	items, err := h.service.Usage(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	m, err := h.service.Retry(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, m)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := mediaID(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	response.NoContent(w)
}

// Redirect responds with 302 to a public object URL. No user-supplied URL is
// ever used, which prevents open redirects.
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("key"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Медиаобъект не найден")
		return
	}
	url, err := h.service.RedirectURL(r.Context(), id, r.URL.Query().Get("variant"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, r, http.StatusNotFound, "not_found", "Медиаобъект не найден")
			return
		}
		h.log.Error("media redirect failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Location", url)
	w.WriteHeader(http.StatusFound)
}

func mediaID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "not_found", "Медиаобъект не найден")
		return uuid.Nil, false
	}
	return id, true
}

func currentActor(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFromContext(r.Context())
	return p
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
		code := "validation_failed"
		status := http.StatusUnprocessableEntity
		if strings.Contains(validationErr.Message, "тип") {
			status = http.StatusUnsupportedMediaType
			code = "unsupported_media_type"
		} else if strings.Contains(validationErr.Message, "размер") {
			status = http.StatusRequestEntityTooLarge
			code = "file_too_large"
		}
		response.Error(w, r, status, code, validationErr.Message)
	case errors.As(err, &conflictErr):
		response.Error(w, r, http.StatusConflict, "conflict", conflictErr.Message)
	case errors.Is(err, ErrProcessing):
		response.Error(w, r, http.StatusUnprocessableEntity, "media_processing_failed", "Не удалось обработать медиафайл")
	case errors.Is(err, ErrNotFound):
		response.Error(w, r, http.StatusNotFound, "not_found", "Медиаобъект не найден")
	default:
		h.log.Error("media operation failed", "request_id", response.RequestID(r.Context()), "error", err)
		response.Error(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}
