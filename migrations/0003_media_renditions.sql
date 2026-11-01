CREATE TABLE media_renditions (
    media_id uuid NOT NULL,
    profile_hash text NOT NULL,
    profile jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    outputs jsonb NOT NULL DEFAULT '{}',
    attempts integer NOT NULL DEFAULT 0,
    lease_expires_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (media_id, profile_hash),
    CONSTRAINT media_renditions_profile_hash_check CHECK (profile_hash ~ '^[a-f0-9]{64}$'),
    CONSTRAINT media_renditions_profile_check CHECK (jsonb_typeof(profile) = 'object'),
    CONSTRAINT media_renditions_status_check CHECK (status IN ('pending', 'processing', 'ready', 'failed')),
    CONSTRAINT media_renditions_outputs_check CHECK (jsonb_typeof(outputs) = 'object'),
    CONSTRAINT media_renditions_attempts_check CHECK (attempts >= 0),
    CONSTRAINT fk_media_renditions_media FOREIGN KEY (media_id) REFERENCES media_objects(id) ON DELETE CASCADE
);

CREATE INDEX idx_media_renditions_queue
    ON media_renditions (created_at)
    WHERE status = 'pending';

CREATE INDEX idx_media_renditions_lease
    ON media_renditions (lease_expires_at)
    WHERE status = 'processing';
