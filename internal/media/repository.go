package media

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound   = errors.New("media not found")
	ErrConflict   = errors.New("media conflict")
	ErrValidation = errors.New("media validation")
	ErrProcessing = errors.New("media processing failed")
)

type Media struct {
	ID               uuid.UUID         `json:"id"`
	ObjectKey        string            `json:"object_key"`
	OriginalFilename string            `json:"original_filename"`
	ContentType      string            `json:"content_type"`
	SizeBytes        int64             `json:"size_bytes"`
	Status           string            `json:"status"`
	Variants         map[string]string `json:"variants"`
	UploadedBy       *uuid.UUID        `json:"uploaded_by,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

// Usage describes one content record that references a media object.
type Usage struct {
	Kind       string    `json:"kind"`
	ID         uuid.UUID `json:"id"`
	FeatureID  uuid.UUID `json:"feature_id"`
	FeatureKey string    `json:"feature_key"`
	Version    int       `json:"version"`
	Status     string    `json:"status"`
	OccurredAt time.Time `json:"occurred_at"`
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, m Media) (Media, error) {
	variantJSON, _ := json.Marshal(m.Variants)
	err := r.pool.QueryRow(ctx, `
		INSERT INTO media_objects
			(id, object_key, original_filename, content_type, size_bytes, status, variants, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at`,
		m.ID, m.ObjectKey, m.OriginalFilename, m.ContentType, m.SizeBytes, m.Status, variantJSON, m.UploadedBy,
	).Scan(&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Media, error) {
	return scanMedia(r.pool.QueryRow(ctx, selectMedia+` WHERE id = $1`, id))
}

// ListParams describes the filters and the ordering of the media list.
type ListParams struct {
	Status             string
	Search             string
	ContentTypePattern string
	CreatedFrom        *time.Time
	CreatedTo          *time.Time
	MinSize            *int64
	MaxSize            *int64
	Sort               string
	CursorTime         *time.Time
	CursorID           *uuid.UUID
	CursorName         *string
	CursorSize         *int64
	Limit              int
}

func (r *Repository) List(ctx context.Context, p ListParams) ([]Media, error) {
	order := "created_at DESC, id DESC"
	keyset := "AND ($8::timestamptz IS NULL OR (created_at, id) < ($8, $9))"
	switch p.Sort {
	case "", "new":
	case "old":
		order = "created_at ASC, id ASC"
		keyset = "AND ($8::timestamptz IS NULL OR (created_at, id) > ($8, $9))"
	case "name":
		order = "original_filename ASC, id ASC"
		keyset = "AND ($10::text IS NULL OR (original_filename, id) > ($10, $9))"
	case "size_desc":
		order = "size_bytes DESC, id DESC"
		keyset = "AND ($11::bigint IS NULL OR (size_bytes, id) < ($11, $9))"
	case "size_asc":
		order = "size_bytes ASC, id ASC"
		keyset = "AND ($11::bigint IS NULL OR (size_bytes, id) > ($11, $9))"
	}
	rows, err := r.pool.Query(ctx, selectMedia+`
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR original_filename ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR content_type LIKE $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at < $5)
		  AND ($6::bigint IS NULL OR size_bytes >= $6)
		  AND ($7::bigint IS NULL OR size_bytes <= $7)
		  AND ($8::timestamptz IS NULL OR TRUE)
		  AND ($9::uuid IS NULL OR TRUE)
		  AND ($10::text IS NULL OR TRUE)
		  AND ($11::bigint IS NULL OR TRUE)
		  `+keyset+`
		ORDER BY `+order+`
		LIMIT $12`,
		p.Status, p.Search, p.ContentTypePattern, p.CreatedFrom, p.CreatedTo,
		p.MinSize, p.MaxSize, p.CursorTime, p.CursorID, p.CursorName, p.CursorSize, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Media, 0, p.Limit)
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Rename updates the display filename of a media object. The object key in
// the bucket never changes, so already published content keeps working.
func (r *Repository) Rename(ctx context.Context, id uuid.UUID, filename string) (Media, error) {
	return scanMedia(r.pool.QueryRow(ctx, `
		UPDATE media_objects
		   SET original_filename = $2
		 WHERE id = $1 AND status <> 'deleted'
		RETURNING id, object_key, original_filename, content_type, size_bytes, status,
		          variants, uploaded_by, created_at, updated_at`, id, filename))
}

// Usage lists content records that reference the media object. Media ids are
// stored as UUID strings inside the JSON payload, so the lookup is a text
// match on the serialized document.
func (r *Repository) Usage(ctx context.Context, id uuid.UUID, limit int) ([]Usage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT 'entry'::text, e.id, e.feature_id, f.key, v.version, v.status, v.created_at
		  FROM feature_entry_versions v
		  JOIN feature_entries e ON e.id = v.entry_id
		  JOIN features f ON f.id = e.feature_id
		 WHERE v.data::text LIKE '%' || $1 || '%'
		UNION ALL
		SELECT 'submission'::text, s.id, s.feature_id, f.key, 0, s.status, s.created_at
		  FROM form_submissions s
		  JOIN features f ON f.id = s.feature_id
		 WHERE s.data::text LIKE '%' || $1 || '%'
		LIMIT $2`, id.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Usage, 0, limit)
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.Kind, &u.ID, &u.FeatureID, &u.FeatureKey, &u.Version, &u.Status, &u.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *Repository) SetProcessing(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE media_objects SET status = 'processing'
		 WHERE id = $1 AND status IN ('processing', 'failed')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SetReady(ctx context.Context, id uuid.UUID, variants map[string]string) (Media, error) {
	raw, _ := json.Marshal(variants)
	return scanMedia(r.pool.QueryRow(ctx, `
		UPDATE media_objects
		   SET status = 'ready', variants = $2
		 WHERE id = $1
		RETURNING id, object_key, original_filename, content_type, size_bytes, status,
		          variants, uploaded_by, created_at, updated_at`, id, raw))
}

func (r *Repository) SetFailed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE media_objects SET status = 'failed' WHERE id = $1`, id)
	return err
}

func (r *Repository) SetDeleted(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE media_objects SET status = 'deleted' WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const selectMedia = `
	SELECT id, object_key, original_filename, content_type, size_bytes, status,
	       variants, uploaded_by, created_at, updated_at
	  FROM media_objects`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMedia(row rowScanner) (Media, error) {
	var m Media
	var variants []byte
	err := row.Scan(&m.ID, &m.ObjectKey, &m.OriginalFilename, &m.ContentType, &m.SizeBytes,
		&m.Status, &variants, &m.UploadedBy, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Media{}, ErrNotFound
	}
	if err != nil {
		return Media{}, err
	}
	m.Variants = make(map[string]string)
	if len(variants) > 0 {
		_ = json.Unmarshal(variants, &m.Variants)
	}
	return m, nil
}
