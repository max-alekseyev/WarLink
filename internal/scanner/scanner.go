package scanner

import (
	"bytes"
	"context"
	"encoding/csv"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var ConflictingProcessNames = []string{
	"winws.exe",
	"winws2.exe",
	"goodbyedpi.exe",
	"byedpi.exe",
	"zapret.exe",
	"warp-svc.exe",
	"warp-cli.exe",
	"wireguard.exe",
	"openvpn.exe",
	"amnezia-vpn.exe",
	"clash-verge.exe",
}

// FindConflicts returns a list of conflicting processes currently running.
func FindConflicts() ([]string, error) {
	cmd := exec.Command("tasklist", "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(&out)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	runningMap := make(map[string]bool)
	for _, rec := range records {
		if len(rec) > 0 {
			name := strings.ToLower(strings.TrimSpace(rec[0]))
			runningMap[name] = true
		}
	}

	var found []string
	for _, conflict := range ConflictingProcessNames {
		if runningMap[strings.ToLower(conflict)] {
			found = append(found, conflict)
		}
	}

	return found, nil
}

// KillProcess kills all instances of a process by executable name.
func KillProcess(name string) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/IM", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd.Run()
}

// StopWinDivertService kills third-party leftover drivers (WinDivert14 from other tools).
// IMPORTANT: We must NEVER call "sc delete WinDivert" on our own driver.
// WinDivert.dll dynamically registers and unregisters the kernel driver via CreateFile/CloseHandle.
// Calling sc delete while a handle exists sets DeleteFlag=1 in the registry, which causes
// STATUS_NO_SUCH_DEVICE (Win32 error 433 ERROR_BAD_DEVICE) on the next WinDivertOpen() call,
// breaking all subsequent winws2 starts until reboot. Killing winws2.exe is sufficient — the
// driver handle is released automatically when the process exits.
func StopWinDivertService() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Only clean up third-party WinDivert14 services (e.g. from GoodbyeDPI, etc.)
	cmd3 := exec.CommandContext(ctx, "sc.exe", "stop", "WinDivert14")
	cmd3.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd3.Run()
	cmd4 := exec.CommandContext(ctx, "sc.exe", "delete", "WinDivert14")
	cmd4.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd4.Run()
}

// KillAllConflicts terminates all known conflicting processes and releases WinDivert.
func KillAllConflicts() ([]string, error) {
	conflicts, err := FindConflicts()
	if err != nil {
		return nil, err
	}

	var killed []string
	for _, p := range conflicts {
		_ = KillProcess(p)
		killed = append(killed, p)
	}

	StopWinDivertService()

	return killed, nil
}

// CleanWinDivertDeleteFlag removes the DeleteFlag=1 registry value from the WinDivert service entry.
// When sc delete is called while WinDivert has open handles, Windows sets DeleteFlag=1 which causes
// all subsequent WinDivertOpen() calls to fail with ERROR_BAD_DEVICE (433) until reboot.
// This function must be called from an elevated (Administrator) process to succeed.
// It is a no-op if the flag is not present.
func CleanWinDivertDeleteFlag() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Use reg.exe to delete the DeleteFlag value — pure Go registry access requires golang.org/x/sys/windows.
	cmd := exec.CommandContext(ctx, "reg.exe", "delete",
		`HKLM\SYSTEM\CurrentControlSet\Services\WinDivert`,
		"/v", "DeleteFlag", "/f",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Run()
}
