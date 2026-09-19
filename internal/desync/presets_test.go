package desync

import (
	"strings"
	"testing"
)

func TestBuiltinPresets(t *testing.T) {
	names := GetAvailablePresetNames()
	if len(names) == 0 {
		t.Fatalf("expected non-empty preset names")
	}

	p := GetPreset("general (ALT9)")
	if p == nil {
		t.Fatalf("preset 'general (ALT9)' not found")
	}

	args := p.BuildArgs("C:\\WarLink\\warlink_core")
	if len(args) == 0 {
		t.Fatalf("expected non-empty arguments for ALT9")
	}

	hasLists := false
	for _, a := range args {
		if strings.Contains(a, "C:\\WarLink\\warlink_core\\lists\\") {
			hasLists = true
			break
		}
	}
	if !hasLists {
		t.Errorf("expected expanded lists path in args, got: %v", args)
	}
}

func TestBuildFilteredArgs(t *testing.T) {
	p := GetPreset("general (ALT9)")
	if p == nil {
		t.Fatalf("preset 'general (ALT9)' not found")
	}

	// 1. When freeInternet == false: only game UDP ports and WARP QUIC desync, NO web hostlists for google
	gameArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", false)
	joinedGame := strings.Join(gameArgs, " ")
	if strings.Contains(joinedGame, "list-google.txt") {
		t.Errorf("expected list-google.txt to be omitted when freeInternet is false")
	}
	if !strings.Contains(joinedGame, "50000-50100") {
		t.Errorf("expected game UDP port 50000-50100 in game args")
	}
	if !strings.Contains(joinedGame, "fake_default_quic") {
		t.Errorf("expected quic fake for WARP in game args")
	}

	// 2. When freeInternet == true: all web hostlists and rules are included
	fullArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", true)
	joinedFull := strings.Join(fullArgs, " ")
	if !strings.Contains(joinedFull, "list-google.txt") {
		t.Errorf("expected list-google.txt to be included when freeInternet is true")
	}
	if !strings.Contains(joinedFull, "list-general.txt") {
		t.Errorf("expected list-general.txt to be included when freeInternet is true")
	}
}

