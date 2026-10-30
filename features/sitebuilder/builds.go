package sitebuilder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/pkg/storage"
)

type AssetRecord struct {
	ObjectKey   string `json:"objectKey"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type BuildRecord struct {
	ID              uuid.UUID              `json:"id"`
	FeatureID       uuid.UUID              `json:"featureId"`
	Key             string                 `json:"key"`
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	SchemaVersionID uuid.UUID              `json:"schemaVersionId"`
	Digest          string                 `json:"digest"`
	ArtifactSize    int64                  `json:"artifactSize"`
	Manifest        json.RawMessage        `json:"manifest"`
	Assets          map[string]AssetRecord `json:"assets"`
	CreatedAt       time.Time              `json:"createdAt"`
}

type BuildService struct {
	pool            *pgxpool.Pool
	store           storage.ObjectStore
	maxArchiveBytes int64
}

func NewBuildService(pool *pgxpool.Pool, store storage.ObjectStore, maxArchiveBytes int64) *BuildService {
	return &BuildService{pool: pool, store: store, maxArchiveBytes: maxArchiveBytes}
}

func (s *BuildService) Import(ctx context.Context, actor uuid.UUID, body []byte) (BuildRecord, bool, error) {
	archive, err := VerifyArchive(body, s.maxArchiveBytes)
	if err != nil {
		return BuildRecord{}, false, err
	}
	assets := make(map[string]AssetRecord, len(archive.Assets))
	for _, asset := range archive.Assets {
		objectKey := path.Join("feature-builds", archive.Digest, asset.Path)
		if err := s.store.Put(ctx, objectKey, bytes.NewReader(asset.Data), asset.Size, storage.PutOptions{
			ContentType: asset.ContentType, CacheControl: "public, max-age=31536000, immutable",
		}); err != nil {
			return BuildRecord{}, false, fmt.Errorf("store feature asset %q: %w", asset.Path, err)
		}
		assets[asset.Path] = AssetRecord{ObjectKey: objectKey, ContentType: asset.ContentType, Size: asset.Size, SHA256: asset.SHA256}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BuildRecord{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var featureID uuid.UUID
	var featureName, featureType string
	err = tx.QueryRow(ctx, `SELECT id, name, type FROM features WHERE key=$1 FOR UPDATE`, archive.Manifest.Key).Scan(&featureID, &featureName, &featureType)
	if errors.Is(err, pgx.ErrNoRows) {
		featureID = uuid.New()
		featureType = archive.Manifest.Content.Mode
		_, err = tx.Exec(ctx, `INSERT INTO features (id,key,name,type,status,revision,created_by) VALUES ($1,$2,$3,$4,'active',1,$5)`,
			featureID, archive.Manifest.Key, archive.Manifest.Name, featureType, actor)
		if err != nil {
			return BuildRecord{}, false, err
		}
		featureName = archive.Manifest.Name
	} else if err != nil {
		return BuildRecord{}, false, err
	} else if featureType != archive.Manifest.Content.Mode {
		return BuildRecord{}, false, fmt.Errorf("feature %q already uses content mode %q; a new build cannot change it to %q", archive.Manifest.Key, featureType, archive.Manifest.Content.Mode)
	}

	var existing BuildRecord
	var existingManifest, existingAssets []byte
	err = tx.QueryRow(ctx, `SELECT id, digest, schema_version_id, artifact_size, manifest, assets, created_at FROM feature_builds WHERE feature_id=$1 AND version=$2`, featureID, archive.Manifest.Version).
		Scan(&existing.ID, &existing.Digest, &existing.SchemaVersionID, &existing.ArtifactSize, &existingManifest, &existingAssets, &existing.CreatedAt)
	if err == nil {
		if existing.Digest != archive.Digest {
			return BuildRecord{}, false, fmt.Errorf("feature version %s@%s is immutable and already has a different digest", archive.Manifest.Key, archive.Manifest.Version)
		}
		existing.FeatureID, existing.Key, existing.Name, existing.Version = featureID, archive.Manifest.Key, archive.Manifest.Name, archive.Manifest.Version
		existing.Manifest = existingManifest
		if err := json.Unmarshal(existingAssets, &existing.Assets); err != nil {
			return BuildRecord{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return BuildRecord{}, false, err
		}
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return BuildRecord{}, false, err
	}

	var schemaVersion int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM feature_versions WHERE feature_id=$1`, featureID).Scan(&schemaVersion); err != nil {
		return BuildRecord{}, false, err
	}
	schemaVersionID := uuid.New()
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO feature_versions (id,feature_id,version,schema,status,created_by,created_at,published_at) VALUES ($1,$2,$3,$4::jsonb,'published',$5,$6,$6)`,
		schemaVersionID, featureID, schemaVersion, string(archive.Manifest.Content.Schema), actor, now); err != nil {
		return BuildRecord{}, false, fmt.Errorf("register manifest content schema: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE features SET name=$2,type=$3,dev_version_id=$4,draft_version_id=NULL,revision=revision+1,updated_at=now() WHERE id=$1`,
		featureID, archive.Manifest.Name, archive.Manifest.Content.Mode, schemaVersionID); err != nil {
		return BuildRecord{}, false, err
	}
	manifestJSON, err := json.Marshal(archive.Manifest)
	if err != nil {
		return BuildRecord{}, false, err
	}
	assetsJSON, err := json.Marshal(assets)
	if err != nil {
		return BuildRecord{}, false, err
	}
	buildID := uuid.New()
	var createdAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO feature_builds (id,feature_id,version,schema_version_id,digest,artifact_size,manifest,assets,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9) RETURNING created_at`,
		buildID, featureID, archive.Manifest.Version, schemaVersionID, archive.Digest, archive.Size, string(manifestJSON), string(assetsJSON), actor).Scan(&createdAt)
	if err != nil {
		return BuildRecord{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BuildRecord{}, false, err
	}
	return BuildRecord{ID: buildID, FeatureID: featureID, Key: archive.Manifest.Key, Name: archive.Manifest.Name, Version: archive.Manifest.Version, SchemaVersionID: schemaVersionID,
		Digest: archive.Digest, ArtifactSize: archive.Size, Manifest: manifestJSON, Assets: assets, CreatedAt: createdAt}, true, nil
}

func (s *BuildService) List(ctx context.Context) ([]BuildRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.id,b.feature_id,f.key,f.name,b.version,b.schema_version_id,b.digest,b.artifact_size,b.manifest,b.assets,b.created_at
		FROM feature_builds b JOIN features f ON f.id=b.feature_id ORDER BY f.key,b.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BuildRecord
	for rows.Next() {
		var item BuildRecord
		var manifest, assets []byte
		if err := rows.Scan(&item.ID, &item.FeatureID, &item.Key, &item.Name, &item.Version, &item.SchemaVersionID, &item.Digest, &item.ArtifactSize, &manifest, &assets, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Manifest = manifest
		if err := json.Unmarshal(assets, &item.Assets); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *BuildService) Build(ctx context.Context, key, version string) (BuildRecord, bool, error) {
	var item BuildRecord
	var manifest, assets []byte
	err := s.pool.QueryRow(ctx, `SELECT b.id,b.feature_id,f.key,f.name,b.version,b.schema_version_id,b.digest,b.artifact_size,b.manifest,b.assets,b.created_at
		FROM feature_builds b JOIN features f ON f.id=b.feature_id WHERE f.key=$1 AND ($2='' OR b.version=$2)
		ORDER BY b.created_at DESC LIMIT 1`, key, version).
		Scan(&item.ID, &item.FeatureID, &item.Key, &item.Name, &item.Version, &item.SchemaVersionID, &item.Digest, &item.ArtifactSize, &manifest, &assets, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildRecord{}, false, nil
	}
	if err != nil {
		return BuildRecord{}, false, err
	}
	item.Manifest = manifest
	if err := json.Unmarshal(assets, &item.Assets); err != nil {
		return BuildRecord{}, false, err
	}
	return item, true, nil
}

func (s *BuildService) Asset(ctx context.Context, digest, assetPath string) (io.ReadCloser, string, error) {
	if len(digest) != 64 || !validAssetPath(assetPath) {
		return nil, "", errors.New("invalid feature asset path")
	}
	var objectKey, contentType string
	var assetsRaw []byte
	err := s.pool.QueryRow(ctx, `SELECT assets FROM feature_builds WHERE digest=$1`, digest).Scan(&assetsRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", pgx.ErrNoRows
	}
	if err != nil {
		return nil, "", err
	}
	var assets map[string]AssetRecord
	if err := json.Unmarshal(assetsRaw, &assets); err != nil {
		return nil, "", err
	}
	asset, ok := assets[assetPath]
	if !ok {
		return nil, "", pgx.ErrNoRows
	}
	objectKey, contentType = asset.ObjectKey, asset.ContentType
	body, err := s.store.Get(ctx, objectKey)
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}
