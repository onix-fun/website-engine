package features

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
	"websiteengine/pkg/principal"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"websiteengine/pkg/environment"
	"websiteengine/pkg/platform/validate"
	"websiteengine/pkg/publication"
	"websiteengine/pkg/response"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,99}$`)

type ValidationError struct {
	Message string
	Details []validate.FieldError
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrValidation }

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }
func (e *ConflictError) Unwrap() error { return ErrConflict }

type SchemaValidationError struct {
	Message string
	Details []validate.FieldError
}

func (e *SchemaValidationError) Error() string { return e.Message }
func (e *SchemaValidationError) Unwrap() error { return ErrValidation }

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

type CreateInput struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (s *Service) Create(ctx context.Context, actor principal.Principal, in CreateInput) (Detail, error) {
	in.Key = strings.TrimSpace(in.Key)
	in.Name = strings.TrimSpace(in.Name)
	if !keyPattern.MatchString(in.Key) {
		return Detail{}, &ValidationError{Message: "Ключ должен начинаться с латинской буквы и содержать только a-z, 0-9, _ и -"}
	}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 200 {
		return Detail{}, &ValidationError{Message: "Название должно содержать от 1 до 200 символов"}
	}
	switch in.Type {
	case "collection", "single", "form":
	default:
		return Detail{}, &ValidationError{Message: "type должен быть collection, single или form"}
	}
	creator := actor.TokenID
	f, err := s.repo.Create(ctx, Feature{
		ID: uuid.New(), Key: in.Key, Name: in.Name, Type: in.Type, CreatedBy: &creator,
	})
	if err != nil {
		return Detail{}, translateDBError(err)
	}
	return Detail{Feature: f}, nil
}

func (s *Service) List(ctx context.Context, status, featureType, search, rawCursor string, limit int) ([]Feature, string, error) {
	if status != "" && status != "active" && status != "archived" {
		return nil, "", &ValidationError{Message: "status должен быть active или archived"}
	}
	switch featureType {
	case "", "collection", "single", "form":
	default:
		return nil, "", &ValidationError{Message: "type должен быть collection, single или form"}
	}
	params := ListParams{Status: status, Type: featureType, Search: strings.TrimSpace(search), Limit: limit + 1}
	if rawCursor != "" {
		cursor, ok := response.DecodeCursor(rawCursor)
		if !ok {
			return nil, "", &ValidationError{Message: "Некорректный cursor"}
		}
		id, _ := uuid.Parse(cursor.ID)
		params.BeforeTime = &cursor.CreatedAt
		params.BeforeID = &id
	}
	items, err := s.repo.List(ctx, params)
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

func (s *Service) Detail(ctx context.Context, id uuid.UUID) (Detail, error) {
	f, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	return s.loadDetail(ctx, s.repo.pool, f)
}

func (s *Service) ByKey(ctx context.Context, key string) (Detail, error) {
	f, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return Detail{}, err
	}
	return s.loadDetail(ctx, s.repo.pool, f)
}

func (s *Service) loadDetail(ctx context.Context, db DBTX, f Feature) (Detail, error) {
	d := Detail{Feature: f}
	if f.DraftVersionID != nil {
		v, err := s.repo.VersionByID(ctx, db, *f.DraftVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Draft = &v
	}
	if f.DevVersionID != nil {
		v, err := s.repo.VersionByID(ctx, db, *f.DevVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Dev = &v
	}
	if f.ProdVersionID != nil {
		v, err := s.repo.VersionByID(ctx, db, *f.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		d.Prod = &v
	}
	return d, nil
}

func (s *Service) UpdateName(ctx context.Context, id uuid.UUID, name string) (Feature, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return Feature{}, &ValidationError{Message: "Название должно содержать от 1 до 200 символов"}
	}
	f, err := s.repo.UpdateName(ctx, id, name)
	return f, translateDBError(err)
}

func (s *Service) Archive(ctx context.Context, id uuid.UUID) (Feature, error) {
	f, err := s.repo.Archive(ctx, id)
	return f, translateDBError(err)
}

func (s *Service) Versions(ctx context.Context, id uuid.UUID, limit int) ([]Version, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Versions(ctx, id, limit)
}

func (s *Service) DraftState(ctx context.Context, id uuid.UUID) (DraftState, error) {
	d, err := s.Detail(ctx, id)
	if err != nil {
		return DraftState{}, err
	}
	return DraftState{Revision: d.Revision, Draft: d.Draft, Dev: d.Dev, Prod: d.Prod}, nil
}

// SaveDraft creates or updates the single editable draft schema version.
func (s *Service) SaveDraft(ctx context.Context, actor principal.Principal, id uuid.UUID, expectedRevision int64, schema json.RawMessage) (DraftState, error) {
	if expectedRevision < 1 {
		return DraftState{}, &ValidationError{Message: "revision обязателен"}
	}
	if len(schema) == 0 {
		return DraftState{}, &ValidationError{Message: "schema обязательна"}
	}
	if _, err := validate.Compile(schema); err != nil {
		return DraftState{}, &SchemaValidationError{Message: err.Error()}
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return DraftState{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	f, err := s.repo.GetForUpdate(ctx, tx, id)
	if err != nil {
		return DraftState{}, err
	}
	if f.Status == "archived" {
		return DraftState{}, &ConflictError{Message: "Нельзя изменять архивный раздел"}
	}
	if f.Revision != expectedRevision {
		return DraftState{}, &ConflictError{Message: "Черновик уже был изменён. Перезагрузите данные"}
	}

	creator := actor.TokenID
	if f.DraftVersionID != nil {
		draft, err := s.repo.VersionByID(ctx, tx, *f.DraftVersionID)
		if err != nil {
			return DraftState{}, err
		}
		if draft.Status != "draft" {
			return DraftState{}, &ConflictError{Message: "Указатель черновика не соответствует draft-версии"}
		}
		if err := s.repo.UpdateDraftSchema(ctx, tx, draft.ID, schema); err != nil {
			return DraftState{}, translateDBError(err)
		}
		if err := s.repo.BumpRevision(ctx, tx, id); err != nil {
			return DraftState{}, translateDBError(err)
		}
	} else {
		number, err := s.repo.NextVersion(ctx, tx, id)
		if err != nil {
			return DraftState{}, err
		}
		version := Version{
			ID: uuid.New(), FeatureID: id, Version: number, Schema: schema, CreatedBy: &creator,
		}
		if err := s.repo.CreateVersion(ctx, tx, version); err != nil {
			return DraftState{}, translateDBError(err)
		}
		if _, err := s.repo.SetDraftPointer(ctx, tx, id, version.ID); err != nil {
			return DraftState{}, translateDBError(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return DraftState{}, translateDBError(err)
	}
	return s.DraftState(ctx, id)
}

// Publish promotes the current draft schema to dev.
func (s *Service) Publish(ctx context.Context, actor principal.Principal, id uuid.UUID, expectedRevision int64) (Detail, error) {
	if expectedRevision < 1 {
		return Detail{}, &ValidationError{Message: "revision обязателен"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	f, err := s.repo.GetForUpdate(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	if f.Status == "archived" {
		return Detail{}, &ConflictError{Message: "Нельзя публиковать архивный раздел"}
	}
	if f.Revision != expectedRevision {
		return Detail{}, &ConflictError{Message: "Черновик уже был изменён. Перезагрузите данные"}
	}
	if f.DraftVersionID == nil {
		return Detail{}, &ConflictError{Message: "Нет черновика для публикации"}
	}
	draft, err := s.repo.VersionByID(ctx, tx, *f.DraftVersionID)
	if err != nil {
		return Detail{}, err
	}
	if _, err := validate.Compile(draft.Schema); err != nil {
		return Detail{}, &SchemaValidationError{Message: err.Error()}
	}
	if f.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *f.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if draft.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Версия dev не может быть позади prod"}
		}
	}
	if err := s.validateEntries(ctx, tx, id, environment.Dev, draft.Schema); err != nil {
		return Detail{}, err
	}
	if err := s.repo.SetVersionPublished(ctx, tx, draft.ID); err != nil {
		return Detail{}, translateDBError(err)
	}
	if _, err := s.repo.SetPointers(ctx, tx, id, nil, &draft.ID, f.ProdVersionID, true); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "schema", FeatureID: id, VersionID: draft.ID,
		Environment: environment.Dev, Action: "publish", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Detail(ctx, id)
}

// Promote switches the prod schema pointer to the current dev schema.
func (s *Service) Promote(ctx context.Context, actor principal.Principal, id uuid.UUID, expectedRevision int64) (Detail, error) {
	if expectedRevision < 1 {
		return Detail{}, &ValidationError{Message: "revision обязателен"}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	f, err := s.repo.GetForUpdate(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	if f.Status == "archived" {
		return Detail{}, &ConflictError{Message: "Нельзя продвигать архивный раздел"}
	}
	if f.Revision != expectedRevision {
		return Detail{}, &ConflictError{Message: "Черновик уже был изменён. Перезагрузите данные"}
	}
	if f.DevVersionID == nil {
		return Detail{}, &ConflictError{Message: "Сначала опубликуйте схему в dev"}
	}
	dev, err := s.repo.VersionByID(ctx, tx, *f.DevVersionID)
	if err != nil {
		return Detail{}, err
	}
	if f.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *f.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if dev.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Версия dev не может быть позади prod"}
		}
	}
	if err := s.validateEntries(ctx, tx, id, environment.Prod, dev.Schema); err != nil {
		return Detail{}, err
	}
	if err := s.repo.SetEnvPointer(ctx, tx, id, environment.Prod, &dev.ID, true); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "schema", FeatureID: id, VersionID: dev.ID,
		Environment: environment.Prod, Action: "promote", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Detail(ctx, id)
}

// Rollback points an environment at an existing published schema version.
func (s *Service) Rollback(ctx context.Context, actor principal.Principal, id uuid.UUID, versionNumber int, env environment.Environment) (Detail, error) {
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

	f, err := s.repo.GetForUpdate(ctx, tx, id)
	if err != nil {
		return Detail{}, err
	}
	version, err := s.repo.VersionByNumber(ctx, tx, id, versionNumber)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Detail{}, &ConflictError{Message: "Версия схемы не найдена"}
		}
		return Detail{}, err
	}
	if version.Status != "published" {
		return Detail{}, &ConflictError{Message: "Можно откатиться только на опубликованную версию"}
	}
	if env == environment.Dev && f.ProdVersionID != nil {
		prod, err := s.repo.VersionByID(ctx, tx, *f.ProdVersionID)
		if err != nil {
			return Detail{}, err
		}
		if version.Version < prod.Version {
			return Detail{}, &ConflictError{Message: "Нельзя откатить dev ниже версии prod"}
		}
	}
	if env == environment.Prod && f.DevVersionID != nil {
		dev, err := s.repo.VersionByID(ctx, tx, *f.DevVersionID)
		if err != nil {
			return Detail{}, err
		}
		if version.Version > dev.Version {
			return Detail{}, &ConflictError{Message: "Нельзя откатить prod выше версии dev"}
		}
	}
	if err := s.repo.SetEnvPointer(ctx, tx, id, env, &version.ID, true); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := publication.Insert(ctx, tx, publication.Event{
		EntityKind: "schema", FeatureID: id, VersionID: version.ID,
		Environment: env, Action: "rollback", ActorID: &actor.TokenID,
	}); err != nil {
		return Detail{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Detail{}, translateDBError(err)
	}
	return s.Detail(ctx, id)
}

func (s *Service) validateEntries(ctx context.Context, db DBTX, featureID uuid.UUID, env environment.Environment, schema json.RawMessage) error {
	entries, err := s.repo.EnvEntriesData(ctx, db, featureID, env)
	if err != nil {
		return err
	}
	for i, data := range entries {
		fieldErrors, err := validate.Validate(schema, data)
		if err != nil {
			return fmt.Errorf("validate %s entry: %w", env, err)
		}
		if len(fieldErrors) > 0 {
			return &SchemaValidationError{
				Message: fmt.Sprintf("Запись #%d несовместима с новой схемой среды %s", i+1, env),
				Details: fieldErrors,
			}
		}
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
			return &ConflictError{Message: "Раздел с таким ключом уже существует или версия конфликтует"}
		case "23000", "23503", "23514", "22P02":
			return &ConflictError{Message: "Операция нарушает целостность данных"}
		}
	}
	return fmt.Errorf("features: %w", err)
}
