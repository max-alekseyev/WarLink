package engine

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/desync"
)

func TestEngineConcurrencyAndMutexSafety(t *testing.T) {
	cfg := &config.Config{
		AutolaunchGame:      false,
		FreeInternetEnabled: false,
		SelectedGameID:      "wardogs",
	}

	eng := New(cfg, func(msg string) {})

	// Test 1: GetBestAlt default
	if alt := eng.GetBestAlt(); alt != "general (ALT6)" {
		t.Fatalf("expected default general (ALT6), got %s", alt)
	}

	// Test 2: SetSelectedAlt updates safely
	eng.SetSelectedAlt("general (ALT11)")
	if alt := eng.GetBestAlt(); alt != "general (ALT11)" {
		t.Fatalf("expected general (ALT11), got %s", alt)
	}

	// Test 3: Concurrent access to GetProgress, GetBestAlt, SetSelectedAlt, IsConnected
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			_ = eng.GetPipelineProgress()
		}()
		go func() {
			defer wg.Done()
			_ = eng.GetBestAlt()
		}()
		go func() {
			defer wg.Done()
			_ = eng.IsConnected()
		}()
		go func() {
			defer wg.Done()
			eng.setProgress(50, 1, 2, "general (ALT9)", "testing...", true)
		}()
	}
	wg.Wait()

	prog := eng.GetPipelineProgress()
	if prog.Percent != 50 {
		t.Fatalf("expected progress 50, got %d", prog.Percent)
	}
}

func TestEngineProgressLockIndependence(t *testing.T) {
	cfg := &config.Config{
		AutolaunchGame: false,
	}
	eng := New(cfg, func(msg string) {})

	done := make(chan bool)
	go func() {
		// Verify GetPipelineProgress returns immediately and doesn't deadlock
		for i := 0; i < 100; i++ {
			p := eng.GetPipelineProgress()
			_ = p.Percent
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("GetPipelineProgress deadlocked or took too long")
	}
}

func TestCheckWinwsArgs(t *testing.T) {
	zapretDir := filepath.Join(deps.GetCoreDir(), "zapret")
	preset := desync.GetPreset("general (ALT13)")
	if preset == nil {
		t.Fatal("preset general (ALT13) not found")
	}
	argsFree := preset.BuildModularArgs(zapretDir, true)
	argsGame := preset.BuildModularArgs(zapretDir, false)
	t.Logf("Free internet args (%d): %v", len(argsFree), argsFree)
	t.Logf("Game only args (%d): %v", len(argsGame), argsGame)
}
