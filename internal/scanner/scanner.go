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

// StopWinDivertService stops the WinDivert driver service if leftover in Windows kernel
func StopWinDivertService() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd1 := exec.CommandContext(ctx, "sc.exe", "stop", "WinDivert")
	cmd1.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd1.Run()

	cmd2 := exec.CommandContext(ctx, "sc.exe", "delete", "WinDivert")
	cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd2.Run()

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

