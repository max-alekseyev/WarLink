package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	AutolaunchGame bool   `json:"autolaunch_game"`
	SelectedAlt    string `json:"selected_alt"`
	LastTested     string `json:"last_tested"`
}

func GetConfigPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exePath), "config.json")
}

func Load() *Config {
	cfg := &Config{
		AutolaunchGame: true,
		SelectedAlt:    "",
		LastTested:     "",
	}

	data, err := os.ReadFile(GetConfigPath())
	if err != nil {
		return cfg
	}

	_ = json.Unmarshal(data, cfg)
	return cfg
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetConfigPath(), data, 0644)
}
