package desync

import (
	"testing"
)

func TestBuiltinPresets(t *testing.T) {
	names := GetAvailablePresetNames()
	if len(names) == 0 {
		t.Fatalf("expected non-empty preset names")
	}

	p := GetPreset("Автокалибровка (Circular Adaptive)")
	if p == nil {
		t.Fatalf("preset 'Автокалибровка (Circular Adaptive)' not found")
	}

	args := p.BuildArgs("C:\\WarLink\\warlink_core")
	if args == nil {
		t.Fatalf("expected non-nil args slice")
	}
}

func TestFetchRemoteDesyncConfig(t *testing.T) {
	presets, err := FetchRemoteDesyncConfig("http://127.0.0.1:8081")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(presets) != 0 {
		t.Fatalf("expected empty presets for Hysteria 2 tunnel")
	}
}
