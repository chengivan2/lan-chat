package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the application configuration
type Config struct {
	Username     string `json:"username"`
	HistoryOn    bool   `json:"history_on"`
	RoomPassword string `json:"room_password"`
}

// getConfigPath returns the cross-platform path to the config file
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lan-chat.json"), nil
}

// LoadConfig attempts to load the configuration file
func LoadConfig() (*Config, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// Return error, usually means file doesn't exist
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveConfig writes the configuration to disk
func SaveConfig(cfg *Config) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// 0600 permissions since it might contain a plaintext room password
	return os.WriteFile(path, data, 0600)
}
