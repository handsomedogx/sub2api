-- Operating expenses are independent of customer billing and test-history cleanup.
CREATE TABLE IF NOT EXISTS internal_request_costs (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL CHECK (source IN ('pelican','monitor')),
    kind TEXT NOT NULL,
    account_id BIGINT NOT NULL DEFAULT 0,
    group_id BIGINT NOT NULL DEFAULT 0,
    monitor_id BIGINT NOT NULL DEFAULT 0,
    requested_model TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    tokens JSONB NOT NULL DEFAULT '{}'::jsonb,
    standard_cost NUMERIC(24,10),
    cost_multiplier NUMERIC(20,10),
    cost_usd NUMERIC(24,10),
    incomplete BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (standard_cost IS NULL OR (standard_cost >= 0 AND standard_cost < 'Infinity'::numeric)),
    CHECK (cost_multiplier IS NULL OR (cost_multiplier >= 0 AND cost_multiplier <= 1000000)),
    CHECK (cost_usd IS NULL OR (cost_usd >= 0 AND cost_usd < 'Infinity'::numeric))
);
CREATE INDEX IF NOT EXISTS idx_internal_request_costs_time ON internal_request_costs(created_at);
CREATE INDEX IF NOT EXISTS idx_internal_request_costs_account_time ON internal_request_costs(account_id,created_at);
CREATE INDEX IF NOT EXISTS idx_internal_request_costs_group_time ON internal_request_costs(group_id,created_at);

-- Import only evidence still retained. Never invent tokens or historical multipliers.
-- Old retried samples only have a combined cost: do not attribute it all to the final account.
INSERT INTO internal_request_costs(id,source,kind,account_id,group_id,requested_model,model,cost_usd,incomplete,status,created_at)
SELECT 'legacy:pelican:'||r.id,'pelican','legacy_group_test',
 CASE WHEN r.attempts='[]'::jsonb THEN COALESCE(r.account_id,0) ELSE 0 END,
 p.group_id,p.model_id,p.model_id,CASE WHEN r.cost_usd>=0 AND r.cost_usd<'Infinity'::numeric THEN r.cost_usd END,
 r.cost_incomplete OR NOT COALESCE(r.cost_usd>=0 AND r.cost_usd<'Infinity'::numeric,false),r.status,r.finished_at
FROM pelican_group_test_results r JOIN pelican_group_test_plans p ON p.id=r.plan_id
ON CONFLICT(id) DO NOTHING;

INSERT INTO internal_request_costs(id,source,kind,account_id,group_id,requested_model,model,cost_usd,incomplete,status,created_at)
SELECT 'legacy:v3:'||id,'monitor','legacy_v3_idle',0,group_id,model,model,CASE WHEN cost_usd>=0 AND cost_usd<'Infinity'::numeric THEN cost_usd END,
 NOT COALESCE(cost_usd>=0 AND cost_usd<'Infinity'::numeric,false),outcome,finished_at
FROM channel_monitor_v3_probe_results WHERE finished_at IS NOT NULL
ON CONFLICT(id) DO NOTHING;

-- V1 and candy history did not retain costs. Their known existence is an unknown expense.
INSERT INTO internal_request_costs(id,source,kind,group_id,requested_model,model,status,created_at)
SELECT 'legacy:candy:'||id,'monitor','legacy_v2_candy',group_id,model,model,verdict,finished_at
FROM channel_monitor_v2_candy_results WHERE finished_at IS NOT NULL
ON CONFLICT(id) DO NOTHING;

INSERT INTO internal_request_costs(id,source,kind,monitor_id,group_id,requested_model,model,status,created_at)
SELECT 'legacy:v1:'||h.id,'monitor','legacy_v1',h.monitor_id,COALESCE(m.group_id,0),h.model,h.model,h.status,h.checked_at
FROM channel_monitor_histories h JOIN channel_monitors m ON m.id=h.monitor_id
-- Use historical timing evidence, not today's mode (which may have changed).
-- Pure quota reads have neither model latency nor endpoint ping latency.
WHERE h.latency_ms IS NOT NULL OR h.ping_latency_ms IS NOT NULL
ON CONFLICT(id) DO NOTHING;

COMMENT ON TABLE internal_request_costs IS 'Internal upstream request cost snapshots; no customer balance or quota changes. Legacy coverage is limited to retained history.';
