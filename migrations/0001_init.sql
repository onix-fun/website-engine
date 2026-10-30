-- Clean initial schema for Website Engine. Every final column and constraint is declared here.

CREATE TABLE access_tokens (
    "id" uuid NOT NULL,"name" text NOT NULL,"token_hash" bytea NOT NULL,"role" text NOT NULL,"created_by" uuid,"expires_at" timestamptz,"revoked_at" timestamptz,"last_used_at" timestamptz,"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT access_tokens_role_check CHECK (role IN ('admin', 'moderator', 'staff', 'user')),
    CONSTRAINT access_tokens_name_check CHECK (length(trim(name)) BETWEEN 1 AND 120),
    CONSTRAINT access_tokens_expiry_check CHECK (expires_at IS NULL OR expires_at > created_at),
    CONSTRAINT fk_access_tokens_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE features (
    "id" uuid NOT NULL,"key" text NOT NULL,"name" text NOT NULL,"type" text NOT NULL,"status" text NOT NULL DEFAULT 'active',"revision" bigint NOT NULL DEFAULT 1,"draft_version_id" uuid,"dev_version_id" uuid,"prod_version_id" uuid,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT features_key_check CHECK (key ~ '^[a-z][a-z0-9_-]{0,99}$'),
    CONSTRAINT features_name_check CHECK (length(trim(name)) BETWEEN 1 AND 200),
    CONSTRAINT features_type_check CHECK (type IN ('collection', 'single', 'form')),
    CONSTRAINT features_status_check CHECK (status IN ('active', 'archived')),
    CONSTRAINT features_revision_check CHECK (revision > 0),
    CONSTRAINT features_draft_not_dev CHECK (draft_version_id IS NULL OR dev_version_id IS NULL OR draft_version_id <> dev_version_id),
    CONSTRAINT features_draft_not_prod CHECK (draft_version_id IS NULL OR prod_version_id IS NULL OR draft_version_id <> prod_version_id),
    CONSTRAINT features_prod_requires_dev CHECK (prod_version_id IS NULL OR dev_version_id IS NOT NULL),
    CONSTRAINT fk_features_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE feature_versions (
    "id" uuid NOT NULL,"feature_id" uuid NOT NULL,"version" integer NOT NULL,"schema" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'draft',"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"published_at" timestamptz,PRIMARY KEY ("id"),
    CONSTRAINT feature_versions_version_check CHECK (version > 0),
    CONSTRAINT feature_versions_schema_check CHECK (jsonb_typeof(schema) = 'object'),
    CONSTRAINT feature_versions_status_check CHECK (status IN ('draft', 'published')),
    CONSTRAINT feature_versions_published_at_check CHECK ( (status = 'draft' AND published_at IS NULL) OR (status = 'published' AND published_at IS NOT NULL) ),
    CONSTRAINT fk_feature_versions_feature FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT,
    CONSTRAINT fk_feature_versions_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE feature_entries (
    "id" uuid NOT NULL,"feature_id" uuid NOT NULL,"single_slot" boolean NOT NULL DEFAULT false,"revision" bigint NOT NULL DEFAULT 1,"draft_version_id" uuid,"dev_version_id" uuid,"prod_version_id" uuid,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),"archived_at" timestamptz,"sort_order" bigint NOT NULL DEFAULT 0,PRIMARY KEY ("id"),
    CONSTRAINT feature_entries_revision_check CHECK (revision > 0),
    CONSTRAINT entries_draft_not_dev CHECK (draft_version_id IS NULL OR dev_version_id IS NULL OR draft_version_id <> dev_version_id),
    CONSTRAINT entries_draft_not_prod CHECK (draft_version_id IS NULL OR prod_version_id IS NULL OR draft_version_id <> prod_version_id),
    CONSTRAINT entries_prod_requires_dev CHECK (prod_version_id IS NULL OR dev_version_id IS NOT NULL),
    CONSTRAINT fk_feature_entries_feature FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT,
    CONSTRAINT fk_feature_entries_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE feature_entry_versions (
    "id" uuid NOT NULL,"entry_id" uuid NOT NULL,"version" integer NOT NULL,"schema_version_id" uuid NOT NULL,"data" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'draft',"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"published_at" timestamptz,PRIMARY KEY ("id"),
    CONSTRAINT entry_versions_version_check CHECK (version > 0),
    CONSTRAINT entry_versions_data_check CHECK (jsonb_typeof(data) = 'object'),
    CONSTRAINT entry_versions_status_check CHECK (status IN ('draft', 'published')),
    CONSTRAINT entry_versions_published_at_check CHECK ( (status = 'draft' AND published_at IS NULL) OR (status = 'published' AND published_at IS NOT NULL) ),
    CONSTRAINT fk_entry_versions_entry FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE RESTRICT,
    CONSTRAINT fk_entry_versions_schema FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_entry_versions_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE form_submissions (
    "id" uuid NOT NULL,"feature_id" uuid NOT NULL,"schema_version_id" uuid NOT NULL,"data" jsonb NOT NULL,"status" text NOT NULL DEFAULT 'new',"processed_by" uuid,"environment" text NOT NULL DEFAULT 'prod',"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),"archived_at" timestamptz,"deleted_at" timestamptz,PRIMARY KEY ("id"),
    CONSTRAINT submissions_data_check CHECK (jsonb_typeof(data) = 'object'),
    CONSTRAINT submissions_status_check CHECK (status IN ('new', 'in_progress', 'processed', 'rejected')),
    CONSTRAINT submissions_environment_check CHECK (environment IN ('dev', 'prod')),
    CONSTRAINT fk_submissions_feature FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT,
    CONSTRAINT fk_submissions_schema FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_submissions_processor FOREIGN KEY (processed_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE media_objects (
    "id" uuid NOT NULL,"object_key" text NOT NULL,"original_filename" text NOT NULL,"content_type" text NOT NULL,"size_bytes" bigint NOT NULL,"status" text NOT NULL DEFAULT 'processing',"variants" jsonb NOT NULL DEFAULT '{}',"uploaded_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT media_objects_object_key_check CHECK (length(object_key) BETWEEN 1 AND 1024),
    CONSTRAINT media_objects_filename_check CHECK (length(original_filename) BETWEEN 1 AND 255),
    CONSTRAINT media_objects_size_check CHECK (size_bytes >= 0),
    CONSTRAINT media_objects_status_check CHECK (status IN ('processing', 'ready', 'failed', 'deleted')),
    CONSTRAINT media_objects_variants_check CHECK (jsonb_typeof(variants) = 'object'),
    CONSTRAINT fk_media_objects_uploader FOREIGN KEY (uploaded_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE content_publication_events (
    "id" uuid NOT NULL,"entity_kind" text NOT NULL,"feature_id" uuid NOT NULL,"entry_id" uuid,"version_id" uuid NOT NULL,"environment" text NOT NULL,"action" text NOT NULL,"actor_token_id" uuid,"metadata" jsonb NOT NULL DEFAULT '{}',"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT publication_events_entity_kind_check CHECK (entity_kind IN ('schema', 'entry')),
    CONSTRAINT publication_events_environment_check CHECK (environment IN ('dev', 'prod')),
    CONSTRAINT publication_events_action_check CHECK (action IN ('publish', 'promote', 'rollback', 'unpublish')),
    CONSTRAINT publication_events_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT publication_events_entry_check CHECK ( (entity_kind = 'schema' AND entry_id IS NULL) OR (entity_kind = 'entry' AND entry_id IS NOT NULL) ),
    CONSTRAINT fk_publication_events_feature FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE CASCADE,
    CONSTRAINT fk_publication_events_entry FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE CASCADE,
    CONSTRAINT fk_publication_events_actor FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_settings (
    "id" boolean NOT NULL DEFAULT true,"name" text NOT NULL DEFAULT 'Website',"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT site_settings_id_check CHECK (id),
    CONSTRAINT site_settings_name_check CHECK (length(trim(name)) BETWEEN 1 AND 200)
);

CREATE TABLE release_sources (
    "id" uuid NOT NULL,"provider" text NOT NULL,"repo_url" text NOT NULL DEFAULT '',"asset_pattern" text NOT NULL DEFAULT '',"auth_ciphertext" bytea,"auth_nonce" bytea,"is_enabled" boolean NOT NULL DEFAULT true,"created_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT release_sources_provider_check CHECK (provider IN ('github', 'gitlab', 'gitea', 'manual')),
    CONSTRAINT release_sources_repo_check CHECK (provider = 'manual' OR repo_url <> ''),
    CONSTRAINT release_sources_auth_check CHECK (auth_ciphertext IS NULL OR auth_nonce IS NOT NULL),
    CONSTRAINT fk_release_sources_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_releases (
    "id" uuid NOT NULL,"version" text NOT NULL,"source_id" uuid,"external_id" text,"tag" text,"commit_sha" text,"artifact_object_key" text NOT NULL,"artifact_sha256" text NOT NULL,"artifact_size" bigint NOT NULL,"manifest" jsonb NOT NULL DEFAULT '{}',"imported_by" uuid,"created_at" timestamptz NOT NULL DEFAULT now(),"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT site_releases_version_check CHECK (length(version) BETWEEN 1 AND 200),
    CONSTRAINT site_releases_sha256_check CHECK (length(artifact_sha256) = 64),
    CONSTRAINT site_releases_size_check CHECK (artifact_size > 0),
    CONSTRAINT site_releases_manifest_check CHECK (jsonb_typeof(manifest) = 'object'),
    CONSTRAINT fk_site_releases_source FOREIGN KEY (source_id) REFERENCES release_sources(id) ON DELETE SET NULL,
    CONSTRAINT fk_site_releases_importer FOREIGN KEY (imported_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_snapshots (
    id uuid NOT NULL,
    sequence bigserial NOT NULL,
    name text NOT NULL,
    digest text NOT NULL,
    payload jsonb NOT NULL,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT site_snapshots_sequence_check CHECK (sequence > 0),
    CONSTRAINT site_snapshots_name_check CHECK (length(trim(name)) BETWEEN 1 AND 160),
    CONSTRAINT site_snapshots_digest_check CHECK (digest ~ '^[a-f0-9]{64}$'),
    CONSTRAINT site_snapshots_payload_check CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT fk_site_snapshots_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_environments (
    "key" text NOT NULL,"name" text NOT NULL,"public_base_url" text NOT NULL DEFAULT '',"domains" text[] NOT NULL DEFAULT '{}',"http_port" integer NOT NULL,"tls_enabled" boolean NOT NULL DEFAULT false,"cors_origins" text[] NOT NULL DEFAULT '{}',"active_release_id" uuid,"active_snapshot_id" uuid,"revision" bigint NOT NULL DEFAULT 1,"updated_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("key"),
    CONSTRAINT site_environments_key_check CHECK (key IN ('dev', 'prod')),
    CONSTRAINT site_environments_name_check CHECK (length(trim(name)) BETWEEN 1 AND 100),
    CONSTRAINT site_environments_port_check CHECK (http_port BETWEEN 1 AND 65535),
    CONSTRAINT site_environments_revision_check CHECK (revision > 0),
    CONSTRAINT fk_site_environments_release FOREIGN KEY (active_release_id) REFERENCES site_releases(id) ON DELETE SET NULL,
    CONSTRAINT fk_site_environments_snapshot FOREIGN KEY (active_snapshot_id) REFERENCES site_snapshots(id) ON DELETE SET NULL
);

CREATE TABLE release_events (
    "id" uuid NOT NULL,"release_id" uuid,"environment" text NOT NULL,"action" text NOT NULL,"actor_token_id" uuid,"metadata" jsonb NOT NULL DEFAULT '{}',"created_at" timestamptz NOT NULL DEFAULT now(),PRIMARY KEY ("id"),
    CONSTRAINT release_events_environment_check CHECK (environment IN ('dev', 'prod')),
    CONSTRAINT release_events_action_check CHECK (action IN ('import', 'activate', 'rollback')),
    CONSTRAINT release_events_metadata_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT fk_release_events_release FOREIGN KEY (release_id) REFERENCES site_releases(id) ON DELETE SET NULL,
    CONSTRAINT fk_release_events_actor FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE feature_builds (
    id uuid NOT NULL,
    feature_id uuid NOT NULL,
    version text NOT NULL,
    schema_version_id uuid NOT NULL, submission_schema_version_id uuid, settings_schema_version_id uuid,
    digest text NOT NULL,
    artifact_size bigint NOT NULL,
    manifest jsonb NOT NULL,
    assets jsonb NOT NULL,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT feature_builds_version_check CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'),
    CONSTRAINT feature_builds_digest_check CHECK (digest ~ '^[a-f0-9]{64}$'),
    CONSTRAINT feature_builds_size_check CHECK (artifact_size > 0),
    CONSTRAINT feature_builds_manifest_check CHECK (jsonb_typeof(manifest) = 'object' AND jsonb_typeof(assets) = 'object'),
    CONSTRAINT fk_feature_builds_feature FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE RESTRICT,
    CONSTRAINT fk_feature_builds_schema FOREIGN KEY (schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_feature_builds_creator FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL,
    CONSTRAINT fk_feature_builds_submission_schema FOREIGN KEY (submission_schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_feature_builds_settings_schema FOREIGN KEY (settings_schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT
);

CREATE TABLE site_draft_config (
    id boolean NOT NULL DEFAULT true,
    config jsonb NOT NULL DEFAULT '{}',
    revision bigint NOT NULL DEFAULT 1,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT site_draft_config_singleton_check CHECK (id),
    CONSTRAINT site_draft_config_revision_check CHECK (revision > 0),
    CONSTRAINT site_draft_config_object_check CHECK (jsonb_typeof(config) = 'object'),
    CONSTRAINT fk_site_draft_config_updater FOREIGN KEY (updated_by) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_snapshot_events (
    id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    environment text NOT NULL,
    action text NOT NULL,
    actor_token_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT site_snapshot_events_environment_check CHECK (environment IN ('dev', 'prod')),
    CONSTRAINT site_snapshot_events_action_check CHECK (action IN ('publish', 'promote', 'rollback')),
    CONSTRAINT fk_site_snapshot_events_snapshot FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE RESTRICT,
    CONSTRAINT fk_site_snapshot_events_actor FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL
);

CREATE TABLE site_snapshot_content (
    snapshot_id uuid NOT NULL,
    feature_key text NOT NULL,
    entry_id uuid NOT NULL,
    version_id uuid NOT NULL,
    sort_order bigint NOT NULL DEFAULT 0,
    data jsonb NOT NULL,
    PRIMARY KEY (snapshot_id, feature_key, entry_id),
    CONSTRAINT site_snapshot_content_key_check CHECK (length(trim(feature_key)) BETWEEN 1 AND 100),
    CONSTRAINT site_snapshot_content_order_check CHECK (sort_order >= 0),
    CONSTRAINT site_snapshot_content_data_check CHECK (jsonb_typeof(data) = 'object'),
    CONSTRAINT fk_site_snapshot_content_snapshot FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE RESTRICT,
    CONSTRAINT fk_site_snapshot_content_entry FOREIGN KEY (entry_id) REFERENCES feature_entries(id) ON DELETE RESTRICT,
    CONSTRAINT fk_site_snapshot_content_version FOREIGN KEY (version_id) REFERENCES feature_entry_versions(id) ON DELETE RESTRICT
);

CREATE TABLE site_metadata_state (
    id boolean NOT NULL DEFAULT TRUE,
    revision bigint NOT NULL DEFAULT 1,
    dev_revision bigint NOT NULL DEFAULT 0,
    prod_revision bigint NOT NULL DEFAULT 0,
    draft jsonb NOT NULL DEFAULT '{"formatVersion":1,"siteName":"Website","pages":{}}'::jsonb,
    dev jsonb NOT NULL DEFAULT '{"formatVersion":1,"siteName":"Website","pages":{}}'::jsonb,
    prod jsonb NOT NULL DEFAULT '{"formatVersion":1,"siteName":"Website","pages":{}}'::jsonb,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_metadata_state_singleton_check CHECK (id),
    CONSTRAINT site_metadata_state_revision_check CHECK (revision > 0 AND dev_revision >= 0 AND prod_revision >= 0),
    CONSTRAINT site_metadata_state_json_check CHECK (jsonb_typeof(draft)='object' AND jsonb_typeof(dev)='object' AND jsonb_typeof(prod)='object'),
    CONSTRAINT fk_site_metadata_state_updater FOREIGN KEY (updated_by) REFERENCES access_tokens(id) ON DELETE SET NULL,
    PRIMARY KEY (id)
);

CREATE TABLE site_html_shells (
    environment text NOT NULL,
    snapshot_id uuid NOT NULL,
    route_id text NOT NULL,
    metadata_revision bigint NOT NULL,
    html text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_html_shells_environment_check CHECK (environment IN ('dev','prod')),
    CONSTRAINT site_html_shells_route_check CHECK (length(route_id) BETWEEN 1 AND 200),
    CONSTRAINT site_html_shells_revision_check CHECK (metadata_revision >= 0),
    CONSTRAINT fk_site_html_shells_snapshot FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE CASCADE,
    PRIMARY KEY (environment,snapshot_id,route_id)
);

CREATE INDEX IF NOT EXISTS "idx_access_tokens_creator" ON "access_tokens" ("created_by");

CREATE UNIQUE INDEX IF NOT EXISTS "uq_access_tokens_token_hash" ON "access_tokens" ("token_hash");

CREATE UNIQUE INDEX IF NOT EXISTS "uq_features_key" ON "features" ("key");

CREATE INDEX IF NOT EXISTS "idx_feature_versions_feature" ON "feature_versions" ("feature_id","version" desc);

CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_versions_feature_version" ON "feature_versions" ("feature_id","version");

CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_versions_identity" ON "feature_versions" ("id","feature_id");

CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entries_identity" ON "feature_entries" ("id","feature_id");

CREATE INDEX IF NOT EXISTS "idx_entry_versions_schema" ON "feature_entry_versions" ("schema_version_id");

CREATE INDEX IF NOT EXISTS "idx_entry_versions_entry" ON "feature_entry_versions" ("entry_id","version" desc);

CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entry_versions_entry_version" ON "feature_entry_versions" ("entry_id","version");

CREATE UNIQUE INDEX IF NOT EXISTS "uq_feature_entry_versions_identity" ON "feature_entry_versions" ("id","entry_id");

CREATE INDEX IF NOT EXISTS "idx_media_objects_status_created" ON "media_objects" ("status","created_at" desc);

CREATE UNIQUE INDEX IF NOT EXISTS "uq_media_objects_object_key" ON "media_objects" ("object_key");

CREATE INDEX IF NOT EXISTS "idx_publication_events_feature" ON "content_publication_events" ("feature_id","created_at" desc);

CREATE INDEX IF NOT EXISTS "idx_site_releases_created" ON "site_releases" ("created_at" desc,"id" desc);

CREATE INDEX IF NOT EXISTS "idx_release_events_env" ON "release_events" ("environment","created_at" desc);

CREATE INDEX idx_access_tokens_active
    ON access_tokens(role, expires_at)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_features_dev
    ON features(dev_version_id)
    WHERE dev_version_id IS NOT NULL;

CREATE INDEX idx_features_prod
    ON features(prod_version_id)
    WHERE prod_version_id IS NOT NULL;

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

CREATE INDEX idx_entry_versions_data
    ON feature_entry_versions USING GIN (data jsonb_path_ops)
    WHERE status = 'published';

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

CREATE INDEX idx_publication_events_entry
    ON content_publication_events(entry_id, created_at DESC)
    WHERE entry_id IS NOT NULL;

CREATE INDEX idx_feature_entries_order
    ON feature_entries(feature_id, sort_order);

CREATE UNIQUE INDEX uq_feature_builds_digest ON feature_builds(digest);

CREATE INDEX idx_feature_builds_schema ON feature_builds(schema_version_id);

CREATE INDEX idx_feature_builds_feature ON feature_builds(feature_id, created_at DESC);

CREATE UNIQUE INDEX uq_feature_builds_feature_version ON feature_builds(feature_id, version);

CREATE UNIQUE INDEX uq_feature_builds_identity ON feature_builds(id, feature_id);

CREATE INDEX idx_site_snapshots_created ON site_snapshots(created_at DESC);

CREATE UNIQUE INDEX uq_site_snapshots_digest ON site_snapshots(digest);

CREATE UNIQUE INDEX uq_site_snapshots_sequence ON site_snapshots(sequence);

CREATE INDEX idx_site_snapshot_events_env
    ON site_snapshot_events(environment, snapshot_id, created_at DESC);

CREATE INDEX idx_site_snapshot_content_order
    ON site_snapshot_content(snapshot_id, feature_key, sort_order);

CREATE UNIQUE INDEX uq_site_snapshot_content_entry
    ON site_snapshot_content(snapshot_id, feature_key, entry_id);

CREATE INDEX idx_site_snapshot_content_data
    ON site_snapshot_content USING GIN (data jsonb_path_ops);

-- Immutable build and snapshot rows remain protected at the database boundary.
CREATE FUNCTION reject_site_artifact_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND TG_TABLE_NAME IN ('feature_builds', 'site_snapshots')
       AND (to_jsonb(OLD) -> 'created_by') IS NOT NULL
       AND (to_jsonb(OLD) -> 'created_by') <> 'null'::jsonb
       AND (to_jsonb(NEW) -> 'created_by') = 'null'::jsonb
       AND (to_jsonb(NEW) - 'created_by') = (to_jsonb(OLD) - 'created_by') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = 'check_violation';
END;
$$;
CREATE TRIGGER feature_builds_are_immutable BEFORE UPDATE OR DELETE ON feature_builds FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();
CREATE TRIGGER site_snapshots_are_immutable BEFORE UPDATE OR DELETE ON site_snapshots FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();
CREATE TRIGGER site_snapshot_content_is_immutable BEFORE UPDATE OR DELETE ON site_snapshot_content FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();


CREATE INDEX idx_site_html_shells_snapshot ON site_html_shells(environment,snapshot_id);
