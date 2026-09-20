package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type GameProfile struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	ExePath      string   `json:"exe_path,omitempty"`
	SteamAppID   string   `json:"steam_app_id,omitempty"`
	ProcessNames []string `json:"process_names,omitempty"`
	PreferredAlt string   `json:"preferred_alt,omitempty"`
	LaunchCount  int      `json:"launch_count"`
	IconURL      string   `json:"icon_url,omitempty"`
	LastPlayed   int64    `json:"last_played"`
	IsDefault    bool     `json:"is_default"`
	Autolaunch   bool     `json:"autolaunch"`
}

var DefaultServerIP = "138.124.103.99"

type Config struct {
	AutolaunchGame      bool          `json:"autolaunch_game"`
	FreeInternetEnabled bool          `json:"free_internet_enabled"`
	SelectedGameID      string        `json:"selected_game_id"`
	SelectedAlt         string        `json:"selected_alt,omitempty"`
	BenchmarkCompleted  bool          `json:"benchmark_completed,omitempty"`
	ServerIP            string        `json:"server_ip,omitempty"`
	Games               []GameProfile `json:"games"`
}

func DefaultGames() []GameProfile {
	return []GameProfile{
		{
			ID:           "wardogs",
			Title:        "WARDOGS",
			SteamAppID:   "1867240",
			ProcessNames: []string{
				"WardogsClient-Win64-Shipping.exe",
				"WardogsLauncher-Shipping.exe",
				"service.exe",
				"control.exe",
				"Elytra-Setup.exe",
			},
			PreferredAlt: "general (ALT13)",
			LaunchCount:  0,
			IconURL:      "/wardogs_icon.png",
			LastPlayed:   time.Now().Unix(),
			IsDefault:    true,
			Autolaunch:   true,
		},
	}
}

func GetConfigPath() string {
	exePath, err := os.Executable()
	if err != nil {
		coreDir := "warlink_core"
		_ = os.MkdirAll(coreDir, 0755)
		return filepath.Join(coreDir, "config.json")
	}
	coreDir := filepath.Join(filepath.Dir(exePath), "warlink_core")
	_ = os.MkdirAll(coreDir, 0755)
	return filepath.Join(coreDir, "config.json")
}

func Load() *Config {
	cfg := &Config{
		AutolaunchGame:      true,
		FreeInternetEnabled: false,
		SelectedGameID:      "wardogs",
		ServerIP:            DefaultServerIP,
		Games:               DefaultGames(),
	}

	targetPath := GetConfigPath()
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return cfg
	}

	_ = json.Unmarshal(data, cfg)

	// Ensure games list is initialized
	if len(cfg.Games) == 0 {
		cfg.Games = DefaultGames()
	} else {
		for i, g := range cfg.Games {
			if g.ID == "wardogs" {
				cfg.Games[i].SteamAppID = "1867240"
				if cfg.Games[i].IconURL == "" {
					cfg.Games[i].IconURL = "https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/1867240/6829090332535af8637c4b6e1dddf3ee8ec3d134.ico"
				}
			}
		}
	}
	if cfg.SelectedGameID == "" {
		cfg.SelectedGameID = cfg.Games[0].ID
	}
	if cfg.ServerIP == "" && DefaultServerIP != "" {
		cfg.ServerIP = DefaultServerIP
	}

	return cfg
}

func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetConfigPath(), data, 0644)
}

func (c *Config) GetSelectedGame() GameProfile {
	for _, g := range c.Games {
		if g.ID == c.SelectedGameID {
			return g
		}
	}
	if len(c.Games) > 0 {
		return c.Games[0]
	}
	return GameProfile{
		ID:         "wardogs",
		Title:      "WARDOGS",
		SteamAppID: "1867240",
		IconURL:    "https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/1867240/6829090332535af8637c4b6e1dddf3ee8ec3d134.ico",
		IsDefault:  true,
	}
}

func (c *Config) UpdateLastPlayed(id string) {
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].LastPlayed = time.Now().Unix()
			c.SelectedGameID = id
			_ = c.Save()
			return
		}
	}
}

func (c *Config) IncrementLaunchCount(id string) {
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].LaunchCount++
			c.Games[i].LastPlayed = time.Now().Unix()
			c.SelectedGameID = id
			_ = c.Save()
			return
		}
	}
}

func (c *Config) AddGameProcess(gameID, procName string) {
	if procName == "" {
		return
	}
	for i, g := range c.Games {
		if g.ID == gameID {
			for _, p := range g.ProcessNames {
				if p == procName {
					return
				}
			}
			c.Games[i].ProcessNames = append(c.Games[i].ProcessNames, procName)
			_ = c.Save()
			return
		}
	}
}

func (c *Config) ToggleGameAutolaunch(id string) bool {
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].Autolaunch = !c.Games[i].Autolaunch
			_ = c.Save()
			return c.Games[i].Autolaunch
		}
	}
	return false
}

func (c *Config) IsGameAutolaunch(id string) bool {
	for _, g := range c.Games {
		if g.ID == id {
			return g.Autolaunch
		}
	}
	return false
}

