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
	"wiresock-connect-service.exe",
	"wiresock-client.exe",
	"wiresock.exe",
	"cmdagent.exe",
	"openvpn.exe",
	"amnezia-vpn.exe",
	"clash-verge.exe",
	"nekoray.exe",
	"hiddify.exe",
	"v2ray.exe",
	"xray.exe",
	// Third-party sing-box / clash GUI proxies creating competing TUN interfaces
	"Throne.exe",
	"throne.exe",
	"ThroneCore.exe",
	"thronecore.exe",
	"flclash.exe",
	"mihomo.exe",
	"clash.exe",
	"clash-meta.exe",
	// Third-party game boosters with conflicting WFP/socket drivers
	"gearup_booster.exe",
	"gearup_ball.exe",
	"gearupbooster.exe",
	"exitlag.exe",
	"lagofast.exe",
	"RedShieldVPN.exe",
	"redshieldvpn.exe",
	"RedShield.exe",
	"adguardsvc.exe",
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
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "warp-svc.exe" || lower == "warp-cli.exe" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stopCmd := exec.CommandContext(ctx, "net.exe", "stop", "CloudflareWARP", "/y")
		stopCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = stopCmd.Run()
		scCmd := exec.CommandContext(ctx, "sc.exe", "stop", "CloudflareWARP")
		scCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = scCmd.Run()
	}

	cmd := exec.Command("taskkill", "/F", "/T", "/IM", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd.Run()
}

// StopWinDivertService stops third-party leftover packet filter services (WinDivert / WinDivert14 from external tools).
func StopWinDivertService() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Stop third-party WinDivert services to prevent packet collision
	cmd1 := exec.CommandContext(ctx, "sc.exe", "stop", "WinDivert14")
	cmd1.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd1.Run()
	cmd2 := exec.CommandContext(ctx, "sc.exe", "stop", "WinDivert")
	cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd2.Run()
}

// KillAllConflicts terminates all known conflicting processes and stops conflicting filter services.
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


