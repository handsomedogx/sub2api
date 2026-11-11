package cyberevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

const MaxRejectionBytes = 1 << 20

// Retention is independent of preview extraction. Never truncate a JSON byte
// string and present it as a complete document. Only confirmed hits call this.
func retainBody(body []byte, limit int) (string, string) {
	if len(body) == 0 {
		return "", "body_unavailable"
	}
	if len(body) > limit {
		return "", "body_storage_limit"
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return "", "input_unavailable_or_invalid"
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return "", "input_unavailable_or_invalid"
	}
	omission := ""
	raw, err := json.Marshal(redactRetainedValue(value, 0, &omission))
	if err != nil {
		return "", "capture_failed"
	}
	if len(raw) > limit {
		return "", "body_storage_limit"
	}
	return string(raw), omission
}

func tokenAccountingField(key string, value any) bool {
	key = strings.ToLower(key)
	if _, ok := value.(json.Number); ok && (key == "tokens" || strings.HasSuffix(key, "_tokens")) {
		return true
	}
	switch key {
	case "input_tokens_details", "output_tokens_details", "prompt_tokens_details", "completion_tokens_details":
		return true
	}
	return false
}

func redactRetainedValue(value any, depth int, omission *string) any {
	if depth > 128 {
		*omission = "structure_limit"
		return "[omitted: nesting limit]"
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for _, k := range sortedKeys(v) {
			if secretField(k) && !tokenAccountingField(k, v[k]) {
				out[RedactText(k)] = "[REDACTED]"
				continue
			}
			if retainedMediaField(k, v) {
				meta := map[string]any{"omitted": "media_or_opaque"}
				if text, ok := v[k].(string); ok {
					meta["encoded_bytes"] = len(text)
				}
				out[RedactText(k)] = meta
				if *omission == "" {
					*omission = "media_or_opaque"
				}
				continue
			}
			out[RedactText(k)] = redactRetainedValue(v[k], depth+1, omission)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactRetainedValue(v[i], depth+1, omission)
		}
		return out
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
			if *omission == "" {
				*omission = "media_or_opaque"
			}
			return map[string]any{"omitted": "media_or_opaque", "encoded_bytes": len(v)}
		}
		// Tool arguments/results can themselves be serialized JSON strings.
		if depth < 128 && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			dec := json.NewDecoder(strings.NewReader(trimmed))
			dec.UseNumber()
			var nested, extra any
			if dec.Decode(&nested) == nil && dec.Decode(&extra) == io.EOF {
				raw, _ := json.Marshal(redactRetainedValue(nested, depth+1, omission))
				return string(raw)
			}
		}
		return RedactText(v)
	default:
		return v
	}
}

func retainedMediaField(key string, parent map[string]any) bool {
	key = strings.ToLower(key)
	if key == "input_audio" {
		return true
	}
	if key != "data" {
		return mediaField(key)
	}
	// Tool results often use a normal data field; only known media containers
	// turn data into an opaque body. Preserve ordinary structured tool content.
	typeName, _ := parent["type"].(string)
	if typeName == "base64" {
		return true
	}
	for _, name := range []string{"mime_type", "mimeType", "media_type"} {
		if mime, ok := parent[name].(string); ok && (strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/")) {
			return true
		}
	}
	return false
}
