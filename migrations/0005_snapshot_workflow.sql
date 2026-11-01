DROP INDEX uq_site_snapshots_digest;
CREATE INDEX idx_site_snapshots_digest ON site_snapshots(digest);

ALTER TABLE site_snapshot_events
    DROP CONSTRAINT site_snapshot_events_action_check,
    ADD CONSTRAINT site_snapshot_events_action_check
        CHECK (action IN ('publish', 'promote', 'rollback', 'deploy'));
