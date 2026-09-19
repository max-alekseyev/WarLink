package config

import (
	"strings"
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

