package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
)

// InternalRequestCost is a cost snapshot, never a user debit or quota command.
// No foreign keys: deleting a test plan/account must not erase operating costs.
type InternalRequestCost struct {
	ID             string       `json:"id"`
	Source         string       `json:"source"`
	Kind           string       `json:"kind"`
	AccountID      int64        `json:"account_id"`
	GroupID        int64        `json:"group_id"`
	MonitorID      int64        `json:"monitor_id"`
	RequestedModel string       `json:"requested_model"`
	Model          string       `json:"model"`
	Tokens         *UsageTokens `json:"tokens"`
	StandardCost   *float64     `json:"standard_cost"`
	CostMultiplier *float64     `json:"cost_multiplier"`
	CostUSD        *float64     `json:"cost_usd"`
	Incomplete     bool         `json:"incomplete"`
	Status         string       `json:"status"`
	CreatedAt      time.Time    `json:"created_at"`
}

type InternalRequestCostWriter interface {
	WriteInternalRequestCosts(context.Context, []InternalRequestCost) error
}

type internalRequestOrigin struct {
	source, kind string
	monitorID    int64
}
type internalRequestOriginKey struct{}

func withMonitorCostOrigin(ctx context.Context, kind string, monitorID int64) context.Context {
	return context.WithValue(ctx, internalRequestOriginKey{}, internalRequestOrigin{"monitor", kind, monitorID})
}

func internalCostOrigin(ctx context.Context) internalRequestOrigin {
	if o, ok := ctx.Value(internalRequestOriginKey{}).(internalRequestOrigin); ok {
		return o
	}
	return internalRequestOrigin{"pelican", "group_test", 0}
}

func persistInternalRequestCosts(ctx context.Context, target any, records []InternalRequestCost) error {
	w, ok := target.(InternalRequestCostWriter)
	if !ok || len(records) == 0 {
		return nil
	} // Lightweight test repositories need not implement accounting.
	for i := range records {
		if records[i].ID == "" {
			records[i].ID = uuid.NewString()
		}
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = w.WriteInternalRequestCosts(writeCtx, records)
		if err == nil {
			return nil
		}
		if writeCtx.Err() != nil {
			break
		}
	}
	slog.Error("internal request cost persistence failed", "records", len(records), "error_type", fmt.Sprintf("%T", err))
	return err
}

func validInternalAmount(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func (s *AccountTestService) startAccountCostSnapshot(ctx context.Context, accountID int64, model string) (context.Context, func(string)) {
	if pelicanUsageFromContext(ctx) != nil || s.internalCostWriter == nil {
		return ctx, nil
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return ctx, nil
	}
	usage := &pelicanTestUsageCollector{model: account.GetMappedModel(model)}
	rate, started := account.CostMultiplier(), time.Now()
	ctx = context.WithValue(ctx, pelicanTestUsageKey{}, usage)
	return ctx, func(status string) {
		records := usage.costRecords(s.internalCostBilling, account, rate, model, 0, internalRequestOrigin{"pelican", "account_test", 0}, started, status)
		_ = persistInternalRequestCosts(ctx, s.internalCostWriter, records)
	}
}

func (c *pelicanTestUsageCollector) costRecords(billing *BillingService, account *Account, rate float64, requested string, groupID int64, origin internalRequestOrigin, started time.Time, status string) []InternalRequestCost {
	requests := c.requests
	if len(requests) == 0 {
		requests = []*pelicanTestUsage{{model: c.model}}
	}
	out := make([]InternalRequestCost, 0, len(requests))
	for _, u := range requests {
		r := InternalRequestCost{Source: origin.source, Kind: origin.kind, GroupID: groupID, MonitorID: origin.monitorID, RequestedModel: requested, Model: u.model, Incomplete: true, Status: status, CreatedAt: started}
		if u.seen {
			tokens := u.tokens
			r.Tokens = &tokens
		}
		if account != nil {
			r.AccountID = account.ID
			v := rate
			r.CostMultiplier = &v
		}
		if u.seen && billing != nil {
			if cost, err := billing.CalculateCostWithServiceTier(u.model, u.tokens, 1, u.serviceTier); err == nil && cost != nil {
				base := cost.TotalCost
				if validInternalAmount(base) {
					r.StandardCost = &base
					if r.CostMultiplier != nil {
						amount := base * rate
						if validInternalAmount(amount) {
							r.CostUSD = &amount
							r.Incomplete = !u.complete || !u.inputSeen || !u.outputSeen
						}
					}
				}
			}
		}
		out = append(out, r)
	}
	return out
}
