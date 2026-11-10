//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorGroupProbeUsesSchedulerAndPersistsVerdict(t *testing.T) {
	for _, correct := range []bool{true, false} {
		t.Run(fmt.Sprint(correct), func(t *testing.T) {
			repo := &quotaModeRepoStub{monitor: &ChannelMonitor{ID: 1, GroupID: int64Ptr(4), PrimaryModel: "gpt-6-astra", Enabled: true, APIKey: "broken-external-ciphertext"}}
			router := &groupTestRouterFake{steps: []routeStep{{account: account(1, "selected")}}}
			groups, _ := newGroupTestService(nil, router, func(_ context.Context, id int64, model string, cfg *PelicanTestConfig) (*ScheduledTestResult, error) {
				require.Equal(t, int64(1), id)
				require.Equal(t, "gpt-6-astra", model)
				require.Equal(t, ChannelMonitorV3ProbeQuestionKind, cfg.QuestionKind)
				// Answer the actual generated arithmetic challenge, not a fixture response.
				parts := regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?\nA:\z`).FindStringSubmatch(cfg.Prompt)
				require.Len(t, parts, 4)
				a, _ := strconv.Atoi(parts[1])
				b, _ := strconv.Atoi(parts[3])
				value := a + b
				if parts[2] == "-" {
					value = a - b
				}
				answer := strconv.Itoa(value)
				if !correct {
					answer = "wrong"
				}
				return &ScheduledTestResult{Status: "success", ResponseText: answer}, nil
			})
			svc := newQuotaModeService(repo)
			svc.groups = groups
			results, err := svc.RunCheck(context.Background(), 1)
			require.NoError(t, err)
			require.Len(t, results, 1)
			if correct {
				require.Equal(t, MonitorStatusOperational, results[0].Status)
			} else {
				require.Equal(t, MonitorStatusFailed, results[0].Status)
			}
			require.Equal(t, []int64{1}, router.released)
			require.Len(t, repo.history, 1)
			require.Equal(t, results[0].Status, repo.history[0].Status)
		})
	}
}

func TestChannelMonitorGroupProbeFailsClosedWhenGroupDisappears(t *testing.T) {
	repo := &quotaModeRepoStub{monitor: &ChannelMonitor{ID: 1, GroupID: int64Ptr(404), PrimaryModel: "gpt", Endpoint: "https://unused.example", APIKey: "unused"}}
	svc := newQuotaModeService(repo)
	svc.groups = &PelicanGroupTestService{groups: groupTestGroupsFake{}}
	results, err := svc.RunCheck(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, MonitorStatusError, results[0].Status)
	require.Equal(t, "group is unavailable", results[0].Message)
}

func TestChannelMonitorGroupModelsRespectAccountAndGroupLists(t *testing.T) {
	group := &Group{ID: 4, Status: StatusActive, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-a"}}}
	repo := &groupModelAccountRepo{accounts: []Account{
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"public-a": "upstream-a", "hidden-b": "upstream-b"}}},
		{ID: 2, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"wrong-platform": "upstream"}}},
	}}
	svc := &PelicanGroupTestService{groups: groupTestGroupsFake{4: group}, router: &gatewayPelicanGroupRouter{gateway: &GatewayService{accountRepo: repo}}}
	models, err := svc.ListTestModels(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, []string{"public-a"}, models)
	_, err = svc.ListTestModels(context.Background(), 404)
	require.Error(t, err)
	repo.err = errors.New("catalog storage is unavailable")
	_, err = svc.ListTestModels(context.Background(), 4)
	require.EqualError(t, err, "catalog storage is unavailable")
}

func TestChannelMonitorGroupCreateNeedsNoExternalCredentials(t *testing.T) {
	accounts := &groupModelAccountRepo{accounts: []Account{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"public-a": "upstream-a"}}}}}
	groups := &PelicanGroupTestService{groups: groupTestGroupsFake{4: {ID: 4, Name: "Actual group", Status: StatusActive, Platform: PlatformOpenAI}}, router: &gatewayPelicanGroupRouter{gateway: &GatewayService{accountRepo: accounts}}}
	repo := &duplicateChannelMonitorRepoStub{}
	svc := NewChannelMonitorService(repo, &duplicateChannelMonitorEncryptor{})
	svc.groups = groups
	params := ChannelMonitorCreateParams{GroupID: int64Ptr(4), Name: "Monitor", PrimaryModel: "public-a", IntervalSeconds: 60, Enabled: true}
	created, err := svc.Create(context.Background(), params)
	require.NoError(t, err)
	require.Equal(t, int64(4), *created.GroupID)
	require.Empty(t, created.Endpoint)
	require.Empty(t, created.APIKey)
	require.Equal(t, "Actual group", created.GroupName)
	require.Equal(t, PlatformOpenAI, created.Provider)
	params.PrimaryModel = "other-group-model"
	_, err = svc.Create(context.Background(), params)
	require.Error(t, err)
	require.Len(t, repo.created, 1)
}

type monitorCacheRateRepo struct {
	ChannelMonitorRepository
	fail bool
}

func (r *monitorCacheRateRepo) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return nil, nil
}
func (r *monitorCacheRateRepo) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*ChannelMonitorAvailability, error) {
	return nil, nil
}
func (r *monitorCacheRateRepo) MonitorUsageCacheRates(context.Context, []int64, time.Time) (map[int64]float64, error) {
	if r.fail {
		return nil, errors.New("usage query failed")
	}
	return map[int64]float64{1: 0, 2: 0.8}, nil
}
func TestChannelMonitorUsageCacheRateKeepsUnknownDistinctFromZero(t *testing.T) {
	repo := &monitorCacheRateRepo{}
	svc := NewChannelMonitorService(repo, nil)
	result := svc.BatchMonitorStatusSummary(context.Background(), []int64{1, 2, 3}, nil, nil)
	require.NotNil(t, result[1].CacheRate24h)
	require.Zero(t, *result[1].CacheRate24h)
	require.InDelta(t, 0.8, *result[2].CacheRate24h, 0.0001)
	require.Nil(t, result[3].CacheRate24h)
	repo.fail = true
	result = svc.BatchMonitorStatusSummary(context.Background(), []int64{1}, nil, nil)
	require.Nil(t, result[1].CacheRate24h)
}

type groupModelAccountRepo struct {
	AccountRepository
	accounts []Account
	err      error
}

func (r *groupModelAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return r.accounts, r.err
}
