package sync

import (
	"cct/config"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func SyncClaudeConfig(cfg *config.Config) error {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home = os.Getenv("HOME")
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Load existing settings if any
	settings := make(map[string]interface{})
	if _, err := os.Stat(settingsPath); err == nil {
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return fmt.Errorf("failed to read settings file: %v", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse settings file: %v", err)
		}
	}

	// Ensure env map exists
	env, ok := settings["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
		settings["env"] = env
	}

	// API Key
	apiKey := ""
	if len(cfg.Server.APIKeys) > 0 {
		apiKey = cfg.Server.APIKeys[0]
	}

	// Host/Port
	host := cfg.Server.Host
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	baseURL := fmt.Sprintf("http://%s:%d", host, cfg.Server.Port)

	// Update env fields
	env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	env["ANTHROPIC_BASE_URL"] = baseURL

	// Collect all available models
	var modelNames []string
	for name := range cfg.Models {
		modelNames = append(modelNames, name)
	}

	if len(modelNames) > 0 {
		firstModel := modelNames[0]
		// Update root model
		settings["model"] = firstModel
		
		// Update env models
		env["ANTHROPIC_MODEL"] = firstModel
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = firstModel
		
		// Distribution
		if len(modelNames) > 1 {
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelNames[1]
		} else {
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = firstModel
		}

		if len(modelNames) > 2 {
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelNames[2]
		} else {
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = firstModel
		}

		// Specific keyword matching (if any)
		for _, name := range modelNames {
			lowerName := strings.ToLower(name)
			if strings.Contains(lowerName, "sonnet") {
				env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = name
			}
			if strings.Contains(lowerName, "haiku") {
				env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = name
			}
			if strings.Contains(lowerName, "opus") {
				env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = name
			}
		}
	}

	// Extra settings from user request
	env["API_TIME0UT_MS"] = "3000000"
	env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"

	// Write back
	updatedData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %v", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %v", err)
	}

	if err := os.WriteFile(settingsPath, updatedData, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %v", err)
	}

	log.Printf("Updated %s with CCT Gateway settings", settingsPath)
	return nil
}
