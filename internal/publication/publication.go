// Package publication records content lifecycle events for auditing.
package publication

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"websiteengine/internal/environment"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Event struct {
	ID          uuid.UUID
	EntityKind  string
	FeatureID   uuid.UUID
	EntryID     *uuid.UUID
	VersionID   uuid.UUID
	Environment environment.Environment
	Action      string
	ActorID     *uuid.UUID
	Metadata    map[string]any
	CreatedAt   time.Time
}

func Insert(ctx context.Context, db DBTX, e Event) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	raw, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO content_publication_events
			(id, entity_kind, feature_id, entry_id, version_id, environment, action, actor_token_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.ID, e.EntityKind, e.FeatureID, e.EntryID, e.VersionID,
		e.Environment.String(), e.Action, e.ActorID, raw)
	return err
}
