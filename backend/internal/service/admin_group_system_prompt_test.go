//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAdminService_GroupSystemPromptCreateAndUpdate(t *testing.T) {
	for _, simple := range []bool{false, true} {
		for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformComposite} {
			t.Run(platform+map[bool]string{false: "/standard", true: "/simple"}[simple], func(t *testing.T) {
				repo := &groupRepoStubForAdmin{}
				invalidator := &authCacheInvalidatorStub{}
				svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
				if simple {
					svc.cfg = &config.Config{RunMode: config.RunModeSimple}
					if platform == PlatformComposite {
						return // Simple mode does not support composite groups.
					}
				}
				created, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
					Name: "prompt", Platform: platform, RateMultiplier: 1, SystemPromptMode: "inject", SystemPrompt: "gateway",
				})
				require.NoError(t, err)
				require.Equal(t, "inject", repo.created.SystemPromptMode)
				require.Equal(t, "gateway", repo.created.SystemPrompt)
				repo.getByID = created
				repo.getByID.ID = 1
				mode, prompt := "replace", "updated"
				_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{SystemPromptMode: &mode, SystemPrompt: &prompt})
				require.NoError(t, err)
				require.Equal(t, mode, repo.updated.SystemPromptMode)
				require.Equal(t, prompt, repo.updated.SystemPrompt)
				require.Equal(t, []int64{1}, invalidator.groupIDs)
				_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Name: "renamed"})
				require.NoError(t, err)
				require.Equal(t, prompt, repo.updated.SystemPrompt)
			})
		}
	}
}
