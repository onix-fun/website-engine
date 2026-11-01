package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/pkg/environment"
)

var ErrNotFound = errors.New("release not found")

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type Source struct {
	ID            uuid.UUID  `json:"id"`
	Provider      string     `json:"provider"`
	RepoURL       string     `json:"repo_url"`
	AssetPattern  string     `json:"asset_pattern"`
	Enabled       bool       `json:"is_enabled"`
	CreatedBy     *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	HasCredential bool       `json:"has_credential"`
}

type Release struct {
	ID           uuid.UUID       `json:"id"`
	Version      string          `json:"version"`
	SourceID     *uuid.UUID      `json:"source_id,omitempty"`
	ExternalID   *string         `json:"external_id,omitempty"`
	Tag          *string         `json:"tag,omitempty"`
	CommitSHA    *string         `json:"commit_sha,omitempty"`
	ObjectKey    string          `json:"-"`
	SHA256       string          `json:"artifact_sha256"`
	ArtifactSize int64           `json:"artifact_size"`
	Manifest     json.RawMessage `json:"manifest"`
	ImportedBy   *uuid.UUID      `json:"imported_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type EnvironmentState struct {
	Key              environment.Environment `json:"key"`
	Name             string                  `json:"name"`
	PublicBaseURL    string                  `json:"public_base_url"`
	HTTPPort         int                     `json:"http_port"`
	CORSOrigins      []string                `json:"cors_origins"`
	ActiveReleaseID  *uuid.UUID              `json:"active_release_id,omitempty"`
	ActiveSnapshotID *uuid.UUID              `json:"active_snapshot_id,omitempty"`
	Revision         int64                   `json:"revision"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type Event struct {
	ID          uuid.UUID               `json:"id"`
	ReleaseID   *uuid.UUID              `json:"release_id,omitempty"`
	Environment environment.Environment `json:"environment"`
	Action      string                  `json:"action"`
	ActorID     *uuid.UUID              `json:"actor_token_id,omitempty"`
	Metadata    json.RawMessage         `json:"metadata"`
	CreatedAt   time.Time               `json:"created_at"`
}

type ListParams struct {
	BeforeTime *time.Time
	BeforeID   *uuid.UUID
	Limit      int
}

func (r *Repository) ListReleases(ctx context.Context, p ListParams) ([]Release, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, version, source_id, external_id, tag, commit_sha,
		       artifact_object_key, artifact_sha256, artifact_size, manifest,
		       imported_by, created_at, updated_at
		  FROM site_releases
		 WHERE ($1::timestamptz IS NULL OR (created_at, id) < ($1, $2))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $3`, p.BeforeTime, p.BeforeID, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Release, 0, p.Limit)
	for rows.Next() {
		rel, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (r *Repository) ReleaseByID(ctx context.Context, id uuid.UUID) (Release, error) {
	return scanRelease(r.pool.QueryRow(ctx, `
		SELECT id, version, source_id, external_id, tag, commit_sha,
		       artifact_object_key, artifact_sha256, artifact_size, manifest,
		       imported_by, created_at, updated_at
		  FROM site_releases WHERE id = $1`, id))
}

type scanner interface{ Scan(...any) error }

func scanRelease(row scanner) (Release, error) {
	var rel Release
	err := row.Scan(&rel.ID, &rel.Version, &rel.SourceID, &rel.ExternalID, &rel.Tag, &rel.CommitSHA,
		&rel.ObjectKey, &rel.SHA256, &rel.ArtifactSize, &rel.Manifest,
		&rel.ImportedBy, &rel.CreatedAt, &rel.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return rel, err
}

func (r *Repository) InsertRelease(ctx context.Context, rel Release) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO site_releases
			(id, version, source_id, external_id, tag, commit_sha, artifact_object_key,
			 artifact_sha256, artifact_size, manifest, imported_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		rel.ID, rel.Version, rel.SourceID, rel.ExternalID, rel.Tag, rel.CommitSHA, rel.ObjectKey,
		rel.SHA256, rel.ArtifactSize, rel.Manifest, rel.ImportedBy)
	return err
}

func (r *Repository) SetActive(ctx context.Context, env environment.Environment, releaseID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE site_environments
		   SET active_release_id = $2, revision = revision + 1
		 WHERE key = $1`, env.String(), releaseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) InsertEvent(ctx context.Context, event Event) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO release_events (id, release_id, environment, action, actor_token_id, metadata)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		event.ID, event.ReleaseID, event.Environment.String(), event.Action, event.ActorID, event.Metadata)
	return err
}

func (r *Repository) Environments(ctx context.Context) ([]EnvironmentState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT key, name, public_base_url, http_port,
		       cors_origins, active_release_id, active_snapshot_id, revision, updated_at
		  FROM site_environments ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvironmentState
	for rows.Next() {
		var e EnvironmentState
		var key string
		if err := rows.Scan(&key, &e.Name, &e.PublicBaseURL, &e.HTTPPort,
			&e.CORSOrigins, &e.ActiveReleaseID, &e.ActiveSnapshotID, &e.Revision, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Key = environment.Environment(key)
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateEnvironment applies the editable environment settings when the
// caller-supplied revision matches; otherwise it returns the current revision
// so the caller can retry. Only the fields the caller provided are updated:
// name, public base URL and CORS origins.
func (r *Repository) UpdateEnvironment(ctx context.Context, env environment.Environment,
	name, publicBaseURL *string, corsOrigins *[]string, revision int64) (EnvironmentState, error) {
	sets := "revision = revision + 1, updated_at = now()"
	args := []any{}
	add := func(column string, v any) {
		args = append(args, v)
		sets += fmt.Sprintf(", %s = $%d", column, len(args))
	}
	if name != nil {
		add("name", *name)
	}
	if publicBaseURL != nil {
		add("public_base_url", *publicBaseURL)
	}
	if corsOrigins != nil {
		add("cors_origins", *corsOrigins)
	}
	args = append(args, revision)
	revArg := len(args)
	args = append(args, env.String())
	keyArg := len(args)
	tag, err := r.pool.Exec(ctx, `UPDATE site_environments SET `+sets+
		` WHERE key = $`+fmt.Sprintf("%d", keyArg)+` AND revision = $`+fmt.Sprintf("%d", revArg), args...)
	if err != nil {
		return EnvironmentState{}, err
	}
	if tag.RowsAffected() == 0 {
		var current int64
		if err := r.pool.QueryRow(ctx,
			`SELECT revision FROM site_environments WHERE key = $1`, env.String()).Scan(&current); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return EnvironmentState{}, ErrNotFound
			}
			return EnvironmentState{}, err
		}
		return EnvironmentState{}, &envConflict{current: current}
	}
	states, err := r.Environments(ctx)
	if err != nil {
		return EnvironmentState{}, err
	}
	for _, st := range states {
		if st.Key == env {
			return st, nil
		}
	}
	return EnvironmentState{}, fmt.Errorf("environment %s vanished after update", env)
}

func (r *Repository) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, provider, repo_url, asset_pattern, is_enabled, created_by, created_at, updated_at,
		       auth_ciphertext IS NOT NULL
		  FROM release_sources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Provider, &s.RepoURL, &s.AssetPattern, &s.Enabled,
			&s.CreatedBy, &s.CreatedAt, &s.UpdatedAt, &s.HasCredential); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SourceCredential returns encrypted provider credentials for internal use.
// The public Source model only exposes whether a credential is present.
func (r *Repository) SourceCredential(ctx context.Context, id uuid.UUID) ([]byte, []byte, error) {
	var ciphertext, nonce []byte
	err := r.pool.QueryRow(ctx, `
		SELECT auth_ciphertext, auth_nonce
		  FROM release_sources WHERE id = $1`, id).Scan(&ciphertext, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return ciphertext, nonce, err
}

func (r *Repository) InsertSource(ctx context.Context, s Source, ciphertext, nonce []byte) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO release_sources (id, provider, repo_url, asset_pattern, auth_ciphertext, auth_nonce, is_enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		s.ID, s.Provider, s.RepoURL, s.AssetPattern, ciphertext, nonce, s.Enabled, s.CreatedBy)
	return err
}

func (r *Repository) DeleteSource(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM release_sources WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FeatureVersionSnapshot is one feature's version pointers at activation
// time. Versions are the feature_versions.version numbers the environment
// pointed to; nil means the environment has no published schema yet.
type FeatureVersionSnapshot struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	DevVersion  *int64 `json:"dev_version"`
	ProdVersion *int64 `json:"prod_version"`
}

// FeatureVersionsSnapshot captures the active features' version pointers at
// the moment of an activation or rollback. The release_events.metadata
// column stores it so operators can audit what the site actually served.
func (r *Repository) FeatureVersionsSnapshot(ctx context.Context) (json.RawMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT f.key, f.type,
		       dv.version, pv.version
		  FROM features f
		  LEFT JOIN feature_versions dv ON dv.id = f.dev_version_id
		  LEFT JOIN feature_versions pv ON pv.id = f.prod_version_id
		 WHERE f.status = 'active'
		 ORDER BY f.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	features := []FeatureVersionSnapshot{}
	for rows.Next() {
		var snap FeatureVersionSnapshot
		if err := rows.Scan(&snap.Key, &snap.Type, &snap.DevVersion, &snap.ProdVersion); err != nil {
			return nil, err
		}
		features = append(features, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"features": features})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}
