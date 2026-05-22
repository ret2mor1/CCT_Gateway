package translator

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"cct/models"
)

// cacheEntry holds a single cached reasoning with its creation time.
type cacheEntry struct {
	reasoning string
	createdAt time.Time
}

// ReasoningCache is a bounded, TTL-based cache for reasoning content.
type ReasoningCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxSize int
	ttl     time.Duration
}

var globalReasoningCache = NewReasoningCache(2000, 2*time.Hour)

// NewReasoningCache creates a cache with the given max entries and TTL.
func NewReasoningCache(maxSize int, ttl time.Duration) *ReasoningCache {
	return &ReasoningCache{
		entries: make(map[string]cacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *ReasoningCache) Set(key, reasoning string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
	if len(c.entries) >= c.maxSize {
		c.evictOldestLocked()
	}
	c.entries[key] = cacheEntry{
		reasoning: reasoning,
		createdAt: time.Now(),
	}
}

func (c *ReasoningCache) Get(key string) string {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return ""
	}
	if time.Since(entry.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return ""
	}
	return entry.reasoning
}

func (c *ReasoningCache) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.entries {
		if now.Sub(v.createdAt) > c.ttl {
			delete(c.entries, k)
		}
	}
}

func (c *ReasoningCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, v := range c.entries {
		if first || v.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.createdAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// --- Context Key Computation ---

// ContextMessage represents a message's role and text for key computation.
type ContextMessage struct {
	Role string
	Text string
}

// ComputeContextKey generates a deterministic cache key from conversation context.
// The key is based on the system prompt and all messages up to and including
// the target assistant message, using only text content (thinking blocks excluded).
func ComputeContextKey(systemPrompt string, messages []ContextMessage) string {
	type keyPart struct {
		Role string `json:"r"`
		Text string `json:"t"`
	}
	parts := make([]keyPart, 0, len(messages)+1)
	if systemPrompt != "" {
		parts = append(parts, keyPart{Role: "system", Text: systemPrompt})
	}
	for _, m := range messages {
		parts = append(parts, keyPart{Role: m.Role, Text: m.Text})
	}
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}

// --- Public API (delegates to global cache) ---

// CacheReasoning stores reasoning content in the cache with the given key.
func CacheReasoning(key, reasoning string) {
	globalReasoningCache.Set(key, reasoning)
}

// GetCachedReasoning retrieves cached reasoning content by key.
func GetCachedReasoning(key string) string {
	return globalReasoningCache.Get(key)
}

// --- Message Text Extraction for Key Computation ---

// ExtractMessageText extracts the canonical text from an Anthropic message
// for use in cache key computation. Only includes "text" type blocks and
// tool_use information, excluding "thinking" blocks (which are stripped by Claude Code).
func ExtractMessageText(msg models.AnthropicMessage) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				switch b["type"] {
				case "text":
					if t, ok := b["text"].(string); ok {
						parts = append(parts, t)
					}
				case "tool_use":
					name, _ := b["name"].(string)
					id, _ := b["id"].(string)
					parts = append(parts, "tool:"+name+":"+id)
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
					parts = append(parts, "result:"+trID+":"+contentStr)
				}
			}
		}
		return strings.Join(parts, "|")
	}
	return ""
}

// buildContextUpTo builds the context message list for all messages from index 0
// through the specified index, converting each to a (role, text) pair.
func buildContextUpTo(messages []models.AnthropicMessage, upToIndex int) []ContextMessage {
	result := make([]ContextMessage, 0, upToIndex+1)
	for i := 0; i <= upToIndex; i++ {
		msg := messages[i]
		text := ExtractMessageText(msg)
		result = append(result, ContextMessage{
			Role: msg.Role,
			Text: text,
		})
	}
	return result
}
