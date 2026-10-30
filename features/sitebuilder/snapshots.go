package sitebuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/pkg/environment"
	"websiteengine/pkg/platform/validate"
	publicruntime "websiteengine/pkg/runtime"
)

var (
	ErrNotFound            = errors.New("site builder object not found")
	ErrConflict            = errors.New("site builder state changed")
	ErrInvalid             = errors.New("site builder validation failed")
	ErrSnapshotUnavailable = errors.New("no active site snapshot")
)

type DraftState struct {
	Revision  int64           `json:"revision"`
	Config    json.RawMessage `json:"config"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type SnapshotRecord struct {
	ID        uuid.UUID `json:"id"`
	Sequence  int64     `json:"sequence"`
	Name      string    `json:"name"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
}

type PinnedBuild struct {
	Key                       string                 `json:"key"`
	Name                      string                 `json:"name"`
	Version                   string                 `json:"version"`
	SchemaVersionID           uuid.UUID              `json:"schemaVersionId"`
	SettingsSchemaVersionID   *uuid.UUID             `json:"settingsSchemaVersionId,omitempty"`
	SubmissionSchemaVersionID *uuid.UUID             `json:"submissionSchemaVersionId,omitempty"`
	Digest                    string                 `json:"digest"`
	Manifest                  json.RawMessage        `json:"manifest"`
	Assets                    map[string]PinnedAsset `json:"assets"`
}

type PinnedAsset struct {
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type SnapshotPayload struct {
	FormatVersion int           `json:"formatVersion"`
	Config        SiteConfig    `json:"config"`
	Builds        []PinnedBuild `json:"builds"`
}

type SnapshotContent struct {
	EntryID    uuid.UUID       `json:"entryId"`
	VersionID  uuid.UUID       `json:"versionId"`
	Version    int             `json:"version"`
	FeatureKey string          `json:"featureKey"`
	SortOrder  int             `json:"sortOrder"`
	Data       json.RawMessage `json:"data"`
}

type PlacementRuntime struct {
	ID         string          `json:"id"`
	Slot       string          `json:"slot"`
	FeatureKey string          `json:"featureKey"`
	Version    string          `json:"version"`
	Digest     string          `json:"digest"`
	Mode       string          `json:"mode"`
	EntryURL   string          `json:"entryUrl"`
	StyleURLs  []string        `json:"styleUrls,omitempty"`
	Arguments  json.RawMessage `json:"arguments"`
	Content    json.RawMessage `json:"content,omitempty"`
	Collection string          `json:"collectionEndpoint,omitempty"`
	DetailLink *DetailLink     `json:"detailLink,omitempty"`
}

type RuntimeComposition struct {
	Snapshot        SnapshotRecord             `json:"snapshot"`
	Metadata        publicruntime.HeadMetadata `json:"metadata"`
	Route           Route                      `json:"route"`
	RouteArgs       map[string]string          `json:"routeArgs,omitempty"`
	Page            Page                       `json:"page"`
	SharedStyleURLs []string                   `json:"sharedStyleUrls,omitempty"`
	Placements      []PlacementRuntime         `json:"placements"`
}

type CollectionPage struct {
	Items      []SnapshotContent `json:"items"`
	NextOffset int               `json:"nextOffset,omitempty"`
	Total      int               `json:"total"`
}

// ContentFeature describes an editable feature from the active dev snapshot.
// It deliberately exposes the schema shipped with the pinned build rather
// than a mutable feature draft.
type ContentFeature struct {
	ID         uuid.UUID       `json:"id"`
	Key        string          `json:"key"`
	Name       string          `json:"name"`
	Mode       string          `json:"mode"`
	Version    string          `json:"version"`
	Schema     json.RawMessage `json:"schema"`
	Settings   json.RawMessage `json:"settingsSchema,omitempty"`
	Submission json.RawMessage `json:"submissionSchema,omitempty"`
}

type SnapshotService struct {
	pool   *pgxpool.Pool
	builds *BuildService
}

func NewSnapshotService(pool *pgxpool.Pool, builds *BuildService) *SnapshotService {
	return &SnapshotService{pool: pool, builds: builds}
}

func (s *SnapshotService) GetDraft(ctx context.Context) (DraftState, error) {
	if _, err := s.pool.Exec(ctx, `INSERT INTO site_draft_config (id,config,revision) VALUES (TRUE,'{"formatVersion":2,"pages":[{"id":"home","name":"Главная","kind":"page","placements":[]}],"routeTree":{"id":"root","type":"index","pageId":"home","children":[]}}'::jsonb,1) ON CONFLICT (id) DO NOTHING`); err != nil {
		return DraftState{}, err
	}
	var state DraftState
	if err := s.pool.QueryRow(ctx, `SELECT revision,config,updated_at FROM site_draft_config WHERE id=TRUE`).Scan(&state.Revision, &state.Config, &state.UpdatedAt); err != nil {
		return DraftState{}, err
	}
	return state, nil
}

func (s *SnapshotService) SaveDraft(ctx context.Context, actor uuid.UUID, expectedRevision int64, raw json.RawMessage) (DraftState, error) {
	cfg, err := ParseSiteConfig(raw)
	if err != nil {
		return DraftState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	registry, err := s.registryForConfig(ctx, cfg)
	if err != nil {
		return DraftState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := cfg.Validate(registry); err != nil {
		return DraftState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DraftState{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO site_draft_config (id,config,revision,updated_by) VALUES (TRUE,'{"formatVersion":2,"pages":[{"id":"home","name":"Главная","kind":"page","placements":[]}],"routeTree":{"id":"root","type":"index","pageId":"home","children":[]}}'::jsonb,1,$1) ON CONFLICT (id) DO NOTHING`, actor); err != nil {
		return DraftState{}, err
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_draft_config WHERE id=TRUE FOR UPDATE`).Scan(&current); err != nil {
		return DraftState{}, err
	}
	if current != expectedRevision {
		return DraftState{}, fmt.Errorf("%w: expected revision %d, current %d", ErrConflict, expectedRevision, current)
	}
	var state DraftState
	err = tx.QueryRow(ctx, `UPDATE site_draft_config SET config=$1::jsonb,revision=revision+1,updated_by=$2,updated_at=now() WHERE id=TRUE RETURNING revision,config,updated_at`, string(raw), actor).Scan(&state.Revision, &state.Config, &state.UpdatedAt)
	if err != nil {
		return DraftState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DraftState{}, err
	}
	return state, nil
}

func (s *SnapshotService) Publish(ctx context.Context, actor uuid.UUID, name string) (SnapshotRecord, error) {
	if _, err := s.GetMetadata(ctx); err != nil {
		return SnapshotRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SnapshotRecord{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var metadataRevision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_metadata_state WHERE id=TRUE FOR UPDATE`).Scan(&metadataRevision); err != nil {
		return SnapshotRecord{}, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT config FROM site_draft_config WHERE id=TRUE FOR UPDATE`).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SnapshotRecord{}, fmt.Errorf("%w: site draft is empty", ErrInvalid)
		}
		return SnapshotRecord{}, err
	}
	cfg, err := ParseSiteConfig(raw)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	registry, err := s.registryForConfig(ctx, cfg)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := cfg.Validate(registry); err != nil {
		return SnapshotRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	builds, err := s.pinnedBuilds(ctx, cfg)
	if err != nil {
		return SnapshotRecord{}, err
	}
	content, err := s.captureContent(ctx, tx, builds)
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := validateDetailKeys(cfg, builds, content); err != nil {
		return SnapshotRecord{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	payload := SnapshotPayload{FormatVersion: 1, Config: cfg, Builds: builds}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return SnapshotRecord{}, err
	}
	checksumInput, err := json.Marshal(payload)
	if err != nil {
		return SnapshotRecord{}, err
	}
	hash := sha256.Sum256(checksumInput)
	digest := hex.EncodeToString(hash[:])
	if name == "" {
		name = "Snapshot " + time.Now().UTC().Format("2006-01-02 15:04 UTC")
	}
	if len([]rune(name)) > 160 {
		return SnapshotRecord{}, fmt.Errorf("%w: snapshot name is too long", ErrInvalid)
	}
	var existing SnapshotRecord
	err = tx.QueryRow(ctx, `SELECT id,sequence,name,digest,created_at FROM site_snapshots WHERE digest=$1`, digest).
		Scan(&existing.ID, &existing.Sequence, &existing.Name, &existing.Digest, &existing.CreatedAt)
	if err == nil {
		metadataState, err := readMetadata(ctx, tx)
		if err != nil {
			return SnapshotRecord{}, err
		}
		if err := s.rebuildShellsTx(ctx, tx, environment.Dev, existing.ID, payload, metadataState.Dev, metadataState.DevRevision); err != nil {
			return SnapshotRecord{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE site_environments SET active_snapshot_id=$1,revision=revision+1,updated_at=now() WHERE key='dev'`, existing.ID); err != nil {
			return SnapshotRecord{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO site_snapshot_events (id,snapshot_id,environment,action,actor_token_id) VALUES ($1,$2,'dev','publish',$3)`, uuid.New(), existing.ID, actor); err != nil {
			return SnapshotRecord{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SnapshotRecord{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SnapshotRecord{}, err
	}
	var record SnapshotRecord
	record.ID = uuid.New()
	record.Name = name
	record.Digest = digest
	err = tx.QueryRow(ctx, `INSERT INTO site_snapshots (id,name,digest,payload,created_by) VALUES ($1,$2,$3,$4::jsonb,$5) RETURNING sequence,created_at`, record.ID, name, digest, string(payloadJSON), actor).Scan(&record.Sequence, &record.CreatedAt)
	if err != nil {
		return SnapshotRecord{}, err
	}
	metadataState, err := readMetadata(ctx, tx)
	if err != nil {
		return SnapshotRecord{}, err
	}
	if err := s.rebuildShellsTx(ctx, tx, environment.Dev, record.ID, payload, metadataState.Dev, metadataState.DevRevision); err != nil {
		return SnapshotRecord{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE site_environments SET active_snapshot_id=$1,revision=revision+1,updated_at=now() WHERE key='dev'`, record.ID)
	if err != nil {
		return SnapshotRecord{}, err
	}
	if tag.RowsAffected() != 1 {
		return SnapshotRecord{}, fmt.Errorf("%w: dev environment is not initialized", ErrInvalid)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO site_snapshot_events (id,snapshot_id,environment,action,actor_token_id) VALUES ($1,$2,'dev','publish',$3)`, uuid.New(), record.ID, actor); err != nil {
		return SnapshotRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SnapshotRecord{}, err
	}
	return record, nil
}

func (s *SnapshotService) List(ctx context.Context, limit int) ([]SnapshotRecord, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id,sequence,name,digest,created_at FROM site_snapshots ORDER BY sequence DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SnapshotRecord{}
	for rows.Next() {
		var r SnapshotRecord
		if err := rows.Scan(&r.ID, &r.Sequence, &r.Name, &r.Digest, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SnapshotService) Promote(ctx context.Context, actor, snapshotID uuid.UUID) (SnapshotRecord, error) {
	return s.setEnvironment(ctx, actor, snapshotID, environment.Prod, "promote", true)
}

func (s *SnapshotService) Rollback(ctx context.Context, actor, snapshotID uuid.UUID, env environment.Environment) (SnapshotRecord, error) {
	return s.setEnvironment(ctx, actor, snapshotID, env, "rollback", false)
}

func (s *SnapshotService) setEnvironment(ctx context.Context, actor, snapshotID uuid.UUID, env environment.Environment, action string, requireDev bool) (SnapshotRecord, error) {
	if env != environment.Dev && env != environment.Prod {
		return SnapshotRecord{}, fmt.Errorf("%w: unsupported environment", ErrInvalid)
	}
	if _, err := s.GetMetadata(ctx); err != nil {
		return SnapshotRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SnapshotRecord{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var metadataRevision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_metadata_state WHERE id=TRUE FOR UPDATE`).Scan(&metadataRevision); err != nil {
		return SnapshotRecord{}, err
	}
	var record SnapshotRecord
	if err := tx.QueryRow(ctx, `SELECT id,sequence,name,digest,created_at FROM site_snapshots WHERE id=$1`, snapshotID).Scan(&record.ID, &record.Sequence, &record.Name, &record.Digest, &record.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SnapshotRecord{}, ErrNotFound
		}
		return SnapshotRecord{}, err
	}
	if requireDev {
		var active *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT active_snapshot_id FROM site_environments WHERE key='dev'`).Scan(&active); err != nil {
			return SnapshotRecord{}, err
		}
		if active == nil || *active != snapshotID {
			return SnapshotRecord{}, fmt.Errorf("%w: only the active dev snapshot can be promoted", ErrConflict)
		}
	}
	var payloadRaw []byte
	if err := tx.QueryRow(ctx, `SELECT payload FROM site_snapshots WHERE id=$1`, snapshotID).Scan(&payloadRaw); err != nil {
		return SnapshotRecord{}, err
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return SnapshotRecord{}, err
	}
	metadataState, err := readMetadata(ctx, tx)
	if err != nil {
		return SnapshotRecord{}, err
	}
	metadata, revision := metadataState.Dev, metadataState.DevRevision
	if env == environment.Prod {
		metadata, revision = metadataState.Prod, metadataState.ProdRevision
	}
	if err := s.rebuildShellsTx(ctx, tx, env, snapshotID, payload, metadata, revision); err != nil {
		return SnapshotRecord{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE site_environments SET active_snapshot_id=$1,revision=revision+1,updated_at=now() WHERE key=$2`, snapshotID, env.String())
	if err != nil {
		return SnapshotRecord{}, err
	}
	if tag.RowsAffected() != 1 {
		return SnapshotRecord{}, fmt.Errorf("%w: environment is not initialized", ErrInvalid)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO site_snapshot_events (id,snapshot_id,environment,action,actor_token_id) VALUES ($1,$2,$3,$4,$5)`, uuid.New(), snapshotID, env.String(), action, actor); err != nil {
		return SnapshotRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SnapshotRecord{}, err
	}
	return record, nil
}

func (s *SnapshotService) Active(ctx context.Context, env environment.Environment) (SnapshotRecord, SnapshotPayload, error) {
	var record SnapshotRecord
	var payloadRaw []byte
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.sequence,s.name,s.digest,s.created_at,s.payload FROM site_environments e JOIN site_snapshots s ON s.id=e.active_snapshot_id WHERE e.key=$1`, env.String()).Scan(&record.ID, &record.Sequence, &record.Name, &record.Digest, &record.CreatedAt, &payloadRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return SnapshotRecord{}, SnapshotPayload{}, ErrSnapshotUnavailable
	}
	if err != nil {
		return SnapshotRecord{}, SnapshotPayload{}, err
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return SnapshotRecord{}, SnapshotPayload{}, err
	}
	return record, payload, nil
}

func (s *SnapshotService) Runtime(ctx context.Context, env environment.Environment, requestPath string, query url.Values) (RuntimeComposition, error) {
	record, payload, err := s.Active(ctx, env)
	if err != nil {
		return RuntimeComposition{}, err
	}
	routes, err := CompileRouteTree(payload.Config.RouteTree)
	if err != nil {
		return RuntimeComposition{}, err
	}
	route, args, ok, err := MatchRoute(routes, requestPath)
	if err != nil {
		return RuntimeComposition{}, err
	}
	if !ok {
		return RuntimeComposition{}, ErrNotFound
	}
	var page Page
	for _, candidate := range payload.Config.Pages {
		if candidate.ID == route.PageID {
			page = candidate
			break
		}
	}
	metadataState, err := s.GetMetadata(ctx)
	if err != nil {
		return RuntimeComposition{}, err
	}
	metadata := metadataState.Dev
	if env == environment.Prod {
		metadata = metadataState.Prod
	}
	result := RuntimeComposition{Snapshot: record, Metadata: metadata.ForPage(page.ID), Route: route, RouteArgs: args, Page: page, Placements: []PlacementRuntime{}}
	sharedStyleSeen := make(map[string]bool)
	registry := payloadBuildRegistry(payload.Builds)
	for _, placement := range page.Placements {
		info, ok := registry.Build(placement.Build.FeatureKey, placement.Build.Version)
		if !ok {
			return RuntimeComposition{}, fmt.Errorf("snapshot references missing build %s@%s", placement.Build.FeatureKey, placement.Build.Version)
		}
		arguments, err := ResolveArguments(info.Manifest.Arguments, placement.Arguments, args, query)
		if err != nil {
			return RuntimeComposition{}, err
		}
		var content json.RawMessage
		var collection string
		if placement.CollectionSource != nil {
			var argumentMap map[string]json.RawMessage
			_ = json.Unmarshal(arguments, &argumentMap)
			lookupValue := scalarJSON(argumentMap[placement.CollectionSource.Argument])
			entry, err := s.findEnvironmentRecord(ctx, env, placement.CollectionSource.FeatureKey, placement.CollectionSource.Field, lookupValue)
			if err != nil {
				return RuntimeComposition{}, err
			}
			content = entry.Data
		} else if info.Manifest.Content.Mode == "single" || info.Manifest.Content.Mode == "form" {
			var data []byte
			column := environmentContentColumn(env)
			err := s.pool.QueryRow(ctx, `SELECT v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.`+column+` WHERE f.key=$1 AND e.single_slot=TRUE AND e.archived_at IS NULL ORDER BY e.created_at,e.id LIMIT 1`, info.Manifest.Key).Scan(&data)
			if err == nil {
				content = data
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return RuntimeComposition{}, err
			}
		} else if info.Manifest.Content.Mode == "collection" {
			if len(info.Manifest.Content.SettingsSchema) > 0 {
				var data []byte
				column := environmentContentColumn(env)
				err := s.pool.QueryRow(ctx, `SELECT v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.`+column+` WHERE f.key=$1 AND e.single_slot=TRUE AND e.archived_at IS NULL ORDER BY e.created_at,e.id LIMIT 1`, info.Manifest.Key).Scan(&data)
				if err == nil {
					content = data
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return RuntimeComposition{}, err
				}
			}
			collection = "/api/v2/content/" + info.Manifest.Key + "/records"
		}
		styleURLs := make([]string, 0, len(info.Manifest.Styles))
		for _, style := range info.Manifest.SharedStyles {
			url := "/feature-assets/" + info.Digest + "/" + style
			if !sharedStyleSeen[style] {
				result.SharedStyleURLs = append(result.SharedStyleURLs, url)
				sharedStyleSeen[style] = true
			}
		}
		for _, style := range info.Manifest.Styles {
			styleURLs = append(styleURLs, "/feature-assets/"+info.Digest+"/"+style)
		}
		result.Placements = append(result.Placements, PlacementRuntime{ID: placement.ID, Slot: placement.Slot, FeatureKey: info.Manifest.Key, Version: info.Version, Digest: info.Digest, Mode: info.Manifest.Content.Mode,
			EntryURL: "/feature-assets/" + info.Digest + "/" + info.Manifest.Entry, StyleURLs: styleURLs, Arguments: arguments, Content: content, Collection: collection, DetailLink: placement.DetailLink})
	}
	return result, nil
}

func (s *SnapshotService) Collection(ctx context.Context, env environment.Environment, key, field, value string, offset, limit int) (CollectionPage, error) {
	_, payload, err := s.Active(ctx, env)
	if err != nil {
		return CollectionPage{}, err
	}
	var collectionBuild *PinnedBuild
	for i := range payload.Builds {
		if payload.Builds[i].Key == key {
			collectionBuild = &payload.Builds[i]
			break
		}
	}
	if collectionBuild == nil {
		return CollectionPage{}, ErrNotFound
	}
	var manifest Manifest
	if err := json.Unmarshal(collectionBuild.Manifest, &manifest); err != nil {
		return CollectionPage{}, err
	}
	if manifest.Content.Mode != "collection" {
		return CollectionPage{}, ErrNotFound
	}
	if field != "" && !hasSchemaProperty(manifest.Content.Schema, field) {
		return CollectionPage{}, fmt.Errorf("%w: unknown collection field %q", ErrInvalid, field)
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	column := environmentContentColumn(env)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.`+column+` WHERE f.key=$1 AND e.single_slot=FALSE AND e.archived_at IS NULL AND ($2='' OR v.data->>$2=$3)`, key, field, value).Scan(&total); err != nil {
		return CollectionPage{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,v.id,e.sort_order,v.version,v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.`+column+` WHERE f.key=$1 AND e.single_slot=FALSE AND e.archived_at IS NULL AND ($2='' OR v.data->>$2=$3) ORDER BY e.sort_order,e.created_at,e.id OFFSET $4 LIMIT $5`, key, field, value, offset, limit)
	if err != nil {
		return CollectionPage{}, err
	}
	defer rows.Close()
	page := CollectionPage{Items: []SnapshotContent{}, Total: total}
	for rows.Next() {
		var item SnapshotContent
		item.FeatureKey = key
		var raw []byte
		if err := rows.Scan(&item.EntryID, &item.VersionID, &item.SortOrder, &item.Version, &raw); err != nil {
			return CollectionPage{}, err
		}
		item.Data = raw
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return CollectionPage{}, err
	}
	if offset+len(page.Items) < total {
		page.NextOffset = offset + len(page.Items)
	}
	return page, nil
}

func (s *SnapshotService) FormSchema(ctx context.Context, env environment.Environment, key string) (uuid.UUID, json.RawMessage, error) {
	_, payload, err := s.Active(ctx, env)
	if err != nil {
		return uuid.Nil, nil, err
	}
	for _, build := range payload.Builds {
		if build.Key != key {
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(build.Manifest, &manifest); err != nil {
			return uuid.Nil, nil, err
		}
		if manifest.Content.Mode != "form" {
			return uuid.Nil, nil, ErrNotFound
		}
		if submissionSchema := manifest.SubmissionSchema(); len(submissionSchema) > 0 {
			if build.SubmissionSchemaVersionID == nil {
				return uuid.Nil, nil, fmt.Errorf("form feature %q has no pinned submission schema version", key)
			}
			return *build.SubmissionSchemaVersionID, submissionSchema, nil
		}
		return build.SchemaVersionID, manifest.Content.Schema, nil
	}
	return uuid.Nil, nil, ErrNotFound
}

// ContentSchema returns the editor schema pinned by the active site snapshot.
// It makes content authoring follow the version that is actually composed on
// the site, even if newer feature builds have been uploaded but not promoted.
func (s *SnapshotService) ContentSchema(ctx context.Context, env environment.Environment, key string) (uuid.UUID, string, json.RawMessage, error) {
	_, payload, err := s.Active(ctx, env)
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	for _, build := range payload.Builds {
		if build.Key != key {
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(build.Manifest, &manifest); err != nil {
			return uuid.Nil, "", nil, err
		}
		return build.SchemaVersionID, manifest.Content.Mode, manifest.Content.Schema, nil
	}
	return uuid.Nil, "", nil, ErrNotFound
}

// SettingsSchema returns collection-level editorial settings pinned by an
// active site snapshot. It is separate from the per-record collection schema.
func (s *SnapshotService) SettingsSchema(ctx context.Context, env environment.Environment, key string) (uuid.UUID, string, json.RawMessage, error) {
	_, payload, err := s.Active(ctx, env)
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	for _, build := range payload.Builds {
		if build.Key != key {
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(build.Manifest, &manifest); err != nil {
			return uuid.Nil, "", nil, err
		}
		if len(manifest.Content.SettingsSchema) == 0 || build.SettingsSchemaVersionID == nil {
			return uuid.Nil, "", nil, ErrNotFound
		}
		return *build.SettingsSchemaVersionID, manifest.Content.Mode, manifest.Content.SettingsSchema, nil
	}
	return uuid.Nil, "", nil, ErrNotFound
}

// ContentCatalog lists only features pinned by the active dev site snapshot.
func (s *SnapshotService) ContentCatalog(ctx context.Context) ([]ContentFeature, error) {
	_, payload, err := s.Active(ctx, environment.Dev)
	if err != nil {
		return nil, err
	}
	items := make([]ContentFeature, 0, len(payload.Builds))
	for _, build := range payload.Builds {
		var manifest Manifest
		if err := json.Unmarshal(build.Manifest, &manifest); err != nil {
			return nil, err
		}
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM features WHERE key=$1 AND status='active'`, build.Key).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		item := ContentFeature{ID: id, Key: build.Key, Name: build.Name, Mode: manifest.Content.Mode, Version: build.Version, Schema: manifest.Content.Schema, Settings: manifest.Content.SettingsSchema}
		if submissionSchema := manifest.SubmissionSchema(); len(submissionSchema) > 0 {
			item.Submission = submissionSchema
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *SnapshotService) findEnvironmentRecord(ctx context.Context, env environment.Environment, key, field, value string) (SnapshotContent, error) {
	column := environmentContentColumn(env)
	rows, err := s.pool.Query(ctx, `SELECT e.id,v.id,e.sort_order,v.version,v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.`+column+` WHERE f.key=$1 AND e.single_slot=FALSE AND e.archived_at IS NULL AND ($2='' OR v.data->>$2=$3) ORDER BY e.sort_order,e.created_at,e.id LIMIT 2`, key, field, value)
	if err != nil {
		return SnapshotContent{}, err
	}
	defer rows.Close()
	var found *SnapshotContent
	for rows.Next() {
		var item SnapshotContent
		var raw []byte
		item.FeatureKey = key
		if err := rows.Scan(&item.EntryID, &item.VersionID, &item.SortOrder, &item.Version, &raw); err != nil {
			return SnapshotContent{}, err
		}
		item.Data = raw
		if found != nil {
			return SnapshotContent{}, fmt.Errorf("%w: detail lookup matched multiple records", ErrConflict)
		}
		found = &item
	}
	if err := rows.Err(); err != nil {
		return SnapshotContent{}, err
	}
	if found == nil {
		return SnapshotContent{}, ErrNotFound
	}
	return *found, nil
}

func (s *SnapshotService) registryForConfig(ctx context.Context, cfg SiteConfig) (buildRegistry, error) {
	refs := configBuildRefs(cfg)
	registry := buildRegistry{items: map[string]BuildInfo{}}
	for key, version := range refs {
		record, ok, err := s.builds.Build(ctx, key, version)
		if err != nil {
			return registry, err
		}
		if !ok {
			return registry, fmt.Errorf("feature build %s@%s was not found", key, version)
		}
		var manifest Manifest
		if err := json.Unmarshal(record.Manifest, &manifest); err != nil {
			return registry, err
		}
		assetDigests := make(map[string]string, len(record.Assets))
		for assetPath, asset := range record.Assets {
			assetDigests[assetPath] = asset.SHA256
		}
		registry.items[buildID(key, version)] = BuildInfo{Manifest: manifest, Digest: record.Digest, Version: record.Version, AssetDigests: assetDigests}
	}
	return registry, nil
}

func (s *SnapshotService) pinnedBuilds(ctx context.Context, cfg SiteConfig) ([]PinnedBuild, error) {
	refs := configBuildRefs(cfg)
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PinnedBuild, 0, len(keys))
	for _, key := range keys {
		version := refs[key]
		record, ok, err := s.builds.Build(ctx, key, version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("feature build %s@%s was not found", key, version)
		}
		var manifestJSON json.RawMessage
		if err := json.Unmarshal(record.Manifest, &manifestJSON); err != nil {
			return nil, err
		}
		assets := make(map[string]PinnedAsset, len(record.Assets))
		for name, a := range record.Assets {
			assets[name] = PinnedAsset{ContentType: a.ContentType, Size: a.Size, SHA256: a.SHA256}
		}
		out = append(out, PinnedBuild{Key: record.Key, Name: record.Name, Version: record.Version, SchemaVersionID: record.SchemaVersionID, SettingsSchemaVersionID: record.SettingsSchemaVersionID, SubmissionSchemaVersionID: record.SubmissionSchemaVersionID, Digest: record.Digest, Manifest: manifestJSON, Assets: assets})
	}
	return out, nil
}

func (s *SnapshotService) captureContent(ctx context.Context, tx pgx.Tx, builds []PinnedBuild) ([]SnapshotContent, error) {
	var all []SnapshotContent
	for _, build := range builds {
		var manifest Manifest
		if err := json.Unmarshal(build.Manifest, &manifest); err != nil {
			return nil, err
		}
		singleSlot := manifest.Content.Mode == "single" || manifest.Content.Mode == "form"
		if manifest.Content.Mode == "collection" && len(manifest.Content.SettingsSchema) > 0 {
			var settingsCount int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.dev_version_id WHERE f.key=$1 AND f.status='active' AND e.archived_at IS NULL AND e.dev_version_id IS NOT NULL AND e.single_slot=TRUE`, build.Key).Scan(&settingsCount); err != nil {
				return nil, err
			}
			if settingsCount > 1 {
				return nil, fmt.Errorf("collection feature %q has more than one published settings entry", build.Key)
			}
			var settings []byte
			err := tx.QueryRow(ctx, `SELECT v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.dev_version_id WHERE f.key=$1 AND f.status='active' AND e.archived_at IS NULL AND e.dev_version_id IS NOT NULL AND e.single_slot=TRUE LIMIT 1`, build.Key).Scan(&settings)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				fieldErrors, err := validate.Validate(manifest.Content.SettingsSchema, settings)
				if err != nil {
					return nil, err
				}
				if len(fieldErrors) > 0 {
					return nil, fmt.Errorf("collection feature %q settings are incompatible with build %s: %s", build.Key, build.Version, fieldErrors[0].Message)
				}
			}
		}
		rows, err := tx.Query(ctx, `SELECT e.id,e.sort_order,v.id,v.version,v.data FROM features f JOIN feature_entries e ON e.feature_id=f.id JOIN feature_entry_versions v ON v.id=e.dev_version_id WHERE f.key=$1 AND f.status='active' AND e.archived_at IS NULL AND e.dev_version_id IS NOT NULL AND e.single_slot=$2 ORDER BY e.sort_order,e.created_at,e.id`, build.Key, singleSlot)
		if err != nil {
			return nil, err
		}
		var featureContent []SnapshotContent
		for rows.Next() {
			var item SnapshotContent
			var raw []byte
			item.FeatureKey = build.Key
			if err := rows.Scan(&item.EntryID, &item.SortOrder, &item.VersionID, &item.Version, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			fieldErrors, err := validate.Validate(manifest.Content.Schema, raw)
			if err != nil {
				rows.Close()
				return nil, err
			}
			if len(fieldErrors) > 0 {
				rows.Close()
				return nil, fmt.Errorf("feature %s entry %s is incompatible with build %s: %s", build.Key, item.EntryID, build.Version, fieldErrors[0].Message)
			}
			item.Data = raw
			featureContent = append(featureContent, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if singleSlot && len(featureContent) > 1 {
			return nil, fmt.Errorf("single feature %q has more than one published content entry", build.Key)
		}
		all = append(all, featureContent...)
	}
	return all, nil
}

func validateDetailKeys(cfg SiteConfig, builds []PinnedBuild, content []SnapshotContent) error {
	buildByKey := map[string]PinnedBuild{}
	for _, build := range builds {
		buildByKey[build.Key] = build
	}
	dataByFeature := map[string][]json.RawMessage{}
	for _, item := range content {
		dataByFeature[item.FeatureKey] = append(dataByFeature[item.FeatureKey], item.Data)
	}
	for _, page := range cfg.Pages {
		for _, placement := range page.Placements {
			if placement.CollectionSource != nil {
				source := placement.CollectionSource
				if _, ok := buildByKey[source.FeatureKey]; !ok {
					return fmt.Errorf("detail collection %q is not included in the snapshot", source.FeatureKey)
				}
				seen := map[string]bool{}
				for _, raw := range dataByFeature[source.FeatureKey] {
					var data map[string]json.RawMessage
					if err := json.Unmarshal(raw, &data); err != nil {
						return err
					}
					key, ok := data[source.Field]
					if !ok || string(key) == "null" {
						return fmt.Errorf("detail lookup field %q is missing from a published record", source.Field)
					}
					var scalar any
					if err := json.Unmarshal(key, &scalar); err != nil {
						return err
					}
					switch scalar.(type) {
					case string, float64, bool:
					default:
						return fmt.Errorf("detail lookup field %q must contain a scalar value", source.Field)
					}
					normalized, _ := json.Marshal(scalar)
					if seen[string(normalized)] {
						return fmt.Errorf("detail lookup field %q is ambiguous in collection %q", source.Field, source.FeatureKey)
					}
					seen[string(normalized)] = true
				}
			}
			if placement.DetailLink != nil {
				sourceFound := false
				for _, candidatePage := range cfg.Pages {
					for _, detailPlacement := range candidatePage.Placements {
						if detailPlacement.CollectionSource != nil && detailPlacement.CollectionSource.FeatureKey == placement.Build.FeatureKey && detailPlacement.CollectionSource.Field == placement.DetailLink.Field {
							sourceFound = true
						}
					}
				}
				if !sourceFound {
					return fmt.Errorf("detail link field %q has no matching detail-page collection binding", placement.DetailLink.Field)
				}
			}
		}
	}
	return nil
}

func configBuildRefs(cfg SiteConfig) map[string]string {
	refs := map[string]string{}
	for _, page := range cfg.Pages {
		for _, placement := range page.Placements {
			refs[placement.Build.FeatureKey] = placement.Build.Version
			if placement.CollectionSource != nil {
				refs[placement.CollectionSource.FeatureKey] = placement.CollectionSource.Version
			}
		}
	}
	return refs
}
func buildID(key, version string) string { return key + "@" + version }
func scalarJSON(value json.RawMessage) string {
	var v any
	if json.Unmarshal(value, &v) != nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return strings.TrimSpace(string(value))
	}
}

// The environment name is an enum controlled by the listener, so using the
// corresponding fixed column here is safe and keeps the query parameterized.
func environmentContentColumn(env environment.Environment) string {
	if env == environment.Prod {
		return "prod_version_id"
	}
	return "dev_version_id"
}

type buildRegistry struct{ items map[string]BuildInfo }

func (r buildRegistry) Build(key, version string) (BuildInfo, bool) {
	value, ok := r.items[buildID(key, version)]
	return value, ok
}

func payloadBuildRegistry(builds []PinnedBuild) buildRegistry {
	registry := buildRegistry{items: map[string]BuildInfo{}}
	for _, build := range builds {
		var manifest Manifest
		if json.Unmarshal(build.Manifest, &manifest) == nil {
			assetDigests := make(map[string]string, len(build.Assets))
			for assetPath, asset := range build.Assets {
				assetDigests[assetPath] = asset.SHA256
			}
			registry.items[buildID(build.Key, build.Version)] = BuildInfo{Manifest: manifest, Digest: build.Digest, Version: build.Version, AssetDigests: assetDigests}
		}
	}
	return registry
}
