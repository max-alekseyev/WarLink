package watcher

import (
	"strings"
	"testing"
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

