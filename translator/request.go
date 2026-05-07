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
				openaiReq.Tools = append(openaiReq.Tools, models.OpenAITool{
					Type:     "function",
					Function: t,
				})
			}
		}
	}

	// Map system prompt
	systemStr := extractText(anthropicReq.System)
	if systemStr != "" {
		openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
			Role:    "system",
			Content: systemStr,
		})
	}

	// Map messages
	for _, msg := range anthropicReq.Messages {
		switch v := msg.Content.(type) {
		case string:
			content := v
			if msg.Role == "assistant" && content == "" {
				content = "."
			}
			openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
				Role:    msg.Role,
				Content: content,
			})
		case []interface{}:
			textBuilder := strings.Builder{}
			var toolCalls []models.OpenAIToolCall
			var toolResultID string
			var toolResultContent string

			for _, block := range v {
				if b, ok := block.(map[string]interface{}); ok {
					blockType, _ := b["type"].(string)
					switch blockType {
					case "text":
						if t, ok := b["text"].(string); ok {
							textBuilder.WriteString(t)
						}
					case "tool_use":
						id, _ := b["id"].(string)
						name, _ := b["name"].(string)
						input, _ := b["input"]
						args, _ := json.Marshal(input)
						toolCalls = append(toolCalls, models.OpenAIToolCall{
							ID:   id,
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      name,
								Arguments: string(args),
							},
						})
					case "tool_result":
						toolResultID, _ = b["tool_use_id"].(string)
						content := b["content"]
						contentStr := ""
						if s, ok := content.(string); ok {
							contentStr = s
						} else {
							cBytes, _ := json.Marshal(content)
							contentStr = string(cBytes)
						}
						toolResultContent = contentStr
					}
				}
			}

			if toolResultID != "" {
				openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
					Role:       "tool",
					ToolCallID: toolResultID,
					Content:    toolResultContent,
				})
			} else {
				content := textBuilder.String()
			if msg.Role == "assistant" && content == "" && len(toolCalls) == 0 {
				content = "."
			}
				openaiReq.Messages = append(openaiReq.Messages, models.OpenAIMessage{
					Role:      msg.Role,
					Content:   content,
					ToolCalls: toolCalls,
				})
			}
		}
	}

	return openaiReq
}

func extractText(content interface{}) string {
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
