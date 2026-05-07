package translator

import (
	"cct/models"
	"encoding/json"
)

func TranslateResponse(openaiRes *models.OpenAIResponse) *models.AnthropicResponse {
	anthropicRes := &models.AnthropicResponse{
		ID:    openaiRes.ID,
		Type:  "message",
		Role:  "assistant",
		Model: openaiRes.Model,
		Usage: models.AnthropicUsage{
			InputTokens:  openaiRes.Usage.PromptTokens,
			OutputTokens: openaiRes.Usage.CompletionTokens,
		},
	}

	if len(openaiRes.Choices) > 0 {
		choice := openaiRes.Choices[0]

		// Handle text content
		if choice.Message.Content != "" {
			anthropicRes.Content = append(anthropicRes.Content, models.AnthropicBlock{
				Type: "text",
				Text: choice.Message.Content,
			})
		}

		// Handle tool calls
		for _, tc := range choice.Message.ToolCalls {
			var input interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)

			anthropicRes.Content = append(anthropicRes.Content, models.AnthropicToolUse{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}

		anthropicRes.StopReason = mapFinishReason(choice.FinishReason)
		if len(choice.Message.ToolCalls) > 0 {
			anthropicRes.StopReason = "tool_use"
		}
	}

	return anthropicRes
}

func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "content_filter"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}
