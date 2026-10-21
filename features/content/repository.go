package content

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
	ErrNotFound   = errors.New("entry not found")
	ErrConflict   = errors.New("entry conflict")
	ErrValidation = errors.New("entry validation")
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Entry struct {
	ID             uuid.UUID  `json:"id"`
	FeatureID      uuid.UUID  `json:"feature_id"`
	SingleSlot     bool       `json:"single_slot"`
	Revision       int64      `json:"revision"`
	DraftVersionID *uuid.UUID `json:"draft_version_id"`
	DevVersionID   *uuid.UUID `json:"dev_version_id"`
	ProdVersionID  *uuid.UUID `json:"prod_version_id"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
}

type Version struct {
	ID              uuid.UUID       `json:"id"`
	EntryID         uuid.UUID       `json:"entry_id"`
	Version         int             `json:"version"`
	SchemaVersionID uuid.UUID       `json:"schema_version_id"`
	Data            json.RawMessage `json:"data"`
	Status          string          `json:"status"`
	CreatedBy       *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	PublishedAt     *time.Time      `json:"published_at"`
}

type Detail struct {
	Entry
	FeatureKey  string   `json:"feature_key"`
	FeatureType string   `json:"feature_type"`
	Draft       *Version `json:"draft"`
	Dev         *Version `json:"dev"`
	Prod        *Version `json:"prod"`
}

type PublicEntry struct {
	ID          uuid.UUID
	Data        json.RawMessage
	Schema      json.RawMessage
	Version     int
	PublishedAt *time.Time
	CreatedAt   time.Time
}

type ListParams struct {
	FeatureID       uuid.UUID
	Search          string
	BeforeTime      *time.Time
	BeforeID        *uuid.UUID
	Limit           int
	IncludeArchived bool
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Begin(ctx context.Context) (pgx.Tx, error) { return r.pool.Begin(ctx) }

func (r *Repository) InsertEntry(ctx context.Context, db DBTX, e Entry) error {
	_, err := db.Exec(ctx, `
		INSERT INTO feature_entries
			(id, feature_id, single_slot, revision, draft_version_id, created_by)
		VALUES ($1, $2, $3, 1, $4, $5)`,
		e.ID, e.FeatureID, e.SingleSlot, e.DraftVersionID, e.CreatedBy)
	return err
}

func (r *Repository) InsertVersion(ctx context.Context, db DBTX, v Version) error {
	return db.QueryRow(ctx, `
		INSERT INTO feature_entry_versions
			(id, entry_id, version, schema_version_id, data, status, created_by)
		VALUES ($1, $2, $3, $4, $5, 'draft', $6)
		RETURNING created_at, status, published_at`,
		v.ID, v.EntryID, v.Version, v.SchemaVersionID, v.Data, v.CreatedBy,
	).Scan(&v.CreatedAt, &v.Status, &v.PublishedAt)
}

func (r *Repository) Get(ctx context.Context, db DBTX, id uuid.UUID) (Entry, error) {
	return scanEntry(db.QueryRow(ctx, selectEntry+` WHERE e.id = $1`, id))
}

func (r *Repository) GetForUpdate(ctx context.Context, db DBTX, id uuid.UUID) (Entry, error) {
	return scanEntry(db.QueryRow(ctx, selectEntry+` WHERE e.id = $1 FOR UPDATE`, id))
}

func (r *Repository) GetDetail(ctx context.Context, db DBTX, id uuid.UUID) (Detail, error) {
	var d Detail
	err := db.QueryRow(ctx, `
		SELECT e.id, e.feature_id, e.single_slot, e.revision, e.draft_version_id,
		       e.dev_version_id, e.prod_version_id, e.created_by, e.created_at, e.updated_at,
		       e.archived_at, f.key, f.type
		  FROM feature_entries e
		  JOIN features f ON f.id = e.feature_id
		 WHERE e.id = $1`, id,
	).Scan(&d.ID, &d.FeatureID, &d.SingleSlot, &d.Revision, &d.DraftVersionID,
		&d.DevVersionID, &d.ProdVersionID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
		&d.ArchivedAt, &d.FeatureKey, &d.FeatureType)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	if d.DraftVersionID != nil {
		v, err := r.VersionByID(ctx, db, *d.DraftVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Draft = &v
	}
	if d.DevVersionID != nil {
		v, err := r.VersionByID(ctx, db, *d.DevVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Dev = &v
	}
	if d.ProdVersionID != nil {
		v, err := r.VersionByID(ctx, db, *d.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Prod = &v
	}
	return d, nil
}

func (r *Repository) List(ctx context.Context, db DBTX, p ListParams) ([]Detail, error) {
	rows, err := db.Query(ctx, `
		SELECT e.id, e.feature_id, e.single_slot, e.revision, e.draft_version_id,
		       e.dev_version_id, e.prod_version_id, e.created_by, e.created_at, e.updated_at,
		       e.archived_at, f.key, f.type
		  FROM feature_entries e
		  JOIN features f ON f.id = e.feature_id
		  LEFT JOIN feature_entry_versions v ON v.id = COALESCE(e.draft_version_id, e.dev_version_id, e.prod_version_id)
		 WHERE e.feature_id = $1
		   AND ($2 OR e.archived_at IS NULL)
		   AND ($3 = '' OR v.data::text ILIKE '%' || $3 || '%')
		   AND ($4::timestamptz IS NULL OR (e.created_at, e.id) < ($4, $5))
		 ORDER BY e.created_at DESC, e.id DESC
		 LIMIT $6`,
		p.FeatureID, p.IncludeArchived, p.Search, p.BeforeTime, p.BeforeID, p.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Detail, 0, p.Limit)
	for rows.Next() {
		var d Detail
		if err := rows.Scan(&d.ID, &d.FeatureID, &d.SingleSlot, &d.Revision, &d.DraftVersionID,
			&d.DevVersionID, &d.ProdVersionID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
			&d.ArchivedAt, &d.FeatureKey, &d.FeatureType); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].DraftVersionID != nil {
			v, err := r.VersionByID(ctx, db, *out[i].DraftVersionID)
			if err != nil {
				return nil, err
			}
			out[i].Draft = &v
		}
		if out[i].DevVersionID != nil {
			v, err := r.VersionByID(ctx, db, *out[i].DevVersionID)
			if err != nil {
				return nil, err
			}
			out[i].Dev = &v
		}
		if out[i].ProdVersionID != nil {
			v, err := r.VersionByID(ctx, db, *out[i].ProdVersionID)
			if err != nil {
				return nil, err
			}
			out[i].Prod = &v
		}
	}
	return out, nil
}

func (r *Repository) VersionByID(ctx context.Context, db DBTX, id uuid.UUID) (Version, error) {
	return scanVersion(db.QueryRow(ctx, selectVersion+` WHERE id = $1`, id))
}

func (r *Repository) VersionByNumber(ctx context.Context, db DBTX, entryID uuid.UUID, number int) (Version, error) {
	return scanVersion(db.QueryRow(ctx, selectVersion+` WHERE entry_id = $1 AND version = $2`, entryID, number))
}

func (r *Repository) Versions(ctx context.Context, db DBTX, entryID uuid.UUID, limit int) ([]Version, error) {
	rows, err := db.Query(ctx, selectVersion+` WHERE entry_id = $1 ORDER BY version DESC LIMIT $2`, entryID, limit)
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

func (r *Repository) NextVersion(ctx context.Context, db DBTX, entryID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM feature_entry_versions WHERE entry_id = $1`, entryID).Scan(&n)
	return n, err
}

func (r *Repository) UpdateDraftVersion(ctx context.Context, db DBTX, id uuid.UUID, schemaVersionID uuid.UUID, data json.RawMessage) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_entry_versions
		   SET data = $2, schema_version_id = $3
		 WHERE id = $1 AND status = 'draft'`, id, data, schemaVersionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SetVersionPublished(ctx context.Context, db DBTX, id, schemaVersionID uuid.UUID) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_entry_versions
		   SET status = 'published', published_at = now(), schema_version_id = $2
		 WHERE id = $1 AND status = 'draft'`, id, schemaVersionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) SetDraftPointer(ctx context.Context, db DBTX, entryID, versionID uuid.UUID, bumpRevision bool) error {
	step := 0
	if bumpRevision {
		step = 1
	}
	tag, err := db.Exec(ctx, `
		UPDATE feature_entries
		   SET draft_version_id = $2, revision = revision + $3
		 WHERE id = $1`, entryID, versionID, step)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SetPointers(ctx context.Context, db DBTX, entryID uuid.UUID, draft, dev, prod *uuid.UUID) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_entries
		   SET draft_version_id = $2, dev_version_id = $3, prod_version_id = $4, revision = revision + 1
		 WHERE id = $1`, entryID, draft, dev, prod)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SetEnvPointer(ctx context.Context, db DBTX, entryID uuid.UUID, env environment.Environment, versionID *uuid.UUID) error {
	column := "dev_version_id"
	if env == environment.Prod {
		column = "prod_version_id"
	}
	tag, err := db.Exec(ctx, `
		UPDATE feature_entries
		   SET `+column+` = $2, revision = revision + 1
		 WHERE id = $1`, entryID, versionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) EnvVersion(ctx context.Context, db DBTX, e Entry, env environment.Environment) (*Version, error) {
	id := e.DevVersionID
	if env == environment.Prod {
		id = e.ProdVersionID
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

func (r *Repository) BumpRevision(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `UPDATE feature_entries SET revision = revision + 1 WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) Archive(ctx context.Context, db DBTX, id uuid.UUID) error {
	tag, err := db.Exec(ctx, `
		UPDATE feature_entries
		   SET archived_at = COALESCE(archived_at, now()), revision = revision + 1
		 WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) PublishedEntries(ctx context.Context, db DBTX, env environment.Environment, featureID uuid.UUID, beforeTime *time.Time, beforeID *uuid.UUID, limit int) ([]PublicEntry, error) {
	entryColumn := "dev_version_id"
	schemaColumn := "dev_version_id"
	if env == environment.Prod {
		entryColumn = "prod_version_id"
		schemaColumn = "prod_version_id"
	}
	rows, err := db.Query(ctx, `
		SELECT e.id, v.data, sv.schema, v.version, v.published_at, e.created_at
		  FROM feature_entries e
		  JOIN features f ON f.id = e.feature_id
		  JOIN feature_entry_versions v ON v.id = e.`+entryColumn+`
		  JOIN feature_versions sv ON sv.id = f.`+schemaColumn+`
		 WHERE e.feature_id = $1
		   AND e.archived_at IS NULL
		   AND ($2::timestamptz IS NULL OR (e.created_at, e.id) < ($2, $3))
		 ORDER BY e.created_at DESC, e.id DESC
		 LIMIT $4`, featureID, beforeTime, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PublicEntry, 0, limit)
	for rows.Next() {
		var p PublicEntry
		if err := rows.Scan(&p.ID, &p.Data, &p.Schema, &p.Version, &p.PublishedAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) PublishedSingle(ctx context.Context, db DBTX, env environment.Environment, featureID uuid.UUID) (*PublicEntry, error) {
	entryColumn := "dev_version_id"
	schemaColumn := "dev_version_id"
	if env == environment.Prod {
		entryColumn = "prod_version_id"
		schemaColumn = "prod_version_id"
	}
	var p PublicEntry
	err := db.QueryRow(ctx, `
		SELECT e.id, v.data, sv.schema, v.version, v.published_at, e.created_at
		  FROM feature_entries e
		  JOIN features f ON f.id = e.feature_id
		  JOIN feature_entry_versions v ON v.id = e.`+entryColumn+`
		  JOIN feature_versions sv ON sv.id = f.`+schemaColumn+`
		 WHERE e.feature_id = $1
		   AND e.single_slot = TRUE
		   AND e.archived_at IS NULL
		 LIMIT 1`, featureID,
	).Scan(&p.ID, &p.Data, &p.Schema, &p.Version, &p.PublishedAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) CountPublished(ctx context.Context, db DBTX, env environment.Environment, featureID uuid.UUID) (int, error) {
	column := "dev_version_id"
	if env == environment.Prod {
		column = "prod_version_id"
	}
	var n int
	err := db.QueryRow(ctx, `
		SELECT count(*) FROM feature_entries e
		  JOIN feature_entry_versions v ON v.id = e.`+column+`
		 WHERE e.feature_id = $1 AND e.archived_at IS NULL`, featureID).Scan(&n)
	return n, err
}

const selectEntry = `
	SELECT e.id, e.feature_id, e.single_slot, e.revision, e.draft_version_id,
	       e.dev_version_id, e.prod_version_id, e.created_by, e.created_at, e.updated_at, e.archived_at
	  FROM feature_entries e`

const selectVersion = `
	SELECT id, entry_id, version, schema_version_id, data, status, created_by, created_at, published_at
	  FROM feature_entry_versions`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (Entry, error) {
	var e Entry
	err := row.Scan(&e.ID, &e.FeatureID, &e.SingleSlot, &e.Revision, &e.DraftVersionID,
		&e.DevVersionID, &e.ProdVersionID, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return e, err
}

func scanVersion(row rowScanner) (Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.EntryID, &v.Version, &v.SchemaVersionID, &v.Data,
		&v.Status, &v.CreatedBy, &v.CreatedAt, &v.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return v, err
}
