package embedded

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCoreFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "warlink_embed_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if err := EnsureCoreFiles(tempDir); err != nil {
		t.Fatalf("EnsureCoreFiles failed: %v", err)
	}

	winwsPath := filepath.Join(tempDir, "bin", "winws2.exe")
	if _, err := os.Stat(winwsPath); err != nil {
		t.Errorf("winws2.exe was not extracted: %v", err)
	}

	cygwinPath := filepath.Join(tempDir, "bin", "cygwin1.dll")
	if _, err := os.Stat(cygwinPath); err != nil {
		t.Errorf("cygwin1.dll was not extracted: %v", err)
	}

	divertPath := filepath.Join(tempDir, "bin", "WinDivert.dll")
	if _, err := os.Stat(divertPath); err != nil {
		t.Errorf("WinDivert.dll was not extracted: %v", err)
	}

	luaPath := filepath.Join(tempDir, "lua", "zapret-lib.lua")
	if _, err := os.Stat(luaPath); err != nil {
		t.Errorf("zapret-lib.lua was not extracted: %v", err)
	}

	listPath := filepath.Join(tempDir, "lists", "list-general.txt")
	if _, err := os.Stat(listPath); err != nil {
		t.Errorf("list-general.txt was not extracted: %v", err)
	}
}
