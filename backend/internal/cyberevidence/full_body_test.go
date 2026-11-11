package cyberevidence

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodePrepared(t *testing.T, p Prepared) Payload {
	t.Helper()
	require.Equal(t, "pending", p.Status)
	raw, err := (testCipher{}).Decrypt(p.Ciphertext)
	require.NoError(t, err)
	var payload Payload
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	return payload
}

func TestRetainedRequestSurvivesPreviewParseAndMessageLimits(t *testing.T) {
	store := &memoryStore{config: Config{Enabled: true, RetentionDays: 7, ParseLimitMiB: 1, BodyLimitMiB: 8}}
	s := New(store, testCipher{}, true)
	defer s.Close()
	messages := make([]any, 350)
	for i := range messages {
		messages[i] = map[string]any{"role": "user", "content": strings.Repeat("x", 4096)}
	}
	messages[0] = map[string]any{"role": "user", "content": "FIRST QUESTION"}
	messages[349] = map[string]any{"role": "user", "content": "LATEST QUESTION"}
	body, err := json.Marshal(map[string]any{"messages": messages, "temperature": 0.5})
	require.NoError(t, err)
	response := []byte(`{"error":{"code":"cyber_policy","message":"` + strings.Repeat("reason ", 2000) + `END OF REJECTION"}}`)
	p := s.PrepareRejection(body, "reason", "session_x", UpstreamRejection{Body: response, OriginalBytes: len(response), Status: 400})
	got := decodePrepared(t, p)
	require.Equal(t, "request_exceeds_parse_limit", got.Omission)
	require.NotContains(t, got.Input, "LATEST QUESTION")
	require.Empty(t, got.RequestOmission)
	require.Contains(t, got.RequestBody, "FIRST QUESTION")
	require.Contains(t, got.RequestBody, "LATEST QUESTION")
	require.Contains(t, got.RequestBody, "temperature")
	var stored struct {
		Messages []any `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.RequestBody), &stored))
	require.Len(t, stored.Messages, 350)
	require.Contains(t, got.UpstreamResponse, "END OF REJECTION")
	require.Greater(t, len(got.UpstreamResponse), 4096)
	require.Empty(t, got.ResponseOmission)
	require.Positive(t, s.retainedBytes.Load())
	p.Release()
	require.Zero(t, s.retainedBytes.Load())
}

func TestRetainedBodiesRedactWithoutTruncatingText(t *testing.T) {
	long := strings.Repeat("long conversation ", 3000) + "TAIL"
	body, _ := json.Marshal(map[string]any{"api_key": "SECRET_CANARY", "input": long, "tools": []any{map[string]any{"arguments": `{"token":"NESTED_SECRET","text":"useful"}`}}, "file_data": "MEDIA_CANARY"})
	raw, omission := retainBody(body, 1<<20)
	require.Equal(t, "media_or_opaque", omission)
	require.Contains(t, raw, long)
	require.Contains(t, raw, "useful")
	require.NotContains(t, raw, "SECRET_CANARY")
	require.NotContains(t, raw, "NESTED_SECRET")
	require.NotContains(t, raw, "MEDIA_CANARY")
	require.True(t, json.Valid([]byte(raw)))
}

func TestRetainedToolDataIsNotMistakenForMedia(t *testing.T) {
	raw, omission := retainBody([]byte(`{"input":[{"type":"function_call_output","output":"{\"data\":{\"answer\":\"TOOL_TEXT\"}}"}],"image":{"source":{"type":"base64","data":"MEDIA_CANARY"}}}`), 1<<20)
	require.Equal(t, "media_or_opaque", omission)
	require.Contains(t, raw, "TOOL_TEXT")
	require.NotContains(t, raw, "MEDIA_CANARY")
	require.True(t, json.Valid([]byte(raw)))
}

func TestRetainedBodyPreservesTokenAccounting(t *testing.T) {
	raw, omission := retainBody([]byte(`{"max_tokens":64000,"usage":{"input_tokens":123,"input_tokens_details":{"cached_tokens":100}},"token":"CREDENTIAL_CANARY"}`), 1<<20)
	require.Empty(t, omission)
	require.Contains(t, raw, `"max_tokens":64000`)
	require.Contains(t, raw, `"input_tokens":123`)
	require.Contains(t, raw, `"cached_tokens":100`)
	require.NotContains(t, raw, "CREDENTIAL_CANARY")
}

func TestRetentionLimitsNeverPretendToBeComplete(t *testing.T) {
	raw, omission := retainBody([]byte(`{"input":"`+strings.Repeat("x", 2048)+`"}`), 1024)
	require.Empty(t, raw)
	require.Equal(t, "body_storage_limit", omission)
	raw, omission = retainBody([]byte(`{"input":`), 1024)
	require.Empty(t, raw)
	require.Equal(t, "input_unavailable_or_invalid", omission)
}

func TestPayloadBudgetSurvivesQueueAndReleasesAfterPersistence(t *testing.T) {
	store := &memoryStore{config: DefaultConfig(), gate: make(chan struct{}), started: make(chan struct{})}
	s := New(store, testCipher{}, true)
	p := s.Prepare([]byte(`{"input":"hello"}`), "reason")
	s.Enqueue(1, p)
	<-store.started
	p.Release()
	require.Positive(t, s.retainedBytes.Load(), "queued ciphertext remains charged after caller releases")
	close(store.gate)
	eventually(t, func() bool { return s.retainedBytes.Load() == 0 })
	s.Close()

	store = &memoryStore{config: DefaultConfig()}
	s = New(store, testCipher{}, true)
	defer s.Close()
	reservation := s.reservePayload(retainedPayloadBudget)
	require.NotNil(t, reservation)
	p = s.Prepare([]byte(`{"input":"hello"}`), "reason")
	require.Equal(t, "capture_busy", p.Status)
	require.Empty(t, p.Ciphertext)
	reservation.release()
	require.Zero(t, s.retainedBytes.Load())
}
