package cyberevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

func mediaField(key string) bool {
	switch strings.ToLower(key) {
	case "image_url", "audio", "data", "file_data", "encrypted_content", "b64_json", "image_base64", "audio_data", "inline_data", "inlinedata":
		return true
	}
	return false
}

// Each value is decoded separately, so a large conversation is never materialized
// as a second full object tree. Only a bounded suffix of sanitized messages lives
// past an iteration. The decoder cannot read beyond the configured byte budget.
func extractRecentInput(body []byte, limit int, p *Payload) {
	view := body[:min(len(body), limit)]
	dec := json.NewDecoder(bytes.NewReader(view))
	dec.UseNumber()
	selected := map[string]json.RawMessage{}
	counts := map[string][2]int{}
	truncated := false
	var parseErr error
	root, err := dec.Token()
	if err != nil || root != json.Delim('{') {
		p.Omission = "input_unavailable_or_invalid"
		p.Truncated = len(body) > limit
		p.ScannedBytes = min(len(view), int(dec.InputOffset()))
		return
	}
	fields := 0
	for dec.More() {
		fields++
		if fields > 128 {
			p.Omission = "structure_limit"
			truncated = true
			break
		}
		token, e := dec.Token()
		if e != nil {
			parseErr = e
			break
		}
		key, ok := token.(string)
		if !ok {
			parseErr = io.ErrUnexpectedEOF
			break
		}
		keep := false
		switch key {
		case "messages", "input", "instructions", "system", "tools", "tool_choice", "previous_response_id", "conversation", "thread_id", "session_id":
			keep = true
		}
		// Duplicate JSON keys use the last value, matching encoding/json.
		delete(selected, key)
		delete(counts, key)
		start := int(dec.InputOffset())
		for start < len(view) && bytes.ContainsRune([]byte(" \r\n\t:"), rune(view[start])) {
			start++
		}
		if keep && (key == "messages" || key == "input") && start < len(view) && view[start] == '[' {
			_, parseErr = dec.Token()
			messages := make([]json.RawMessage, 0)
			retainedBytes, seen := 0, 0
			for parseErr == nil && dec.More() {
				if seen >= 4096 {
					p.Omission = "structure_limit"
					truncated = true
					break
				}
				var value any
				parseErr = dec.Decode(&value)
				if parseErr != nil {
					break
				}
				seen++
				raw, cut := sanitizeValue(value, 48*1024)
				truncated = truncated || cut
				messages = append(messages, raw)
				retainedBytes += len(raw) + 1
				for len(messages) > 256 || retainedBytes > 80*1024 {
					retainedBytes -= len(messages[0]) + 1
					messages[0] = nil
					messages = messages[1:]
					truncated = true
				}
			}
			if len(messages) > 0 || (parseErr == nil && p.Omission == "") {
				selected[key], _ = json.Marshal(messages)
			}
			counts[key] = [2]int{seen, len(messages)}
			if parseErr != nil || p.Omission == "structure_limit" {
				break
			}
			_, parseErr = dec.Token()
		} else {
			var value any
			if keep {
				parseErr = dec.Decode(&value)
				if parseErr == nil {
					budget := 4 * 1024
					if key == "input" {
						budget = 48 * 1024
					}
					raw, cut := sanitizeValue(value, budget)
					selected[key] = raw
					truncated = truncated || cut
					if cut {
						p.OmittedFields = append(p.OmittedFields, key)
					}
				}
			} else {
				// RawMessage validates unknown fields without allocating their object tree.
				var ignored json.RawMessage
				parseErr = dec.Decode(&ignored)
			}
		}
		if parseErr != nil {
			break
		}
	}
	if parseErr == nil && p.Omission == "" {
		_, parseErr = dec.Token()
		if parseErr == nil {
			var extra json.RawMessage
			if e := dec.Decode(&extra); e != io.EOF {
				parseErr = io.ErrUnexpectedEOF
			}
		}
	}
	p.ScannedBytes = min(len(view), int(dec.InputOffset()))
	p.ScanComplete = parseErr == nil && p.Omission == "" && len(body) <= limit
	if !p.ScanComplete {
		truncated = true
		if len(body) > limit {
			p.Omission = "request_exceeds_parse_limit"
			p.ScannedBytes = len(view)
		} else if p.Omission == "" {
			p.Omission = "input_unavailable_or_invalid"
		}
	}
	for _, count := range counts {
		p.MessagesScanned += count[0]
		p.MessagesRetained += count[1]
	}
	p.MessagesOmitted = p.MessagesScanned - p.MessagesRetained
	_, messages := selected["messages"]
	_, input := selected["input"]
	if messages || input {
		raw, e := json.Marshal(selected)
		if e == nil && len(raw) <= MaxStoredBytes {
			p.Input = string(raw)
			p.StoredBytes = len(raw)
		} else {
			p.Omission = "structured_input_exceeds_limit"
			truncated = true
		}
	} else if p.Omission == "" {
		p.Omission = "no_visible_input"
	}
	p.Truncated = truncated
	p.Complete = p.Input != "" && p.ScanComplete && !truncated
}

func sanitizeValue(value any, maxBytes int) (json.RawMessage, bool) {
	budget := maxBytes / 2
	for budget >= 128 {
		l := limiter{remaining: budget}
		raw, err := json.Marshal(l.walk(value, 0))
		if err == nil && len(raw) <= maxBytes {
			return raw, l.truncated
		}
		budget /= 2
	}
	return json.RawMessage(`"[omitted: structure limit]"`), true
}
