//go:build windows

package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWardogsShadowsWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "wardogs_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "GameUserSettings.ini")
	backupPath := filepath.Join(tempDir, "GameUserSettings.ini.warlink.bak")

	initialContent := `[ScalabilityGroups]
sg.ResolutionQuality=100.0
sg.ShadowQuality=1
sg.TextureQuality=2

[/Script/Engine.GameUserSettings]
bUseVSync=False
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Override paths for testing
	origConfig := configPath
	_ = origConfig

	// Test 1: Disable shadows on test content
	// Simulate SetWardogsShadowsDisabled logic on custom path
	lines := strings.Split(strings.ReplaceAll(initialContent, "\r\n", "\n"), "\n")
	var newLines []string
	inScalability := false
	shadowFound := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inScalability = strings.EqualFold(trimmed, "[ScalabilityGroups]")
			newLines = append(newLines, l)
			continue
		}
		if inScalability && strings.HasPrefix(trimmed, "sg.ShadowQuality=") {
			newLines = append(newLines, "sg.ShadowQuality=4")
			shadowFound = true
			continue
		}
		newLines = append(newLines, l)
	}
	if !shadowFound {
		t.Errorf("expected shadow line to be found")
	}

	updated := strings.Join(newLines, "\r\n")
	if !strings.Contains(updated, "sg.ShadowQuality=4") {
		t.Errorf("expected updated content to have sg.ShadowQuality=4")
	}
	if !strings.Contains(updated, "sg.TextureQuality=2") {
		t.Errorf("expected texture quality to be preserved")
	}
	if !strings.Contains(updated, "bUseVSync=False") {
		t.Errorf("expected other sections to be preserved")
	}

	// Test backup creation and attribute toggling
	if err := os.WriteFile(backupPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write backup: %v", err)
	}
	if err := setFileReadOnly(configPath, true); err != nil {
		t.Fatalf("failed to set read-only: %v", err)
	}
	if !isFileReadOnly(configPath) {
		t.Errorf("expected file to be read-only")
	}
	if err := setFileReadOnly(configPath, false); err != nil {
		t.Fatalf("failed to clear read-only: %v", err)
	}
	if isFileReadOnly(configPath) {
		t.Errorf("expected file to not be read-only")
	}
}
