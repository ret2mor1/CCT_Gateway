package translator

import (
	"cct/models"
	"testing"
	"time"
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
					map[string]interface{}{
						"type": "tool_use",
						"id":   "call_123",
						"name": " Bash ",
						"input": map[string]interface{}{},
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

	// Build the request with a tool use to ensure reasoning content is restored
	anthropicReq := &models.AnthropicRequest{
		Model:  "claude-3",
		System: "You are a helpful assistant.",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: userPrompt},
			{Role: "assistant", Content: []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": assistantText,
				},
				map[string]interface{}{
					"type": "tool_use",
					"id":   "call_123",
					"name": "Bash",
					"input": map[string]interface{}{},
				},
			}},
			{Role: "user", Content: "Does it support parallel execution?"},
		},
	}

	// Compute the context key programmatically
	contextMsgs := buildContextUpTo(anthropicReq.Messages, 1)
	cacheKey := ComputeContextKey("You are a helpful assistant.", contextMsgs)

	// Populate cache with the computed key
	CacheReasoning(cacheKey, reasoning)

	// Translate request -- the assistant message should get reasoning restored
	openaiReq := TranslateRequest(anthropicReq, "claude-3")

	// Expect 4 messages: system + user + assistant + user
	if len(openaiReq.Messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(openaiReq.Messages))
	}

	assistantMsg := openaiReq.Messages[2]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected third message to be assistant, got %s", assistantMsg.Role)
	}
	if assistantMsg.ReasoningContent != reasoning {
		t.Errorf("Expected restored ReasoningContent '%s', got '%s'", reasoning, assistantMsg.ReasoningContent)
	}
}

func TestReasoningCacheNoRestorationWithoutToolCalls(t *testing.T) {
	userPrompt := "Optimize this Go program"
	assistantText := "Here is the optimized code..."
	reasoning := "Evaluating performance bottlenecks in loops..."

	// Build a standard request (no tool calls)
	anthropicReq := &models.AnthropicRequest{
		Model:  "claude-3",
		System: "You are a helpful assistant.",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: userPrompt},
			{Role: "assistant", Content: assistantText},
			{Role: "user", Content: "Does it support parallel execution?"},
		},
	}

	contextMsgs := buildContextUpTo(anthropicReq.Messages, 1)
	cacheKey := ComputeContextKey("You are a helpful assistant.", contextMsgs)

	CacheReasoning(cacheKey, reasoning)

	openaiReq := TranslateRequest(anthropicReq, "claude-3")

	if len(openaiReq.Messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(openaiReq.Messages))
	}

	assistantMsg := openaiReq.Messages[2]
	if assistantMsg.ReasoningContent != "" {
		t.Errorf("Expected empty ReasoningContent for non-tool-calling message to avoid context pollution, but got '%s'", assistantMsg.ReasoningContent)
	}
}

func TestCacheIsolation(t *testing.T) {
	// Two different conversations with the same initial prompt should have different keys
	// when the total context differs
	system := "System prompt"
	msgs1 := []ContextMessage{
		{Role: "user", Text: "Hello"},
		{Role: "assistant", Text: "Hi there"},
	}
	msgs2 := []ContextMessage{
		{Role: "user", Text: "Hello"},
		{Role: "assistant", Text: "Hi there"},
		{Role: "user", Text: "Follow up"},
	}

	// Same prefix context -> same key (correct for same conversation state)
	key1 := ComputeContextKey(system, msgs1)
	key2Prefix := ComputeContextKey(system, msgs2[:2])
	if key1 != key2Prefix {
		t.Errorf("Expected same key for same context prefix, got different")
	}

	// Different total context -> different key
	key2 := ComputeContextKey(system, msgs2)
	if key1 == key2 {
		t.Errorf("Expected different keys for different total context")
	}

	// Different system prompt -> different key
	msgs3 := []ContextMessage{
		{Role: "user", Text: "Hello"},
		{Role: "assistant", Text: "Hi there"},
	}
	key3 := ComputeContextKey("Different system", msgs3)
	if key1 == key3 {
		t.Errorf("Expected different keys for different system prompt")
	}
}

func TestCacheEviction(t *testing.T) {
	cache := NewReasoningCache(3, 1*time.Hour)
	cache.Set("key1", "reasoning1")
	time.Sleep(time.Millisecond)
	cache.Set("key2", "reasoning2")
	time.Sleep(time.Millisecond)
	cache.Set("key3", "reasoning3")
	time.Sleep(time.Millisecond)

	// Adding a 4th should evict the oldest (key1)
	cache.Set("key4", "reasoning4")

	if cache.Get("key1") != "" {
		t.Errorf("Expected key1 to be evicted")
	}
	if cache.Get("key4") != "reasoning4" {
		t.Errorf("Expected key4 to be present")
	}
	// key2 and key3 should still be present
	if cache.Get("key2") != "reasoning2" {
		t.Errorf("Expected key2 to be present")
	}
	if cache.Get("key3") != "reasoning3" {
		t.Errorf("Expected key3 to be present")
	}
}

func TestCacheTTL(t *testing.T) {
	cache := NewReasoningCache(100, 1*time.Millisecond)
	cache.Set("key1", "reasoning1")

	time.Sleep(2 * time.Millisecond)

	if cache.Get("key1") != "" {
		t.Errorf("Expected key1 to be expired")
	}
}

func TestExtractMessageText(t *testing.T) {
	// String content
	msg1 := models.AnthropicMessage{Role: "user", Content: "Hello"}
	if text := ExtractMessageText(msg1); text != "Hello" {
		t.Errorf("Expected 'Hello', got '%s'", text)
	}

	// Array content with text and thinking blocks
	msg2 := models.AnthropicMessage{
		Role: "assistant",
		Content: []interface{}{
			map[string]interface{}{"type": "thinking", "thinking": "My thoughts..."},
			map[string]interface{}{"type": "text", "text": "Response text"},
		},
	}
	if text := ExtractMessageText(msg2); text != "Response text" {
		t.Errorf("Expected 'Response text', got '%s'", text)
	}

	// Array content with tool_use
	msg3 := models.AnthropicMessage{
		Role: "assistant",
		Content: []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "call_123", "name": "Bash"},
			map[string]interface{}{"type": "text", "text": "Used bash"},
		},
	}
	if text := ExtractMessageText(msg3); text != "tool:Bash:call_123|Used bash" {
		t.Errorf("Expected 'tool:Bash:call_123|Used bash', got '%s'", text)
	}
}
