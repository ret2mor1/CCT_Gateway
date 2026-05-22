package proxy

import (
	"bufio"
	"bytes"
	"cct/auth"
	"cct/config"
	"cct/limiter"
	"cct/models"
	"cct/translator"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type Proxy struct {
	cfg     *config.Config
	limiter *limiter.Manager
}

func NewProxy(cfg *config.Config, l *limiter.Manager) *Proxy {
	return &Proxy{
		cfg:     cfg,
		limiter: l,
	}
}

func (p *Proxy) HandleMessages(w http.ResponseWriter, r *http.Request) {
	if !p.authenticate(w, r, "anthropic-messages") {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	var anthropicReq models.AnthropicRequest
	if err := json.Unmarshal(bodyBytes, &anthropicReq); err != nil {
		log.Printf("Failed to decode Anthropic request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Route selection
	route, provider, err := p.resolveRoute(anthropicReq.Model)
	if err != nil {
		http.Error(w, err.Error(), p.errorToStatus(err))
		return
	}

	// Rate limiting
	p.limiter.Wait(r.Context(), route.Provider)

	// Translate request
	openaiReq := translator.TranslateRequest(&anthropicReq, route.Model)
	reqBody, _ := json.Marshal(openaiReq)

	// log.Printf("Raw Anthropic Request: %s", string(bodyBytes))
	log.Printf("Proxying Anthropic request: %s -> %s (via %s)",
		anthropicReq.Model, route.Model, route.Provider)
	// log.Printf("Translated OpenAI Request: %s", string(reqBody))

	resp, err := p.doBackendRequest(provider, reqBody)
	if err != nil {
		// log.Printf("Backend request failed for %s: %v", anthropicReq.Model, err)
		http.Error(w, "Backend request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.handleBackendError(w, resp)
		return
	}

	if anthropicReq.Stream {
		p.handleAnthropicStreamingResponse(w, resp, &anthropicReq)
	} else {
		p.handleAnthropicStandardResponse(w, resp, &anthropicReq)
	}
}

func (p *Proxy) HandleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if !p.authenticate(w, r, "openai-chat") {
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	var openaiReq models.OpenAIRequest
	if err := json.Unmarshal(bodyBytes, &openaiReq); err != nil {
		log.Printf("Failed to decode OpenAI request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Route selection
	route, provider, err := p.resolveRoute(openaiReq.Model)
	if err != nil {
		http.Error(w, err.Error(), p.errorToStatus(err))
		return
	}

	// Rate limiting
	p.limiter.Wait(r.Context(), route.Provider)

	// Update model name to backend's model
	originalModel := openaiReq.Model
	openaiReq.Model = route.Model
	reqBody, _ := json.Marshal(openaiReq)

	log.Printf("Proxying OpenAI request: %s -> %s (via %s)",
		originalModel, route.Model, route.Provider)

	resp, err := p.doBackendRequest(provider, reqBody)
	if err != nil {
		log.Printf("Backend request failed for %s: %v", openaiReq.Model, err)
		http.Error(w, "Backend request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.handleBackendError(w, resp)
		return
	}

	if openaiReq.Stream {
		p.handleOpenAIStreamingResponse(w, resp)
	} else {
		p.handleOpenAIStandardResponse(w, resp, originalModel)
	}
}

func (p *Proxy) HandleListModels(w http.ResponseWriter, r *http.Request) {
	// Support both protocols for model listing auth
	if !p.authenticate(w, r, "openai-chat") && !p.authenticate(w, r, "anthropic-messages") {
		return
	}

	var modelList []map[string]interface{}
	now := time.Now().Unix()

	for name := range p.cfg.Models {
		modelList = append(modelList, map[string]interface{}{
			"id":       name,
			"object":   "model",
			"created":  now,
			"owned_by": "cct",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   modelList,
	})
}

func (p *Proxy) authenticate(w http.ResponseWriter, r *http.Request, protocolName string) bool {
	protocol, ok := p.cfg.Protocols[protocolName]
	if !ok || !protocol.Enabled {
		return true // Protocol not configured or disabled, skip auth
	}

	if len(p.cfg.Server.APIKeys) == 0 {
		return true
	}

	clientKey := auth.ExtractAPIKey(r, protocol.Auth)
	if !auth.IsAuthorized(clientKey, p.cfg.Server.APIKeys) {
		log.Printf("Unauthorized %s request from %s", protocolName, r.RemoteAddr)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (p *Proxy) resolveRoute(modelName string) (*config.Route, config.Provider, error) {
	modelCfg, ok := p.cfg.Models[modelName]
	if !ok {
		log.Printf("Model not found in config: %s", modelName)
		return nil, config.Provider{}, fmt.Errorf("Model %s not found", modelName)
	}

	route := p.selectRoute(modelCfg)
	if route == nil {
		return nil, config.Provider{}, fmt.Errorf("No available routes for model")
	}

	provider, ok := p.cfg.Providers[route.Provider]
	if !ok {
		log.Printf("Provider %s not found for model %s", route.Provider, modelName)
		return nil, config.Provider{}, fmt.Errorf("Provider configuration error")
	}

	return route, provider, nil
}

func (p *Proxy) errorToStatus(err error) int {
	if strings.Contains(err.Error(), "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), "No available routes") {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func (p *Proxy) doBackendRequest(provider config.Provider, body []byte) (*http.Response, error) {
	backendURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(provider.BaseURL, "/"))
	req, err := http.NewRequest(http.MethodPost, backendURL, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !provider.Transport.VerifySSL},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(provider.Transport.Timeout) * time.Second,
	}
	if provider.Transport.Timeout == 0 {
		client.Timeout = 120 * time.Second
	}

	return client.Do(req)
}

func (p *Proxy) handleBackendError(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	log.Printf("Backend error (%s): %s", resp.Status, string(body))
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) selectRoute(m config.Model) *config.Route {
	if len(m.Routes) == 0 {
		return nil
	}
	if len(m.Routes) == 1 {
		return &m.Routes[0]
	}

	totalWeight := 0
	for _, r := range m.Routes {
		totalWeight += r.Weight
	}

	if totalWeight == 0 {
		return &m.Routes[0]
	}

	r := rand.Intn(totalWeight)
	current := 0
	for _, route := range m.Routes {
		current += route.Weight
		if r < current {
			return &route
		}
	}
	return &m.Routes[0]
}

// buildResponseCacheKey computes the cache key for a newly generated response.
// The key is based on the system prompt, all messages in the request, and the
// assistant's newly generated reply, matching the lookup key computed in TranslateRequest.
func buildResponseCacheKey(anthropicReq *models.AnthropicRequest, assistantText string) string {
	var contextMsgs []translator.ContextMessage
	for _, msg := range anthropicReq.Messages {
		contextMsgs = append(contextMsgs, translator.ContextMessage{
			Role: msg.Role,
			Text: translator.ExtractMessageText(msg),
		})
	}
	contextMsgs = append(contextMsgs, translator.ContextMessage{
		Role: "assistant",
		Text: assistantText,
	})
	systemText := translator.ExtractText(anthropicReq.System)
	return translator.ComputeContextKey(systemText, contextMsgs)
}

func (p *Proxy) handleAnthropicStandardResponse(w http.ResponseWriter, resp *http.Response, anthropicReq *models.AnthropicRequest) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read backend response: %v", err)
		http.Error(w, "Failed to read backend response", http.StatusInternalServerError)
		return
	}

	var openaiRes models.OpenAIResponse
	if err := json.Unmarshal(body, &openaiRes); err != nil {
		log.Printf("Failed to decode backend response: %v", err)
		http.Error(w, "Failed to decode backend response", http.StatusInternalServerError)
		return
	}

	if len(openaiRes.Choices) > 0 {
		choice := openaiRes.Choices[0]
		if choice.Message.ReasoningContent != "" {
			var parts []string
			contentStr := ""
			if choice.Message.Content != nil {
				switch v := choice.Message.Content.(type) {
				case string:
					contentStr = v
				default:
					bytes, _ := json.Marshal(v)
					contentStr = string(bytes)
				}
			}
			if contentStr != "" {
				parts = append(parts, contentStr)
			}
			for _, tc := range choice.Message.ToolCalls {
				parts = append(parts, "tool:"+strings.TrimSpace(tc.Function.Name)+":"+tc.ID)
			}
			assistantText := strings.Join(parts, "|")

			cacheKey := buildResponseCacheKey(anthropicReq, assistantText)
			translator.CacheReasoning(cacheKey, choice.Message.ReasoningContent)
		}
	}

	anthropicRes := translator.TranslateResponse(&openaiRes)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicRes)
}

func (p *Proxy) handleOpenAIStandardResponse(w http.ResponseWriter, resp *http.Response, originalModel string) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read backend response: %v", err)
		http.Error(w, "Failed to read backend response", http.StatusInternalServerError)
		return
	}

	var openaiRes models.OpenAIResponse
	if err := json.Unmarshal(body, &openaiRes); err != nil {
		log.Printf("Failed to decode backend response: %v", err)
		http.Error(w, "Failed to decode backend response", http.StatusInternalServerError)
		return
	}

	// Update model name to what the client requested
	openaiRes.Model = originalModel

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiRes)
}

func (p *Proxy) handleAnthropicStreamingResponse(w http.ResponseWriter, resp *http.Response, anthropicReq *models.AnthropicRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	p.sendEvent(w, flusher, "message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":    "proxy-id",
			"type":  "message",
			"role":  "assistant",
			"model": "proxy-model",
			"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})

	reader := bufio.NewReader(resp.Body)
	var activeToolID string
	var nextBlockIndex int
	var currentToolBlockIndex int
	var textBlockStarted bool
	var thinkingBlockStarted bool
	var thinkingBlockStopped bool
	var fullTextBuilder strings.Builder
	var fullReasoningBuilder strings.Builder
	var toolCallsInfo []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		type Choice struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content,omitempty"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		}
		var fullChunk struct {
			Choices []Choice `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &fullChunk); err != nil {
			continue
		}

		if len(fullChunk.Choices) > 0 {
			choice := fullChunk.Choices[0]
			delta := choice.Delta

			// Handle reasoning content (thinking block)
			if delta.ReasoningContent != "" {
				fullReasoningBuilder.WriteString(delta.ReasoningContent)
				if !thinkingBlockStarted {
					p.sendEvent(w, flusher, "content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": nextBlockIndex,
						"content_block": map[string]string{
							"type":     "thinking",
							"thinking": "",
						},
					})
					thinkingBlockStarted = true
				}
				p.sendEvent(w, flusher, "content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": nextBlockIndex,
					"delta": map[string]interface{}{
						"type":     "thinking_delta",
						"thinking": delta.ReasoningContent,
					},
				})
			}

			// Handle text content
			if delta.Content != "" {
				if activeToolID == "" {
					fullTextBuilder.WriteString(delta.Content)
					if thinkingBlockStarted && !thinkingBlockStopped {
						p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
							"type":  "content_block_stop",
							"index": nextBlockIndex,
						})
						thinkingBlockStopped = true
						nextBlockIndex++
					}
					if !textBlockStarted {
						p.sendEvent(w, flusher, "content_block_start", map[string]interface{}{
							"type":  "content_block_start",
							"index": nextBlockIndex,
							"content_block": map[string]string{
								"type": "text",
								"text": "",
							},
						})
						textBlockStarted = true
					}
					p.sendEvent(w, flusher, "content_block_delta", map[string]interface{}{
						"type":  "content_block_delta",
						"index": nextBlockIndex,
						"delta": map[string]string{
							"type": "text_delta",
							"text": delta.Content,
						},
					})
				}
			}

			// Handle tool calls
			if len(delta.ToolCalls) > 0 {
				if thinkingBlockStarted && !thinkingBlockStopped {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": nextBlockIndex,
					})
					thinkingBlockStopped = true
					nextBlockIndex++
				}

				// Close any open text block before starting tool blocks
				if textBlockStarted {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": nextBlockIndex,
					})
					textBlockStarted = false
					nextBlockIndex++
				}

				for _, tc := range delta.ToolCalls {
					if tc.ID != "" {
						activeToolID = tc.ID
						toolCallsInfo = append(toolCallsInfo, "tool:"+strings.TrimSpace(tc.Function.Name)+":"+tc.ID)
						currentToolBlockIndex = nextBlockIndex
						nextBlockIndex++
						p.sendEvent(w, flusher, "content_block_start", map[string]interface{}{
							"type":  "content_block_start",
							"index": currentToolBlockIndex,
							"content_block": map[string]interface{}{
								"type":  "tool_use",
								"id":    tc.ID,
								"name":  tc.Function.Name,
								"input": map[string]interface{}{},
							},
						})
					}
					if tc.Function.Arguments != "" {
						p.sendEvent(w, flusher, "content_block_delta", map[string]interface{}{
							"type":  "content_block_delta",
							"index": currentToolBlockIndex,
							"delta": map[string]interface{}{
								"type":         "input_json_delta",
								"partial_json": tc.Function.Arguments,
							},
						})
					}
				}
			}

			if choice.FinishReason != "" {
				if thinkingBlockStarted && !thinkingBlockStopped {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": nextBlockIndex,
					})
					thinkingBlockStopped = true
				}

				if activeToolID != "" {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": currentToolBlockIndex,
					})
					activeToolID = ""
				} else if textBlockStarted {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": nextBlockIndex,
					})
				} else if nextBlockIndex > 0 {
					p.sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": nextBlockIndex - 1,
					})
				}

				stopReason := "end_turn"
				if choice.FinishReason == "tool_calls" {
					stopReason = "tool_use"
				}
				p.sendEvent(w, flusher, "message_delta", map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]interface{}{
						"stop_reason":   stopReason,
						"stop_sequence": nil,
					},
					"usage": map[string]int{"output_tokens": 0},
				})
			}
		}
	}

	p.sendEvent(w, flusher, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})

	// Cache reasoning content for future turns
	fullReasoning := fullReasoningBuilder.String()
	if fullReasoning != "" {
		var parts []string
		textStr := fullTextBuilder.String()
		if textStr != "" {
			parts = append(parts, textStr)
		}
		parts = append(parts, toolCallsInfo...)
		assistantText := strings.Join(parts, "|")

		cacheKey := buildResponseCacheKey(anthropicReq, assistantText)
		translator.CacheReasoning(cacheKey, fullReasoning)
	}
}

func (p *Proxy) sendEvent(w io.Writer, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
	flusher.Flush()
}

func (p *Proxy) handleOpenAIStreamingResponse(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		w.Write(line)
		flusher.Flush()
	}
}
