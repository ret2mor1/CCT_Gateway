package translator

import (
	"cct/models"
	"encoding/json"
	"strings"
)

func TranslateRequest(anthropicReq *models.AnthropicRequest, remoteModel string) *models.OpenAIRequest {
	openaiReq := &models.OpenAIRequest{
		Stream:      anthropicReq.Stream,
		Temperature: anthropicReq.Temperature,
		TopP:        anthropicReq.TopP,
		Model:       remoteModel,
	}

	// Map tokens
	openaiReq.MaxTokens = anthropicReq.MaxTokens
	openaiReq.MaxCompletionTokens = anthropicReq.MaxTokens

	// Map Tools
	if anthropicReq.Tools != nil {
		if tools, ok := anthropicReq.Tools.([]interface{}); ok {
			for _, t := range tools {
				// Translate Anthropic tool definition to OpenAI function format
				if toolMap, ok := t.(map[string]interface{}); ok {
					newFunction := make(map[string]interface{})
					for k, v := range toolMap {
						if k == "input_schema" {
							newFunction["parameters"] = v
						} else if k == "name" {
							if s, ok := v.(string); ok {
								newFunction[k] = strings.TrimSpace(s)
							} else {
								newFunction[k] = v
							}
						} else {
							newFunction[k] = v
						}
					}
					openaiReq.Tools = append(openaiReq.Tools, models.OpenAITool{
						Type:     "function",
						Function: newFunction,
					})
				} else {
					openaiReq.Tools = append(openaiReq.Tools, models.OpenAITool{
						Type:     "function",
						Function: t,
					})
				}
			}
		}
	}

	// Map system prompt
	systemStr := ExtractText(anthropicReq.System)
	if systemStr != "" {
		openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
			Role:    "system",
			Content: systemStr,
		})
	}

	// Map messages
	for idx, msg := range anthropicReq.Messages {
		switch v := msg.Content.(type) {
		case string:
			content := v
			if msg.Role == "assistant" && content == "" {
				content = "."
			}
			openaiMsg := models.OpenAIMessage{
				Role:    msg.Role,
				Content: content,
			}
			if msg.Role == "assistant" {
				var precedingUser string
				for j := idx - 1; j >= 0; j-- {
					if anthropicReq.Messages[j].Role == "user" {
						precedingUser = ExtractText(anthropicReq.Messages[j].Content)
						break
					}
				}
				if precedingUser != "" {
					if cached := GetCachedReasoning(precedingUser, content); cached != "" {
						openaiMsg.ReasoningContent = cached
					}
				}
			}
			openaiReq.Messages = append(openaiReq.Messages, openaiMsg)
		case []interface{}:
			var assistantText strings.Builder
			var assistantReasoning strings.Builder
			var assistantToolCalls []models.OpenAIToolCall
			var userText strings.Builder
			var toolResultMsgs []models.OpenAIMessage

			for _, block := range v {
				if b, ok := block.(map[string]interface{}); ok {
					blockType, _ := b["type"].(string)
					switch blockType {
					case "thinking":
						if t, ok := b["thinking"].(string); ok {
							assistantReasoning.WriteString(t)
						}
					case "text":
						if t, ok := b["text"].(string); ok {
							if msg.Role == "assistant" {
								assistantText.WriteString(t)
							} else {
								userText.WriteString(t)
							}
						}
					case "tool_use":
						id, _ := b["id"].(string)
						name, _ := b["name"].(string)
						input, _ := b["input"]
						args, _ := json.Marshal(input)
						assistantToolCalls = append(assistantToolCalls, models.OpenAIToolCall{
							ID:   id,
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      strings.TrimSpace(name),
								Arguments: string(args),
							},
						})
					case "tool_result":
						trID, _ := b["tool_use_id"].(string)
						content := b["content"]
						contentStr := ""
						if s, ok := content.(string); ok {
							contentStr = s
						} else if content != nil {
							cBytes, _ := json.Marshal(content)
							contentStr = string(cBytes)
						}
						toolResultMsgs = append(toolResultMsgs, models.OpenAIMessage{
							Role:       "tool",
							ToolCallID: trID,
							Content:    contentStr,
						})
					}
				}
			}

			if msg.Role == "assistant" {
				content := assistantText.String()
				if content == "" && len(assistantToolCalls) == 0 {
					content = "."
				}
				openaiMsg := models.OpenAIMessage{
					Role:      "assistant",
					Content:   content,
					ToolCalls: assistantToolCalls,
				}
				reasoningStr := assistantReasoning.String()
				if reasoningStr != "" {
					openaiMsg.ReasoningContent = reasoningStr
				} else {
					var precedingUser string
					for j := idx - 1; j >= 0; j-- {
						if anthropicReq.Messages[j].Role == "user" {
							precedingUser = ExtractText(anthropicReq.Messages[j].Content)
							break
						}
					}
					if precedingUser != "" {
						if cached := GetCachedReasoning(precedingUser, content); cached != "" {
							openaiMsg.ReasoningContent = cached
						}
					}
				}
				openaiReq.Messages = append(openaiReq.Messages, openaiMsg)
			} else if msg.Role == "user" {
				if len(toolResultMsgs) > 0 {
					openaiReq.Messages = append(openaiReq.Messages, toolResultMsgs...)
				}
				content := userText.String()
				if content != "" || len(toolResultMsgs) == 0 {
					if content != "" {
						openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
							Role:    "user",
							Content: content,
						})
					} else {
						// empty string fallback
						openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
							Role:    "user",
							Content: ".",
						})
					}
				}
			} else {
				content := userText.String() + assistantText.String()
				openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
					Role:    msg.Role,
					Content: content,
				})
			}
		}
	}

	return openaiReq
}

func ExtractText(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		text := ""
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				if b["type"] == "text" {
					if t, ok := b["text"].(string); ok {
						text += t
					}
				}
			}
		}
		return text
	}
	return ""
}
