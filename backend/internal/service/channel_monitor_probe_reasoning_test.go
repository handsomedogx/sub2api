//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDeepseekMonitorProbeDisablesThinkingBeforeSmallBudget(t *testing.T) {
	swapMonitorHTTPClient(t)
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode probe request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer := answerFromOpenAIRequest(request)
		thinking, _ := request["thinking"].(map[string]any)
		content, finish := "", "length"
		if thinking["type"] == "disabled" {
			content, finish = answer, "stop"
		}
		// Reasoning can contain the expected number even when the final answer is absent.
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
			"finish_reason": finish,
			"message":       map[string]any{"content": content, "reasoning_content": "Considering answer " + answer},
		}}})
	}))
	defer server.Close()
	result := runCheckForModel(context.Background(), MonitorProviderDeepseek, server.URL, "test-key", "deepseek-v4.1-flash", nil)
	require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
	require.Equal(t, float64(monitorChallengeMaxTokens), request["max_tokens"], "the probe stays bounded")
	require.Equal(t, false, request["stream"])
}

func TestMonitorProbeStillRequiresTheFinalArithmeticAnswer(t *testing.T) {
	for _, finalAnswer := range []string{"", "999999"} {
		t.Run("final answer="+finalAnswer, func(t *testing.T) {
			swapMonitorHTTPClient(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode probe request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
					"finish_reason": "stop",
					"message": map[string]any{
						"content": finalAnswer, "reasoning_content": "PRIVATE_THINKING " + answerFromOpenAIRequest(request),
					},
				}}})
			}))
			defer server.Close()
			result := runCheckForModel(context.Background(), MonitorProviderDeepseek, server.URL, "test-key", "deepseek-v4.1-flash", nil)
			require.Equal(t, MonitorStatusFailed, result.Status)
			require.NotContains(t, result.Message, "PRIVATE_THINKING")
			if finalAnswer == "" {
				require.Contains(t, result.Message, "reasoning without a final answer")
			} else {
				require.Contains(t, result.Message, "challenge mismatch")
			}
		})
	}
}

func TestDeepseekMonitorProbeKeepsExplicitThinkingAndBudgetOverrides(t *testing.T) {
	adapter, mode, ok := providerAdapterFor(MonitorProviderDeepseek, "")
	require.True(t, ok)
	for _, overrideMode := range []string{MonitorBodyOverrideModeMerge, MonitorBodyOverrideModeReplace} {
		t.Run(overrideMode, func(t *testing.T) {
			override := map[string]any{
				"thinking": map[string]any{"type": "enabled"}, "max_tokens": 2048,
			}
			if overrideMode == MonitorBodyOverrideModeReplace {
				override["messages"] = []map[string]any{{"role": "user", "content": "custom question"}}
				override["model"] = "custom-model"
			}
			body, err := buildRequestBody(adapter, MonitorProviderDeepseek, mode, "deepseek-v4.1-flash", "question", &CheckOptions{BodyOverrideMode: overrideMode, BodyOverride: override})
			require.NoError(t, err)
			require.Equal(t, "enabled", gjson.GetBytes(body, "thinking.type").String())
			require.EqualValues(t, 2048, gjson.GetBytes(body, "max_tokens").Int())
		})
	}
	chat, err := providerOpenAIChatAdapter.buildBody("gpt-test", "question")
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(chat, "thinking").Exists(), "DeepSeek defaults stay provider-specific")
}

func TestMonitorProbeEmptyFinalAnswerDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"chat token limit", `{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"PRIVATE_THINKING 34"}}]}`, "output token limit reached"},
		{"responses token limit", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning"}]}`, "output token limit reached"},
		{"anthropic token limit", `{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"PRIVATE_THINKING"}]}`, "output token limit reached"},
		{"gemini token limit", `{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, "output token limit reached"},
		{"reasoning only", `{"choices":[{"finish_reason":"stop","message":{"content":null,"reasoning_content":"PRIVATE_THINKING 34"}}]}`, "reasoning without a final answer"},
		{"empty response", `{"choices":[{"message":{"content":"   "}}]}`, "without final answer text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &openAICaptureHandler{rawResponse: tc.body}
			endpoint := setupFakeOpenAI(t, handler)
			result := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "test-key", "test-model", nil)
			require.Equal(t, MonitorStatusFailed, result.Status)
			require.Contains(t, result.Message, tc.want)
			require.NotContains(t, result.Message, "PRIVATE_THINKING")
			require.NotContains(t, result.Message, "challenge mismatch")
		})
	}
}
