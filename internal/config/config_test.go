package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConfigLoadAndDefaultGames(t *testing.T) {
	cfg := Load()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Games) == 0 {
		t.Fatal("expected default games list to be populated")
	}
	g := cfg.GetSelectedGame()
	if g.ID != "wardogs" && g.ID != cfg.SelectedGameID {
		t.Fatalf("unexpected selected game: %v", g)
	}
}

func TestConfigEncapsulationInWarLinkCore(t *testing.T) {
	p := GetConfigPath()
	if !strings.Contains(p, "warlink_core") {
		t.Errorf("expected config path to reside in warlink_core, got %s", p)
	}
	if !strings.HasSuffix(p, "config.json") {
		t.Errorf("expected config path to end with config.json, got %s", p)
	}
}

func TestDefaultGamesExcludesControlAndService(t *testing.T) {
	games := DefaultGames()
	for _, g := range games {
		if g.ID == "wardogs" {
			for _, p := range g.ProcessNames {
				if strings.EqualFold(p, "control.exe") || strings.EqualFold(p, "service.exe") {
					t.Fatalf("DefaultGames() contains forbidden process: %s", p)
				}
			}
		}
	}
}

func TestConfigAtomicSaveAndBackup(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	t.Setenv("WARLINK_CONFIG_PATH", configPath)

	cfg := Load()
	cfg.SelectedAlt = "test_alt_1"
	if err := cfg.Save(); err != nil {
		t.Fatalf("first Save() failed: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	// Update and save again to verify .bak creation
	cfg.SelectedAlt = "test_alt_2"
	if err := cfg.Save(); err != nil {
		t.Fatalf("second Save() failed: %v", err)
	}

	bakPath := filepath.Join(tmpDir, "config.json.bak")
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if !strings.Contains(string(bakData), "test_alt_1") {
		t.Fatalf("expected backup to contain previous alt 'test_alt_1', got %s", string(bakData))
	}
}

func TestConfigSelfHealingFromBackup(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	t.Setenv("WARLINK_CONFIG_PATH", configPath)

	// 1. Initial valid save
	cfg := Load()
	cfg.SetFreeInternet(true)
	cfg.SetSelectedAlt("alt_healed")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Make sure backup exists with valid data
	bakPath := filepath.Join(tmpDir, "config.json.bak")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() for backup failed: %v", err)
	}
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("backup file expected at %s", bakPath)
	}

	// 2. Corrupt config.json by writing broken content
	if err := os.WriteFile(configPath, []byte("{ broken_json: "), 0644); err != nil {
		t.Fatalf("failed to write corrupted config: %v", err)
	}

	// 3. Load should self-heal from config.json.bak
	healedCfg := Load()
	if !healedCfg.IsFreeInternetEnabled() {
		t.Errorf("expected FreeInternetEnabled to be healed from backup (true)")
	}
	if healedCfg.GetSelectedAlt() != "alt_healed" {
		t.Errorf("expected SelectedAlt 'alt_healed', got '%s'", healedCfg.GetSelectedAlt())
	}

	// 4. Verify config.json was restored on disk
	repairedData, err := os.ReadFile(configPath)
	if err != nil || len(repairedData) == 0 || !strings.Contains(string(repairedData), "alt_healed") {
		t.Errorf("config.json was not restored on disk: %s", string(repairedData))
	}
}

func TestConfigConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	t.Setenv("WARLINK_CONFIG_PATH", configPath)

	cfg := Load()
	var wg sync.WaitGroup

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = cfg.GetSelectedGame()
			_ = cfg.IsFreeInternetEnabled()
			_ = cfg.IsGameAutolaunch("wardogs")
			_ = cfg.GetSelectedAlt()
			_ = cfg.GetServerIP()
			_ = cfg.GetGames()
		}(i)
	}

	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cfg.SetFreeInternet(id%2 == 0)
			cfg.UpdateLastPlayed("wardogs")
			cfg.IncrementLaunchCount("wardogs")
			cfg.AddGameProcess("wardogs", "dummy_process.exe")
		}(i)
	}

	wg.Wait()
}
