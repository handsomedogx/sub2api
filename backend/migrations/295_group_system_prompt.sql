ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS system_prompt_mode VARCHAR(20) NOT NULL DEFAULT 'passthrough',
    ADD COLUMN IF NOT EXISTS system_prompt TEXT NOT NULL DEFAULT '';

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_system_prompt_mode_check;

ALTER TABLE groups
    ADD CONSTRAINT groups_system_prompt_mode_check
    CHECK (system_prompt_mode IN ('passthrough', 'inject', 'replace'));

COMMENT ON COLUMN groups.system_prompt_mode IS
    'Group-level client system prompt policy: passthrough, inject, or replace';

COMMENT ON COLUMN groups.system_prompt IS
    'Gateway-managed system prompt used by inject/replace policy';
