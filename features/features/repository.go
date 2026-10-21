package features

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/internal/environment"
)

var (
	ErrNotFound   = errors.New("feature not found")
	ErrConflict   = errors.New("feature conflict")
	ErrValidation = errors.New("feature validation")
)

type Feature struct {
	ID             uuid.UUID  `json:"id"`
	Key            string     `json:"key"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	Revision       int64      `json:"revision"`
	DraftVersionID *uuid.UUID `json:"draft_version_id"`
	DevVersionID   *uuid.UUID `json:"dev_version_id"`
	ProdVersionID  *uuid.UUID `json:"prod_version_id"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Version struct {
	ID          uuid.UUID       `json:"id"`
	FeatureID   uuid.UUID       `json:"feature_id"`
	Version     int             `json:"version"`
	Schema      json.RawMessage `json:"schema"`
	Status      string          `json:"status"`
	CreatedBy   *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	PublishedAt *time.Time      `json:"published_at"`
}

type Detail struct {
	Feature
	Draft *Version `json:"draft"`
	Dev   *Version `json:"dev"`
	Prod  *Version `json:"prod"`
}

type DraftState struct {
	Revision int64    `json:"revision"`
	Draft    *Version `json:"draft"`
	Dev      *Version `json:"dev"`
	Prod     *Version `json:"prod"`
}

type ListParams struct {
	Status     string
	Type       string
	Search     string
	BeforeTime *time.Time
	BeforeID   *uuid.UUID
	Limit      int
}

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

func (r *Repository) Create(ctx context.Context, f Feature) (Feature, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO features (id, key, name, type, status, revision, created_by)
		VALUES ($1, $2, $3, $4, 'active', 1, $5)
		RETURNING id, key, name, type, status, revision, draft_version_id,
		          dev_version_id, prod_version_id, created_by, created_at, updated_at`,
		f.ID, f.Key, f.Name, f.Type, f.CreatedBy,
	).Scan(&f.ID, &f.Key, &f.Name, &f.Type, &f.Status, &f.Revision,
		&f.DraftVersionID, &f.DevVersionID, &f.ProdVersionID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (Feature, error) {
	return scanFeature(r.pool.QueryRow(ctx, selectFeature+` WHERE id = $1`, id))
}

func (r *Repository) GetByKey(ctx context.Context, key string) (Feature, error) {
	return scanFeature(r.pool.QueryRow(ctx, selectFeature+` WHERE key = $1`, key))
}

func (r *Repository) GetForUpdate(ctx context.Context, db DBTX, id uuid.UUID) (Feature, error) {
	return scanFeature(db.QueryRow(ctx, selectFeature+` WHERE id = $1 FOR UPDATE`, id))
}

func (r *Repository) List(ctx context.Context, p ListParams) ([]Feature, error) {
	rows, err := r.pool.Query(ctx, selectFeature+`
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR type = $2)
		  AND ($3 = '' OR name ILIKE '%' || $3 || '%' OR key ILIKE '%' || $3 || '%')
		  AND ($4::timestamptz IS NULL OR (created_at, id) < ($4, $5))
		ORDER BY created_at DESC, id DESC
		LIMIT $6`,
		p.Status, p.Type, p.Search, p.BeforeTime, p.BeforeID, p.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Feature, 0, p.Limit)
	for rows.Next() {
		f, err := scanFeature(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateName(ctx context.Context, id uuid.UUID, name string) (Feature, error) {
	return scanFeature(r.pool.QueryRow(ctx, `
		UPDATE features SET name = $2, revision = revision + 1
		 WHERE id = $1
		RETURNING id, key, name, type, status, revision, draft_version_id,
		          dev_version_id, prod_version_id, created_by, created_at, updated_at`, id, name))
}

func (r *Repository) Archive(ctx context.Context, id uuid.UUID) (Feature, error) {
	return scanFeature(r.pool.QueryRow(ctx, `
		UPDATE features SET status = 'archived', revision = revision + 1
		 WHERE id = $1
		RETURNING id, key, name, type, status, revision, draft_version_id,
		          dev_version_id, prod_version_id, created_by, created_at, updated_at`, id))
}

func (r *Repository) Versions(ctx context.Context, featureID uuid.UUID, limit int) ([]Version, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, feature_id, version, schema, status, created_by, created_at, published_at
		  FROM feature_versions
		 WHERE feature_id = $1
		 ORDER BY version DESC
		 LIMIT $2`, featureID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) VersionByID(ctx context.Context, db DBTX, id uuid.UUID) (Version, error) {
	return scanVersion(db.QueryRow(ctx, `
		SELECT id, feature_id, version, schema, status, created_by, created_at, published_at
		  FROM feature_versions WHERE id = $1`, id))
}

func (r *Repository) VersionByNumber(ctx context.Context, db DBTX, featureID uuid.UUID, number int) (Version, error) {
	return scanVersion(db.QueryRow(ctx, `
		SELECT id, feature_id, version, schema, status, created_by, created_at, published_at
		  FROM feature_versions WHERE feature_id = $1 AND version = $2`, featureID, number))
}

func (r *Repository) NextVersion(ctx context.Context, db DBTX, featureID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM feature_versions WHERE feature_id = $1`,
		featureID).Scan(&n)
	return n, err
}

func (r *Repository) CreateVersion(ctx context.Context, db DBTX, v Version) error {
	return db.QueryRow(ctx, `
		INSERT INTO feature_versions (id, feature_id, version, schema, status, created_by)
		VALUES ($1, $2, $3, $4, 'draft', $5)
		RETURNING created_at, status, published_at`,
		v.ID, v.FeatureID, v.Version, v.Schema, v.CreatedBy,
	).Scan(&v.CreatedAt, &v.Status, &v.PublishedAt)
}

func (r *Repository) UpdateDraftSchema(ctx context.Context, db DBTX, id uuid.UUID, schema json.RawMessage) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_versions SET schema = $2
		 WHERE id = $1 AND status = 'draft'`, id, schema)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SetVersionPublished(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_versions
		   SET status = 'published', published_at = now()
		 WHERE id = $1 AND status = 'draft'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SetPointers(ctx context.Context, db DBTX, featureID uuid.UUID, draft, dev, prod *uuid.UUID, bumpRevision bool) (Feature, error) {
	revisionStep := 0
	if bumpRevision {
		revisionStep = 1
	}
	return scanFeature(db.QueryRow(ctx, `
		UPDATE features
		   SET draft_version_id = $2, dev_version_id = $3, prod_version_id = $4, revision = revision + $5
		 WHERE id = $1
		RETURNING id, key, name, type, status, revision, draft_version_id,
		          dev_version_id, prod_version_id, created_by, created_at, updated_at`,
		featureID, draft, dev, prod, revisionStep))
}

func (r *Repository) SetEnvPointer(ctx context.Context, db DBTX, featureID uuid.UUID, env environment.Environment, versionID *uuid.UUID, bumpRevision bool) error {
	column := "dev_version_id"
	if env == environment.Prod {
		column = "prod_version_id"
	}
	revisionStep := 0
	if bumpRevision {
		revisionStep = 1
	}
	tag, err := db.Exec(ctx, `
		UPDATE features
		   SET `+column+` = $2, revision = revision + $3
		 WHERE id = $1`, featureID, versionID, revisionStep)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) EnvVersion(ctx context.Context, db DBTX, f Feature, env environment.Environment) (*Version, error) {
	id := f.DevVersionID
	if env == environment.Prod {
		id = f.ProdVersionID
	}
	if id == nil {
		return nil, nil
	}
	v, err := r.VersionByID(ctx, db, *id)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Repository) EnvEntriesData(ctx context.Context, db DBTX, featureID uuid.UUID, env environment.Environment) ([]json.RawMessage, error) {
	column := "dev_version_id"
	if env == environment.Prod {
		column = "prod_version_id"
	}
	rows, err := db.Query(ctx, `
		SELECT v.data
		  FROM feature_entries e
		  JOIN feature_entry_versions v ON v.id = e.`+column+`
		 WHERE e.feature_id = $1
		   AND e.archived_at IS NULL`, featureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]json.RawMessage, 0)
	for rows.Next() {
		var data json.RawMessage
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, rows.Err()
}

func (r *Repository) SetDraftPointer(ctx context.Context, db DBTX, featureID, versionID uuid.UUID) (Feature, error) {
	return scanFeature(db.QueryRow(ctx, `
		UPDATE features
		   SET draft_version_id = $2, revision = revision + 1
		 WHERE id = $1
		RETURNING id, key, name, type, status, revision, draft_version_id,
		          dev_version_id, prod_version_id, created_by, created_at, updated_at`,
		featureID, versionID))
}

// PublishedSchemas loads the current published schema for a set of feature keys.
func (r *Repository) PublishedByKeys(ctx context.Context, env environment.Environment, keys []string) ([]PublishedFeature, error) {
	column := "dev_version_id"
	if env == environment.Prod {
		column = "prod_version_id"
	}
	rows, err := r.pool.Query(ctx, `
		SELECT f.id, f.key, f.name, f.type, f.updated_at,
		       v.id, v.version, v.schema, v.published_at
		  FROM features f
		  JOIN feature_versions v ON v.id = f.`+column+`
		 WHERE f.status = 'active'
		   AND f.key = ANY($1::text[])
		 ORDER BY f.key`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PublishedFeature, 0, len(keys))
	for rows.Next() {
		var p PublishedFeature
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.Type, &p.UpdatedAt,
			&p.VersionID, &p.Version, &p.Schema, &p.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type PublishedFeature struct {
	ID          uuid.UUID
	Key         string
	Name        string
	Type        string
	UpdatedAt   time.Time
	VersionID   uuid.UUID
	Version     int
	Schema      json.RawMessage
	PublishedAt *time.Time
}

const selectFeature = `
	SELECT id, key, name, type, status, revision, draft_version_id,
	       dev_version_id, prod_version_id, created_by, created_at, updated_at
	  FROM features`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFeature(row rowScanner) (Feature, error) {
	var f Feature
	err := row.Scan(&f.ID, &f.Key, &f.Name, &f.Type, &f.Status, &f.Revision,
		&f.DraftVersionID, &f.DevVersionID, &f.ProdVersionID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Feature{}, ErrNotFound
	}
	return f, err
}

func scanVersion(row rowScanner) (Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.FeatureID, &v.Version, &v.Schema, &v.Status,
		&v.CreatedBy, &v.CreatedAt, &v.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return v, err
}

func (r *Repository) BumpRevision(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `UPDATE features SET revision = revision + 1 WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
