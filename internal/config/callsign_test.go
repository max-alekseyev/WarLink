package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestNobelCallsignFormat(t *testing.T) {
	seeds := []string{
		"7242-4759-5899-5372",
		"4819-9229-7348-7018",
		"8580-6052-9036-3230",
		"7028-8237-5593-3718",
	}

	for _, s := range seeds {
		cs := GenerateDeterministicNobelCallsign(s)
		parts := strings.Fields(cs)
		if len(parts) != 3 {
			t.Fatalf("expected 3 parts in callsign %q, got %d", cs, len(parts))
		}
		num, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("expected 3rd part to be numeric in %q, got err: %v", cs, err)
		}
		if num < 100 || num > 999 {
			t.Fatalf("expected 3 digits (100..999) in %q, got %d", cs, num)
		}
	}
}

func TestNobelCallsignNoCollision(t *testing.T) {
	used := make(map[string]bool)
	seed1 := "7242-4759-5899-5372"
	seed2 := "4819-9229-7348-7018"

	c1 := GenerateUniqueNobelCallsign(seed1, used)
	used[c1] = true
	c2 := GenerateUniqueNobelCallsign(seed2, used)
	used[c2] = true

	if c1 == c2 {
		t.Fatalf("collision detected between seed1 and seed2: %q == %q", c1, c2)
	}

	// Force collision test
	forcedCandidate := c1
	c3 := GenerateUniqueNobelCallsign(seed1, used)
	if c3 == forcedCandidate {
		t.Fatalf("GenerateUniqueNobelCallsign returned duplicate callsign %q", c3)
	}
}

func TestIsOldTwoWordNobelCallsign(t *testing.T) {
	if !IsOldTwoWordNobelCallsign("Планк Оксфорд") {
		t.Fatalf("expected 'Планк Оксфорд' to be recognized as legacy two-word callsign")
	}
	if IsOldTwoWordNobelCallsign("Планк Оксфорд 419") {
		t.Fatalf("did not expect 3-word callsign with digits to be recognized as legacy")
	}
	if IsOldTwoWordNobelCallsign("Torrento") {
		t.Fatalf("did not expect custom nick 'Torrento' to be recognized as legacy callsign")
	}
}
