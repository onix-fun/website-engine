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
	schemas     SchemaProvider
	media       MediaValidator
}

type SchemaProvider interface {
	ContentSchema(context.Context, environment.Environment, string) (uuid.UUID, string, json.RawMessage, error)
}

type SettingsSchemaProvider interface {
	SettingsSchema(context.Context, environment.Environment, string) (uuid.UUID, string, json.RawMessage, error)
}

type MediaValidator interface {
	ValidateContent(context.Context, json.RawMessage, json.RawMessage, bool) error
}

func NewService(repo *Repository, featureRepo *features.Repository) *Service {
	return &Service{repo: repo, featureRepo: featureRepo}
}

func (s *Service) SetSchemaProvider(provider SchemaProvider) {
	s.schemas = provider
}

func (s *Service) SetMediaValidator(validator MediaValidator) {
	s.media = validator
}

func (s *Service) List(ctx context.Context, featureID uuid.UUID, includeArchived bool, search, rawCursor string, limit int, manualOrder bool) ([]Detail, string, error) {
	feature, err := s.featureRepo.GetByID(ctx, featureID)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.authoringSchema(ctx, s.repo.pool, feature); err != nil {
		return nil, "", err
	}
	if manualOrder && feature.Type != "collection" {
		return nil, "", &ValidationError{Message: "Ручная сортировка доступна только для collection-фич"}
	}
	params := ListParams{FeatureID: featureID, Search: strings.TrimSpace(search), Limit: limit + 1, IncludeArchived: includeArchived, ExcludeSingleSlot: feature.Type == "collection", OrderBySort: manualOrder}
	if rawCursor != "" {
		cursor, ok := response.DecodeCursor(rawCursor)
		if !ok {
			return nil, "", &ValidationError{Message: "Некорректный cursor"}
		}
		if manualOrder && cursor.SortOrder == nil {
			return nil, "", &ValidationError{Message: "Некорректный cursor ручной сортировки"}
		}
		id, _ := uuid.Parse(cursor.ID)
		params.BeforeTime = &cursor.CreatedAt
		params.BeforeID = &id
		params.BeforeSortOrder = cursor.SortOrder
	}
	items, err := s.repo.List(ctx, s.repo.pool, params)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		if manualOrder {
			next = response.EncodeOrderedCursor(last.SortOrder, last.CreatedAt, last.ID)
		} else {
			next = response.EncodeCursor(last.CreatedAt, last.ID)
		}
	}
	return items, next, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	return s.repo.GetDetail(ctx, s.repo.pool, id)
}

func (s *Service) GetSettings(ctx context.Context, featureID uuid.UUID) (Detail, error) {
	feature, err := s.featureRepo.GetByID(ctx, featureID)
	if err != nil {
		return Detail{}, err
	}
	if feature.Type != "collection" {
		return Detail{}, &ConflictError{Message: "Общие настройки доступны только для collection-фич"}
	}
	if _, err := s.authoringSettingsSchema(ctx, s.repo.pool, feature); err != nil {
		return Detail{}, err
	}
	entry, err := s.repo.GetSingleSlotByFeature(ctx, s.repo.pool, featureID)
	if err != nil {
		return Detail{}, err
	}
	return s.repo.GetDetail(ctx, s.repo.pool, entry.ID)
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
		ID: entryID, FeatureID: featureID, SingleSlot: feature.Type == "single" || feature.Type == "form",
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

// CreateSettings creates the collection's single shared content object. The
// unique single-slot index makes concurrent first saves safe.
func (s *Service) CreateSettings(ctx context.Context, actor principal.Principal, featureID uuid.UUID, data json.RawMessage) (Detail, error) {
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
	if feature.Type != "collection" {
		return Detail{}, &ConflictError{Message: "Общие настройки доступны только для collection-фич"}
	}
	if feature.Status == "archived" {
		return Detail{}, &ConflictError{Message: "Нельзя редактировать архивную Feature"}
	}
	schema, err := s.authoringSettingsSchema(ctx, tx, feature)
	if err != nil {
		return Detail{}, err
	}
	creator := actor.TokenID
	entryID, versionID := uuid.New(), uuid.New()
	if err := s.repo.InsertEntry(ctx, tx, Entry{ID: entryID, FeatureID: featureID, SingleSlot: true, DraftVersionID: &versionID, CreatedBy: &creator}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := s.repo.InsertVersion(ctx, tx, Version{ID: versionID, EntryID: entryID, Version: 1, SchemaVersionID: schema.ID, Data: data, CreatedBy: &creator}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Get(ctx, entryID)
}

// Import atomically imports one object into an existing draft or creates one
// or more new drafts. Every object is checked against the current manifest
// schema before the transaction writes any rows.
func (s *Service) Import(ctx context.Context, actor principal.Principal, featureID uuid.UUID, entryID *uuid.UUID, expectedRevision int64, records []json.RawMessage) ([]Detail, error) {
	if len(records) == 0 || len(records) > 500 {
		return nil, &ValidationError{Message: "Импорт должен содержать от 1 до 500 записей"}
	}
	if entryID != nil && (len(records) != 1 || expectedRevision < 1) {
		return nil, &ValidationError{Message: "Для замены текущей записи передайте один объект и его revision"}
	}
	for _, data := range records {
		if err := validateDataObject(data); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	feature, err := s.featureRepo.GetForUpdate(ctx, tx, featureID)
	if err != nil {
		return nil, err
	}
	if feature.Status == "archived" {
		return nil, &ConflictError{Message: "Нельзя импортировать записи в архивную Feature"}
	}
	if (feature.Type == "single" || feature.Type == "form") && len(records) > 1 {
		return nil, &ValidationError{Message: "У single/form Feature может быть только одна запись"}
	}
	if (feature.Type == "single" || feature.Type == "form") && entryID == nil {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM feature_entries WHERE feature_id=$1 AND single_slot=TRUE AND archived_at IS NULL)`, featureID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, &ConflictError{Message: "У single-Feature уже есть запись. Откройте её и импортируйте JSON из редактора записи"}
		}
	}
	schema, err := s.authoringSchema(ctx, tx, feature)
	if err != nil {
		return nil, err
	}
	for _, data := range records {
		if err := s.validateDataVersion(ctx, tx, schema.Schema, data); err != nil {
			return nil, err
		}
	}

	creator := actor.TokenID
	createdIDs := make([]uuid.UUID, 0, len(records))
	if entryID != nil {
		entry, err := s.repo.GetForUpdate(ctx, tx, *entryID)
		if err != nil {
			return nil, err
		}
		if entry.FeatureID != featureID {
			return nil, &ConflictError{Message: "Запись принадлежит другой Feature"}
		}
		if entry.ArchivedAt != nil {
			return nil, &ConflictError{Message: "Нельзя импортировать данные в архивную запись"}
		}
		if entry.Revision != expectedRevision {
			return nil, &ConflictError{Message: "Запись изменилась. Обновите страницу и повторите импорт"}
		}
		if entry.DraftVersionID != nil {
			if err := s.repo.UpdateDraftVersion(ctx, tx, *entry.DraftVersionID, schema.ID, records[0]); err != nil {
				return nil, translateDBError(err)
			}
			if err := s.repo.BumpRevision(ctx, tx, entry.ID); err != nil {
				return nil, translateDBError(err)
			}
		} else {
			number, err := s.repo.NextVersion(ctx, tx, entry.ID)
			if err != nil {
				return nil, err
			}
			version := Version{ID: uuid.New(), EntryID: entry.ID, Version: number, SchemaVersionID: schema.ID, Data: records[0], CreatedBy: &creator}
			if err := s.repo.InsertVersion(ctx, tx, version); err != nil {
				return nil, translateDBError(err)
			}
			if err := s.repo.SetDraftPointer(ctx, tx, entry.ID, version.ID, true); err != nil {
				return nil, translateDBError(err)
			}
		}
		createdIDs = append(createdIDs, entry.ID)
	} else {
		for _, data := range records {
			id, versionID := uuid.New(), uuid.New()
			entry := Entry{ID: id, FeatureID: featureID, SingleSlot: feature.Type == "single" || feature.Type == "form", DraftVersionID: &versionID, CreatedBy: &creator}
			if err := s.repo.InsertEntry(ctx, tx, entry); err != nil {
				return nil, translateDBError(err)
			}
			version := Version{ID: versionID, EntryID: id, Version: 1, SchemaVersionID: schema.ID, Data: data, CreatedBy: &creator}
			if err := s.repo.InsertVersion(ctx, tx, version); err != nil {
				return nil, translateDBError(err)
			}
			createdIDs = append(createdIDs, id)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, translateDBError(err)
	}
	out := make([]Detail, 0, len(createdIDs))
	for _, id := range createdIDs {
		detail, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, detail)
	}
	return out, nil
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
	schemaVersion, err := s.authoringSchemaForEntry(ctx, tx, feature, entry)
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
	schema, err := s.envSchemaForEntry(ctx, tx, feature, entry, environment.Dev)
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
	if s.media != nil {
		if err := s.media.ValidateContent(ctx, schema.Schema, draft.Data, true); err != nil {
			return Detail{}, &ConflictError{Message: "Публикация ожидает готовности медиа: " + err.Error()}
		}
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
	schema, err := s.envSchemaForEntry(ctx, tx, feature, entry, environment.Prod)
	if err != nil {
		return Detail{}, err
	}
	if err := s.validateDataVersion(ctx, tx, schema.Schema, dev.Data); err != nil {
		return Detail{}, err
	}
	if s.media != nil {
		if err := s.media.ValidateContent(ctx, schema.Schema, dev.Data, true); err != nil {
			return Detail{}, &ConflictError{Message: "Продвижение ожидает готовности медиа для prod: " + err.Error()}
		}
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

// Reorder changes the explicit display order of all active records in a
// collection. The caller must submit the complete set of record IDs so a
// partial update cannot silently drop records from the order.
func (s *Service) Reorder(ctx context.Context, featureID uuid.UUID, entryIDs []uuid.UUID) error {
	if len(entryIDs) == 0 {
		return &ValidationError{Message: "Порядок коллекции не может быть пустым"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var featureType string
	if err := tx.QueryRow(ctx, `SELECT type FROM features WHERE id=$1 AND status='active' FOR UPDATE`, featureID).Scan(&featureType); err != nil {
		return translateDBError(err)
	}
	if featureType != "collection" {
		return &ValidationError{Message: "Изменять порядок можно только у коллекции"}
	}
	rows, err := tx.Query(ctx, `SELECT id FROM feature_entries WHERE feature_id=$1 AND single_slot=FALSE AND archived_at IS NULL FOR UPDATE`, featureID)
	if err != nil {
		return err
	}
	existing := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(existing) != len(entryIDs) {
		return &ValidationError{Message: "Нужно указать все активные записи коллекции ровно один раз"}
	}
	seen := make(map[uuid.UUID]bool, len(entryIDs))
	for _, id := range entryIDs {
		if !existing[id] || seen[id] {
			return &ValidationError{Message: "Список записей содержит чужие или повторяющиеся ID"}
		}
		seen[id] = true
	}
	for order, id := range entryIDs {
		if _, err := tx.Exec(ctx, `UPDATE feature_entries SET sort_order=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND feature_id=$2`, id, featureID, order); err != nil {
			return translateDBError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return translateDBError(err)
	}
	return nil
}

// Move swaps one active collection record with its adjacent item in the
// persisted manual order. It works across paginated admin lists.
func (s *Service) Move(ctx context.Context, featureID, entryID uuid.UUID, direction string) error {
	if direction != "up" && direction != "down" {
		return &ValidationError{Message: "direction должен быть up или down"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var featureType string
	if err := tx.QueryRow(ctx, `SELECT type FROM features WHERE id=$1 AND status='active' FOR UPDATE`, featureID).Scan(&featureType); err != nil {
		return translateDBError(err)
	}
	if featureType != "collection" {
		return &ValidationError{Message: "Изменять порядок можно только у коллекции"}
	}
	rows, err := tx.Query(ctx, `SELECT id FROM feature_entries WHERE feature_id=$1 AND single_slot=FALSE AND archived_at IS NULL ORDER BY sort_order,created_at,id FOR UPDATE`, featureID)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	index := -1
	for i, id := range ids {
		if id == entryID {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNotFound
	}
	other := index - 1
	if direction == "down" {
		other = index + 1
	}
	if other < 0 || other >= len(ids) {
		return nil
	}
	ids[index], ids[other] = ids[other], ids[index]
	for order, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE feature_entries SET sort_order=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND feature_id=$2`, id, featureID, order); err != nil {
			return translateDBError(err)
		}
	}
	return translateDBError(tx.Commit(ctx))
}

func (s *Service) authoringSchema(ctx context.Context, db DBTX, feature features.Feature) (features.Version, error) {
	if s.schemas != nil {
		id, mode, schema, err := s.schemas.ContentSchema(ctx, environment.Dev, feature.Key)
		if err != nil {
			return features.Version{}, &ConflictError{Message: "Feature не размещена в активном dev-снапшоте или схема недоступна: " + err.Error()}
		}
		if mode != feature.Type {
			return features.Version{}, &ConflictError{Message: "Режим Feature не совпадает с активным dev-снапшотом"}
		}
		return features.Version{ID: id, FeatureID: feature.ID, Schema: schema, Status: "published"}, nil
	}
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
	if s.schemas != nil {
		id, mode, schema, err := s.schemas.ContentSchema(ctx, env, feature.Key)
		if err != nil {
			return features.Version{}, &ConflictError{Message: "Feature не размещена в активном " + env.String() + "-снапшоте или схема недоступна: " + err.Error()}
		}
		if mode != feature.Type {
			return features.Version{}, &ConflictError{Message: "Режим Feature не совпадает с активным " + env.String() + "-снапшотом"}
		}
		return features.Version{ID: id, FeatureID: feature.ID, Schema: schema, Status: "published"}, nil
	}
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

func (s *Service) authoringSchemaForEntry(ctx context.Context, db DBTX, feature features.Feature, entry Entry) (features.Version, error) {
	if feature.Type == "collection" && entry.SingleSlot {
		return s.authoringSettingsSchema(ctx, db, feature)
	}
	return s.authoringSchema(ctx, db, feature)
}

func (s *Service) envSchemaForEntry(ctx context.Context, db DBTX, feature features.Feature, entry Entry, env environment.Environment) (features.Version, error) {
	if feature.Type == "collection" && entry.SingleSlot {
		return s.envSettingsSchema(ctx, db, feature, env)
	}
	return s.envSchema(ctx, db, feature, env)
}

func (s *Service) authoringSettingsSchema(ctx context.Context, _ DBTX, feature features.Feature) (features.Version, error) {
	provider, ok := s.schemas.(SettingsSchemaProvider)
	if !ok {
		return features.Version{}, &ConflictError{Message: "У collection-фичи не объявлена схема общих настроек"}
	}
	id, mode, schema, err := provider.SettingsSchema(ctx, environment.Dev, feature.Key)
	if err != nil {
		return features.Version{}, &ConflictError{Message: "Общие настройки не размещены в активном dev-снапшоте: " + err.Error()}
	}
	if mode != feature.Type {
		return features.Version{}, &ConflictError{Message: "Режим Feature не совпадает с активным dev-снапшотом"}
	}
	return features.Version{ID: id, FeatureID: feature.ID, Schema: schema, Status: "published"}, nil
}

func (s *Service) envSettingsSchema(ctx context.Context, _ DBTX, feature features.Feature, env environment.Environment) (features.Version, error) {
	provider, ok := s.schemas.(SettingsSchemaProvider)
	if !ok {
		return features.Version{}, &ConflictError{Message: "У collection-фичи не объявлена схема общих настроек"}
	}
	id, mode, schema, err := provider.SettingsSchema(ctx, env, feature.Key)
	if err != nil {
		return features.Version{}, &ConflictError{Message: "Общие настройки не размещены в активном " + env.String() + "-снапшоте: " + err.Error()}
	}
	if mode != feature.Type {
		return features.Version{}, &ConflictError{Message: "Режим Feature не совпадает с активным " + env.String() + "-снапшотом"}
	}
	return features.Version{ID: id, FeatureID: feature.ID, Schema: schema, Status: "published"}, nil
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
