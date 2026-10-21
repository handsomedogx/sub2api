package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupSystemPromptAdminVisibility(t *testing.T) {
	group := &service.Group{ID: 1, SystemPromptMode: "replace", SystemPrompt: "internal instructions"}
	adminJSON, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"system_prompt_mode":"replace"`)
	require.Contains(t, string(adminJSON), `"system_prompt":"internal instructions"`)
	userJSON, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "system_prompt")
	require.NotContains(t, string(userJSON), "internal instructions")
}
