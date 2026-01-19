package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Service represents a managed I2S service
type Service struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	BaseURL     string `yaml:"base_url"`
	Priority    int    `yaml:"priority"`
}

// Config holds the application configuration
type Config struct {
	APIPort         int       `yaml:"api_port"`
	Services        []Service `yaml:"services"`
	PollIntervalMs  int       `yaml:"poll_interval_ms"`
	DefaultService  string    `yaml:"default_service"`
}

// Default returns the default configuration
func Default() *Config {
	return &Config{
		APIPort:        8090,
		PollIntervalMs: 2000,
		DefaultService: "",
		Services: []Service{
			{
				Name:        "usboveri2s",
				DisplayName: "USB Media Player",
				BaseURL:     "http://localhost:8090",
				Priority:    1,
			},
			{
				Name:        "usbaudio",
				DisplayName: "USB Audio Bridge",
				BaseURL:     "http://localhost:8092",
				Priority:    2,
			},
		},
	}
}

// Load reads configuration from file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
