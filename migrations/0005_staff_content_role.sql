ALTER TABLE access_tokens
    DROP CONSTRAINT access_tokens_role_check;

ALTER TABLE access_tokens
    ADD CONSTRAINT access_tokens_role_check
    CHECK (role IN ('admin', 'moderator', 'staff', 'user'));
