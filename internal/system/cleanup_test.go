package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupLegacyWardogsTweaksLogic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "wardogs_cleanup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "GameUserSettings.ini")
	backupFile := filepath.Join(tempDir, "GameUserSettings.ini.warlink.bak")

	originalContent := "sg.ShadowQuality=3\r\n[ScalabilityGroups]\r\nsg.ViewDistanceQuality=3\r\n"
	tweakedContent := "sg.ShadowQuality=4\r\n[ScalabilityGroups]\r\nsg.ViewDistanceQuality=3\r\n"

	// Case 1: No backup file exists -> configFile should not be modified
	_ = os.WriteFile(configFile, []byte(tweakedContent), 0644)
	if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
		t.Errorf("backup file should not exist initially")
	}

	// Case 2: Backup file exists -> restore original and delete backup
	_ = os.WriteFile(backupFile, []byte(originalContent), 0644)

	bakData, err := os.ReadFile(backupFile)
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	_ = os.WriteFile(configFile, bakData, 0644)
	_ = os.Remove(backupFile)

	restored, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read restored config: %v", err)
	}
	if string(restored) != originalContent {
		t.Errorf("expected restored content %q, got %q", originalContent, string(restored))
	}
	if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
		t.Errorf("expected backup file to be removed after cleanup")
	}
}
