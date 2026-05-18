package translator

import (
	"sync"
)

var (
	reasoningCache = make(map[string]string)
	cacheMu        sync.RWMutex
)

func CacheReasoning(userPrompt, assistantText, reasoning string) {
	if userPrompt == "" || assistantText == "" || reasoning == "" {
		return
	}
	key := userPrompt + "\n" + assistantText
	cacheMu.Lock()
	reasoningCache[key] = reasoning
	cacheMu.Unlock()
}

func GetCachedReasoning(userPrompt, assistantText string) string {
	if userPrompt == "" || assistantText == "" {
		return ""
	}
	key := userPrompt + "\n" + assistantText
	cacheMu.RLock()
	val := reasoningCache[key]
	cacheMu.RUnlock()
	return val
}
