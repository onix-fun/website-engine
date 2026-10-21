CREATE TABLE "access_tokens" ("id" uuid NOT NULL,"name" text NOT NULL,"token_hash" bytea NOT NULL,"role" text NOT NULL,"created_by" uuid,"expires_at" timestamptz,"revoked_at" timestamptz,"last_used_at" timestamptz,"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_access_tokens_creator" ON "access_tokens" ("created_by");
CREATE UNIQUE INDEX IF NOT EXISTS "uq_access_tokens_token_hash" ON "access_tokens" ("token_hash");
CREATE TABLE "features" ("id" uuid NOT NULL,"key" text NOT NULL,"name" text NOT NULL,"type" text NOT NULL,"status" text NOT NULL DEFAULT 'active',"revision" bigint NOT NULL DEFAULT 1,"draft_version_id" uuid,"dev_version_id" uuid,"prod_version_id" uuid,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uq_features_key" ON "features" ("key");
CREATE TABLE "feature_versions" ("id" uuid NOT NULL,"feature_id" uuid NOT NULL,"version" integer NOT NULL,"schema" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'draft',"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"published_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_feature_versions_feature" ON "feature_versions" ("feature_id","version" desc);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_versions_feature_version" ON "feature_versions" ("feature_id","version");
CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_versions_identity" ON "feature_versions" ("id","feature_id");
CREATE TABLE "feature_entries" ("id" uuid NOT NULL,"feature_id" uuid NOT NULL,"single_slot" boolean NOT NULL DEFAULT false,"revision" bigint NOT NULL DEFAULT 1,"draft_version_id" uuid,"dev_version_id" uuid,"prod_version_id" uuid,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),"archived_at" timestamptz,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entries_identity" ON "feature_entries" ("id","feature_id");
CREATE TABLE "feature_entry_versions" ("id" uuid NOT NULL,"entry_id" uuid NOT NULL,"version" integer NOT NULL,"schema_version_id" uuid NOT NULL,"data" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'draft',"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"published_at" timestamptz,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_entry_versions_schema" ON "feature_entry_versions" ("schema_version_id");
CREATE INDEX IF NOT EXISTS "idx_entry_versions_entry" ON "feature_entry_versions" ("entry_id","version" desc);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entry_versions_entry_version" ON "feature_entry_versions" ("entry_id","version");
CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entry_versions_identity" ON "feature_entry_versions" ("id","entry_id");
CREATE TABLE "form_submissions" ("id" uuid NOT NULL,"feature_id" uuid NOT NULL,"schema_version_id" uuid NOT NULL,"data" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'new',"processed_by" uuid,"environment" text NOT NULL DEFAULT 'prod',"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),"archived_at" timestamptz,"deleted_at" timestamptz,PRIMARY KEY ("id"));
CREATE TABLE "media_objects" ("id" uuid NOT NULL,"object_key" text NOT NULL,"original_filename" text NOT NULL,"content_type" text NOT NULL,"size_bytes" bigint NOT NULL,"status" text NOT NULL DEFAULT 'processing',"variants" jsonb NOT NULL DEFAULT '{}',"uploaded_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_media_objects_status_created" ON "media_objects" ("status","created_at" desc);
CREATE UNIQUE INDEX IF NOT EXISTS "uq_media_objects_object_key" ON "media_objects" ("object_key");
CREATE TABLE "content_publication_events" ("id" uuid NOT NULL,"entity_kind" text NOT NULL,"feature_id" uuid NOT NULL,"entry_id" uuid,"version_id" uuid NOT NULL,"environment" text NOT NULL,"action" text NOT NULL,"actor_token_id" uuid,"metadata" jsonb NOT NULL DEFAULT '{}',"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_publication_events_feature" ON "content_publication_events" ("feature_id","created_at" desc);
CREATE TABLE "site_settings" ("id" boolean NOT NULL DEFAULT true,"name" text NOT NULL DEFAULT 'Website',"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE TABLE "release_sources" ("id" uuid NOT NULL,"provider" text NOT NULL,"repo_url" text NOT NULL DEFAULT '',"asset_pattern" text NOT NULL DEFAULT '',"auth_ciphertext" bytea,"auth_nonce" bytea,"is_enabled" boolean NOT NULL DEFAULT true,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE TABLE "site_releases" ("id" uuid NOT NULL,"version" text NOT NULL,"source_id" uuid,"external_id" text,"tag" text,"commit_sha" text,"artifact_object_key" text NOT NULL,"artifact_sha256" text NOT NULL,"artifact_size" bigint NOT NULL,"manifest" jsonb NOT NULL DEFAULT '{}',"imported_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_site_releases_created" ON "site_releases" ("created_at" desc,"id" desc);
CREATE TABLE "site_environments" ("key" text NOT NULL,"name" text NOT NULL,"public_base_url" text NOT NULL DEFAULT '',"domains" text[] NOT NULL DEFAULT '{}',"http_port" integer NOT NULL,"tls_enabled" boolean NOT NULL DEFAULT false,"cors_origins" text[] NOT NULL DEFAULT '{}',"active_release_id" uuid,"revision" bigint NOT NULL DEFAULT 1,"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("key"));
CREATE TABLE "release_events" ("id" uuid NOT NULL,"release_id" uuid,"environment" text NOT NULL,"action" text NOT NULL,"actor_token_id" uuid,"metadata" jsonb NOT NULL DEFAULT '{}',"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_release_events_env" ON "release_events" ("environment","created_at" desc);
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
