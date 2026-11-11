//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInternalCostsIncludedOnceFilteredAndFrozen(t *testing.T) {
	f := newChannelOperationsFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, err := integrationDB.Exec(`UPDATE accounts SET type='apikey',credentials='{"base_url":"https://internal.example/v1"}',extra='{"cost_multiplier":0.075}' WHERE id=$1`, f.account)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,group_id,model,total_cost,actual_cost,created_at) VALUES($1,$2,$3,$4,$5,'model',100,20,$6)`, f.user, f.key, f.account, uuid.NewString(), f.group, now.Add(-time.Hour))
	require.NoError(t, err)
	base, rate, cost := 10.0, 0.075, 0.75
	var monitorID int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO channel_monitors(name,provider,endpoint,api_key_encrypted,primary_model,interval_seconds,created_by)
 VALUES('Internal heartbeat','openai','https://internal.example','fixture','model',60,$1) RETURNING id`, f.user).Scan(&monitorID))
	t.Cleanup(func() { _, _ = integrationDB.Exec(`DELETE FROM channel_monitors WHERE id=$1`, monitorID) })
	rows := []service.InternalRequestCost{
		{ID: uuid.NewString(), Source: "pelican", Kind: "group_test", AccountID: f.account, GroupID: f.group, RequestedModel: "model", Model: "model", StandardCost: &base, CostMultiplier: &rate, CostUSD: &cost, Status: "failed", CreatedAt: now.Add(-time.Minute)},
		{ID: uuid.NewString(), Source: "monitor", Kind: "v1_group", MonitorID: monitorID, AccountID: f.account, GroupID: f.group, RequestedModel: "model", Model: "model", StandardCost: &base, CostMultiplier: &rate, CostUSD: &cost, Status: "success", CreatedAt: now.Add(-time.Minute)},
	}
	t.Cleanup(func() {
		for _, r := range rows {
			_, _ = integrationDB.Exec(`DELETE FROM internal_request_costs WHERE id=$1`, r.ID)
		}
	})
	require.NoError(t, writeInternalRequestCosts(context.Background(), integrationDB, rows))
	require.NoError(t, writeInternalRequestCosts(context.Background(), integrationDB, rows), "retry cannot double charge an expense")
	q := service.UpstreamOperationsFilter{From: now.Add(-2 * time.Hour), Until: now, AccountID: f.account, ModelScope: "public", Dimension: "account", Sort: "requests", Direction: "desc", Page: 1, PageSize: 20}
	read := func() map[string]any {
		out, e := f.repo.ReadProfitOperations(context.Background(), q)
		require.NoError(t, e)
		var data map[string]any
		require.NoError(t, json.Unmarshal(out.Usage, &data))
		return data
	}
	data := read()
	sum := data["summary"].(map[string]any)
	require.Equal(t, 1.0, sum["requests"])
	require.Equal(t, 2.0, sum["internal_requests"])
	require.InDelta(t, 9.0, sum["estimated_cost"], 1e-9)
	require.InDelta(t, 11.0, sum["estimated_profit"], 1e-9)
	require.Len(t, data["internal_records"].([]any), 2)
	for _, raw := range data["internal_records"].([]any) {
		record := raw.(map[string]any)
		require.Equal(t, "Operations account", record["account_name"])
		require.Equal(t, float64(f.group), record["group_id"])
		require.Equal(t, fmt.Sprintf("operations-%d", f.channel), record["group_name"])
		if record["source"] == "monitor" {
			require.Equal(t, "Internal heartbeat", record["monitor_name"])
		}
	}
	_, err = integrationDB.Exec(`DELETE FROM channel_monitors WHERE id=$1`, monitorID)
	require.NoError(t, err)
	data = read()
	require.Len(t, data["internal_records"].([]any), 2, "deleting a monitor must preserve expense records")
	for _, raw := range data["internal_records"].([]any) {
		record := raw.(map[string]any)
		if record["source"] == "monitor" {
			require.Nil(t, record["monitor_name"])
			require.Equal(t, float64(monitorID), record["monitor_id"])
		}
	}
	q.Source = "user"
	sum = read()["summary"].(map[string]any)
	require.Equal(t, 0.0, sum["internal_requests"])
	require.InDelta(t, 7.5, sum["estimated_cost"], 1e-9)
	q.Source = "monitor"
	sum = read()["summary"].(map[string]any)
	require.Equal(t, 0.0, sum["requests"])
	require.InDelta(t, -0.75, sum["estimated_profit"], 1e-9)
	_, err = integrationDB.Exec(`UPDATE accounts SET extra='{"cost_multiplier":0.9}',deleted_at=NOW() WHERE id=$1`, f.account)
	require.NoError(t, err)
	sum = read()["summary"].(map[string]any)
	require.InDelta(t, 0.75, sum["monitor_cost"], 1e-9, "snapshot stays frozen")
	q.Model = "does-not-match"
	require.Empty(t, read()["internal_records"])
	q.Model = "model"
	q.GroupID = f.group
	q.Source = "internal"
	require.Len(t, read()["internal_records"].([]any), 2)
	unknown := service.InternalRequestCost{ID: uuid.NewString(), Source: "monitor", Kind: "v1_group", AccountID: f.account, GroupID: f.group, RequestedModel: "model", Model: "model", Incomplete: true, Status: "failed", CreatedAt: now.Add(-time.Minute)}
	rows = append(rows, unknown)
	require.NoError(t, writeInternalRequestCosts(context.Background(), integrationDB, []service.InternalRequestCost{unknown}))
	sum = read()["summary"].(map[string]any)
	require.Nil(t, sum["estimated_cost"])
	require.Nil(t, sum["estimated_profit"])
	require.Equal(t, 1.0, sum["unknown_internal_requests"])
	require.InDelta(t, 1.5, sum["covered_internal_cost"], 1e-9)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_logs WHERE account_id=$1`, f.account).Scan(&count))
	require.Equal(t, 1, count, "internal requests never enter customer billing")
}

func TestInternalCostsWithoutUserTrafficAndUnknownAttribution(t *testing.T) {
	f := newChannelOperationsFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	amount := 2.0
	records := []service.InternalRequestCost{
		{ID: uuid.NewString(), Source: "monitor", Kind: "v1_group", AccountID: f.outsider, GroupID: f.group, Model: "quiet-model", CostUSD: &amount, Status: "success", CreatedAt: now.Add(-time.Minute)},
		{ID: uuid.NewString(), Source: "monitor", Kind: "v1_endpoint", GroupID: f.group, Model: "quiet-model", Incomplete: true, Status: "failed", CreatedAt: now.Add(-time.Minute)},
	}
	t.Cleanup(func() {
		for _, r := range records {
			_, _ = integrationDB.Exec(`DELETE FROM internal_request_costs WHERE id=$1`, r.ID)
		}
	})
	require.NoError(t, writeInternalRequestCosts(context.Background(), integrationDB, records))
	_, err := integrationDB.Exec(`UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, f.outsider)
	require.NoError(t, err)
	q := service.UpstreamOperationsFilter{From: now.Add(-time.Hour), Until: now, GroupID: f.group, Source: "internal", ModelScope: "recorded", Dimension: "account", Sort: "requests", Direction: "desc", Page: 1, PageSize: 20}
	out, err := f.repo.ReadProfitOperations(context.Background(), q)
	require.NoError(t, err)
	var data struct {
		Summary struct {
			Requests int      `json:"requests"`
			Internal int      `json:"internal_requests"`
			Cost     *float64 `json:"estimated_cost"`
		}
		Rows []struct{ Key string }
	}
	require.NoError(t, json.Unmarshal(out.Usage, &data))
	require.Zero(t, data.Summary.Requests)
	require.Equal(t, 2, data.Summary.Internal)
	require.Nil(t, data.Summary.Cost)
	require.Len(t, data.Rows, 2)
	require.Contains(t, string(out.Usage), "Unattributed internal requests")
	q.Upstream = "unknown:unattributed_internal"
	require.NoError(t, service.ValidateProfitOperationsFilter(q))
	filtered, err := f.repo.ReadProfitOperations(context.Background(), q)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(filtered.Usage, &data))
	require.Equal(t, 1, data.Summary.Internal)
	require.Equal(t, "0", data.Rows[0].Key)
	q.Upstream = ""
	q.AccountID = f.outsider
	out, err = f.repo.ReadProfitOperations(context.Background(), q)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(out.Usage, &data))
	require.Equal(t, 1, data.Summary.Internal)
	require.InDelta(t, 2, *data.Summary.Cost, 1e-9)
	require.NotContains(t, string(out.Usage), "account_missing", "a soft-deleted internal-only account must remain attributable")
}

func TestInternalCostLegacyMigrationRetainsAmountsAndSurvivesCleanup(t *testing.T) {
	f := newChannelOperationsFixture(t)
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var plan, id int64
	require.NoError(t, tx.QueryRow(`INSERT INTO pelican_group_test_plans(group_id,model_id,pelican_config) VALUES($1,'legacy-model','{}') RETURNING id`, f.group).Scan(&plan))
	require.NoError(t, tx.QueryRow(`INSERT INTO pelican_group_test_results(plan_id,account_id,attempts,status,started_at,finished_at,cost_usd,cost_incomplete) VALUES($1,$2,'[{"account_id":999}]','failed',NOW(),NOW(),3.5,false) RETURNING id`, plan, f.account).Scan(&id))
	var monitor, probe, quota int64
	require.NoError(t, tx.QueryRow(`INSERT INTO channel_monitors(name,provider,endpoint,api_key_encrypted,primary_model,interval_seconds,created_by,check_mode) VALUES('legacy mode switch','openai','https://example.invalid','mock','model',60,$1,'quota') RETURNING id`, f.user).Scan(&monitor))
	require.NoError(t, tx.QueryRow(`INSERT INTO channel_monitor_histories(monitor_id,model,status,latency_ms) VALUES($1,'model','operational',100) RETURNING id`, monitor).Scan(&probe))
	require.NoError(t, tx.QueryRow(`INSERT INTO channel_monitor_histories(monitor_id,model,status) VALUES($1,'model','operational') RETURNING id`, monitor).Scan(&quota))
	migration, err := migrations.FS.ReadFile("339_internal_request_costs.sql")
	require.NoError(t, err)
	// Re-execution imports retained evidence once and never revalues its saved amount.
	require.NoError(t, execMigrationStatements(ctx, tx, string(migration)))
	require.NoError(t, execMigrationStatements(ctx, tx, string(migration)))
	var count, account int64
	var cost float64
	var rate any
	require.NoError(t, tx.QueryRow(`SELECT count(*),max(account_id),max(cost_usd),max(cost_multiplier) FROM internal_request_costs WHERE id=$1`, "legacy:pelican:"+fmt.Sprint(id)).Scan(&count, &account, &cost, &rate))
	require.EqualValues(t, 1, count)
	require.Zero(t, account, "combined retry cost must not be charged entirely to the final account")
	require.Equal(t, 3.5, cost)
	require.Nil(t, rate)
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM internal_request_costs WHERE id=$1`, "legacy:v1:"+fmt.Sprint(probe)).Scan(&count))
	require.EqualValues(t, 1, count, "mode changes must not erase earlier probe evidence")
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM internal_request_costs WHERE id=$1`, "legacy:v1:"+fmt.Sprint(quota)).Scan(&count))
	require.Zero(t, count, "quota reads have no model expense")
	_, err = tx.Exec(`DELETE FROM pelican_group_test_plans WHERE id=$1`, plan)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(`SELECT count(*) FROM internal_request_costs WHERE id=$1`, "legacy:pelican:"+fmt.Sprint(id)).Scan(&count))
	require.EqualValues(t, 1, count)
}

// lib/pq accepts the migration's multi-statement SQL directly. Kept separate
// from ApplyMigrations so the regression runs inside a rollback-only fixture.
func execMigrationStatements(ctx context.Context, tx *sql.Tx, raw string) error {
	_, err := tx.ExecContext(ctx, strings.TrimSpace(raw))
	return err
}
