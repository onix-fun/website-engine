-- A site snapshot can deploy a historical content set to either environment
-- independently. Keep the two editorial pointers independent for this flow.
ALTER TABLE feature_entries
    DROP CONSTRAINT entries_prod_requires_dev;
