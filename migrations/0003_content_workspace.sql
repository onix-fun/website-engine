-- Move the active content pointers out of immutable snapshot payloads. Keep
-- site_snapshot_content intact so historical snapshots remain inspectable.
ALTER TABLE feature_builds
    ADD COLUMN submission_schema_version_id uuid;

ALTER TABLE feature_builds
    ADD CONSTRAINT fk_feature_builds_submission_schema
    FOREIGN KEY (submission_schema_version_id) REFERENCES feature_versions(id) ON DELETE RESTRICT;

UPDATE feature_entries AS entry
SET dev_version_id = snapshot_content.version_id,
    updated_at = now()
FROM site_environments AS env
JOIN site_snapshot_content AS snapshot_content
  ON snapshot_content.snapshot_id = env.active_snapshot_id
WHERE env.key = 'dev'
  AND snapshot_content.entry_id = entry.id;

UPDATE feature_entries AS entry
SET prod_version_id = snapshot_content.version_id,
    updated_at = now()
FROM site_environments AS env
JOIN site_snapshot_content AS snapshot_content
  ON snapshot_content.snapshot_id = env.active_snapshot_id
WHERE env.key = 'prod'
  AND snapshot_content.entry_id = entry.id;
