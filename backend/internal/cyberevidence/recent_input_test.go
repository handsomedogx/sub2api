package cyberevidence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecentInputRetainsLatestMessagesAfterLargeMedia(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":"data:image/png;base64,` + strings.Repeat("a", 2<<20) + `"},{"type":"text","text":"initial visible text"}]},{"role":"assistant","tool_calls":[{"id":"call_x","function":{"arguments":"{\"api_key\":\"SECRET_CANARY\"}"}}]},{"role":"tool","tool_call_id":"call_x","content":"useful result"},{"role":"user","content":"LATEST QUESTION"}]}`)
	p := Snapshot(body, "upstream reason")
	require.True(t, p.ScanComplete)
	require.True(t, p.Truncated)
	require.False(t, p.Complete)
	require.Equal(t, 4, p.MessagesRetained)
	require.Contains(t, p.Input, "LATEST QUESTION")
	require.Contains(t, p.Input, "call_x")
	require.Contains(t, p.Input, "useful result")
	require.Contains(t, p.Input, "encoded_bytes")
	require.NotContains(t, p.Input, "SECRET_CANARY")
	require.LessOrEqual(t, len(p.Input), MaxStoredBytes)
	require.True(t, json.Valid([]byte(p.Input)))
}

func TestRecentInputDropsOldestMessagesAndBoundsEscapedText(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"messages":[`)
	for i := 0; i < 300; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"role":"user","content":"message-%03d %s"}`, i, strings.Repeat(`\u0001`, 500))
	}
	b.WriteString(`],"tools":[{"description":"` + strings.Repeat("x", 20000) + `"}]}`)
	p := Snapshot([]byte(b.String()), "reason")
	require.True(t, p.ScanComplete)
	require.True(t, p.Truncated)
	require.Equal(t, 300, p.MessagesScanned)
	require.Positive(t, p.MessagesOmitted)
	require.Equal(t, 300, p.MessagesRetained+p.MessagesOmitted)
	require.Contains(t, p.Input, "message-299")
	require.NotContains(t, p.Input, "message-000")
	require.LessOrEqual(t, len(p.Input), MaxStoredBytes)
	require.True(t, json.Valid([]byte(p.Input)))
	require.Contains(t, p.OmittedFields, "tools")
}

func TestRecentInputParseBudgetKeepsCompletedPrefixAndMetadata(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":"READABLE QUESTION"},{"role":"tool","content":"` + strings.Repeat("x", 2<<20) + `"}]}`)
	p := snapshotWithLimit(body, "reason", "sess_x", 1<<20)
	require.False(t, p.ScanComplete)
	require.True(t, p.Truncated)
	require.Equal(t, "request_exceeds_parse_limit", p.Omission)
	require.Contains(t, p.Input, "READABLE QUESTION")
	require.Equal(t, 1<<20, p.ScannedBytes)
	require.Equal(t, len(body), p.OriginalBytes)
	require.True(t, json.Valid([]byte(p.Input)))

	store := &memoryStore{config: Config{Enabled: true, RetentionDays: 7, ParseLimitMiB: 1}}
	s := New(store, testCipher{}, true)
	defer s.Close()
	prepared := s.Prepare([]byte(`{"input":"`+strings.Repeat("x", 2<<20)+`"}`), "REJECTION REASON")
	defer prepared.Release()
	require.Equal(t, "pending", prepared.Status)
	raw, err := (testCipher{}).Decrypt(prepared.Ciphertext)
	require.NoError(t, err)
	var payload Payload
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.Empty(t, payload.Input)
	require.Equal(t, "REJECTION REASON", payload.Reason)
	require.Equal(t, "request_exceeds_parse_limit", payload.Omission)
	require.Error(t, s.UpdateConfig(context.Background(), Config{Enabled: true, RetentionDays: 7, ParseLimitMiB: 65}))
}

func TestRecentInputDoesNotTreatInvalidOrDuplicateFieldsAsComplete(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"user","content":"valid prefix"}],"input":`,
		`{"input":"valid prefix"} trailing`,
	} {
		p := Snapshot([]byte(body), "reason")
		require.False(t, p.ScanComplete)
		require.False(t, p.Complete)
		require.Equal(t, "input_unavailable_or_invalid", p.Omission)
	}
	p := Snapshot([]byte(`{"input":"first","input":"last"}`), "reason")
	require.True(t, p.Complete)
	require.Contains(t, p.Input, "last")
	require.NotContains(t, p.Input, "first")
	p = Snapshot([]byte(`{"messages":[]}`), "reason")
	require.Equal(t, `{"messages":[]}`, p.Input)
	require.True(t, p.Complete)
}
