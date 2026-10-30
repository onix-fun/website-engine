ALTER TABLE feature_builds
    ADD COLUMN settings_schema_version_id uuid;

ALTER TABLE feature_builds
    ADD CONSTRAINT fk_feature_builds_settings_schema
    FOREIGN KEY (settings_schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT;
