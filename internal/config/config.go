package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	mu                  sync.RWMutex  `json:"-"`
	AutolaunchGame      bool          `json:"autolaunch_game"`
	FreeInternetEnabled bool          `json:"free_internet_enabled"`
	SelectedGameID      string        `json:"selected_game_id"`
	SelectedAlt         string        `json:"selected_alt,omitempty"`
	BenchmarkCompleted  bool          `json:"benchmark_completed,omitempty"`
	ServerIP            string        `json:"server_ip,omitempty"`
	HMACSecret          string        `json:"hmac_secret,omitempty"`
	ObfsPassword        string        `json:"obfs_password,omitempty"`
	Games               []GameProfile `json:"games"`
}

func (c *Config) Lock()    { c.mu.Lock() }
func (c *Config) Unlock()  { c.mu.Unlock() }
func (c *Config) RLock()   { c.mu.RLock() }
func (c *Config) RUnlock() { c.mu.RUnlock() }

func DefaultGames() []GameProfile {
	return []GameProfile{
		{
			ID:           "wardogs",
			Title:        "WARDOGS",
			SteamAppID:   "1867240",
			ProcessNames: []string{
				"WardogsClient-Win64-Shipping.exe",
				"WardogsLauncher-Shipping.exe",
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
	if envPath := os.Getenv("WARLINK_CONFIG_PATH"); envPath != "" {
		_ = os.MkdirAll(filepath.Dir(envPath), 0755)
		return envPath
	}
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
	dir := filepath.Dir(targetPath)
	bakPath := filepath.Join(dir, "config.json.bak")

	data, err := os.ReadFile(targetPath)
	isCorruptedOrEmpty := err != nil || len(strings.TrimSpace(string(data))) == 0
	if !isCorruptedOrEmpty {
		if uErr := json.Unmarshal(data, cfg); uErr != nil {
			isCorruptedOrEmpty = true
		}
	}

	if isCorruptedOrEmpty {
		// Self-healing: if config.json is corrupted or empty, restore from config.json.bak
		bakData, bErr := os.ReadFile(bakPath)
		if bErr == nil && len(strings.TrimSpace(string(bakData))) > 0 {
			backupCfg := &Config{
				AutolaunchGame:      true,
				FreeInternetEnabled: false,
				SelectedGameID:      "wardogs",
				ServerIP:            DefaultServerIP,
				Games:               DefaultGames(),
			}
			if uErr := json.Unmarshal(bakData, backupCfg); uErr == nil {
				cfg = backupCfg
				// Restore config.json from valid backup
				_ = os.WriteFile(targetPath, bakData, 0644)
			}
		}
	}

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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	configPath := GetConfigPath()
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "config_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Create backup copy config.json.bak
	bakPath := filepath.Join(dir, "config.json.bak")
	if existingData, err := os.ReadFile(configPath); err == nil && len(existingData) > 0 {
		_ = os.WriteFile(bakPath, existingData, 0644)
	}

	// Atomic replacement via os.Rename
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(configPath)
		if err2 := os.Rename(tmpPath, configPath); err2 != nil {
			return fmt.Errorf("failed to rename temp config file: %w", err2)
		}
	}

	return nil
}

func (c *Config) GetSelectedGame() GameProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
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
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].LastPlayed = time.Now().Unix()
			c.SelectedGameID = id
			_ = c.saveLocked()
			return
		}
	}
}

func (c *Config) IncrementLaunchCount(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].LaunchCount++
			c.Games[i].LastPlayed = time.Now().Unix()
			c.SelectedGameID = id
			_ = c.saveLocked()
			return
		}
	}
}

func (c *Config) AddGameProcess(gameID, procName string) {
	if procName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, g := range c.Games {
		if g.ID == gameID {
			for _, p := range g.ProcessNames {
				if p == procName {
					return
				}
			}
			c.Games[i].ProcessNames = append(c.Games[i].ProcessNames, procName)
			_ = c.saveLocked()
			return
		}
	}
}

func (c *Config) ToggleGameAutolaunch(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, g := range c.Games {
		if g.ID == id {
			c.Games[i].Autolaunch = !c.Games[i].Autolaunch
			_ = c.saveLocked()
			return c.Games[i].Autolaunch
		}
	}
	return false
}

func (c *Config) IsGameAutolaunch(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, g := range c.Games {
		if g.ID == id {
			return g.Autolaunch
		}
	}
	return false
}

func (c *Config) SetFreeInternet(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.FreeInternetEnabled = enabled
	_ = c.saveLocked()
}

func (c *Config) IsFreeInternetEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.FreeInternetEnabled
}

func (c *Config) SetSelectedGameID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SelectedGameID = id
	_ = c.saveLocked()
}

func (c *Config) GetSelectedGameID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SelectedGameID
}

func (c *Config) SetSelectedAlt(alt string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SelectedAlt = alt
	_ = c.saveLocked()
}

func (c *Config) GetSelectedAlt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SelectedAlt
}

func (c *Config) SetAutolaunchGame(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AutolaunchGame = enabled
	_ = c.saveLocked()
}

func (c *Config) IsAutolaunchGame() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AutolaunchGame
}

func (c *Config) SetBenchmarkCompleted(completed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.BenchmarkCompleted = completed
	_ = c.saveLocked()
}

func (c *Config) IsBenchmarkCompleted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BenchmarkCompleted
}

func (c *Config) SetServerIP(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ServerIP = ip
	_ = c.saveLocked()
}

func (c *Config) GetServerIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ServerIP
}

func (c *Config) GetGames() []GameProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := make([]GameProfile, len(c.Games))
	copy(res, c.Games)
	return res
}
