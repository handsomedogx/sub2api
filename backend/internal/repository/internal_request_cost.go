package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func writeInternalRequestCosts(ctx context.Context, db *sql.DB, records []service.InternalRequestCost) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range records {
		tokens, err := json.Marshal(r.Tokens)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO internal_request_costs
 (id,source,kind,account_id,group_id,monitor_id,requested_model,model,tokens,standard_cost,cost_multiplier,cost_usd,incomplete,status,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15) ON CONFLICT(id) DO NOTHING`,
			r.ID, r.Source, r.Kind, r.AccountID, r.GroupID, r.MonitorID, r.RequestedModel, r.Model, string(tokens), r.StandardCost, r.CostMultiplier, r.CostUSD, r.Incomplete, r.Status, r.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *pelicanGroupTestRepository) WriteInternalRequestCosts(ctx context.Context, rows []service.InternalRequestCost) error {
	return writeInternalRequestCosts(ctx, r.db, rows)
}
func (r *channelMonitorRepository) WriteInternalRequestCosts(ctx context.Context, rows []service.InternalRequestCost) error {
	return writeInternalRequestCosts(ctx, r.db, rows)
}

var _ service.InternalRequestCostWriter = (*pelicanGroupTestRepository)(nil)
var _ service.InternalRequestCostWriter = (*channelMonitorRepository)(nil)
