// Package publication records content lifecycle events for auditing.
package publication

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"websiteengine/pkg/environment"
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

// Lister reads publication audit events for administrative views.
type Lister interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// EventRow is a stored publication audit event as returned to the admin API.
type EventRow struct {
	ID          uuid.UUID               `json:"id"`
	EntityKind  string                  `json:"entity_kind"`
	FeatureID   uuid.UUID               `json:"feature_id"`
	FeatureKey  string                  `json:"feature_key"`
	EntryID     *uuid.UUID              `json:"entry_id,omitempty"`
	VersionID   uuid.UUID               `json:"version_id"`
	Environment environment.Environment `json:"environment"`
	Action      string                  `json:"action"`
	ActorID     *uuid.UUID              `json:"actor_token_id,omitempty"`
	Metadata    json.RawMessage         `json:"metadata"`
	CreatedAt   time.Time               `json:"created_at"`
}

// ListParams filters the audit feed. Environment and EntityKind are optional:
// empty values mean "no filter". EntityKind values are feature and entry.
type ListParams struct {
	FeatureID   *uuid.UUID
	Environment environment.Environment
	EntityKind  string
	Action      string
	BeforeTime  *time.Time
	BeforeID    *uuid.UUID
	Limit       int
}

// ListParamsValid reports whether the given entity_kind filter is accepted.
func ListParamsValid(kind string) bool { return kind == "" || kind == "feature" || kind == "entry" }

// ListEvents returns the newest audit events in cursor order. The returned
// next-cursor is empty on the last page.
func ListEvents(ctx context.Context, db Lister, p ListParams) ([]EventRow, string, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	where := " WHERE TRUE"
	args := []any{}
	cond := func(next any) string {
		args = append(args, next)
		return "$" + strconv.Itoa(len(args))
	}
	if p.EntityKind != "" {
		where += " AND e.entity_kind = " + cond(p.EntityKind)
	}
	if p.FeatureID != nil {
		where += " AND e.feature_id = " + cond(*p.FeatureID)
	}
	if p.Environment.Valid() {
		where += " AND e.environment = " + cond(p.Environment.String())
	}
	if p.Action != "" {
		where += " AND e.action = " + cond(p.Action)
	}
	if p.BeforeTime != nil && p.BeforeID != nil {
		where += " AND (e.created_at, e.id) < (" + cond(*p.BeforeTime) + ", " + cond(*p.BeforeID) + ")"
	}

	query := `
		SELECT e.id, e.entity_kind, e.feature_id, f.key, e.entry_id, e.version_id,
		       e.environment, e.action, e.actor_token_id, e.metadata, e.created_at
		  FROM content_publication_events e
		  JOIN features f ON f.id = e.feature_id` + where + `
		 ORDER BY e.created_at DESC, e.id DESC
		 LIMIT ` + strconv.Itoa(p.Limit+1)
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []EventRow{}
	for rows.Next() {
		var ev EventRow
		if err := rows.Scan(&ev.ID, &ev.EntityKind, &ev.FeatureID, &ev.FeatureKey, &ev.EntryID,
			&ev.VersionID, &ev.Environment, &ev.Action, &ev.ActorID, &ev.Metadata, &ev.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > p.Limit {
		out = out[:p.Limit]
		last := out[len(out)-1]
		next = response_cursor(last.CreatedAt, last.ID)
	}
	return out, next, nil
}

func response_cursor(t time.Time, id uuid.UUID) string {
	raw, _ := json.Marshal(struct {
		CreatedAt time.Time `json:"t"`
		ID        string    `json:"i"`
	}{CreatedAt: t.UTC(), ID: id.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeEventCursor parses an opaque next-cursor. The second return is false
// when the cursor is malformed.
func DecodeEventCursor(raw string) (*time.Time, *uuid.UUID, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, nil, false
	}
	var c struct {
		CreatedAt time.Time `json:"t"`
		ID        string    `json:"i"`
	}
	if err := json.Unmarshal(decoded, &c); err != nil {
		return nil, nil, false
	}
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return nil, nil, false
	}
	t, idPtr := c.CreatedAt, id
	return &t, &idPtr, true
}
