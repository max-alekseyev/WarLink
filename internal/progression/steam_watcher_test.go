package progression

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSteamScreenshotsDirs(t *testing.T) {
	dirs := FindSteamScreenshotsDirs()
	t.Logf("Found %d Steam WARDOGS screenshot directories: %v", len(dirs), dirs)
	for _, d := range dirs {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("Returned directory does not exist or is not a dir: %s", d)
		}
	}
}

func TestSteamWatcher_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "steam_shots_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	called := false
	watcher := NewSteamWatcher(
		func(res *ProgressionResult, filePath string) {
			called = true
		},
		func(format string, args ...interface{}) {},
	)

	watcher.mu.Lock()
	watcher.dirs = []string{tempDir}
	watcher.mu.Unlock()

	// Initial scan to register empty dir
	watcher.scanOnce()

	// Write an invalid image file
	testFilePath := filepath.Join(tempDir, "test_shot.jpg")
	_ = os.WriteFile(testFilePath, []byte("not an image"), 0644)

	// Scan again - should handle decode error safely without crashing
	watcher.scanOnce()

	if called {
		t.Errorf("Callback should not have been called for invalid image")
	}
}
