ALTER TABLE site_environments
    DROP COLUMN IF EXISTS domains,
    DROP COLUMN IF EXISTS tls_enabled;
