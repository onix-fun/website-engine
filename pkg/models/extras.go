package models

// ExtraDDL holds the schema objects GORM cannot express through struct
// tags: CHECK constraints, foreign keys (including the deferrable composite
// ones that keep the version pointers consistent), partial and GIN indexes.
//
// The statements are appended after the CREATE TABLE statements produced by
// GORM, therefore the circular references between features, feature_versions
// and feature_entries can be expressed as ALTER TABLE ... ADD CONSTRAINT.
//
// Every invariant that used to live in a PL/pgSQL trigger is enforced by the
// Go services instead; the statements below are purely declarative.
const ExtraDDL = `
-- ---------------------------------------------------------------------------
-- Access tokens
-- ---------------------------------------------------------------------------

ALTER TABLE access_tokens
    ADD CONSTRAINT access_tokens_role_check
    CHECK (role IN ('admin', 'moderator', 'user'));

ALTER TABLE access_tokens
    ADD CONSTRAINT access_tokens_name_check
    CHECK (length(trim(name)) BETWEEN 1 AND 120);

ALTER TABLE access_tokens
    ADD CONSTRAINT access_tokens_expiry_check
    CHECK (expires_at IS NULL OR expires_at > created_at);

ALTER TABLE access_tokens
    ADD CONSTRAINT fk_access_tokens_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

CREATE INDEX idx_access_tokens_active
    ON access_tokens(role, expires_at)
    WHERE revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Features and schema versions
-- ---------------------------------------------------------------------------

ALTER TABLE features
    ADD CONSTRAINT features_key_check
    CHECK (key ~ '^[a-z][a-z0-9_-]{0,99}$');

ALTER TABLE features
    ADD CONSTRAINT features_name_check
    CHECK (length(trim(name)) BETWEEN 1 AND 200);

ALTER TABLE features
    ADD CONSTRAINT features_type_check
    CHECK (type IN ('collection', 'single', 'form'));

ALTER TABLE features
    ADD CONSTRAINT features_status_check
    CHECK (status IN ('active', 'archived'));

ALTER TABLE features
    ADD CONSTRAINT features_revision_check
    CHECK (revision > 0);

ALTER TABLE features
    ADD CONSTRAINT features_draft_not_dev
    CHECK (draft_version_id IS NULL OR dev_version_id IS NULL OR draft_version_id <> dev_version_id);

ALTER TABLE features
    ADD CONSTRAINT features_draft_not_prod
    CHECK (draft_version_id IS NULL OR prod_version_id IS NULL OR draft_version_id <> prod_version_id);

ALTER TABLE features
    ADD CONSTRAINT features_prod_requires_dev
    CHECK (prod_version_id IS NULL OR dev_version_id IS NOT NULL);

ALTER TABLE features
    ADD CONSTRAINT fk_features_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE features
    ADD CONSTRAINT fk_features_draft_version
    FOREIGN KEY (draft_version_id, id)
    REFERENCES feature_versions(id, feature_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE features
    ADD CONSTRAINT fk_features_dev_version
    FOREIGN KEY (dev_version_id, id)
    REFERENCES feature_versions(id, feature_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE features
    ADD CONSTRAINT fk_features_prod_version
    FOREIGN KEY (prod_version_id, id)
    REFERENCES feature_versions(id, feature_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_features_dev
    ON features(dev_version_id)
    WHERE dev_version_id IS NOT NULL;

CREATE INDEX idx_features_prod
    ON features(prod_version_id)
    WHERE prod_version_id IS NOT NULL;

ALTER TABLE feature_versions
    ADD CONSTRAINT feature_versions_version_check
    CHECK (version > 0);

ALTER TABLE feature_versions
    ADD CONSTRAINT feature_versions_schema_check
    CHECK (jsonb_typeof(schema) = 'object');

ALTER TABLE feature_versions
    ADD CONSTRAINT feature_versions_status_check
    CHECK (status IN ('draft', 'published'));

ALTER TABLE feature_versions
    ADD CONSTRAINT feature_versions_published_at_check
    CHECK (
        (status = 'draft' AND published_at IS NULL)
        OR
        (status = 'published' AND published_at IS NOT NULL)
    );

ALTER TABLE feature_versions
    ADD CONSTRAINT fk_feature_versions_feature
    FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT;

ALTER TABLE feature_versions
    ADD CONSTRAINT fk_feature_versions_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Content entries and immutable content versions
-- ---------------------------------------------------------------------------

ALTER TABLE feature_entries
    ADD CONSTRAINT feature_entries_revision_check
    CHECK (revision > 0);

ALTER TABLE feature_entries
    ADD CONSTRAINT entries_draft_not_dev
    CHECK (draft_version_id IS NULL OR dev_version_id IS NULL OR draft_version_id <> dev_version_id);

ALTER TABLE feature_entries
    ADD CONSTRAINT entries_draft_not_prod
    CHECK (draft_version_id IS NULL OR prod_version_id IS NULL OR draft_version_id <> prod_version_id);

ALTER TABLE feature_entries
    ADD CONSTRAINT entries_prod_requires_dev
    CHECK (prod_version_id IS NULL OR dev_version_id IS NOT NULL);

ALTER TABLE feature_entries
    ADD CONSTRAINT fk_feature_entries_feature
    FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT;

ALTER TABLE feature_entries
    ADD CONSTRAINT fk_feature_entries_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE feature_entries
    ADD CONSTRAINT fk_entries_draft_version
    FOREIGN KEY (draft_version_id, id)
    REFERENCES feature_entry_versions(id, entry_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE feature_entries
    ADD CONSTRAINT fk_entries_dev_version
    FOREIGN KEY (dev_version_id, id)
    REFERENCES feature_entry_versions(id, entry_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE feature_entries
    ADD CONSTRAINT fk_entries_prod_version
    FOREIGN KEY (prod_version_id, id)
    REFERENCES feature_entry_versions(id, entry_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX uq_feature_entries_single_slot
    ON feature_entries(feature_id)
    WHERE single_slot = TRUE AND archived_at IS NULL;

CREATE INDEX idx_feature_entries_feature
    ON feature_entries(feature_id, created_at DESC)
    WHERE archived_at IS NULL;

CREATE INDEX idx_feature_entries_dev
    ON feature_entries(dev_version_id)
    WHERE dev_version_id IS NOT NULL;

CREATE INDEX idx_feature_entries_prod
    ON feature_entries(prod_version_id)
    WHERE prod_version_id IS NOT NULL;

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT entry_versions_version_check
    CHECK (version > 0);

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT entry_versions_data_check
    CHECK (jsonb_typeof(data) = 'object');

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT entry_versions_status_check
    CHECK (status IN ('draft', 'published'));

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT entry_versions_published_at_check
    CHECK (
        (status = 'draft' AND published_at IS NULL)
        OR
        (status = 'published' AND published_at IS NOT NULL)
    );

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT fk_entry_versions_entry
    FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE RESTRICT;

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT fk_entry_versions_schema
    FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT;

ALTER TABLE feature_entry_versions
    ADD CONSTRAINT fk_entry_versions_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

CREATE INDEX idx_entry_versions_data
    ON feature_entry_versions USING GIN (data jsonb_path_ops)
    WHERE status = 'published';

-- ---------------------------------------------------------------------------
-- Form submissions
-- ---------------------------------------------------------------------------

ALTER TABLE form_submissions
    ADD CONSTRAINT submissions_data_check
    CHECK (jsonb_typeof(data) = 'object');

ALTER TABLE form_submissions
    ADD CONSTRAINT submissions_status_check
    CHECK (status IN ('new', 'in_progress', 'processed', 'rejected'));

ALTER TABLE form_submissions
    ADD CONSTRAINT submissions_environment_check
    CHECK (environment IN ('dev', 'prod'));

ALTER TABLE form_submissions
    ADD CONSTRAINT fk_submissions_feature
    FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT;

ALTER TABLE form_submissions
    ADD CONSTRAINT fk_submissions_schema
    FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT;

ALTER TABLE form_submissions
    ADD CONSTRAINT fk_submissions_processor
    FOREIGN KEY (processed_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

CREATE INDEX idx_submissions_feature_created
    ON form_submissions(feature_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_submissions_status_created
    ON form_submissions(status, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_submissions_data
    ON form_submissions USING GIN (data jsonb_path_ops)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_submissions_environment_created
    ON form_submissions(environment, created_at DESC)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Media objects
-- ---------------------------------------------------------------------------

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_object_key_check
    CHECK (length(object_key) BETWEEN 1 AND 1024);

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_filename_check
    CHECK (length(original_filename) BETWEEN 1 AND 255);

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_size_check
    CHECK (size_bytes >= 0);

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_status_check
    CHECK (status IN ('processing', 'ready', 'failed', 'deleted'));

ALTER TABLE media_objects
    ADD CONSTRAINT media_objects_variants_check
    CHECK (jsonb_typeof(variants) = 'object');

ALTER TABLE media_objects
    ADD CONSTRAINT fk_media_objects_uploader
    FOREIGN KEY (uploaded_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Publication audit
-- ---------------------------------------------------------------------------

ALTER TABLE content_publication_events
    ADD CONSTRAINT publication_events_entity_kind_check
    CHECK (entity_kind IN ('schema', 'entry'));

ALTER TABLE content_publication_events
    ADD CONSTRAINT publication_events_environment_check
    CHECK (environment IN ('dev', 'prod'));

ALTER TABLE content_publication_events
    ADD CONSTRAINT publication_events_action_check
    CHECK (action IN ('publish', 'promote', 'rollback', 'unpublish'));

ALTER TABLE content_publication_events
    ADD CONSTRAINT publication_events_metadata_check
    CHECK (jsonb_typeof(metadata) = 'object');

ALTER TABLE content_publication_events
    ADD CONSTRAINT publication_events_entry_check
    CHECK (
        (entity_kind = 'schema' AND entry_id IS NULL)
        OR
        (entity_kind = 'entry' AND entry_id IS NOT NULL)
    );

ALTER TABLE content_publication_events
    ADD CONSTRAINT fk_publication_events_feature
    FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE CASCADE;

ALTER TABLE content_publication_events
    ADD CONSTRAINT fk_publication_events_entry
    FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE CASCADE;

ALTER TABLE content_publication_events
    ADD CONSTRAINT fk_publication_events_actor
    FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL;

CREATE INDEX idx_publication_events_entry
    ON content_publication_events(entry_id, created_at DESC)
    WHERE entry_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Site settings, release sources, releases and environments
-- ---------------------------------------------------------------------------

ALTER TABLE site_settings
    ADD CONSTRAINT site_settings_id_check
    CHECK (id);

ALTER TABLE site_settings
    ADD CONSTRAINT site_settings_name_check
    CHECK (length(trim(name)) BETWEEN 1 AND 200);

ALTER TABLE release_sources
    ADD CONSTRAINT release_sources_provider_check
    CHECK (provider IN ('github', 'gitlab', 'gitea', 'manual'));

ALTER TABLE release_sources
    ADD CONSTRAINT release_sources_repo_check
    CHECK (provider = 'manual' OR repo_url <> '');

ALTER TABLE release_sources
    ADD CONSTRAINT release_sources_auth_check
    CHECK (auth_ciphertext IS NULL OR auth_nonce IS NOT NULL);

ALTER TABLE release_sources
    ADD CONSTRAINT fk_release_sources_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_releases
    ADD CONSTRAINT site_releases_version_check
    CHECK (length(version) BETWEEN 1 AND 200);

ALTER TABLE site_releases
    ADD CONSTRAINT site_releases_sha256_check
    CHECK (length(artifact_sha256) = 64);

ALTER TABLE site_releases
    ADD CONSTRAINT site_releases_size_check
    CHECK (artifact_size > 0);

ALTER TABLE site_releases
    ADD CONSTRAINT site_releases_manifest_check
    CHECK (jsonb_typeof(manifest) = 'object');

ALTER TABLE site_releases
    ADD CONSTRAINT fk_site_releases_source
    FOREIGN KEY (source_id) REFERENCES release_sources(id) ON DELETE SET NULL;

ALTER TABLE site_releases
    ADD CONSTRAINT fk_site_releases_importer
    FOREIGN KEY (imported_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_environments
    ADD CONSTRAINT site_environments_key_check
    CHECK (key IN ('dev', 'prod'));

ALTER TABLE site_environments
    ADD CONSTRAINT site_environments_name_check
    CHECK (length(trim(name)) BETWEEN 1 AND 100);

ALTER TABLE site_environments
    ADD CONSTRAINT site_environments_port_check
    CHECK (http_port BETWEEN 1 AND 65535);

ALTER TABLE site_environments
    ADD CONSTRAINT site_environments_revision_check
    CHECK (revision > 0);

ALTER TABLE site_environments
    ADD CONSTRAINT fk_site_environments_release
    FOREIGN KEY (active_release_id) REFERENCES site_releases(id) ON DELETE SET NULL;

ALTER TABLE release_events
    ADD CONSTRAINT release_events_environment_check
    CHECK (environment IN ('dev', 'prod'));

ALTER TABLE release_events
    ADD CONSTRAINT release_events_action_check
    CHECK (action IN ('import', 'activate', 'rollback'));

ALTER TABLE release_events
    ADD CONSTRAINT release_events_metadata_check
    CHECK (jsonb_typeof(metadata) = 'object');

ALTER TABLE release_events
    ADD CONSTRAINT fk_release_events_release
    FOREIGN KEY (release_id) REFERENCES site_releases(id) ON DELETE SET NULL;

ALTER TABLE release_events
    ADD CONSTRAINT fk_release_events_actor
    FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Feature builds, page composition and immutable site snapshots
-- ---------------------------------------------------------------------------

ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_version_check
    CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$');

ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_digest_check
    CHECK (digest ~ '^[a-f0-9]{64}$');

ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_size_check
    CHECK (artifact_size > 0);

ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_manifest_check
    CHECK (jsonb_typeof(manifest) = 'object' AND jsonb_typeof(assets) = 'object');

ALTER TABLE feature_builds
    ADD CONSTRAINT fk_feature_builds_feature
    FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT;

ALTER TABLE feature_builds
    ADD CONSTRAINT fk_feature_builds_schema
    FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT;

ALTER TABLE feature_builds
    ADD CONSTRAINT fk_feature_builds_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_draft_config
    ADD CONSTRAINT site_draft_config_singleton_check
    CHECK (id);

ALTER TABLE site_draft_config
    ADD CONSTRAINT site_draft_config_revision_check
    CHECK (revision > 0);

ALTER TABLE site_draft_config
    ADD CONSTRAINT site_draft_config_object_check
    CHECK (jsonb_typeof(config) = 'object');

ALTER TABLE site_draft_config
    ADD CONSTRAINT fk_site_draft_config_updater
    FOREIGN KEY (updated_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_sequence_check
    CHECK (sequence > 0);

ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_name_check
    CHECK (length(trim(name)) BETWEEN 1 AND 160);

ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_digest_check
    CHECK (digest ~ '^[a-f0-9]{64}$');

ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_payload_check
    CHECK (jsonb_typeof(payload) = 'object');

ALTER TABLE site_snapshots
    ADD CONSTRAINT fk_site_snapshots_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_environments
    ADD CONSTRAINT fk_site_environments_snapshot
    FOREIGN KEY (active_snapshot_id) REFERENCES site_snapshots(id) ON DELETE SET NULL;

ALTER TABLE site_snapshot_events
    ADD CONSTRAINT site_snapshot_events_environment_check
    CHECK (environment IN ('dev', 'prod'));

ALTER TABLE site_snapshot_events
    ADD CONSTRAINT site_snapshot_events_action_check
    CHECK (action IN ('publish', 'promote', 'rollback'));

ALTER TABLE site_snapshot_events
    ADD CONSTRAINT fk_site_snapshot_events_snapshot
    FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE RESTRICT;

ALTER TABLE site_snapshot_events
    ADD CONSTRAINT fk_site_snapshot_events_actor
    FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_key_check
    CHECK (length(trim(feature_key)) BETWEEN 1 AND 100);

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_order_check
    CHECK (sort_order >= 0);

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_data_check
    CHECK (jsonb_typeof(data) = 'object');

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT fk_site_snapshot_content_snapshot
    FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE RESTRICT;

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT fk_site_snapshot_content_entry
    FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE RESTRICT;

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT fk_site_snapshot_content_version
    FOREIGN KEY (version_id) REFERENCES feature_entry_versions(id) ON DELETE RESTRICT;

CREATE INDEX idx_site_snapshot_content_data
    ON site_snapshot_content USING GIN (data jsonb_path_ops);
`
