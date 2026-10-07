package embedded

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSingBoxEmbedded(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "warlink_singbox_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := EnsureSingBoxEmbedded(tempDir); err != nil {
		t.Fatalf("EnsureSingBoxEmbedded failed: %v", err)
	}

	wintunPath := filepath.Join(tempDir, "wintun.dll")
	if fi, err := os.Stat(wintunPath); err != nil || fi.Size() < 100000 {
		t.Errorf("wintun.dll was not properly extracted: %v", err)
	}

	singboxPath := filepath.Join(tempDir, "sing-box.exe")
	if fi, err := os.Stat(singboxPath); err != nil || fi.Size() < 10000000 {
		t.Errorf("sing-box.exe was not properly extracted or decompressed: %v", err)
	}
}
