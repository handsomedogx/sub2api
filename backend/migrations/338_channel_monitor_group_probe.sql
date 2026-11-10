-- NULL preserves external URL/API-key monitors. A removed group's ID is kept:
-- the service reports an unavailable group rather than falling back to a key.
ALTER TABLE channel_monitors ADD COLUMN group_id BIGINT CHECK (group_id > 0);
ALTER TABLE channel_monitors DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai','anthropic','gemini','grok','antigravity','kimi','zhipu','deepseek','minimax','opencode_go','composite'));
