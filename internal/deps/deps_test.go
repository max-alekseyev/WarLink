package deps

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestEnsureSingBoxFiles(t *testing.T) {
	err := EnsureSingBoxFiles(func(msg string) {
		t.Log(msg)
	})
	if err != nil {
		t.Fatalf("EnsureSingBoxFiles failed: %v", err)
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

func TestPurgeLegacyZapretArtifacts(t *testing.T) {
	var logs []string
	PurgeLegacyZapretArtifacts(func(msg string) {
		logs = append(logs, msg)
	})
	// Should run cleanly without panics
}
