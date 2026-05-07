package translator

import (
	"cct/config"
	"cct/models"
	"testing"
)

func TestTranslateRequest(t *testing.T) {
	cfg := &config.Config{
		ModelMapping: map[string]string{
			"claude-3-5-sonnet-20240620": "meta/llama-3.1-405b-instruct",
		},
	}

	anthropicReq := &models.AnthropicRequest{
		Model:  "claude-3-5-sonnet-20240620",
		System: "You are a helpful assistant.",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: "Hello!"},
		},
		MaxTokens: 1024,
		Stream:    true,
	}

	openaiReq := TranslateRequest(anthropicReq, cfg)

	if openaiReq.Model != "meta/llama-3.1-405b-instruct" {
		t.Errorf("Expected model meta/llama-3.1-405b-instruct, got %s", openaiReq.Model)
	}

	if len(openaiReq.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(openaiReq.Messages))
	}

	if openaiReq.Messages[0].Role != "system" || openaiReq.Messages[0].Content != "You are a helpful assistant." {
		t.Errorf("System message mapping failed")
	}

	if openaiReq.Messages[1].Role != "user" || openaiReq.Messages[1].Content != "Hello!" {
		t.Errorf("User message mapping failed")
	}

	if !openaiReq.Stream {
		t.Errorf("Stream flag should be true")
	}
}

func TestTranslateRequestArraySystem(t *testing.T) {
	cfg := &config.Config{}
	anthropicReq := &models.AnthropicRequest{
		Model: "claude-3",
		System: []interface{}{
			map[string]interface{}{"type": "text", "text": "Part 1. "},
			map[string]interface{}{"type": "text", "text": "Part 2."},
		},
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	openaiReq := TranslateRequest(anthropicReq, cfg)

	if len(openaiReq.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(openaiReq.Messages))
	}

	if openaiReq.Messages[0].Content != "Part 1. Part 2." {
		t.Errorf("Array system mapping failed, got: %s", openaiReq.Messages[0].Content)
	}
}

func TestTranslateResponse(t *testing.T) {
	openaiRes := &models.OpenAIResponse{
		ID:    "chatcmpl-123",
		Model: "meta/llama-3.1-405b-instruct",
		Choices: []struct {
			Message struct {
				Role      string                  `json:"role"`
				Content   string                  `json:"content"`
				ToolCalls []models.OpenAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Message: struct {
					Role      string                  `json:"role"`
					Content   string                  `json:"content"`
					ToolCalls []models.OpenAIToolCall `json:"tool_calls"`
				}{
					Role:    "assistant",
					Content: "Hi there!",
				},
				FinishReason: "stop",
			},
		},
		Usage: models.OpenAIUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
		},
	}

	anthropicRes := TranslateResponse(openaiRes)

	if anthropicRes.ID != "chatcmpl-123" {
		t.Errorf("ID mapping failed")
	}

	if len(anthropicRes.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(anthropicRes.Content))
	}

	block, ok := anthropicRes.Content[0].(models.AnthropicBlock)
	if !ok || block.Text != "Hi there!" {
		t.Errorf("Content mapping failed")
	}

	if anthropicRes.Usage.InputTokens != 10 || anthropicRes.Usage.OutputTokens != 5 {
		t.Errorf("Usage mapping failed")
	}

	if anthropicRes.StopReason != "end_turn" {
		t.Errorf("Stop reason mapping failed, got %s", anthropicRes.StopReason)
	}
}
