package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// ListTestModels reads local routing data only; it never probes a supplier.
// Both V1 monitors and Pelican use this catalog rather than pricing candidates.
func (s *PelicanGroupTestService) ListTestModels(ctx context.Context, groupID int64) ([]string, error) {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || group.Status != StatusActive {
		return nil, ErrGroupNotFound
	}
	router, ok := s.router.(*gatewayPelicanGroupRouter)
	if !ok || router.gateway == nil {
		return nil, fmt.Errorf("group model catalog is unavailable")
	}
	accounts, err := router.gateway.accountRepo.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for i := range accounts {
		a := &accounts[i]
		if !a.IsSchedulable() || (a.Platform != group.Platform && group.Platform != PlatformComposite && !mixedListingAccountAllowed(group.Platform, a)) {
			continue
		}
		mapping := a.GetModelMapping()
		models := make([]string, 0, len(mapping))
		for model := range mapping {
			models = append(models, model)
		}
		if len(mapping) == 0 {
			models = DefaultModelIDsForPlatform(a.Platform)
		}
		models = append(models, groupAllowedConcreteModels(a, &groupID)...)
		for _, model := range models {
			if strings.TrimSpace(model) == "" || strings.Contains(model, "*") || !group.ModelAllowlist.Allows(model) || !a.IsModelSupportedInGroup(&groupID, model) {
				continue
			}
			if a.Platform != group.Platform && group.Platform != PlatformComposite && !mixedListingModelAllowed(group.Platform, model) {
				continue
			}
			set[model] = true
		}
	}
	if group.Platform == PlatformComposite {
		aliases, err := router.gateway.GetCompositeRouteModels(ctx, &groupID, CompositeRouteEndpointAny)
		if err != nil {
			return nil, err
		}
		for _, model := range aliases {
			if group.ModelAllowlist.Allows(model) {
				set[model] = true
			}
		}
	}
	models := make([]string, 0, len(set))
	for model := range set {
		models = append(models, model)
	}
	sort.Strings(models)
	return models, nil
}

func monitorGroupProvider(platform string) string {
	if platform == PlatformAnthropic {
		return MonitorProviderAnthropic
	}
	return platform
}

func (s *ChannelMonitorService) validateGroupMonitor(ctx context.Context, m *ChannelMonitor) error {
	if m.GroupID == nil {
		return nil
	}
	if *m.GroupID <= 0 || s.groups == nil || defaultCheckMode(m.CheckMode) != MonitorCheckModeProbe {
		return ErrChannelMonitorInvalidCheckMode
	}
	group, err := s.groups.groups.GetByID(ctx, *m.GroupID)
	if err != nil {
		if !m.Enabled {
			return nil
		}
		return err
	}
	if group == nil {
		if !m.Enabled {
			return nil
		}
		return ErrGroupNotFound
	}
	if m.Enabled && group.Status != StatusActive {
		return ErrGroupNotFound
	}
	m.Provider, m.GroupName = monitorGroupProvider(group.Platform), group.Name
	m.Endpoint, m.AccountID, m.TemplateID = "", nil, nil
	m.ExtraHeaders, m.BodyOverrideMode, m.BodyOverride = map[string]string{}, MonitorBodyOverrideModeOff, nil
	m.APIMode = MonitorAPIModeChatCompletions
	if !m.Enabled {
		return nil
	}
	models, err := s.groups.ListTestModels(ctx, *m.GroupID)
	if err != nil {
		return err
	}
	for _, model := range append([]string{m.PrimaryModel}, m.ExtraModels...) {
		if !slices.Contains(models, model) {
			return fmt.Errorf("%w: model is not available in the selected group", ErrChannelMonitorMissingPrimaryModel)
		}
	}
	return nil
}

func (s *ChannelMonitorService) runGroupChecks(ctx context.Context, m *ChannelMonitor) []*CheckResult {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	results := make([]*CheckResult, len(models))
	var wg sync.WaitGroup
	for i, model := range models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()
			res := &CheckResult{Model: model, Status: MonitorStatusError, CheckedAt: time.Now()}
			results[i] = res
			if s.groups == nil {
				res.Message = "group probe is unavailable"
				return
			}
			group, err := s.groups.groups.GetByID(ctx, *m.GroupID)
			if err != nil || group == nil || group.Status != StatusActive {
				res.Message = "group is unavailable"
				return
			}
			if !group.ModelAllowlist.Allows(model) {
				res.Message = "model is not allowed by the group"
				return
			}
			challenge := generateChallenge()
			runCtx, cancel := context.WithTimeout(ctx, channelMonitorV3ProbeTimeout)
			defer cancel()
			plan := &PelicanGroupTestPlan{GroupID: group.ID, ModelID: model, PelicanConfig: &PelicanTestConfig{
				QuestionKind: ChannelMonitorV3ProbeQuestionKind, Prompt: challenge.Prompt, ModelID: model, ParallelCount: 1,
			}}
			started := time.Now()
			sample := s.groups.runSample(withMonitorCostOrigin(runCtx, "v1_group", m.ID), plan, group)
			if runCtx.Err() != nil {
				res.Message = "group probe cancelled or timed out"
				return
			}
			ms := int(time.Since(started).Milliseconds())
			res.LatencyMs = &ms
			if sample == nil {
				res.Message = "group probe returned no result"
				return
			}
			if sample.Status != "success" {
				res.Message = truncateMessage(sanitizeErrorMessage(sample.ErrorMessage))
				return
			}
			if !validateChallenge(sample.ResponseText, challenge.Expected) {
				res.Status, res.Message = MonitorStatusFailed, "challenge mismatch"
				return
			}
			finalizeOperationalOrDegraded(res, time.Duration(ms)*time.Millisecond, ms)
		}(i, model)
	}
	wg.Wait()
	return results
}
