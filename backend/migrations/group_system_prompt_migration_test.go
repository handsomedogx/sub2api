package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupSystemPromptMigration(t *testing.T) {
	content, err := FS.ReadFile("295_group_system_prompt.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS system_prompt_mode VARCHAR(20) NOT NULL DEFAULT 'passthrough'")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS system_prompt TEXT NOT NULL DEFAULT ''")
	require.Contains(t, sql, "CHECK (system_prompt_mode IN ('passthrough', 'inject', 'replace'))")
}
