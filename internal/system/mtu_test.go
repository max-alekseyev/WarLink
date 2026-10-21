package system

import (
	"testing"
)

func TestDetectOptimalWintunMTU(t *testing.T) {
	mtu := DetectOptimalWintunMTU()
	if mtu < 1280 || mtu > 1400 {
		t.Fatalf("DetectOptimalWintunMTU returned unexpected MTU: %d (expected 1280..1400)", mtu)
	}
	t.Logf("Detected optimal Wintun MTU: %d", mtu)
}
