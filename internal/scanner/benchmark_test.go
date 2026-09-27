package scanner

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"warlink/internal/desync"
)

func TestFormatResultLine(t *testing.T) {
	res1 := CheckResult{
		Name:    "DiscordMain",
		HTTPOk:  true,
		TLS2Ok:  true,
		TLS3Ok:  true,
		PingMs:  12,
		Success: true,
	}

	line1 := formatResultLine(res1)
	if !strings.Contains(line1, "DiscordMain") {
		t.Errorf("expected DiscordMain in output, got: %s", line1)
	}
	if !strings.Contains(line1, "HTTP:OK") || !strings.Contains(line1, "TLS1.2:OK") || !strings.Contains(line1, "TLS1.3:OK") {
		t.Errorf("expected HTTP and TLS OK in output, got: %s", line1)
	}
	if !strings.Contains(line1, "Ping: 12 ms") {
		t.Errorf("expected 12 ms ping in output, got: %s", line1)
	}

	resDNS := CheckResult{
		Name:    "CloudflareDNS1111",
		PingMs:  3,
		Success: true,
	}
	lineDNS := formatResultLine(resDNS)
	if !strings.Contains(lineDNS, "CloudflareDNS1111") || !strings.Contains(lineDNS, "Ping: 3 ms") {
		t.Errorf("expected DNS name and ping in output, got: %s", lineDNS)
	}
}

func TestBenchmarkTargets_NonEmpty(t *testing.T) {
	if len(BenchmarkTargets) < 15 {
		t.Errorf("expected at least 15 benchmark targets, got %d", len(BenchmarkTargets))
	}
}

func TestPreset_Execution(t *testing.T) {
	zapretDir := filepath.Join("..", "..", "warlink_core", "zapret")
	presets := desync.BuiltinPresets
	if len(presets) == 0 {
		t.Fatal("no builtin presets")
	}
	p := &presets[0]
	t.Logf("Testing preset: %s", p.Name)
	args := p.BuildModularArgs(zapretDir, true)
	t.Logf("Args count: %d", len(args))
	start := time.Now()
	results := probeAllEndpoints(BenchmarkTargets)
	t.Logf("Got %d results in %v", len(results), time.Since(start))
}
