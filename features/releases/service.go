package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"websiteengine/pkg/principal"

	"github.com/google/uuid"

	"websiteengine/pkg/artifact"
	"websiteengine/pkg/environment"
	"websiteengine/pkg/secrets"
	"websiteengine/pkg/storage"

	releaseprovider "websiteengine/features/releases/provider"
)

type Service struct {
	repo     *Repository
	store    storage.ObjectStore
	box      *secrets.Box
	maxBytes int64
	prov     *Provider
}

func NewService(repo *Repository, store storage.ObjectStore, box *secrets.Box, maxBytes int64) *Service {
	if maxBytes <= 0 {
		maxBytes = 512 << 20
	}
	return &Service{repo: repo, store: store, box: box, maxBytes: maxBytes, prov: NewProvider()}
}

type ImportInput struct {
	Version string
	Tag     string
	File    io.Reader
	Size    int64
}

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,199}$`)

func (s *Service) ImportManual(ctx context.Context, actor principal.Principal, in ImportInput) (Release, error) {
	version := strings.TrimSpace(in.Version)
	if !versionPattern.MatchString(version) {
		return Release{}, errors.New("version must be 1-200 URL-safe characters")
	}
	if in.Size <= 0 || in.Size > s.maxBytes {
		return Release{}, fmt.Errorf("release archive must be between 1 byte and %d bytes", s.maxBytes)
	}
	tmp, err := os.CreateTemp("", "release-*.zip")
	if err != nil {
		return Release{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, io.LimitReader(in.File, s.maxBytes+1)); err != nil {
		tmp.Close()
		return Release{}, err
	}
	if err := tmp.Close(); err != nil {
		return Release{}, err
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return Release{}, err
	}
	if info.Size() > s.maxBytes {
		return Release{}, fmt.Errorf("release archive exceeds %d bytes", s.maxBytes)
	}
	dir, err := os.MkdirTemp("", "release-validate-*")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(dir)
	manifest, err := artifact.Extract(tmpName, dir, artifact.DefaultLimits())
	if err != nil {
		return Release{}, err
	}

	id := uuid.New()
	objectKey := filepath.ToSlash(filepath.Join("releases", id.String()+".zip"))
	f, err := os.Open(tmpName)
	if err != nil {
		return Release{}, err
	}
	defer f.Close()
	if err := s.store.Put(ctx, objectKey, f, manifest.Size, storage.PutOptions{
		ContentType: "application/zip", CacheControl: "public, max-age=31536000, immutable",
	}); err != nil {
		return Release{}, err
	}
	manifestJSON, _ := json.Marshal(map[string]any{
		"files": len(manifest.Files), "sha256": manifest.SHA256, "size": manifest.Size,
	})
	rel := Release{
		ID: id, Version: version, ObjectKey: objectKey, SHA256: manifest.SHA256,
		ArtifactSize: manifest.Size, Manifest: manifestJSON, ImportedBy: &actor.TokenID,
	}
	if in.Tag != "" {
		rel.Tag = &in.Tag
	}
	if err := s.repo.InsertRelease(ctx, rel); err != nil {
		_ = s.store.Delete(context.Background(), objectKey)
		return Release{}, err
	}
	_ = s.repo.InsertEvent(ctx, Event{
		ID: uuid.New(), ReleaseID: &id, Environment: environment.Dev, Action: "import",
		ActorID: &actor.TokenID, Metadata: json.RawMessage(`{}`),
	})
	return s.repo.ReleaseByID(ctx, id)
}

func (s *Service) Activate(ctx context.Context, actor principal.Principal, env environment.Environment, releaseID uuid.UUID) (Release, error) {
	if !env.Valid() {
		return Release{}, errors.New("environment must be dev or prod")
	}
	rel, err := s.repo.ReleaseByID(ctx, releaseID)
	if err != nil {
		return Release{}, err
	}
	if err := s.repo.SetActive(ctx, env, releaseID); err != nil {
		return Release{}, err
	}
	action := "activate"
	if err := s.repo.InsertEvent(ctx, Event{
		ID: uuid.New(), ReleaseID: &releaseID, Environment: env, Action: action,
		ActorID: &actor.TokenID, Metadata: json.RawMessage(`{}`),
	}); err != nil {
		return Release{}, err
	}
	return rel, nil
}

func (s *Service) Rollback(ctx context.Context, actor principal.Principal, env environment.Environment, releaseID uuid.UUID) (Release, error) {
	if !env.Valid() {
		return Release{}, errors.New("environment must be dev or prod")
	}
	rel, err := s.repo.ReleaseByID(ctx, releaseID)
	if err != nil {
		return Release{}, err
	}
	if err := s.repo.SetActive(ctx, env, releaseID); err != nil {
		return Release{}, err
	}
	if err := s.repo.InsertEvent(ctx, Event{
		ID: uuid.New(), ReleaseID: &releaseID, Environment: env, Action: "rollback",
		ActorID: &actor.TokenID, Metadata: json.RawMessage(`{}`),
	}); err != nil {
		return Release{}, err
	}
	return rel, nil
}

func (s *Service) List(ctx context.Context, beforeTime *time.Time, beforeID *uuid.UUID, limit int) ([]Release, string, error) {
	items, err := s.repo.ListReleases(ctx, ListParams{BeforeTime: beforeTime, BeforeID: beforeID, Limit: limit + 1})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return items, next, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Release, error) {
	return s.repo.ReleaseByID(ctx, id)
}

func (s *Service) Environments(ctx context.Context) ([]EnvironmentState, error) {
	return s.repo.Environments(ctx)
}

func (s *Service) Sources(ctx context.Context) ([]Source, error) { return s.repo.ListSources(ctx) }

type SourceInput struct {
	Provider     string
	RepoURL      string
	AssetPattern string
	Token        string
	Enabled      bool
}

func (s *Service) CreateSource(ctx context.Context, actor principal.Principal, in SourceInput) (Source, error) {
	if in.Provider != "manual" && in.Provider != "github" && in.Provider != "gitlab" && in.Provider != "gitea" {
		return Source{}, errors.New("provider must be manual, github, gitlab or gitea")
	}
	if in.Provider != "manual" && !strings.HasPrefix(in.RepoURL, "https://") {
		return Source{}, errors.New("repo_url must be an absolute https URL")
	}
	var ciphertext, nonce []byte
	var err error
	if token := strings.TrimSpace(in.Token); token != "" {
		if s.box == nil {
			return Source{}, errors.New("release credential encryption is not configured")
		}
		ciphertext, nonce, err = s.box.Encrypt(token)
		if err != nil {
			return Source{}, err
		}
	}
	src := Source{
		ID: uuid.New(), Provider: in.Provider, RepoURL: strings.TrimSpace(in.RepoURL),
		AssetPattern: strings.TrimSpace(in.AssetPattern), Enabled: in.Enabled,
		CreatedBy: &actor.TokenID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		HasCredential: len(ciphertext) > 0,
	}
	if err := s.repo.InsertSource(ctx, src, ciphertext, nonce); err != nil {
		return Source{}, err
	}
	return src, nil
}

func (s *Service) DeleteSource(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteSource(ctx, id)
}

func encodeCursor(t time.Time, id uuid.UUID) string {
	return t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}

func ParseCursor(raw string) (*time.Time, *uuid.UUID, error) {
	if raw == "" {
		return nil, nil, nil
	}
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, nil, errors.New("invalid cursor")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil, errors.New("invalid cursor")
	}
	return &t, &id, nil
}

func (s *Service) FindAsset(assets []releaseprovider.ReleaseAsset, pattern string) *releaseprovider.ReleaseAsset {
	if len(assets) == 0 {
		return nil
	}
	if strings.TrimSpace(pattern) == "" {
		for i := range assets {
			if strings.HasSuffix(strings.ToLower(assets[i].Name), ".zip") {
				return &assets[i]
			}
		}
		return &assets[0]
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		for i := range assets {
			if strings.Contains(strings.ToLower(assets[i].Name), strings.ToLower(pattern)) {
				return &assets[i]
			}
		}
		return nil
	}
	for i := range assets {
		if re.MatchString(assets[i].Name) {
			return &assets[i]
		}
	}
	return nil
}

type Provider struct {
	github *releaseprovider.GitHub
	gitlab *releaseprovider.GitLab
	gitea  *releaseprovider.Gitea
}

func NewProvider() *Provider {
	return &Provider{
		github: releaseprovider.NewGitHub(),
		gitlab: releaseprovider.NewGitLab(),
		gitea:  releaseprovider.NewGitea(),
	}
}

func (p *Provider) ForSource(src Source) (releaseprovider.Provider, error) {
	switch src.Provider {
	case "github":
		return p.github, nil
	case "gitlab":
		return p.gitlab, nil
	case "gitea":
		return p.gitea, nil
	default:
		return nil, errors.New("unsupported provider")
	}
}

func (s *Service) SyncFromSource(ctx context.Context, actor principal.Principal, src Source, assetURL string) (Release, error) {
	p, err := s.Provider().ForSource(src)
	if err != nil {
		return Release{}, err
	}
	body, _, err := p.DownloadAsset(ctx, assetURL)
	if err != nil {
		return Release{}, err
	}
	defer body.Close()
	tmp, err := os.CreateTemp("", "release-sync-*.zip")
	if err != nil {
		return Release{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, io.LimitReader(body, s.maxBytes+1)); err != nil {
		tmp.Close()
		return Release{}, err
	}
	if err := tmp.Close(); err != nil {
		return Release{}, err
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return Release{}, err
	}
	if info.Size() > s.maxBytes {
		return Release{}, fmt.Errorf("release archive exceeds %d bytes", s.maxBytes)
	}
	dir, err := os.MkdirTemp("", "release-validate-*")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(dir)
	manifest, err := artifact.Extract(tmpName, dir, artifact.DefaultLimits())
	if err != nil {
		return Release{}, err
	}
	id := uuid.New()
	objectKey := filepath.ToSlash(filepath.Join("releases", id.String()+".zip"))
	f, err := os.Open(tmpName)
	if err != nil {
		return Release{}, err
	}
	defer f.Close()
	if err := s.store.Put(ctx, objectKey, f, manifest.Size, storage.PutOptions{
		ContentType: "application/zip", CacheControl: "public, max-age=31536000, immutable",
	}); err != nil {
		return Release{}, err
	}
	manifestJSON, _ := json.Marshal(map[string]any{
		"files": len(manifest.Files), "sha256": manifest.SHA256, "size": manifest.Size,
	})
	rel := Release{
		ID: id, Version: manifest.SHA256[:12], SourceID: &src.ID, ObjectKey: objectKey,
		SHA256: manifest.SHA256, ArtifactSize: manifest.Size, Manifest: manifestJSON,
		ImportedBy: &actor.TokenID,
	}
	if err := s.repo.InsertRelease(ctx, rel); err != nil {
		_ = s.store.Delete(context.Background(), objectKey)
		return Release{}, err
	}
	_ = s.repo.InsertEvent(ctx, Event{
		ID: uuid.New(), ReleaseID: &id, Environment: environment.Dev, Action: "import",
		ActorID: &actor.TokenID, Metadata: json.RawMessage(`{}`),
	})
	return s.repo.ReleaseByID(ctx, id)
}

func (s *Service) Provider() *Provider {
	if s.prov == nil {
		s.prov = NewProvider()
	}
	return s.prov
}
