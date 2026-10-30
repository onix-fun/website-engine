-- Add feature archives, editable site composition, immutable snapshots and
-- snapshot-pinned content. Legacy whole-site releases remain available during
-- the staging migration window.

ALTER TABLE feature_entries
    ADD COLUMN sort_order bigint NOT NULL DEFAULT 0;

CREATE INDEX idx_feature_entries_order
    ON feature_entries(feature_id, sort_order);

ALTER TABLE site_environments
    ADD COLUMN active_snapshot_id uuid;

CREATE TABLE feature_builds (
    id uuid NOT NULL,
    feature_id uuid NOT NULL,
    version text NOT NULL,
    schema_version_id uuid NOT NULL,
    digest text NOT NULL,
    artifact_size bigint NOT NULL,
    manifest jsonb NOT NULL,
    assets jsonb NOT NULL,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uq_feature_builds_digest ON feature_builds(digest);
CREATE INDEX idx_feature_builds_schema ON feature_builds(schema_version_id);
CREATE INDEX idx_feature_builds_feature ON feature_builds(feature_id, created_at DESC);
CREATE UNIQUE INDEX uq_feature_builds_feature_version ON feature_builds(feature_id, version);
CREATE UNIQUE INDEX uq_feature_builds_identity ON feature_builds(id, feature_id);

CREATE TABLE site_draft_config (
    id boolean NOT NULL DEFAULT true,
    config jsonb NOT NULL DEFAULT '{}',
    revision bigint NOT NULL DEFAULT 1,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

CREATE TABLE site_snapshots (
    id uuid NOT NULL,
    sequence bigserial NOT NULL,
    name text NOT NULL,
    digest text NOT NULL,
    payload jsonb NOT NULL,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_site_snapshots_created ON site_snapshots(created_at DESC);
CREATE UNIQUE INDEX uq_site_snapshots_digest ON site_snapshots(digest);
CREATE UNIQUE INDEX uq_site_snapshots_sequence ON site_snapshots(sequence);

CREATE TABLE site_snapshot_events (
    id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    environment text NOT NULL,
    action text NOT NULL,
    actor_token_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);
CREATE INDEX idx_site_snapshot_events_env
    ON site_snapshot_events(environment, snapshot_id, created_at DESC);

CREATE TABLE site_snapshot_content (
    snapshot_id uuid NOT NULL,
    feature_key text NOT NULL,
    entry_id uuid NOT NULL,
    version_id uuid NOT NULL,
    sort_order bigint NOT NULL DEFAULT 0,
    data jsonb NOT NULL,
    PRIMARY KEY (snapshot_id, feature_key, entry_id)
);
CREATE INDEX idx_site_snapshot_content_order
    ON site_snapshot_content(snapshot_id, feature_key, sort_order);
CREATE UNIQUE INDEX uq_site_snapshot_content_entry
    ON site_snapshot_content(snapshot_id, feature_key, entry_id);

ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_version_check
    CHECK (version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$');
ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_digest_check CHECK (digest ~ '^[a-f0-9]{64}$');
ALTER TABLE feature_builds
    ADD CONSTRAINT feature_builds_size_check CHECK (artifact_size > 0);
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
    ADD CONSTRAINT site_draft_config_singleton_check CHECK (id);
ALTER TABLE site_draft_config
    ADD CONSTRAINT site_draft_config_revision_check CHECK (revision > 0);
ALTER TABLE site_draft_config
    ADD CONSTRAINT site_draft_config_object_check CHECK (jsonb_typeof(config) = 'object');
ALTER TABLE site_draft_config
    ADD CONSTRAINT fk_site_draft_config_updater
    FOREIGN KEY (updated_by) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_sequence_check CHECK (sequence > 0);
ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_name_check CHECK (length(trim(name)) BETWEEN 1 AND 160);
ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_digest_check CHECK (digest ~ '^[a-f0-9]{64}$');
ALTER TABLE site_snapshots
    ADD CONSTRAINT site_snapshots_payload_check CHECK (jsonb_typeof(payload) = 'object');
ALTER TABLE site_snapshots
    ADD CONSTRAINT fk_site_snapshots_creator
    FOREIGN KEY (created_by) REFERENCES access_tokens(id) ON DELETE SET NULL;
ALTER TABLE site_environments
    ADD CONSTRAINT fk_site_environments_snapshot
    FOREIGN KEY (active_snapshot_id) REFERENCES site_snapshots(id) ON DELETE SET NULL;

ALTER TABLE site_snapshot_events
    ADD CONSTRAINT site_snapshot_events_environment_check CHECK (environment IN ('dev', 'prod'));
ALTER TABLE site_snapshot_events
    ADD CONSTRAINT site_snapshot_events_action_check CHECK (action IN ('publish', 'promote', 'rollback'));
ALTER TABLE site_snapshot_events
    ADD CONSTRAINT fk_site_snapshot_events_snapshot
    FOREIGN KEY (snapshot_id) REFERENCES site_snapshots(id) ON DELETE RESTRICT;
ALTER TABLE site_snapshot_events
    ADD CONSTRAINT fk_site_snapshot_events_actor
    FOREIGN KEY (actor_token_id) REFERENCES access_tokens(id) ON DELETE SET NULL;

ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_key_check CHECK (length(trim(feature_key)) BETWEEN 1 AND 100);
ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_order_check CHECK (sort_order >= 0);
ALTER TABLE site_snapshot_content
    ADD CONSTRAINT site_snapshot_content_data_check CHECK (jsonb_typeof(data) = 'object');
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

-- The API only inserts feature builds and snapshots. Enforce the same
-- immutability at the database boundary so an alternate writer cannot alter
-- the artifacts or content pinned by an environment.
CREATE FUNCTION reject_site_artifact_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    -- Creator metadata is not part of the artifact contract. Preserve the
    -- existing ON DELETE SET NULL audit behavior without allowing payload
    -- changes or deletion of a pinned row.
    IF TG_OP = 'UPDATE'
       AND TG_TABLE_NAME IN ('feature_builds', 'site_snapshots')
       AND (to_jsonb(OLD) -> 'created_by') IS NOT NULL
       AND (to_jsonb(OLD) -> 'created_by') <> 'null'::jsonb
       AND (to_jsonb(NEW) -> 'created_by') = 'null'::jsonb
       AND (to_jsonb(NEW) - 'created_by') = (to_jsonb(OLD) - 'created_by') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER feature_builds_are_immutable
    BEFORE UPDATE OR DELETE ON feature_builds
    FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();

CREATE TRIGGER site_snapshots_are_immutable
    BEFORE UPDATE OR DELETE ON site_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();

CREATE TRIGGER site_snapshot_content_is_immutable
    BEFORE UPDATE OR DELETE ON site_snapshot_content
    FOR EACH ROW EXECUTE FUNCTION reject_site_artifact_mutation();
