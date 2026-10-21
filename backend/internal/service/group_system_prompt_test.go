package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeGroupSystemPromptPolicy(t *testing.T) {
	mode, prompt, err := NormalizeGroupSystemPromptPolicy("", "saved")
	require.NoError(t, err)
	require.Equal(t, GroupSystemPromptModePassthrough, mode)
	require.Equal(t, "saved", prompt)

	_, _, err = NormalizeGroupSystemPromptPolicy("inject", "   ")
	require.Error(t, err)
	_, _, err = NormalizeGroupSystemPromptPolicy("invalid", "x")
	require.Error(t, err)
	_, _, err = NormalizeGroupSystemPromptPolicy("replace", strings.Repeat("x", GroupSystemPromptMaxBytes+1))
	require.Error(t, err)
}

func TestApplyGroupSystemPromptPolicy_Anthropic(t *testing.T) {
	t.Run("inject string", func(t *testing.T) {
		body := []byte(`{"model":"claude","system":"client","messages":[{"role":"user","content":"hi"}]}`)
		out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, GroupSystemPromptProtocolAnthropic)
		require.NoError(t, err)
		require.Equal(t, "gateway\n\nclient", gjson.GetBytes(out, "system").String())
	})

	t.Run("inject blocks preserves cache control", func(t *testing.T) {
		body := []byte(`{"system":[{"type":"text","text":"client","cache_control":{"type":"ephemeral"}}],"messages":[]}`)
		out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, GroupSystemPromptProtocolAnthropic)
		require.NoError(t, err)
		require.Equal(t, "gateway", gjson.GetBytes(out, "system.0.text").String())
		require.Equal(t, "client", gjson.GetBytes(out, "system.1.text").String())
		require.Equal(t, "ephemeral", gjson.GetBytes(out, "system.1.cache_control.type").String())
	})

	t.Run("replace", func(t *testing.T) {
		out, err := ApplyGroupSystemPromptPolicy([]byte(`{"system":[{"type":"text","text":"client"}]}`), &Group{SystemPromptMode: "replace", SystemPrompt: "gateway"}, GroupSystemPromptProtocolAnthropic)
		require.NoError(t, err)
		require.Len(t, gjson.GetBytes(out, "system").Array(), 1)
		require.Equal(t, "gateway", gjson.GetBytes(out, "system.0.text").String())
	})

	t.Run("claude code billing attribution stays first", func(t *testing.T) {
		billing := "x-anthropic-billing-header: cc_version=2.1.161.abc; cc_entrypoint=cli;"
		body := []byte(`{"system":[{"type":"text","text":"` + billing + `"},{"type":"text","text":"client instructions"}]}`)

		injected, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, GroupSystemPromptProtocolAnthropic)
		require.NoError(t, err)
		require.Equal(t, billing, gjson.GetBytes(injected, "system.0.text").String())
		require.Equal(t, "gateway", gjson.GetBytes(injected, "system.1.text").String())
		require.Equal(t, "client instructions", gjson.GetBytes(injected, "system.2.text").String())

		replaced, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "replace", SystemPrompt: "gateway"}, GroupSystemPromptProtocolAnthropic)
		require.NoError(t, err)
		require.Len(t, gjson.GetBytes(replaced, "system").Array(), 2)
		require.Equal(t, billing, gjson.GetBytes(replaced, "system.0.text").String())
		require.Equal(t, "gateway", gjson.GetBytes(replaced, "system.1.text").String())
	})
}

func TestApplyGroupSystemPromptPolicy_ChatCompletions(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"old"},{"role":"developer","content":"dev"},{"role":"user","content":"hi"}]}`)
	out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "replace", SystemPrompt: "gateway"}, GroupSystemPromptProtocolChatCompletions)
	require.NoError(t, err)
	require.Equal(t, "system", gjson.GetBytes(out, "messages.0.role").String())
	require.Equal(t, "gateway", gjson.GetBytes(out, "messages.0.content").String())
	require.Equal(t, "user", gjson.GetBytes(out, "messages.1.role").String())
	require.Len(t, gjson.GetBytes(out, "messages").Array(), 2)
}

func TestApplyGroupSystemPromptPolicy_Responses(t *testing.T) {
	t.Run("inject instructions", func(t *testing.T) {
		out, err := ApplyGroupSystemPromptPolicy([]byte(`{"instructions":"client","input":"hello"}`), &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, GroupSystemPromptProtocolResponses)
		require.NoError(t, err)
		require.Equal(t, "gateway\n\nclient", gjson.GetBytes(out, "instructions").String())
		require.Equal(t, "hello", gjson.GetBytes(out, "input").String())
	})

	t.Run("replace removes instruction roles from array input", func(t *testing.T) {
		body := []byte(`{"instructions":"client","input":[{"role":"developer","content":"dev"},{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`)
		out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "replace", SystemPrompt: "gateway"}, GroupSystemPromptProtocolResponses)
		require.NoError(t, err)
		require.Equal(t, "gateway", gjson.GetBytes(out, "instructions").String())
		require.Len(t, gjson.GetBytes(out, "input").Array(), 1)
		require.Equal(t, "user", gjson.GetBytes(out, "input.0.role").String())
	})
}

func TestApplyGroupSystemPromptPolicy_Gemini(t *testing.T) {
	body := []byte(`{"systemInstruction":{"role":"user","parts":[{"text":"client"}]},"contents":[]}`)
	out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, GroupSystemPromptProtocolGemini)
	require.NoError(t, err)
	require.Equal(t, "user", gjson.GetBytes(out, "systemInstruction.role").String())
	require.Equal(t, "gateway", gjson.GetBytes(out, "systemInstruction.parts.0.text").String())
	require.Equal(t, "client", gjson.GetBytes(out, "systemInstruction.parts.1.text").String())
}

func TestApplyGroupSystemPromptPolicy_PassthroughKeepsBytes(t *testing.T) {
	body := []byte("{ \"model\" : \"x\", \"system\" : \"client\" }")
	out, err := ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: "passthrough", SystemPrompt: "saved"}, GroupSystemPromptProtocolAnthropic)
	require.NoError(t, err)
	require.Equal(t, body, out)
}

func TestGroupSystemPromptAuthCacheRoundtrip(t *testing.T) {
	key := &APIKey{
		ID: 1, Key: "sk-prompt-cache", User: &User{ID: 2, Status: StatusActive},
		Group: &Group{ID: 3, Hydrated: true, SystemPromptMode: "replace", SystemPrompt: "网关指令\nKeep whitespace.  "},
	}
	svc := &APIKeyService{}
	encoded, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(encoded, &cached))
	materialized, used, err := svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.Equal(t, key.Group.SystemPromptMode, materialized.Group.SystemPromptMode)
	require.Equal(t, key.Group.SystemPrompt, materialized.Group.SystemPrompt)

	cached.Snapshot.Version = 30 // Before group prompt fields were cached.
	_, used, err = svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.False(t, used)
}

func TestGroupSystemPromptSimpleModeAndDuplicate(t *testing.T) {
	create := &CreateGroupInput{SystemPromptMode: "inject", SystemPrompt: "gateway"}
	normalizeCreateGroupInputForSimpleMode(create)
	require.Equal(t, "inject", create.SystemPromptMode)
	require.Equal(t, "gateway", create.SystemPrompt)
	update := &UpdateGroupInput{SystemPromptMode: &create.SystemPromptMode, SystemPrompt: &create.SystemPrompt}
	normalizeUpdateGroupInputForSimpleMode(update)
	require.Equal(t, "inject", *update.SystemPromptMode)
	require.Equal(t, "gateway", *update.SystemPrompt)
	copy := cloneGroupForDuplicate(&Group{SystemPromptMode: "replace", SystemPrompt: "copied"}, "operation")
	require.Equal(t, "replace", copy.SystemPromptMode)
	require.Equal(t, "copied", copy.SystemPrompt)
}

func TestApplyGroupSystemPromptPolicy_InvalidBodies(t *testing.T) {
	for _, protocol := range []GroupSystemPromptProtocol{
		GroupSystemPromptProtocolAnthropic, GroupSystemPromptProtocolChatCompletions,
		GroupSystemPromptProtocolResponses, GroupSystemPromptProtocolGemini,
	} {
		for _, body := range []string{`null`, `[]`, `{"broken"}`} {
			_, err := ApplyGroupSystemPromptPolicy([]byte(body), &Group{SystemPromptMode: "inject", SystemPrompt: "gateway"}, protocol)
			require.Error(t, err, "%s: %s", protocol, body)
		}
	}
}
