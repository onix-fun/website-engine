package sitebuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"websiteengine/pkg/environment"
	publicruntime "websiteengine/pkg/runtime"
)

type SiteMetadata struct {
	FormatVersion  int                             `json:"formatVersion"`
	SiteName       string                          `json:"siteName"`
	FaviconMediaID string                          `json:"faviconMediaId,omitempty"`
	Description    string                          `json:"description,omitempty"`
	OGTitle        string                          `json:"ogTitle,omitempty"`
	OGDescription  string                          `json:"ogDescription,omitempty"`
	OGImageMediaID string                          `json:"ogImageMediaId,omitempty"`
	Pages          map[string]SiteMetadataOverride `json:"pages,omitempty"`
}

type SiteMetadataOverride struct {
	SiteName       *string `json:"siteName,omitempty"`
	FaviconMediaID *string `json:"faviconMediaId,omitempty"`
	Description    *string `json:"description,omitempty"`
	OGTitle        *string `json:"ogTitle,omitempty"`
	OGDescription  *string `json:"ogDescription,omitempty"`
	OGImageMediaID *string `json:"ogImageMediaId,omitempty"`
}

type SiteMetadataState struct {
	Revision     int64          `json:"revision"`
	DevRevision  int64          `json:"devRevision"`
	ProdRevision int64          `json:"prodRevision"`
	Pages        []MetadataPage `json:"pages"`
	Draft        SiteMetadata   `json:"draft"`
	Dev          SiteMetadata   `json:"dev"`
	Prod         SiteMetadata   `json:"prod"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type MetadataPage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func DefaultSiteMetadata() SiteMetadata {
	return SiteMetadata{FormatVersion: 1, SiteName: "Website", Pages: map[string]SiteMetadataOverride{}}
}

func ParseSiteMetadata(raw []byte) (SiteMetadata, error) {
	var metadata SiteMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return SiteMetadata{}, fmt.Errorf("decode site metadata: %w", err)
	}
	if err := metadata.Validate(); err != nil {
		return SiteMetadata{}, err
	}
	if metadata.Pages == nil {
		metadata.Pages = map[string]SiteMetadataOverride{}
	}
	return metadata, nil
}

func (metadata SiteMetadata) Validate() error {
	if metadata.FormatVersion != 1 {
		return errors.New("metadata formatVersion must be 1")
	}
	if len(strings.TrimSpace(metadata.SiteName)) == 0 || len([]rune(metadata.SiteName)) > 200 {
		return errors.New("siteName must contain between 1 and 200 characters")
	}
	for label, value := range map[string]string{
		"description": metadata.Description,
		"ogTitle":     metadata.OGTitle, "ogDescription": metadata.OGDescription,
	} {
		if len([]rune(value)) > 2048 {
			return fmt.Errorf("%s is too long", label)
		}
	}
	if err := validateMetadataMediaID("faviconMediaId", metadata.FaviconMediaID); err != nil {
		return err
	}
	if err := validateMetadataMediaID("ogImageMediaId", metadata.OGImageMediaID); err != nil {
		return err
	}
	for pageID, override := range metadata.Pages {
		if strings.TrimSpace(pageID) == "" || len(pageID) > 100 {
			return errors.New("page metadata key is invalid")
		}
		for label, value := range map[string]*string{
			"siteName":    override.SiteName,
			"description": override.Description, "ogTitle": override.OGTitle,
			"ogDescription": override.OGDescription,
		} {
			if value != nil && len([]rune(*value)) > 2048 {
				return fmt.Errorf("page %q %s is too long", pageID, label)
			}
		}
		if override.SiteName != nil && (strings.TrimSpace(*override.SiteName) == "" || len([]rune(*override.SiteName)) > 200) {
			return fmt.Errorf("page %q siteName must contain between 1 and 200 characters", pageID)
		}
		if override.FaviconMediaID != nil {
			if err := validateMetadataMediaID("faviconMediaId", *override.FaviconMediaID); err != nil {
				return fmt.Errorf("page %q: %w", pageID, err)
			}
		}
		if override.OGImageMediaID != nil {
			if err := validateMetadataMediaID("ogImageMediaId", *override.OGImageMediaID); err != nil {
				return fmt.Errorf("page %q: %w", pageID, err)
			}
		}
	}
	return nil
}

func validateMetadataMediaID(name, raw string) error {
	if raw == "" {
		return nil
	}
	if _, err := uuid.Parse(raw); err != nil {
		return fmt.Errorf("%s must reference a media object", name)
	}
	return nil
}

func (s *SnapshotService) validateMetadataMedia(ctx context.Context, queryer metadataQueryer, metadata SiteMetadata) error {
	ids := []string{metadata.FaviconMediaID, metadata.OGImageMediaID}
	for _, override := range metadata.Pages {
		if override.FaviconMediaID != nil {
			ids = append(ids, *override.FaviconMediaID)
		}
		if override.OGImageMediaID != nil {
			ids = append(ids, *override.OGImageMediaID)
		}
	}
	seen := map[string]bool{}
	for _, raw := range ids {
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		id, err := uuid.Parse(raw)
		if err != nil {
			return err
		}
		var ready bool
		if err := queryer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_objects WHERE id=$1 AND status='ready')`, id).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("metadata references unavailable media object %s", raw)
		}
	}
	return nil
}

func (metadata SiteMetadata) ForPage(pageID string) publicruntime.HeadMetadata {
	result := publicruntime.HeadMetadata{
		Title: metadata.SiteName, Description: metadata.Description,
		FaviconURL: mediaURL(metadata.FaviconMediaID), OGTitle: metadata.OGTitle, OGDescription: metadata.OGDescription,
		OGImageURL: mediaURL(metadata.OGImageMediaID),
	}
	if result.OGTitle == "" {
		result.OGTitle = result.Title
	}
	if result.OGDescription == "" {
		result.OGDescription = result.Description
	}
	if override, ok := metadata.Pages[pageID]; ok {
		if override.SiteName != nil {
			result.Title = *override.SiteName
		}
		if override.FaviconMediaID != nil {
			result.FaviconURL = mediaURL(*override.FaviconMediaID)
		}
		if override.Description != nil {
			result.Description = *override.Description
		}
		if override.OGTitle != nil {
			result.OGTitle = *override.OGTitle
		}
		if override.OGDescription != nil {
			result.OGDescription = *override.OGDescription
		}
		if override.OGImageMediaID != nil {
			result.OGImageURL = mediaURL(*override.OGImageMediaID)
		}
	}
	if result.OGTitle == "" {
		result.OGTitle = result.Title
	}
	if result.OGDescription == "" {
		result.OGDescription = result.Description
	}
	return result
}

func mediaURL(id string) string {
	if id == "" {
		return ""
	}
	return "/media/" + id
}

type metadataQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readMetadata(ctx context.Context, queryer metadataQueryer) (SiteMetadataState, error) {
	var state SiteMetadataState
	var draft, dev, prod []byte
	err := queryer.QueryRow(ctx, `SELECT revision,dev_revision,prod_revision,draft,dev,prod,updated_at FROM site_metadata_state WHERE id=TRUE`).
		Scan(&state.Revision, &state.DevRevision, &state.ProdRevision, &draft, &dev, &prod, &state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteMetadataState{}, ErrNotFound
	}
	if err != nil {
		return SiteMetadataState{}, err
	}
	if err := json.Unmarshal(draft, &state.Draft); err != nil {
		return SiteMetadataState{}, err
	}
	if err := json.Unmarshal(dev, &state.Dev); err != nil {
		return SiteMetadataState{}, err
	}
	if err := json.Unmarshal(prod, &state.Prod); err != nil {
		return SiteMetadataState{}, err
	}
	return state, nil
}

func (s *SnapshotService) GetMetadata(ctx context.Context) (SiteMetadataState, error) {
	defaultJSON, _ := json.Marshal(DefaultSiteMetadata())
	if _, err := s.pool.Exec(ctx, `INSERT INTO site_metadata_state (id,draft,dev,prod) VALUES (TRUE,$1::jsonb,$1::jsonb,$1::jsonb) ON CONFLICT (id) DO NOTHING`, string(defaultJSON)); err != nil {
		return SiteMetadataState{}, err
	}
	state, err := readMetadata(ctx, s.pool)
	if err != nil {
		return SiteMetadataState{}, err
	}
	if draft, err := s.GetDraft(ctx); err == nil {
		config, err := ParseSiteConfig(draft.Config)
		if err != nil {
			return SiteMetadataState{}, err
		}
		state.Pages = make([]MetadataPage, 0, len(config.Pages))
		for _, page := range config.Pages {
			state.Pages = append(state.Pages, MetadataPage{ID: page.ID, Name: page.Name})
		}
	} else {
		return SiteMetadataState{}, err
	}
	return state, nil
}

func (s *SnapshotService) SaveMetadata(ctx context.Context, actor uuid.UUID, expectedRevision int64, raw json.RawMessage) (SiteMetadataState, error) {
	metadata, err := ParseSiteMetadata(raw)
	if err != nil {
		return SiteMetadataState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := s.validateMetadataMedia(ctx, s.pool, metadata); err != nil {
		return SiteMetadataState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return SiteMetadataState{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SiteMetadataState{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO site_metadata_state (id,draft,dev,prod) VALUES (TRUE,$1::jsonb,$1::jsonb,$1::jsonb) ON CONFLICT (id) DO NOTHING`, string(mustJSON(DefaultSiteMetadata()))); err != nil {
		return SiteMetadataState{}, err
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_metadata_state WHERE id=TRUE FOR UPDATE`).Scan(&current); err != nil {
		return SiteMetadataState{}, err
	}
	if current != expectedRevision {
		return SiteMetadataState{}, fmt.Errorf("%w: expected metadata revision %d, current %d", ErrConflict, expectedRevision, current)
	}
	if _, err := tx.Exec(ctx, `UPDATE site_metadata_state SET draft=$1::jsonb,revision=revision+1,updated_by=$2,updated_at=now() WHERE id=TRUE`, string(encoded), actor); err != nil {
		return SiteMetadataState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SiteMetadataState{}, err
	}
	return s.GetMetadata(ctx)
}

func (s *SnapshotService) PublishMetadata(ctx context.Context, actor uuid.UUID) (SiteMetadataState, error) {
	return s.setMetadataEnvironment(ctx, actor, environment.Dev, false)
}

func (s *SnapshotService) PromoteMetadata(ctx context.Context, actor uuid.UUID) (SiteMetadataState, error) {
	return s.setMetadataEnvironment(ctx, actor, environment.Prod, true)
}

func (s *SnapshotService) setMetadataEnvironment(ctx context.Context, actor uuid.UUID, env environment.Environment, promote bool) (SiteMetadataState, error) {
	if _, err := s.GetMetadata(ctx); err != nil {
		return SiteMetadataState{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SiteMetadataState{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM site_metadata_state WHERE id=TRUE FOR UPDATE`).Scan(&revision); err != nil {
		return SiteMetadataState{}, err
	}
	state, err := readMetadata(ctx, tx)
	if err != nil {
		return SiteMetadataState{}, err
	}
	metadata := state.Draft
	if promote {
		metadata = state.Dev
		if state.DevRevision == 0 {
			return SiteMetadataState{}, fmt.Errorf("%w: metadata has not been published to dev", ErrInvalid)
		}
	}
	if err := metadata.Validate(); err != nil {
		return SiteMetadataState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := s.validateMetadataMedia(ctx, tx, metadata); err != nil {
		return SiteMetadataState{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	active, payload, activeErr := activeSnapshotTx(ctx, tx, env)
	if activeErr != nil && !errors.Is(activeErr, ErrSnapshotUnavailable) {
		return SiteMetadataState{}, activeErr
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return SiteMetadataState{}, err
	}
	nextRevision := state.DevRevision + 1
	if env == environment.Prod {
		nextRevision = state.ProdRevision + 1
	}
	if activeErr == nil {
		if err := s.rebuildShellsTx(ctx, tx, env, active.ID, payload, metadata, nextRevision); err != nil {
			return SiteMetadataState{}, err
		}
	}
	if env == environment.Dev {
		_, err = tx.Exec(ctx, `UPDATE site_metadata_state SET dev=$1::jsonb,dev_revision=dev_revision+1,updated_by=$2,updated_at=now() WHERE id=TRUE`, string(metadataJSON), actor)
	} else {
		_, err = tx.Exec(ctx, `UPDATE site_metadata_state SET prod=$1::jsonb,prod_revision=prod_revision+1,updated_by=$2,updated_at=now() WHERE id=TRUE`, string(metadataJSON), actor)
	}
	if err != nil {
		return SiteMetadataState{}, err
	}
	state, err = readMetadata(ctx, tx)
	if err != nil {
		return SiteMetadataState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SiteMetadataState{}, err
	}
	return s.GetMetadata(ctx)
}

func (s *SnapshotService) rebuildShellsTx(ctx context.Context, tx pgx.Tx, env environment.Environment, snapshotID uuid.UUID, payload SnapshotPayload, metadata SiteMetadata, metadataRevision int64) error {
	routes, err := CompileRouteTree(payload.Config.RouteTree)
	if err != nil {
		return err
	}
	pageByID := make(map[string]Page, len(payload.Config.Pages))
	for _, page := range payload.Config.Pages {
		pageByID[page.ID] = page
	}
	if _, err := tx.Exec(ctx, `DELETE FROM site_html_shells WHERE environment=$1 AND snapshot_id=$2`, env.String(), snapshotID); err != nil {
		return err
	}
	for _, route := range routes {
		page, ok := pageByID[route.PageID]
		if !ok {
			return fmt.Errorf("route %q references missing page %q", route.ID, route.PageID)
		}
		shell, err := publicruntime.RenderShell(metadata.ForPage(page.ID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO site_html_shells (environment,snapshot_id,route_id,metadata_revision,html) VALUES ($1,$2,$3,$4,$5)`, env.String(), snapshotID, route.ID, metadataRevision, string(shell)); err != nil {
			return err
		}
	}
	return nil
}

func activeSnapshotTx(ctx context.Context, tx pgx.Tx, env environment.Environment) (SnapshotRecord, SnapshotPayload, error) {
	var record SnapshotRecord
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT s.id,s.sequence,s.name,s.digest,s.created_at,s.payload FROM site_environments e JOIN site_snapshots s ON s.id=e.active_snapshot_id WHERE e.key=$1 FOR UPDATE OF e`, env.String()).
		Scan(&record.ID, &record.Sequence, &record.Name, &record.Digest, &record.CreatedAt, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return SnapshotRecord{}, SnapshotPayload{}, ErrSnapshotUnavailable
	}
	if err != nil {
		return SnapshotRecord{}, SnapshotPayload{}, err
	}
	var payload SnapshotPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return SnapshotRecord{}, SnapshotPayload{}, err
	}
	return record, payload, nil
}

func (s *SnapshotService) HTMLShell(ctx context.Context, env environment.Environment, requestPath string) ([]byte, bool, error) {
	record, payload, err := s.Active(ctx, env)
	if errors.Is(err, ErrSnapshotUnavailable) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	routes, err := CompileRouteTree(payload.Config.RouteTree)
	if err != nil {
		return nil, false, err
	}
	route, _, matched, err := MatchRoute(routes, requestPath)
	if err != nil || !matched {
		return nil, false, err
	}
	var shell []byte
	err = s.pool.QueryRow(ctx, `SELECT html FROM site_html_shells WHERE environment=$1 AND snapshot_id=$2 AND route_id=$3`, env.String(), record.ID, route.ID).Scan(&shell)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return shell, true, nil
}

func mustJSON(value any) []byte { encoded, _ := json.Marshal(value); return encoded }
