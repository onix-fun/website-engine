package overview

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Counts struct {
	FeaturesActive   int64 `json:"features_active"`
	FeaturesArchived int64 `json:"features_archived"`
	DraftFeatures    int64 `json:"draft_features"`
	DraftEntries     int64 `json:"draft_entries"`
	NewSubmissions   int64 `json:"new_submissions"`
	MediaReady       int64 `json:"media_ready"`
	FailedMedia      int64 `json:"failed_media"`
}

type DraftFeature struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NewSubmission struct {
	ID          uuid.UUID `json:"id"`
	FeatureKey  string    `json:"feature_key"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	DataPreview string    `json:"data_preview"`
}

type FailedMedia struct {
	ID               uuid.UUID `json:"id"`
	OriginalFilename string    `json:"original_filename"`
	CreatedAt        time.Time `json:"created_at"`
}

type Attention struct {
	DraftFeatures  []DraftFeature  `json:"draft_features"`
	NewSubmissions []NewSubmission `json:"new_submissions"`
	FailedMedia    []FailedMedia   `json:"failed_media"`
}

type Activity struct {
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	FeatureKey *string   `json:"feature_key"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Overview struct {
	Counts         Counts     `json:"counts"`
	Attention      Attention  `json:"attention"`
	RecentActivity []Activity `json:"recent_activity"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Load(ctx context.Context) (Overview, error) {
	out := Overview{
		Attention: Attention{
			DraftFeatures:  []DraftFeature{},
			NewSubmissions: []NewSubmission{},
			FailedMedia:    []FailedMedia{},
		},
		RecentActivity: []Activity{},
	}
	if err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM features WHERE status = 'active'),
		  (SELECT count(*) FROM features WHERE status = 'archived'),
		  (SELECT count(*) FROM features WHERE draft_version_id IS NOT NULL),
		  (SELECT count(*) FROM feature_entries WHERE draft_version_id IS NOT NULL AND archived_at IS NULL),
		  (SELECT count(*) FROM form_submissions WHERE status = 'new' AND deleted_at IS NULL AND archived_at IS NULL),
		  (SELECT count(*) FROM media_objects WHERE status = 'ready'),
		  (SELECT count(*) FROM media_objects WHERE status = 'failed')`,
	).Scan(&out.Counts.FeaturesActive, &out.Counts.FeaturesArchived, &out.Counts.DraftFeatures,
		&out.Counts.DraftEntries, &out.Counts.NewSubmissions, &out.Counts.MediaReady, &out.Counts.FailedMedia); err != nil {
		return Overview{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, key, name, type, revision, updated_at
		  FROM features
		 WHERE draft_version_id IS NOT NULL AND status = 'active'
		 ORDER BY updated_at DESC
		 LIMIT 5`)
	if err != nil {
		return Overview{}, err
	}
	for rows.Next() {
		var f DraftFeature
		if err := rows.Scan(&f.ID, &f.Key, &f.Name, &f.Type, &f.Revision, &f.UpdatedAt); err != nil {
			rows.Close()
			return Overview{}, err
		}
		out.Attention.DraftFeatures = append(out.Attention.DraftFeatures, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, err
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
		SELECT s.id, f.key, s.status, s.created_at,
		       COALESCE(NULLIF(s.data->>'name',''), NULLIF(s.data->>'title',''),
		                NULLIF(s.data->>'email',''), left(s.data::text, 120))
		  FROM form_submissions s
		  JOIN features f ON f.id = s.feature_id
		 WHERE s.status = 'new' AND s.deleted_at IS NULL AND s.archived_at IS NULL
		 ORDER BY s.created_at DESC
		 LIMIT 5`)
	if err != nil {
		return Overview{}, err
	}
	for rows.Next() {
		var s NewSubmission
		if err := rows.Scan(&s.ID, &s.FeatureKey, &s.Status, &s.CreatedAt, &s.DataPreview); err != nil {
			rows.Close()
			return Overview{}, err
		}
		out.Attention.NewSubmissions = append(out.Attention.NewSubmissions, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, err
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
		SELECT id, original_filename, created_at
		  FROM media_objects
		 WHERE status = 'failed'
		 ORDER BY created_at DESC
		 LIMIT 5`)
	if err != nil {
		return Overview{}, err
	}
	for rows.Next() {
		var m FailedMedia
		if err := rows.Scan(&m.ID, &m.OriginalFilename, &m.CreatedAt); err != nil {
			rows.Close()
			return Overview{}, err
		}
		out.Attention.FailedMedia = append(out.Attention.FailedMedia, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Overview{}, err
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
		SELECT kind, title, feature_key, occurred_at FROM (
			(SELECT 'feature' AS kind, 'Feature: ' || name AS title, key AS feature_key, updated_at AS occurred_at
			   FROM features ORDER BY updated_at DESC LIMIT 10)
			UNION ALL
			(SELECT 'entry' AS kind, 'Запись в «' || f.name || '»' AS title, f.key AS feature_key, e.updated_at AS occurred_at
			   FROM feature_entries e JOIN features f ON f.id = e.feature_id
			  WHERE e.archived_at IS NULL
			  ORDER BY e.updated_at DESC LIMIT 10)
			UNION ALL
			(SELECT 'submission' AS kind, 'Заявка: ' || f.name AS title, f.key AS feature_key, s.created_at AS occurred_at
			   FROM form_submissions s JOIN features f ON f.id = s.feature_id
			  WHERE s.deleted_at IS NULL
			  ORDER BY s.created_at DESC LIMIT 10)
			UNION ALL
			(SELECT 'media' AS kind, 'Медиа: ' || original_filename AS title, NULL::text AS feature_key, created_at AS occurred_at
			   FROM media_objects ORDER BY created_at DESC LIMIT 10)
		) activity
		ORDER BY occurred_at DESC
		LIMIT 10`)
	if err != nil {
		return Overview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.Kind, &a.Title, &a.FeatureKey, &a.OccurredAt); err != nil {
			return Overview{}, err
		}
		out.RecentActivity = append(out.RecentActivity, a)
	}
	if err := rows.Err(); err != nil {
		return Overview{}, err
	}
	return out, nil
}
