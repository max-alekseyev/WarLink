package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasZapret(t *testing.T) {
	err := PrepareZapret(func(msg string) {
		t.Log(msg)
	})
	if err != nil {
		t.Fatalf("PrepareZapret failed: %v", err)
	}

	if !HasZapret() {
		t.Fatalf("expected HasZapret() to return true after PrepareZapret")
	}
}

func TestAddGatewayToZapretExclude(t *testing.T) {
	// Test adding initial IP
	ip1 := "198.51.100.1"
	if err := AddGatewayToZapretExclude(ip1); err != nil {
		t.Fatalf("AddGatewayToZapretExclude failed: %v", err)
	}

	zapretDir := GetZapretDir()
	excludePath := filepath.Join(zapretDir, "lists", "ipset-exclude-user.txt")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	if !strings.Contains(string(data), "# warlink-gateway\n"+ip1) {
		t.Errorf("expected %s in exclude file, got: %s", ip1, string(data))
	}

	// Test changing to new IP: old IP should be removed, new added
	ip2 := "198.51.100.2"
	if err := AddGatewayToZapretExclude(ip2); err != nil {
		t.Fatalf("AddGatewayToZapretExclude ip2 failed: %v", err)
	}
	data, err = os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}
	if strings.Contains(string(data), ip1) {
		t.Errorf("expected old IP %s to be replaced, but still found in: %s", ip1, string(data))
	}
	if !strings.Contains(string(data), "# warlink-gateway\n"+ip2) {
		t.Errorf("expected new IP %s in: %s", ip2, string(data))
	}
}
