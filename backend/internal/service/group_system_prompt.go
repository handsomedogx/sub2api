package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	GroupSystemPromptModePassthrough = "passthrough"
	GroupSystemPromptModeInject      = "inject"
	GroupSystemPromptModeReplace     = "replace"

	GroupSystemPromptMaxBytes = 64 * 1024
)

type GroupSystemPromptProtocol string

const (
	GroupSystemPromptProtocolAnthropic       GroupSystemPromptProtocol = "anthropic"
	GroupSystemPromptProtocolChatCompletions GroupSystemPromptProtocol = "chat_completions"
	GroupSystemPromptProtocolResponses       GroupSystemPromptProtocol = "responses"
	GroupSystemPromptProtocolGemini          GroupSystemPromptProtocol = "gemini"
)

// NormalizeGroupSystemPromptPolicy validates the persisted group policy while
// keeping the prompt bytes intact. An omitted mode is backwards-compatible and
// means passthrough.
func NormalizeGroupSystemPromptPolicy(mode, prompt string) (string, string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = GroupSystemPromptModePassthrough
	}
	switch mode {
	case GroupSystemPromptModePassthrough, GroupSystemPromptModeInject, GroupSystemPromptModeReplace:
	default:
		return "", "", fmt.Errorf("system_prompt_mode must be one of passthrough, inject, replace")
	}
	if len(prompt) > GroupSystemPromptMaxBytes {
		return "", "", fmt.Errorf("system_prompt exceeds %d bytes", GroupSystemPromptMaxBytes)
	}
	if mode != GroupSystemPromptModePassthrough && strings.TrimSpace(prompt) == "" {
		return "", "", fmt.Errorf("system_prompt is required when system_prompt_mode is %s", mode)
	}
	return mode, prompt, nil
}

// ApplyGroupSystemPromptPolicy rewrites only the client-visible instruction
// surface for the inbound protocol. Provider/OAuth compatibility transforms run
// later and remain free to add their own internal prompts.
func ApplyGroupSystemPromptPolicy(body []byte, group *Group, protocol GroupSystemPromptProtocol) ([]byte, error) {
	if group == nil {
		return body, nil
	}
	mode, prompt, err := NormalizeGroupSystemPromptPolicy(group.SystemPromptMode, group.SystemPrompt)
	if err != nil {
		return nil, err
	}
	if mode == GroupSystemPromptModePassthrough {
		return body, nil
	}

	switch protocol {
	case GroupSystemPromptProtocolAnthropic:
		return applyAnthropicGroupSystemPrompt(body, mode, prompt)
	case GroupSystemPromptProtocolChatCompletions:
		return applyChatCompletionsGroupSystemPrompt(body, mode, prompt)
	case GroupSystemPromptProtocolResponses:
		return applyResponsesGroupSystemPrompt(body, mode, prompt)
	case GroupSystemPromptProtocolGemini:
		return applyGeminiGroupSystemPrompt(body, mode, prompt)
	default:
		return nil, fmt.Errorf("unsupported system prompt protocol %q", protocol)
	}
}

func decodeTopLevelObject(body []byte) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("parse request body for system prompt policy: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	return obj, nil
}

func encodeTopLevelObject(obj map[string]json.RawMessage) ([]byte, error) {
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode request body after system prompt policy: %w", err)
	}
	return out, nil
}

func rawJSONString(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func isNullOrEmptyRaw(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// isClaudeCodeBillingAttributionSystemBlock identifies the non-behavioral
// Claude Code billing attribution block. Group prompt replacement may remove
// client instructions, but this transport/attribution marker must survive so
// official Claude Code OAuth traffic keeps the same upstream compatibility.
func isClaudeCodeBillingAttributionSystemBlock(raw json.RawMessage) bool {
	var block struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		return false
	}
	return strings.HasPrefix(block.Text, claudeCodeBillingHeaderPrefix) &&
		strings.Contains(block.Text, claudeCodeEntrypointMarker)
}

func applyAnthropicGroupSystemPrompt(body []byte, mode, prompt string) ([]byte, error) {
	obj, err := decodeTopLevelObject(body)
	if err != nil {
		return nil, err
	}
	raw, exists := obj["system"]
	if mode == GroupSystemPromptModeReplace {
		// Keep only non-behavioral Claude Code attribution metadata from an
		// array-shaped system. All client-authored instruction blocks are removed.
		if exists && !isNullOrEmptyRaw(raw) {
			var blocks []json.RawMessage
			if err := json.Unmarshal(raw, &blocks); err == nil {
				gatewayBlock, _ := json.Marshal(map[string]string{"type": "text", "text": prompt})
				kept := make([]json.RawMessage, 0, 2)
				for _, block := range blocks {
					if isClaudeCodeBillingAttributionSystemBlock(block) {
						kept = append(kept, block)
					}
				}
				kept = append(kept, gatewayBlock)
				encoded, marshalErr := json.Marshal(kept)
				if marshalErr != nil {
					return nil, marshalErr
				}
				obj["system"] = encoded
				return encodeTopLevelObject(obj)
			}
		}
		obj["system"] = rawJSONString(prompt)
		return encodeTopLevelObject(obj)
	}

	if !exists || isNullOrEmptyRaw(raw) {
		obj["system"] = rawJSONString(prompt)
		return encodeTopLevelObject(obj)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		obj["system"] = rawJSONString(prompt + "\n\n" + text)
		return encodeTopLevelObject(obj)
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("anthropic system must be a string or array")
	}
	gatewayBlock, _ := json.Marshal(map[string]string{"type": "text", "text": prompt})
	// Claude Code's billing attribution block is intentionally kept at the
	// beginning. It is not a user instruction and some OAuth compatibility
	// paths rely on its canonical leading position.
	insertAt := 0
	for insertAt < len(blocks) && isClaudeCodeBillingAttributionSystemBlock(blocks[insertAt]) {
		insertAt++
	}
	withGateway := make([]json.RawMessage, 0, len(blocks)+1)
	withGateway = append(withGateway, blocks[:insertAt]...)
	withGateway = append(withGateway, gatewayBlock)
	withGateway = append(withGateway, blocks[insertAt:]...)
	blocks = withGateway
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return nil, err
	}
	obj["system"] = encoded
	return encodeTopLevelObject(obj)
}

func applyChatCompletionsGroupSystemPrompt(body []byte, mode, prompt string) ([]byte, error) {
	obj, err := decodeTopLevelObject(body)
	if err != nil {
		return nil, err
	}
	var messages []json.RawMessage
	if raw, ok := obj["messages"]; ok && !isNullOrEmptyRaw(raw) {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, fmt.Errorf("chat completions messages must be an array")
		}
	}

	filtered := make([]json.RawMessage, 0, len(messages)+1)
	gatewayMessage, _ := json.Marshal(map[string]any{"role": "system", "content": prompt})
	filtered = append(filtered, gatewayMessage)
	for _, msg := range messages {
		if mode == GroupSystemPromptModeReplace {
			var envelope struct {
				Role string `json:"role"`
			}
			if err := json.Unmarshal(msg, &envelope); err != nil {
				return nil, fmt.Errorf("parse chat completions message: %w", err)
			}
			role := strings.ToLower(strings.TrimSpace(envelope.Role))
			if role == "system" || role == "developer" {
				continue
			}
		}
		filtered = append(filtered, msg)
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	obj["messages"] = encoded
	return encodeTopLevelObject(obj)
}

func applyResponsesGroupSystemPrompt(body []byte, mode, prompt string) ([]byte, error) {
	obj, err := decodeTopLevelObject(body)
	if err != nil {
		return nil, err
	}

	if mode == GroupSystemPromptModeInject {
		if raw, ok := obj["instructions"]; ok && !isNullOrEmptyRaw(raw) {
			var current string
			if err := json.Unmarshal(raw, &current); err != nil {
				return nil, fmt.Errorf("responses instructions must be a string")
			}
			obj["instructions"] = rawJSONString(prompt + "\n\n" + current)
		} else {
			obj["instructions"] = rawJSONString(prompt)
		}
		return encodeTopLevelObject(obj)
	}

	obj["instructions"] = rawJSONString(prompt)
	rawInput, ok := obj["input"]
	if !ok || isNullOrEmptyRaw(rawInput) || len(bytes.TrimSpace(rawInput)) == 0 || bytes.TrimSpace(rawInput)[0] != '[' {
		return encodeTopLevelObject(obj)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawInput, &items); err != nil {
		return nil, fmt.Errorf("responses input must be a string or array")
	}
	filtered := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var envelope struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(item, &envelope); err != nil {
			return nil, fmt.Errorf("parse responses input item: %w", err)
		}
		role := strings.ToLower(strings.TrimSpace(envelope.Role))
		if role == "system" || role == "developer" {
			continue
		}
		filtered = append(filtered, item)
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	obj["input"] = encoded
	return encodeTopLevelObject(obj)
}

func applyGeminiGroupSystemPrompt(body []byte, mode, prompt string) ([]byte, error) {
	obj, err := decodeTopLevelObject(body)
	if err != nil {
		return nil, err
	}
	gatewayPart, _ := json.Marshal(map[string]string{"text": prompt})
	if mode == GroupSystemPromptModeReplace {
		instruction, _ := json.Marshal(map[string]any{"parts": []json.RawMessage{gatewayPart}})
		obj["systemInstruction"] = instruction
		return encodeTopLevelObject(obj)
	}

	var instruction map[string]json.RawMessage
	rawInstruction, ok := obj["systemInstruction"]
	if !ok || isNullOrEmptyRaw(rawInstruction) {
		instruction = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(rawInstruction, &instruction); err != nil {
		return nil, fmt.Errorf("gemini systemInstruction must be an object")
	}
	var parts []json.RawMessage
	if rawParts, ok := instruction["parts"]; ok && !isNullOrEmptyRaw(rawParts) {
		if err := json.Unmarshal(rawParts, &parts); err != nil {
			return nil, fmt.Errorf("gemini systemInstruction.parts must be an array")
		}
	}
	parts = append([]json.RawMessage{gatewayPart}, parts...)
	encodedParts, err := json.Marshal(parts)
	if err != nil {
		return nil, err
	}
	instruction["parts"] = encodedParts
	encodedInstruction, err := json.Marshal(instruction)
	if err != nil {
		return nil, err
	}
	obj["systemInstruction"] = encodedInstruction
	return encodeTopLevelObject(obj)
}
