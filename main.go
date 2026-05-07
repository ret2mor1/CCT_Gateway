package main

import (
	"bufio"
	"bytes"
	"cct/config"
	"cct/models"
	"cct/translator"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var cfg *config.Config
var rateLimitChans map[string]chan struct{}

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	port := flag.Int("port", 0, "Port to listen on (overrides config)")
	flag.Parse()

	var err error
	cfg, err = config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}

	// Initialize rate limiters for each provider
	rateLimitChans = make(map[string]chan struct{})
	for name, provider := range cfg.Providers {
		if provider.Limits.RPM > 0 {
			log.Printf("Rate limiting enabled for provider %s: %d RPM", name, provider.Limits.RPM)
			ch := make(chan struct{}, provider.Limits.RPM)
			// Fill the bucket initially
			for i := 0; i < provider.Limits.RPM; i++ {
				ch <- struct{}{}
			}
			rateLimitChans[name] = ch

			// Refill periodically
			go func(pName string, rpm int, c chan struct{}) {
				interval := 60 * 1000 / rpm
				ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
				for range ticker.C {
					select {
					case c <- struct{}{}:
					default:
					}
				}
			}(name, provider.Limits.RPM, ch)
		}
	}

	if p, ok := cfg.Protocols["anthropic-messages"]; ok && p.Enabled {
		http.HandleFunc("/v1/messages", handleMessages)
		log.Printf("Protocol enabled: anthropic-messages -> /v1/messages")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting CCT proxy on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	// Authentication logic based on protocol configuration
	if p, ok := cfg.Protocols["anthropic-messages"]; ok && p.Enabled && len(cfg.Server.APIKeys) > 0 {
		clientKey := extractAPIKey(r, p.Auth)
		authorized := false
		for _, k := range cfg.Server.APIKeys {
			if clientKey == k {
				authorized = true
				break
			}
		}
		// log.Printf("Headers received: %v", r.Header)

		if !authorized {
			log.Printf("Unauthorized anthropic-messages request from %s", r.RemoteAddr)
			// log.Printf("Headers received: %v", r.Header)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
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
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var anthropicReq models.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&anthropicReq); err != nil {
		log.Printf("Failed to decode Anthropic request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Route selection
	modelCfg, ok := cfg.Models[anthropicReq.Model]
	if !ok {
		log.Printf("Model not found in config: %s", anthropicReq.Model)
		http.Error(w, fmt.Sprintf("Model %s not found", anthropicReq.Model), http.StatusNotFound)
		return
	}

	route := selectRoute(modelCfg)
	if route == nil {
		http.Error(w, "No available routes for model", http.StatusServiceUnavailable)
		return
	}

	provider, ok := cfg.Providers[route.Provider]
	if !ok {
		log.Printf("Provider %s not found for model %s", route.Provider, anthropicReq.Model)
		http.Error(w, "Provider configuration error", http.StatusInternalServerError)
		return
	}

	// Rate limiting for the selected provider
	if ch, exists := rateLimitChans[route.Provider]; exists {
		<-ch
	}

	openaiReq := translator.TranslateRequest(&anthropicReq, route.Model)
	reqBody, _ := json.Marshal(openaiReq)

	log.Printf("Proxying request: %s -> %s (via %s)", 
		anthropicReq.Model, route.Model, route.Provider)

	backendURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(provider.BaseURL, "/"))
	req, err := http.NewRequest(http.MethodPost, backendURL, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		http.Error(w, "Failed to create backend request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	// Configure transport based on provider settings
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !provider.Transport.VerifySSL},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(provider.Transport.Timeout) * time.Second,
	}
	if provider.Transport.Timeout == 0 {
		client.Timeout = 120 * time.Second // Default timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Backend request failed: %v", err)
		http.Error(w, "Backend request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Backend error (%s): %s", resp.Status, string(body))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	if anthropicReq.Stream {
		handleStreamingResponse(w, resp)
	} else {
		handleStandardResponse(w, resp)
	}
}

func selectRoute(m config.Model) *config.Route {
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

	rand.Seed(time.Now().UnixNano())
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

func extractAPIKey(r *http.Request, auth config.AuthConfig) string {
	var val string
	if auth.Location == "header" {
		val = r.Header.Get(auth.KeyName)
	}

	if auth.Prefix != "" && strings.HasPrefix(val, auth.Prefix+" ") {
		val = strings.TrimPrefix(val, auth.Prefix+" ")
	}
	return val
}

func handleStandardResponse(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read backend response: %v", err)
		http.Error(w, "Failed to read backend response", http.StatusInternalServerError)
		return
	}
	// log.Printf("Backend response body: %s", string(body))

	var openaiRes models.OpenAIResponse
	if err := json.Unmarshal(body, &openaiRes); err != nil {
		log.Printf("Failed to decode backend response: %v", err)
		http.Error(w, "Failed to decode backend response", http.StatusInternalServerError)
		return
	}

	anthropicRes := translator.TranslateResponse(&openaiRes)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicRes)
}

func handleStreamingResponse(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	sendEvent(w, flusher, "message_start", map[string]interface{}{
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
	var currentBlockIndex int
	var textBlockStarted bool

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
		// log.Printf("Backend chunk: %s", data)

		type Choice struct {
			Delta struct {
				Content   string `json:"content"`
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

			if delta.Content != "" {
				if activeToolID == "" {
					if currentBlockIndex == 0 && !textBlockStarted {
						sendEvent(w, flusher, "content_block_start", map[string]interface{}{
							"type":  "content_block_start",
							"index": 0,
							"content_block": map[string]string{
								"type": "text",
								"text": "",
							},
						})
						textBlockStarted = true
					}
					sendEvent(w, flusher, "content_block_delta", map[string]interface{}{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]string{
							"type": "text_delta",
							"text": delta.Content,
						},
					})
				}
			}

			if len(delta.ToolCalls) > 0 {
				tc := delta.ToolCalls[0]
				if tc.ID != "" {
					activeToolID = tc.ID
					currentBlockIndex++
					sendEvent(w, flusher, "content_block_start", map[string]interface{}{
						"type":  "content_block_start",
						"index": currentBlockIndex,
						"content_block": map[string]interface{}{
							"type": "tool_use",
							"id":   tc.ID,
							"name": tc.Function.Name,
							"input": map[string]interface{}{},
						},
					})
				}
				if tc.Function.Arguments != "" {
					sendEvent(w, flusher, "content_block_delta", map[string]interface{}{
						"type":  "content_block_delta",
						"index": currentBlockIndex,
						"delta": map[string]interface{}{
							"type":             "input_json_delta",
							"partial_json": tc.Function.Arguments,
						},
					})
				}
			}

			if choice.FinishReason != "" {
				if activeToolID != "" {
					sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": currentBlockIndex,
					})
					activeToolID = ""
				} else if currentBlockIndex == 0 {
					sendEvent(w, flusher, "content_block_stop", map[string]interface{}{
						"type":  "content_block_stop",
						"index": 0,
					})
				}

				stopReason := "end_turn"
				if choice.FinishReason == "tool_calls" {
					stopReason = "tool_use"
				}
				sendEvent(w, flusher, "message_delta", map[string]interface{}{
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

	sendEvent(w, flusher, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}

func sendEvent(w io.Writer, flusher http.Flusher, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
	flusher.Flush()
}

