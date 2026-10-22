package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"websiteengine/pkg/principal"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"websiteengine/features/features"
	"websiteengine/pkg/environment"
	"websiteengine/pkg/platform/validate"
	"websiteengine/pkg/publication"
	"websiteengine/pkg/response"
)

type ValidationError struct {
	Message string
	Details []validate.FieldError
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrValidation }

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
func (e *ConflictError) Unwrap() error { return ErrConflict }

type Service struct {
	repo        *Repository
	featureRepo *features.Repository
}

func NewService(repo *Repository, featureRepo *features.Repository) *Service {
	return &Service{repo: repo, featureRepo: featureRepo}
}

func (s *Service) List(ctx context.Context, featureID uuid.UUID, includeArchived bool, search, rawCursor string, limit int) ([]Detail, string, error) {
	feature, err := s.featureRepo.GetByID(ctx, featureID)
	if err != nil {
		return nil, "", err
	}
	if feature.Type == "form" {
		return nil, "", &ConflictError{Message: "У form-Feature нет контентных записей"}
	}
	params := ListParams{FeatureID: featureID, Search: strings.TrimSpace(search), Limit: limit + 1, IncludeArchived: includeArchived}
	if rawCursor != "" {
		cursor, ok := response.DecodeCursor(rawCursor)
		if !ok {
			return nil, "", &ValidationError{Message: "Некорректный cursor"}
		}
		id, _ := uuid.Parse(cursor.ID)
		params.BeforeTime = &cursor.CreatedAt
		params.BeforeID = &id
	}
	items, err := s.repo.List(ctx, s.repo.pool, params)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = response.EncodeCursor(last.CreatedAt, last.ID)
	}
	return items, next, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	return s.repo.GetDetail(ctx, s.repo.pool, id)
}

func (s *Service) Versions(ctx context.Context, id uuid.UUID, limit int) ([]Version, error) {
	if _, err := s.repo.Get(ctx, s.repo.pool, id); err != nil {
		return nil, err
	}
	return s.repo.Versions(ctx, s.repo.pool, id, limit)
}

// Create inserts a first draft version. The entry is not public until publish.
func (s *Service) Create(ctx context.Context, actor principal.Principal, featureID uuid.UUID, data json.RawMessage) (Detail, error) {
	if err := validateDataObject(data); err != nil {
		return Detail{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	feature, err := s.featureRepo.GetForUpdate(ctx, tx, featureID)
	if err != nil {
		return Detail{}, err
	}
	if feature.Status == "archived" {
		return Detail{}, &ConflictError{Message: "Нельзя добавлять записи в архивную Feature"}
	}
	if feature.Type == "form" {
		return Detail{}, &ConflictError{Message: "Для form-Feature записи создаются через отправку формы"}
	}
	schemaVersion, err := s.authoringSchema(ctx, tx, feature)
	if err != nil {
		return Detail{}, err
	}
	// Drafts are stored as-is: the schema is enforced when the entry is
	// published, not while the editor autosaves incomplete data.

	creator := actor.TokenID
	entryID := uuid.New()
	versionID := uuid.New()
	entry := Entry{
		ID: entryID, FeatureID: featureID, SingleSlot: feature.Type == "single",
		DraftVersionID: &versionID, CreatedBy: &creator,
	}
	if err := s.repo.InsertEntry(ctx, tx, entry); err != nil {
		return Detail{}, translateDBError(err)
	}
	version := Version{
		ID: versionID, EntryID: entryID, Version: 1, SchemaVersionID: schemaVersion.ID,
		Data: data, CreatedBy: &creator,
	}
	if err := s.repo.InsertVersion(ctx, tx, version); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

func (s *Service) SaveDraft(ctx context.Context, actor principal.Principal, entryID uuid.UUID, expectedRevision int64, data json.RawMessage) (Detail, error) {
	if expectedRevision < 1 {
		return Detail{}, &ValidationError{Message: "revision обязателен"}
	}
	if err := validateDataObject(data); err != nil {
		return Detail{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	if entry.ArchivedAt != nil {
		return Detail{}, &ConflictError{Message: "Нельзя редактировать архивную запись"}
	}
	if entry.Revision != expectedRevision {
		return Detail{}, &ConflictError{Message: "Черновик записи уже был изменён. Перезагрузите данные"}
	}
	feature, err := s.featureRepo.GetForUpdate(ctx, tx, entry.FeatureID)
	if err != nil {
		return Detail{}, err
	}
	if feature.Status == "archived" {
		return Detail{}, &ConflictError{Message: "Feature находится в архиве"}
	}
	schemaVersion, err := s.authoringSchema(ctx, tx, feature)
	if err != nil {
		return Detail{}, err
	}
	// Lenient draft save: see CreateEntry — schema conformance is enforced
	// on publish/promote.

	creator := actor.TokenID
	if entry.DraftVersionID != nil {
		if err := s.repo.UpdateDraftVersion(ctx, tx, *entry.DraftVersionID, schemaVersion.ID, data); err != nil {
			return Detail{}, translateDBError(err)
		}
		if err := s.repo.BumpRevision(ctx, tx, entryID); err != nil {
			return Detail{}, translateDBError(err)
		}
	} else {
		number, err := s.repo.NextVersion(ctx, tx, entryID)
		if err != nil {
			return Detail{}, err
		}
		version := Version{
			ID: uuid.New(), EntryID: entryID, Version: number, SchemaVersionID: schemaVersion.ID,
			Data: data, CreatedBy: &creator,
		}
		if err := s.repo.InsertVersion(ctx, tx, version); err != nil {
			return Detail{}, translateDBError(err)
		}
		if err := s.repo.SetDraftPointer(ctx, tx, entryID, version.ID, true); err != nil {
			return Detail{}, translateDBError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// Publish creates an immutable version from the draft and publishes it to dev.
func (s *Service) Publish(ctx context.Context, actor principal.Principal, entryID uuid.UUID, expectedRevision int64) (Detail, error) {
	if expectedRevision < 1 {
		return Detail{}, &ValidationError{Message: "revision обязателен"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	if entry.ArchivedAt != nil {
		return Detail{}, &ConflictError{Message: "Нельзя публиковать архивную запись"}
	}
	if entry.Revision != expectedRevision {
		return Detail{}, &ConflictError{Message: "Черновик записи уже был изменён. Перезагрузите данные"}
	}
	if entry.DraftVersionID == nil {
		return Detail{}, &ConflictError{Message: "Нет черновика записи для публикации"}
	}
	feature, err := s.featureRepo.GetForUpdate(ctx, tx, entry.FeatureID)
	if err != nil {
		return Detail{}, err
	}
	schema, err := s.envSchema(ctx, tx, feature, environment.Dev)
	if err != nil {
		return Detail{}, err
	}
	draft, err := s.repo.VersionByID(ctx, tx, *entry.DraftVersionID)
	if err != nil {
		return Detail{}, err
	}
	if draft.Status != "draft" {
		return Detail{}, &ConflictError{Message: "Указатель черновика не соответствует draft-версии"}
	}
	if err := s.validateDataVersion(ctx, tx, schema.Schema, draft.Data); err != nil {
		return Detail{}, err
	}
	if entry.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *entry.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if draft.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Версия dev не может быть позади prod"}
		}
	}
	if err := s.repo.SetVersionPublished(ctx, tx, draft.ID, schema.ID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := s.repo.SetPointers(ctx, tx, entryID, nil, &draft.ID, entry.ProdVersionID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "entry", FeatureID: entry.FeatureID, EntryID: &entryID, VersionID: draft.ID,
		Environment: environment.Dev, Action: "publish", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// Promote switches the prod pointer to the current dev version.
func (s *Service) Promote(ctx context.Context, actor principal.Principal, entryID uuid.UUID, expectedRevision int64) (Detail, error) {
	if expectedRevision < 1 {
		return Detail{}, &ValidationError{Message: "revision обязателен"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	if entry.ArchivedAt != nil {
		return Detail{}, &ConflictError{Message: "Нельзя продвигать архивную запись"}
	}
	if entry.Revision != expectedRevision {
		return Detail{}, &ConflictError{Message: "Черновик записи уже был изменён. Перезагрузите данные"}
	}
	if entry.DevVersionID == nil {
		return Detail{}, &ConflictError{Message: "Сначала опубликуйте запись в dev"}
	}
	dev, err := s.repo.VersionByID(ctx, tx, *entry.DevVersionID)
	if err != nil {
		return Detail{}, err
	}
	feature, err := s.featureRepo.GetForUpdate(ctx, tx, entry.FeatureID)
	if err != nil {
		return Detail{}, err
	}
	schema, err := s.envSchema(ctx, tx, feature, environment.Prod)
	if err != nil {
		return Detail{}, err
	}
	if err := s.validateDataVersion(ctx, tx, schema.Schema, dev.Data); err != nil {
		return Detail{}, err
	}
	if entry.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *entry.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if dev.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Версия dev не может быть позади prod"}
		}
	}
	if err := s.repo.SetEnvPointer(ctx, tx, entryID, environment.Prod, &dev.ID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "entry", FeatureID: entry.FeatureID, EntryID: &entryID, VersionID: dev.ID,
		Environment: environment.Prod, Action: "promote", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// Rollback switches an environment pointer to an older published version.
func (s *Service) Rollback(ctx context.Context, actor principal.Principal, entryID uuid.UUID, versionNumber int, env environment.Environment) (Detail, error) {
	if versionNumber < 1 {
		return Detail{}, &ValidationError{Message: "version должен быть положительным"}
	}
	if !env.Valid() {
		return Detail{}, &ValidationError{Message: "environment должен быть dev или prod"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	version, err := s.repo.VersionByNumber(ctx, tx, entryID, versionNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Detail{}, &ConflictError{Message: "Версия записи не найдена"}
		}
		return Detail{}, err
	}
	if version.Status != "published" {
		return Detail{}, &ConflictError{Message: "Можно откатиться только на опубликованную версию"}
	}
	if env == environment.Dev && entry.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *entry.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if version.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Нельзя откатить dev ниже версии prod"}
		}
	}
	if env == environment.Prod && entry.DevVersionID != nil {
		dev, err := s.repo.VersionByID(ctx, tx, *entry.DevVersionID)
		if err != nil {
			return Detail{}, err
		}
		if version.Version > dev.Version {
			return Detail{}, &ConflictError{Message: "Нельзя откатить prod выше версии dev"}
		}
	}
	if err := s.repo.SetEnvPointer(ctx, tx, entryID, env, &version.ID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "entry", FeatureID: entry.FeatureID, EntryID: &entryID, VersionID: version.ID,
		Environment: env, Action: "rollback", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// Unpublish removes a version from one environment while keeping the draft and
// the other environment untouched.
func (s *Service) Unpublish(ctx context.Context, actor principal.Principal, entryID uuid.UUID, env environment.Environment) (Detail, error) {
	if !env.Valid() {
		return Detail{}, &ValidationError{Message: "environment должен быть dev или prod"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	var current *uuid.UUID
	if env == environment.Dev {
		current = entry.DevVersionID
		if entry.ProdVersionID != nil {
			return Detail{}, &ConflictError{Message: "Сначала снимите запись с публикации в prod"}
		}
	} else {
		current = entry.ProdVersionID
	}
	if current == nil {
		return Detail{}, &ConflictError{Message: "Запись уже снята с публикации в этой среде"}
	}
	if err := s.repo.SetEnvPointer(ctx, tx, entryID, env, nil); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "entry", FeatureID: entry.FeatureID, EntryID: &entryID, VersionID: *current,
		Environment: env, Action: "unpublish", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// DiscardDraft removes the draft pointer while keeping every version row and
// the published pointer intact. The editor then falls back to the published
// version and no autosave is triggered until the user edits again.
func (s *Service) DiscardDraft(ctx context.Context, entryID uuid.UUID) (Detail, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	entry, err := s.repo.GetForUpdate(ctx, tx, entryID)
	if err != nil {
		return Detail{}, err
	}
	if entry.DraftVersionID == nil {
		return Detail{}, &ConflictError{Message: "У записи нет черновика"}
	}
	if entry.DevVersionID == nil && entry.ProdVersionID == nil {
		return Detail{}, &ConflictError{
			Message: "Нельзя отменить единственный черновик: сначала опубликуйте запись",
		}
	}
	if err := s.repo.SetPointers(ctx, tx, entryID, nil, entry.DevVersionID, entry.ProdVersionID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

func (s *Service) Archive(ctx context.Context, entryID uuid.UUID) (Detail, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := s.repo.GetForUpdate(ctx, tx, entryID); err != nil {
		return Detail{}, err
	}
	if err := s.repo.Archive(ctx, tx, entryID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

func (s *Service) authoringSchema(ctx context.Context, db DBTX, feature features.Feature) (features.Version, error) {
	id := feature.DraftVersionID
	if id == nil {
		id = feature.DevVersionID
	}
	if id == nil {
		id = feature.ProdVersionID
	}
	if id == nil {
		return features.Version{}, &ConflictError{Message: "Сначала создайте и опубликуйте схему Feature"}
	}
	version, err := s.featureRepo.VersionByID(ctx, db, *id)
	if err != nil {
		return features.Version{}, err
	}
	return version, nil
}

func (s *Service) envSchema(ctx context.Context, db DBTX, feature features.Feature, env environment.Environment) (features.Version, error) {
	id := feature.DevVersionID
	if env == environment.Prod {
		id = feature.ProdVersionID
	}
	if id == nil {
		return features.Version{}, &ConflictError{Message: "Сначала опубликуйте схему Feature в " + env.String()}
	}
	version, err := s.featureRepo.VersionByID(ctx, db, *id)
	if err != nil {
		return features.Version{}, err
	}
	return version, nil
}

func (s *Service) validateDataVersion(_ context.Context, _ DBTX, schema, data json.RawMessage) error {
	fieldErrors, err := validate.Validate(schema, data)
	if err != nil {
		return fmt.Errorf("validate entry data: %w", err)
	}
	if len(fieldErrors) > 0 {
		return &ValidationError{Message: "Данные не соответствуют опубликованной схеме", Details: fieldErrors}
	}
	return nil
}

type PublicEntryDTO struct {
	ID          uuid.UUID       `json:"id"`
	Data        json.RawMessage `json:"data"`
	PublishedAt *string         `json:"published_at,omitempty"`
}

type PublicFeatureDTO struct {
	Type        string            `json:"type"`
	Version     int               `json:"version"`
	Environment string            `json:"environment"`
	Entries     *[]PublicEntryDTO `json:"entries,omitempty"`
	Data        json.RawMessage   `json:"data,omitempty"`
	Schema      json.RawMessage   `json:"schema,omitempty"`
}

// PublicContent returns a bounded BFF payload for a set of feature keys.
func (s *Service) PublicContent(ctx context.Context, env environment.Environment, keys []string, limit int) (map[string]PublicFeatureDTO, error) {
	published, err := s.featureRepo.PublishedByKeys(ctx, env, keys)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PublicFeatureDTO, len(published))
	for _, f := range published {
		dto := PublicFeatureDTO{Type: f.Type, Version: f.Version, Environment: env.String()}
		switch f.Type {
		case "form":
			dto.Schema = f.Schema
		case "single":
			entry, err := s.repo.PublishedSingle(ctx, s.repo.pool, env, f.ID)
			if err != nil {
				return nil, err
			}
			if entry != nil {
				projected, err := projectPublic(entry.Data, entry.Schema)
				if err != nil {
					return nil, err
				}
				dto.Data = projected
			}
		case "collection":
			entries, err := s.repo.PublishedEntries(ctx, s.repo.pool, env, f.ID, nil, nil, limit)
			if err != nil {
				return nil, err
			}
			projected := make([]PublicEntryDTO, 0, len(entries))
			for _, entry := range entries {
				item, err := projectEntry(entry)
				if err != nil {
					return nil, err
				}
				projected = append(projected, item)
			}
			dto.Entries = &projected
		}
		out[f.Key] = dto
	}
	return out, nil
}

func (s *Service) PublicFeature(ctx context.Context, env environment.Environment, key string, limit int) (PublicFeatureDTO, error) {
	m, err := s.PublicContent(ctx, env, []string{key}, limit)
	if err != nil {
		return PublicFeatureDTO{}, err
	}
	dto, ok := m[key]
	if !ok {
		return PublicFeatureDTO{}, ErrNotFound
	}
	return dto, nil
}

// PublicEntries returns one cursor page of published collection entries.
func (s *Service) PublicEntries(ctx context.Context, env environment.Environment, key string, rawCursor string, limit int) (PublicFeatureDTO, string, error) {
	featuresList, err := s.featureRepo.PublishedByKeys(ctx, env, []string{key})
	if err != nil {
		return PublicFeatureDTO{}, "", err
	}
	if len(featuresList) == 0 {
		return PublicFeatureDTO{}, "", ErrNotFound
	}
	f := featuresList[0]
	if f.Type != "collection" {
		return PublicFeatureDTO{}, "", &ConflictError{Message: "Feature не является коллекцией"}
	}
	var beforeTime *time.Time
	var beforeID *uuid.UUID
	if rawCursor != "" {
		cursor, ok := response.DecodeCursor(rawCursor)
		if !ok {
			return PublicFeatureDTO{}, "", &ValidationError{Message: "Некорректный cursor"}
		}
		id, _ := uuid.Parse(cursor.ID)
		beforeTime = &cursor.CreatedAt
		beforeID = &id
	}
	entries, err := s.repo.PublishedEntries(ctx, s.repo.pool, env, f.ID, beforeTime, beforeID, limit+1)
	if err != nil {
		return PublicFeatureDTO{}, "", err
	}
	next := ""
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[len(entries)-1]
		next = response.EncodeCursor(last.CreatedAt, last.ID)
	}
	projected := make([]PublicEntryDTO, 0, len(entries))
	for _, entry := range entries {
		item, err := projectEntry(entry)
		if err != nil {
			return PublicFeatureDTO{}, "", err
		}
		projected = append(projected, item)
	}
	dto := PublicFeatureDTO{Type: f.Type, Version: f.Version, Environment: env.String(), Entries: &projected}
	return dto, next, nil
}

func projectEntry(entry PublicEntry) (PublicEntryDTO, error) {
	data, err := projectPublic(entry.Data, entry.Schema)
	if err != nil {
		return PublicEntryDTO{}, err
	}
	item := PublicEntryDTO{ID: entry.ID, Data: data}
	if entry.PublishedAt != nil {
		s := entry.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		item.PublishedAt = &s
	}
	return item, nil
}

func projectPublic(data, schema json.RawMessage) (json.RawMessage, error) {
	projected, err := validate.StripPrivateRaw(data, schema)
	if err != nil {
		return nil, fmt.Errorf("project public data: %w", err)
	}
	return projected, nil
}

func validateDataObject(data json.RawMessage) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return &ValidationError{Message: "data обязательна"}
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return &ValidationError{Message: "data должна быть JSON-объектом"}
	}
	return nil
}

func translateDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrValidation) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if strings.Contains(pgErr.ConstraintName, "single_slot") {
				return &ConflictError{Message: "У single-Feature уже есть активная запись"}
			}
			return &ConflictError{Message: "Конфликт уникальности записи"}
		case "23000", "23503", "23514", "22P02":
			return &ConflictError{Message: "Операция нарушает целостность данных"}
		}
	}
	return fmt.Errorf("content: %w", err)
}
