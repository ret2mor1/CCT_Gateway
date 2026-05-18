package translator

import (
	"cct/models"
	"testing"
)

func TestTranslateRequest(t *testing.T) {
	anthropicReq := &models.AnthropicRequest{
		Model:  "claude-3-5-sonnet-20240620",
		System: "You are a helpful assistant.",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: "Hello!"},
		},
		MaxTokens: 1024,
		Stream:    true,
	}

	openaiReq := TranslateRequest(anthropicReq, "meta/llama-3.1-405b-instruct")

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

	openaiReq := TranslateRequest(anthropicReq, "claude-3")

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
				Role             string                  `json:"role"`
				Content          interface{}             `json:"content"`
				ReasoningContent string                  `json:"reasoning_content,omitempty"`
				ToolCalls        []models.OpenAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Message: struct {
					Role             string                  `json:"role"`
					Content          interface{}             `json:"content"`
					ReasoningContent string                  `json:"reasoning_content,omitempty"`
					ToolCalls        []models.OpenAIToolCall `json:"tool_calls"`
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

func TestTranslateRequestToolTrimmingAndReasoning(t *testing.T) {
	anthropicReq := &models.AnthropicRequest{
		Model: "claude-3",
		Tools: []interface{}{
			map[string]interface{}{
				"name":         " Bash ", // has spaces
				"description":  "Run shell commands",
				"input_schema": map[string]interface{}{"type": "object"},
			},
		},
		Messages: []models.AnthropicMessage{
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{
						"type":     "thinking",
						"thinking": "Thinking process...",
					},
					map[string]interface{}{
						"type": "text",
						"text": "Hello, world!",
					},
				},
			},
		},
	}

	openaiReq := TranslateRequest(anthropicReq, "claude-3")

	// Verify tool name trimming
	if len(openaiReq.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(openaiReq.Tools))
	}
	toolMap, ok := openaiReq.Tools[0].Function.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected tool function to be map")
	}
	if toolMap["name"] != "Bash" {
		t.Errorf("Expected tool name to be trimmed to 'Bash', got '%v'", toolMap["name"])
	}

	// Verify message reasoning content mapping
	if len(openaiReq.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(openaiReq.Messages))
	}
	msg := openaiReq.Messages[0]
	if msg.ReasoningContent != "Thinking process..." {
		t.Errorf("Expected ReasoningContent 'Thinking process...', got '%s'", msg.ReasoningContent)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Expected Content 'Hello, world!', got '%v'", msg.Content)
	}
}

func TestTranslateResponseWithReasoning(t *testing.T) {
	openaiRes := &models.OpenAIResponse{
		ID:    "chatcmpl-123",
		Model: "deepseek-reasoner",
		Choices: []struct {
			Message struct {
				Role             string                  `json:"role"`
				Content          interface{}             `json:"content"`
				ReasoningContent string                  `json:"reasoning_content,omitempty"`
				ToolCalls        []models.OpenAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Message: struct {
					Role             string                  `json:"role"`
					Content          interface{}             `json:"content"`
					ReasoningContent string                  `json:"reasoning_content,omitempty"`
					ToolCalls        []models.OpenAIToolCall `json:"tool_calls"`
				}{
					Role:             "assistant",
					Content:          "Final answer",
					ReasoningContent: "Thinking step by step...",
				},
				FinishReason: "stop",
			},
		},
	}

	anthropicRes := TranslateResponse(openaiRes)

	if len(anthropicRes.Content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(anthropicRes.Content))
	}

	thinkingBlock, ok := anthropicRes.Content[0].(models.AnthropicThinkingBlock)
	if !ok {
		t.Fatalf("Expected first block to be AnthropicThinkingBlock, got %T", anthropicRes.Content[0])
	}
	if thinkingBlock.Thinking != "Thinking step by step..." {
		t.Errorf("Expected thinking block to be 'Thinking step by step...', got '%s'", thinkingBlock.Thinking)
	}

	textBlock, ok := anthropicRes.Content[1].(models.AnthropicBlock)
	if !ok {
		t.Fatalf("Expected second block to be AnthropicBlock, got %T", anthropicRes.Content[1])
	}
	if textBlock.Text != "Final answer" {
		t.Errorf("Expected text block to be 'Final answer', got '%s'", textBlock.Text)
	}
}

func TestReasoningCacheRestoration(t *testing.T) {
	userPrompt := "Optimize this Go program"
	assistantText := "Here is the optimized code..."
	reasoning := "Evaluating performance bottlenecks in loops..."

	// Populate Cache
	CacheReasoning(userPrompt, assistantText, reasoning)

	// Build request mimicking history sent back by the client without thinking blocks
	anthropicReq := &models.AnthropicRequest{
		Model: "claude-3",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: userPrompt},
			{Role: "assistant", Content: assistantText},
			{Role: "user", Content: "Does it support parallel execution?"},
		},
	}

	openaiReq := TranslateRequest(anthropicReq, "claude-3")

	// The assistant message's ReasoningContent must be automatically recovered from cache!
	if len(openaiReq.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(openaiReq.Messages))
	}

	assistantMsg := openaiReq.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected second message to be assistant")
	}

	if assistantMsg.ReasoningContent != reasoning {
		t.Errorf("Expected restored ReasoningContent '%s', got '%s'", reasoning, assistantMsg.ReasoningContent)
	}
}
