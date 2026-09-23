package deps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

func TestJobObject(t *testing.T) {
	if err := InitGlobalJobObject(); err != nil {
		t.Fatalf("InitGlobalJobObject failed: %v", err)
	}

	// Idempotency check
	if err := InitGlobalJobObject(); err != nil {
		t.Fatalf("Second InitGlobalJobObject failed: %v", err)
	}

	cmd := exec.Command("cmd.exe", "/c", "ping", "-n", "10", "127.0.0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start dummy process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if err := AssignProcessToJob(cmd.Process.Pid); err != nil {
		t.Fatalf("AssignProcessToJob failed: %v", err)
	}
}

func TestFlushDNSResolverCache(t *testing.T) {
	if err := FlushDNSResolverCache(); err != nil {
		t.Fatalf("FlushDNSResolverCache failed: %v", err)
	}
}

func TestCleanupZombieWintunAdapter(t *testing.T) {
	var logs []string
	CleanupZombieWintunAdapter(func(msg string) {
		logs = append(logs, msg)
	})
	if len(logs) == 0 {
		t.Log("CleanupZombieWintunAdapter completed with no logs")
	}
}

func TestResetLoopbackProxy(t *testing.T) {
	var logs []string
	if err := ResetLoopbackProxy(func(msg string) {
		logs = append(logs, msg)
	}); err != nil {
		t.Fatalf("ResetLoopbackProxy failed: %v", err)
	}
}

func TestRestoreWindowsNetworkStack(t *testing.T) {
	var logs []string
	RestoreWindowsNetworkStack(func(msg string) {
		logs = append(logs, msg)
	})
	if len(logs) == 0 {
		t.Errorf("expected logs from RestoreWindowsNetworkStack")
	}
}
