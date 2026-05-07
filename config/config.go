package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Host    string   `yaml:"host"`
		Port    int      `yaml:"port"`
		APIKeys []string `yaml:"api_keys"`
	} `yaml:"server"`
	Providers map[string]Provider       `yaml:"providers"`
	Models    map[string]Model          `yaml:"models"`
	Protocols map[string]ProtocolConfig `yaml:"protocols"`
}

type ProtocolConfig struct {
	Enabled bool       `yaml:"enabled"`
	Auth    AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
	KeyName  string `yaml:"key_name"`
	Location string `yaml:"location"` // e.g., "header"
	Prefix   string `yaml:"prefix"`   // e.g., "Bearer"
}

type Provider struct {
	Protocol string `yaml:"protocol"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Limits   struct {
		RPM int `yaml:"rpm"`
	} `yaml:"limits"`
	Transport struct {
		Timeout   int  `yaml:"timeout"`
		Retries   int  `yaml:"retries"`
		VerifySSL bool `yaml:"verify_ssl"`
	} `yaml:"transport"`
}

type Model struct {
	Routes []Route `yaml:"routes"`
}

type Route struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Weight   int    `yaml:"weight"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand environment variables like ${NVIDIA_API_KEY}
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	err = yaml.Unmarshal([]byte(expanded), &cfg)
	if err != nil {
		return nil, err
	}

	// Set defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 4000
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}

	return &cfg, nil
}
