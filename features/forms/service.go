package forms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"websiteengine/features/features"
	"websiteengine/pkg/environment"
	"websiteengine/internal/platform/validate"
	"websiteengine/presentation/http/response"
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

// Submit validates and stores a public form submission atomically against the
// currently published schema version.
func (s *Service) Submit(ctx context.Context, env environment.Environment, key string, data json.RawMessage) (Submission, error) {
	if err := validateObject(data); err != nil {
		return Submission{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return Submission{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	form, err := s.repo.LookupFormByKey(ctx, tx, env, key, true)
	if err != nil {
		return Submission{}, err
	}
	fieldErrors, err := validate.Validate(form.Schema, data)
	if err != nil {
		return Submission{}, fmt.Errorf("validate form data: %w", err)
	}
	if len(fieldErrors) > 0 {
		return Submission{}, &ValidationError{Message: "Данные не соответствуют форме", Details: fieldErrors}
	}
	submission := Submission{
		ID: uuid.New(), FeatureID: form.FeatureID, FeatureKey: form.FeatureKey,
		SchemaVersionID: form.VersionID, Environment: form.Environment.String(), Data: data, Status: "new",
	}
	if err := s.repo.InsertSubmission(ctx, tx, submission); err != nil {
		return Submission{}, translateDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Submission{}, translateDBError(err)
	}
	return submission, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Submission, error) {
	return s.repo.Get(ctx, s.repo.pool, id)
}

type ListFilter struct {
	Environment     environment.Environment
	Status          string
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	Search          string
	SearchField     string
	IncludeArchived bool
	Sort            string
	BeforeTime      *time.Time
	BeforeID        *uuid.UUID
	Limit           int
}

func (s *Service) List(ctx context.Context, key string, filter ListFilter) ([]Submission, string, []string, error) {
	feature, form, fields, err := s.formContext(ctx, filter.Environment, key)
	if err != nil {
		return nil, "", nil, err
	}
	_ = feature
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, "", nil, &ValidationError{Message: "Некорректный status"}
	}
	searchFields, err := chooseSearchFields(fields, filter.SearchField)
	if err != nil {
		return nil, "", nil, err
	}
	if filter.Search != "" && len(searchFields) == 0 {
		return nil, "", nil, &ValidationError{Message: "В схеме формы нет полей, доступных для поиска"}
	}
	params := ListParams{
		FeatureID: form.FeatureID, Environment: filter.Environment, Status: filter.Status, CreatedFrom: filter.CreatedFrom,
		CreatedTo: filter.CreatedTo, SearchFields: searchFields, Search: filter.Search,
		IncludeArchived: filter.IncludeArchived, SortAsc: filter.Sort == "asc",
		BeforeTime: filter.BeforeTime, BeforeID: filter.BeforeID, Limit: filter.Limit + 1,
	}
	items, err := s.repo.List(ctx, s.repo.pool, params)
	if err != nil {
		return nil, "", nil, err
	}
	next := ""
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		last := items[len(items)-1]
		next = response.EncodeCursor(last.CreatedAt, last.ID)
	}
	return items, next, fields, nil
}

// Export returns a capped result set and the searchable field names for CSV.
func (s *Service) Export(ctx context.Context, key string, filter ListFilter) ([]Submission, []string, error) {
	_, form, fields, err := s.formContext(ctx, filter.Environment, key)
	if err != nil {
		return nil, nil, err
	}
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, nil, &ValidationError{Message: "Некорректный status"}
	}
	searchFields, err := chooseSearchFields(fields, filter.SearchField)
	if err != nil {
		return nil, nil, err
	}
	if filter.Search != "" && len(searchFields) == 0 {
		return nil, nil, &ValidationError{Message: "В схеме формы нет полей, доступных для поиска"}
	}
	items, err := s.repo.List(ctx, s.repo.pool, ListParams{
		FeatureID: form.FeatureID, Environment: filter.Environment, Status: filter.Status, CreatedFrom: filter.CreatedFrom,
		CreatedTo: filter.CreatedTo, SearchFields: searchFields, Search: filter.Search,
		IncludeArchived: filter.IncludeArchived, SortAsc: filter.Sort == "asc", Limit: 10000,
	})
	return items, fields, err
}

func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, status string, actor uuid.UUID) (Submission, error) {
	if !validStatus(status) {
		return Submission{}, &ValidationError{Message: "status должен быть new, in_progress, processed или rejected"}
	}
	submission, err := s.repo.UpdateStatus(ctx, id, status, actor)
	return submission, translateDBError(err)
}

func (s *Service) Archive(ctx context.Context, id uuid.UUID) (Submission, error) {
	if err := s.repo.Archive(ctx, id); err != nil {
		return Submission{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return translateDBError(s.repo.Delete(ctx, id))
}

func (s *Service) formContext(ctx context.Context, env environment.Environment, key string) (features.Feature, FormSchema, []string, error) {
	feature, err := s.featureRepo.GetByKey(ctx, key)
	if err != nil {
		return features.Feature{}, FormSchema{}, nil, err
	}
	if feature.Type != "form" {
		return features.Feature{}, FormSchema{}, nil, &ValidationError{Message: "Feature не является формой"}
	}
	var schema json.RawMessage
	var schemaID uuid.UUID
	var schemaVersion int
	id := feature.DevVersionID
	if env == environment.Prod {
		id = feature.ProdVersionID
	}
	if id == nil {
		id = feature.DevVersionID
	}
	if id == nil {
		id = feature.ProdVersionID
	}
	if id == nil {
		id = feature.DraftVersionID
	}
	if id != nil {
		v, err := s.featureRepo.VersionByID(ctx, s.repo.pool, *id)
		if err != nil {
			return features.Feature{}, FormSchema{}, nil, err
		}
		schema = v.Schema
		schemaID = v.ID
		schemaVersion = v.Version
	}
	fields := schemaFields(schema)
	return feature, FormSchema{FeatureID: feature.ID, FeatureKey: feature.Key, VersionID: schemaID, Version: schemaVersion, Schema: schema, Environment: env}, fields, nil
}

func schemaFields(schema json.RawMessage) []string {
	if len(schema) == 0 {
		return nil
	}
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(schema))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil
	}
	props, _ := root["properties"].(map[string]any)
	fields := make([]string, 0, len(props))
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		typ, _ := prop["type"].(string)
		switch typ {
		case "string", "number", "integer", "boolean", "":
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}

func chooseSearchFields(fields []string, requested string) ([]string, error) {
	if requested == "" {
		if len(fields) > 10 {
			return fields[:10], nil
		}
		return fields, nil
	}
	for _, field := range fields {
		if field == requested {
			return []string{requested}, nil
		}
	}
	return nil, &ValidationError{Message: "Поле поиска отсутствует в схеме формы"}
}

func validStatus(status string) bool {
	switch status {
	case "new", "in_progress", "processed", "rejected":
		return true
	}
	return false
}

func validateObject(data json.RawMessage) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return &ValidationError{Message: "Тело заявки обязательно"}
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return &ValidationError{Message: "Тело заявки должно быть JSON-объектом"}
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
		case "23000", "23503", "23514":
			return &ConflictError{Message: "Операция нарушает целостность данных"}
		}
	}
	return fmt.Errorf("forms: %w", err)
}
