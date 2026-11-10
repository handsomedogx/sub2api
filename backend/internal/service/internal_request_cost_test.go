//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type internalCostTestWriter struct {
	rows      []InternalRequestCost
	calls     int
	failFirst bool
}

func (w *internalCostTestWriter) WriteInternalRequestCosts(ctx context.Context, rows []InternalRequestCost) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.calls++
	if w.failFirst && w.calls == 1 {
		return errors.New("transient")
	}
	w.rows = append(w.rows, rows...)
	return nil
}

func TestInternalCostSnapshotsAndUnknownUsage(t *testing.T) {
	billing := newTestBillingService()
	account := &Account{ID: 42, Extra: map[string]any{AccountCostMultiplierExtraKey: 0.075}}
	u := &pelicanTestUsage{protocol: "chat", model: "claude-sonnet-4"}
	u.read(`{"model":"claude-sonnet-4","usage":{"prompt_tokens":1000,"completion_tokens":200},"choices":[{"finish_reason":"stop"}]}`)
	collector := &pelicanTestUsageCollector{model: "claude-sonnet-4", requests: []*pelicanTestUsage{u}}
	rows := collector.costRecords(billing, account, 0.075, "public-model", 7, internalRequestOrigin{"monitor", "v3_idle", 0}, time.Now(), "failed")
	require.Len(t, rows, 1)
	r := rows[0]
	require.False(t, r.Incomplete)
	require.NotNil(t, r.StandardCost)
	require.NotNil(t, r.CostUSD)
	require.InDelta(t, *r.StandardCost*0.075, *r.CostUSD, 1e-12)
	require.Equal(t, "monitor", r.Source)
	require.Equal(t, "failed", r.Status)
	require.Equal(t, 7, int(r.GroupID))
	account.Extra[AccountCostMultiplierExtraKey] = 9.0
	require.Equal(t, 0.075, *r.CostMultiplier, "later edits do not revalue stored snapshots")
	u.complete = false
	partial := collector.costRecords(billing, account, 0.075, "public-model", 7, internalRequestOrigin{"pelican", "group_test", 0}, time.Now(), "failed")[0]
	require.True(t, partial.Incomplete)
	require.NotNil(t, partial.CostUSD, "retain known partial expense")
	empty := (&pelicanTestUsageCollector{model: "gpt-4o"}).costRecords(billing, account, 0.075, "gpt-4o", 7, internalRequestOrigin{}, time.Now(), "failed")
	require.Len(t, empty, 1)
	require.Nil(t, empty[0].CostUSD)
	require.True(t, empty[0].Incomplete)
	unlinked := collector.costRecords(billing, nil, 0, "gpt-4o", 0, internalRequestOrigin{"monitor", "v1_endpoint", 3}, time.Now(), "success")[0]
	require.NotNil(t, unlinked.StandardCost)
	require.Nil(t, unlinked.CostUSD)
	require.Nil(t, unlinked.CostMultiplier)
}

func TestInternalCostPersistsAfterCancellationAndRetriesIdempotently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &internalCostTestWriter{failFirst: true}
	rows := []InternalRequestCost{{Source: "pelican"}}
	require.NoError(t, persistInternalRequestCosts(ctx, writer, rows))
	require.Equal(t, 2, writer.calls)
	require.Len(t, writer.rows, 1)
	require.NotEmpty(t, writer.rows[0].ID)
	require.Equal(t, rows[0].ID, writer.rows[0].ID)
}

type internalGroupCostRepo struct {
	PelicanGroupTestRepository
	internalCostTestWriter
}

func TestInternalCostGroupRetriesUseEachAccountAndOrigin(t *testing.T) {
	a, b := account(11, "first"), account(12, "second")
	a.Extra = map[string]any{AccountCostMultiplierExtraKey: 0.075}
	b.Extra = map[string]any{AccountCostMultiplierExtraKey: 0.15}
	writer := &internalGroupCostRepo{PelicanGroupTestRepository: newGroupTestRepoFake()}
	svc, _ := newGroupTestService(nil, &groupTestRouterFake{steps: []routeStep{{account: a}, {account: b}}}, func(ctx context.Context, id int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		u := startPelicanTestUsage(ctx, "anthropic")
		u.read(`{"type":"message_start","message":{"model":"claude-sonnet-4","usage":{"input_tokens":1000,"output_tokens":500}}}`)
		u.read(`{"type":"message_stop"}`)
		if id == 11 {
			a.Extra[AccountCostMultiplierExtraKey] = 99.0 // A concurrent probe must not change this attempt's cost.
			return &ScheduledTestResult{Status: "failed", ErrorMessage: "The service is temporarily unavailable. Please retry later."}, nil
		}
		return &ScheduledTestResult{Status: "success", ResponseText: "<svg/>"}, nil
	})
	svc.repo = writer
	svc.billing = newTestBillingService()
	result := svc.runSample(withMonitorCostOrigin(context.Background(), "v1_group", 123), groupTestPlan(1), &Group{ID: 4})
	require.Equal(t, "success", result.Status)
	require.Len(t, writer.rows, 2)
	require.Equal(t, int64(11), writer.rows[0].AccountID)
	require.Equal(t, int64(12), writer.rows[1].AccountID)
	require.Equal(t, "failed", writer.rows[0].Status)
	require.Equal(t, "success", writer.rows[1].Status)
	for _, r := range writer.rows {
		require.Equal(t, "monitor", r.Source)
		require.Equal(t, "v1_group", r.Kind)
		require.Equal(t, int64(123), r.MonitorID)
	}
	require.InDelta(t, 0.0105*(.075+.15), *result.CostUSD, 1e-12)
	require.InDelta(t, *writer.rows[0].CostUSD+*writer.rows[1].CostUSD, *result.CostUSD, 1e-12)
}

func TestInternalCostManualBackgroundAndGroupNeverDoubleCount(t *testing.T) {
	for _, mode := range []string{"manual", "background", "group"} {
		t.Run(mode, func(t *testing.T) {
			a := &Account{ID: 11, Name: "test", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
				Credentials: map[string]any{"api_key": "mock-key", "base_url": "http://chat.example/v1", "model_mapping": map[string]any{"alias": "claude-sonnet-4"}},
				Extra:       map[string]any{AccountCostMultiplierExtraKey: 0.075, openai_compat.ExtraKeyResponsesSupported: false}}
			body := "data: {\"model\":\"claude-sonnet-4\",\"choices\":[{\"delta\":{\"content\":\"<svg></svg>\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":500}}\n\ndata: [DONE]\n\n"
			svc, _ := adaptiveCNAccountTestService(a, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))})
			repo := &internalGroupCostRepo{PelicanGroupTestRepository: newGroupTestRepoFake()}
			svc.internalCostWriter = repo
			svc.internalCostBilling = newTestBillingService()
			switch mode {
			case "manual":
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				require.NoError(t, svc.TestPelicanAccountConnection(c, 11, "alias", "draw", "medium"))
			case "background":
				result, err := svc.RunPelicanBackground(context.Background(), 11, "alias", &PelicanTestConfig{QuestionKind: "pelican", Prompt: "draw"})
				require.NoError(t, err)
				require.Equal(t, "success", result.Status)
			case "group":
				groups, _ := newGroupTestService(nil, &groupTestRouterFake{steps: []routeStep{{account: a}}}, svc.RunPelicanBackground)
				groups.repo = repo
				groups.billing = svc.internalCostBilling
				plan := groupTestPlan(1)
				plan.ModelID = "alias"
				result := groups.runSample(context.Background(), plan, &Group{ID: 4})
				require.Equal(t, "success", result.Status)
			}
			require.Len(t, repo.rows, 1)
			require.InDelta(t, .0105*.075, *repo.rows[0].CostUSD, 1e-12)
			require.False(t, repo.rows[0].Incomplete)
		})
	}
}

func TestInternalCostV1EndpointCollectsReportedUsage(t *testing.T) {
	swapMonitorHTTPClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"claude-sonnet-4","choices":[{"message":{"content":"42"}}],"usage":{"prompt_tokens":1000,"completion_tokens":10}}`)
	}))
	defer server.Close()
	collector := &pelicanTestUsageCollector{model: "claude-sonnet-4"}
	ctx := context.WithValue(context.Background(), pelicanTestUsageKey{}, collector)
	_, _, status, err := callProvider(ctx, MonitorProviderOpenAI, server.URL, "mock-key", "claude-sonnet-4", "answer", nil)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Len(t, collector.requests, 1)
	rows := collector.costRecords(newTestBillingService(), nil, 0, "claude-sonnet-4", 0, internalRequestOrigin{"monitor", "v1_endpoint", 1}, time.Now(), "success")
	require.NotNil(t, rows[0].StandardCost)
	require.Nil(t, rows[0].CostUSD)
	require.Equal(t, 1000, rows[0].Tokens.InputTokens)
}
