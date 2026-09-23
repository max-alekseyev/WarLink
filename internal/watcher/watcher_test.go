package watcher

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeGameToken(t *testing.T) {
	cases := map[string]string{
		"WardogsClient-Win64-Shipping.exe": "wardogs",
		"DeadByDaylight-Win64-Shipping.exe": "deadbydaylight",
		"Helldivers2Client.exe": "helldivers2",
		"cs2.exe": "cs2",
		"WARDOGS": "wardogs",
	}

	for in, expected := range cases {
		out := NormalizeGameToken(in)
		if out != expected {
			t.Errorf("NormalizeGameToken(%q) = %q, want %q", in, out, expected)
		}
	}
}

func TestResolveWardogs(t *testing.T) {
	info := ResolveGameProcessNames("wardogs", "1867240", "WARDOGS", "", nil)
	t.Logf("Resolved processes: %v", info.ProcessNames)

	hasShipping := false
	for _, p := range info.ProcessNames {
		if strings.Contains(strings.ToLower(p), "shipping") {
			hasShipping = true
			break
		}
	}
	if !hasShipping {
		t.Errorf("expected to find a shipping executable for WARDOGS, got %v", info.ProcessNames)
	}
}

func TestSelectBestProcess(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: "",
		},
		{
			name:     "launcher only",
			input:    []string{"WardogsLauncher-Shipping.exe"},
			expected: "WardogsLauncher-Shipping.exe",
		},
		{
			name:     "launcher and client",
			input:    []string{"WardogsLauncher-Shipping.exe", "WardogsClient-Win64-Shipping.exe"},
			expected: "WardogsClient-Win64-Shipping.exe",
		},
		{
			name:     "client first then launcher",
			input:    []string{"WardogsClient-Win64-Shipping.exe", "WardogsLauncher-Shipping.exe"},
			expected: "WardogsClient-Win64-Shipping.exe",
		},
		{
			name:     "generic client over updater",
			input:    []string{"game_update.exe", "game_client.exe"},
			expected: "game_client.exe",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := SelectBestProcess(tc.input)
			if actual != tc.expected {
				t.Errorf("SelectBestProcess(%v) = %q, want %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestTargetInfoCache(t *testing.T) {
	InvalidateTargetCache("")

	info1 := ResolveGameProcessNames("cache-test-game", "", "GameAlpha", "", []string{"proc1.exe"})
	if len(info1.ProcessNames) == 0 {
		t.Fatalf("expected non-empty process names")
	}

	// Should return cached result, ignoring new title/custom paths
	info2 := ResolveGameProcessNames("cache-test-game", "", "GameBeta", "", []string{"proc2.exe"})
	if info2.NormalizedTitle != info1.NormalizedTitle {
		t.Errorf("expected cached title %q, got %q", info1.NormalizedTitle, info2.NormalizedTitle)
	}

	// Invalidate and verify re-resolution
	InvalidateTargetCache("cache-test-game")
	info3 := ResolveGameProcessNames("cache-test-game", "", "GameBeta", "", []string{"proc2.exe"})
	if info3.NormalizedTitle != "gamebeta" {
		t.Errorf("expected updated title %q, got %q", "gamebeta", info3.NormalizedTitle)
	}
}

func TestIsAnyProcessRunningStrictEquality(t *testing.T) {
	// explorer.exe is guaranteed to run in a Windows user desktop session
	running, name := IsAnyProcessRunning([]string{"explorer.exe"}, "")
	if !running {
		t.Skip("explorer.exe is not running in this test environment")
	}
	if !strings.EqualFold(name, "explorer.exe") {
		t.Errorf("expected explorer.exe, got %q", name)
	}

	// Substring "plorer.exe" must NOT match explorer.exe with strict equality
	substringRunning, _ := IsAnyProcessRunning([]string{"plorer.exe"}, "")
	if substringRunning {
		t.Errorf("strict equality failed: 'plorer.exe' substring unexpectedly matched a running process")
	}
}

func TestGameWatcherDeduplication(t *testing.T) {
	// explorer.exe is guaranteed to run in a Windows user desktop session
	running, _ := IsAnyProcessRunning([]string{"explorer.exe"}, "")
	if !running {
		t.Skip("explorer.exe is not running in this test environment")
	}

	var foundCount int
	var mu sync.Mutex

	w := New(
		func() TargetInfo {
			return TargetInfo{
				GameID:          "test-dedup",
				ProcessNames:    []string{"explorer.exe"},
				NormalizedTitle: "explorer",
			}
		},
		nil,
		nil,
		func(gameID, procName string) {
			mu.Lock()
			foundCount++
			mu.Unlock()
		},
	)

	w.Start()
	// Allow multiple ticks (ticker is 1000ms)
	time.Sleep(2200 * time.Millisecond)
	w.Stop()

	mu.Lock()
	count := foundCount
	mu.Unlock()

	// Should have fired strictly once despite multiple ticks
	if count != 1 {
		t.Errorf("expected onProcessFound to be called exactly once, called %d times", count)
	}
}

