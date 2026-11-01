CREATE TABLE site_design_system_state (
    id boolean NOT NULL DEFAULT TRUE,
    revision bigint NOT NULL DEFAULT 1,
    draft jsonb NOT NULL DEFAULT '{"formatVersion":1,"tokens":{},"mobileTokens":{},"fonts":[],"fontRoles":{"body":"Alumni Sans SC","display":"Alumni Sans SC","header":"Alumni Sans SC","hero":"Alumni Sans SC"}}'::jsonb,
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT site_design_system_state_singleton_check CHECK (id),
    CONSTRAINT site_design_system_state_revision_check CHECK (revision > 0),
    CONSTRAINT site_design_system_state_json_check CHECK (jsonb_typeof(draft)='object'),
    CONSTRAINT fk_site_design_system_state_updater FOREIGN KEY (updated_by) REFERENCES access_tokens(id) ON DELETE SET NULL,
    PRIMARY KEY (id)
);
