//go:build windows

package troubleshooter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
	"warlink/internal/deps"
	"warlink/internal/scanner"
)

// checkDriversAndServices inspects Wintun, sing-box and conflicting processes on Windows.
func checkDriversAndServices() []CheckResult {
	results := make([]CheckResult, 0, 4)

	// 1. Sing-box router binary integrity
	sbResult := CheckResult{
		ID:       "singbox_binary",
		Category: "driver",
		Title:    "Игровой сетевой роутер sing-box",
		Status:   "ok",
		Message:  "Исполняемый файл роутера готов к работе",
	}
	sbPath := filepath.Join(deps.GetCoreDir(), "singbox", "sing-box.exe")
	if _, errStat := os.Stat(sbPath); errStat != nil {
		fallbackSb := filepath.Join(deps.GetCoreDir(), "sing-box.exe")
		if _, errFb := os.Stat(fallbackSb); errFb != nil {
			sbResult.Status = "warning"
			sbResult.Message = "Исполняемый файл sing-box будет распакован при запуске"
			sbResult.CanAutoFix = true
		}
	}
	results = append(results, sbResult)

	// Check third-party packet filter drivers that conflict with Wintun/TUN routing
	conflictDrivers := []struct {
		svc  string
		name string
	}{
		{"WinDivert", "WinDivert Packet Filter"},
		{"WinDivert14", "WinDivert 1.4"},
		{"npcap", "Npcap Packet Filter"},
		{"npf", "WinPcap Packet Filter"},
		{"AdguardDriver", "AdGuard WFP Network Filter"},
		{"AdguardWfp", "AdGuard WFP Network Filter"},
		{"cFosSpeed", "cFosSpeed Net Shaper"},
	}
	for _, cd := range conflictDrivers {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+cd.svc, registry.QUERY_VALUE); err == nil {
			k.Close()
			results = append(results, CheckResult{
				ID:       "driver_" + strings.ToLower(cd.svc),
				Category: "driver",
				Title:    "Сторонний сетевой драйвер: " + cd.name,
				Status:   "warning",
				Message:  "Обнаружен сторонний драйвер перехвата пакетов",
				Detail:   "Может вызывать задержки и разрывы игровых UDP-сессий",
				CanAutoFix: false,
			})
		}
	}

	// 2. Conflicting 3rd-party DPI/filtering processes
	procResult := CheckResult{
		ID:       "conflicting_apps",
		Category: "driver",
		Title:    "Конфликтующие сетевые процессы",
		Status:   "ok",
		Message:  "Конфликтующие сетевые службы не обнаружены",
	}

	conflicts := findConflictingProcesses()
	if len(conflicts) > 0 {
		procResult.Status = "warning"
		procResult.Message = fmt.Sprintf("Обнаружены конфликтующие программы: %s", strings.Join(conflicts, ", "))
		procResult.Detail = "Параллельная работа сторонних VPN/TUN или DPI-утилит перехватывает сетевой стек и вызывает сбои"
		procResult.CanAutoFix = true
	}
	results = append(results, procResult)

	// 3. Wintun Adapter & Files Integrity
	wintunResult := CheckResult{
		ID:       "wintun_adapter",
		Category: "driver",
		Title:    "Виртуальный сетевой адаптер Wintun",
		Status:   "ok",
		Message:  "Драйвер Wintun готов к туннелированию",
	}

	wintunPath := filepath.Join(deps.GetCoreDir(), "singbox", "wintun.dll")
	if _, errStat := os.Stat(wintunPath); errStat != nil {
		// Fallback check root of core
		fallbackPath := filepath.Join(deps.GetCoreDir(), "wintun.dll")
		if _, errFb := os.Stat(fallbackPath); errFb != nil {
			wintunResult.Status = "warning"
			wintunResult.Message = "Файл wintun.dll будет распакован при первом подключении"
			wintunResult.CanAutoFix = true
		}
	}
	results = append(results, wintunResult)

	return results
}

// findConflictingProcesses checks if any third-party DPI or network filtering utilities are running.
func findConflictingProcesses() []string {
	cmd := exec.Command("tasklist", "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(out), "\n")
	foundMap := make(map[string]bool)
	found := make([]string, 0)

	targets := map[string]string{
		// DPI bypass utilities (WinDivert/pcap conflicts)
		"goodbyedpi.exe":           "GoodbyeDPI",
		"zapret.exe":               "Zapret (сторонний)",
		"byedpi.exe":               "ByeDPI",
		"ciadpi.exe":               "ByeDPI (ciadpi)",
		"spoof-dpi.exe":            "SpoofDPI",
		"spoofdpi.exe":             "SpoofDPI",
		"green-tunnel.exe":         "Green Tunnel",
		"greentunnel.exe":          "Green Tunnel",
		"flowseal.exe":             "Flowseal Zapret",
		"power-tunnel.exe":         "PowerTunnel",
		"powertunnel.exe":          "PowerTunnel",

		// TUN/VPN proxies that intercept 0.0.0.0/0, produce virtual 0ms pings, or take over routing
		"throne.exe":               "Throne (активен TUN)",
		"thronecore.exe":           "Throne Core",
		"clash.exe":                "Clash",
		"clash-verge.exe":          "Clash Verge",
		"clash-meta.exe":           "Clash Meta",
		"clash-nyanpasu.exe":       "Clash Nyanpasu",
		"mihomo.exe":               "Mihomo",
		"mihomo-party.exe":         "Mihomo Party",
		"flclash.exe":              "FlClash",
		"hiddify.exe":              "Hiddify",
		"hiddify-cli.exe":          "Hiddify Core",
		"nekoray.exe":              "NekoRay",
		"nekobox.exe":              "NekoBox",
		"v2ray.exe":                "v2ray",
		"v2rayn.exe":               "v2rayN",
		"xray.exe":                 "Xray Core",
		"amneziavpn.exe":           "Amnezia VPN",
		"amneziawg.exe":            "AmneziaWG",
		"wireguard.exe":            "WireGuard",
		"openvpn.exe":              "OpenVPN",
		"openvpnserv.exe":          "OpenVPN Service",
		"tailscale.exe":            "Tailscale",
		"tailscaled.exe":           "Tailscale Service",
		"zerotier-one_x64.exe":     "ZeroTier",
		"zerotier-one.exe":         "ZeroTier",
		"windscribe.exe":           "Windscribe",
		"windscribeservice.exe":    "Windscribe Service",
		"protonvpn.exe":            "ProtonVPN",
		"nordvpn.exe":              "NordVPN",
		"mullvad-vpn.exe":          "Mullvad VPN",
		"warp-svc.exe":             "Cloudflare WARP",
		"cloudflare-warp.exe":      "Cloudflare WARP",
		"outline.exe":              "Outline VPN",
		"proxifier.exe":            "Proxifier",

		// Traffic shapers, game network optimizers and filters
		"killernetworkservice.exe": "Killer Network Service",
		"killerservice.exe":        "Killer Network",
		"kapsmode.exe":             "Killer Network Filter",
		"cfosspeed.exe":            "cFosSpeed",
		"cfosspeed64.exe":          "cFosSpeed",
		"gamefirst.exe":            "ASUS ROG GameFirst",
		"dragoncenter.exe":         "MSI Lan Manager",
		"msicenter.exe":            "MSI Lan Manager",
		"omencommandcenter.exe":    "HP OMEN Network Booster",
		"omennetworkoptimizer.exe": "HP OMEN Network Booster",
		"lenovovantage.exe":        "Lenovo Vantage Network",
		"smartbyte.exe":            "SmartByte Network Service",
		"adguard.exe":              "AdGuard",
		"adguardsvc.exe":           "AdGuard Service",
		"wireshark.exe":            "Wireshark (Npcap)",
		"fiddler.exe":              "Fiddler",
	}

	for _, line := range lines {
		lower := strings.ToLower(line)
		for exe, name := range targets {
			if strings.Contains(lower, `"`+exe+`"`) {
				if !foundMap[name] {
					foundMap[name] = true
					found = append(found, name)
				}
			}
		}
	}

	return found
}

// fixPlatformIssues resolves Windows driver and adapter conflicts.
func fixPlatformIssues() []string {
	actions := make([]string, 0)

	// 1. Terminate conflicting standalone VPN/TUN/DPI processes
	killed, _ := scanner.KillAllConflicts()
	for _, proc := range killed {
		actions = append(actions, fmt.Sprintf("Остановлен конфликтующий процесс: %s", proc))
	}

	// 2. Extra taskkill pass for legacy DPI tools and zombie boosters
	dpiProcesses := []string{"goodbyedpi.exe", "ciadpi.exe", "byedpi.exe", "spoof-dpi.exe", "spoofdpi.exe", "Throne.exe", "ThroneCore.exe"}
	for _, proc := range dpiProcesses {
		killCmd := exec.Command("taskkill", "/F", "/T", "/IM", proc)
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if err := killCmd.Run(); err == nil {
			actions = append(actions, fmt.Sprintf("Завершен процесс: %s", proc))
		}
	}

	// 3. Stop conflicting third-party driver services if running
	scanner.StopWinDivertService()

	// 4. Cleanup zombie Wintun / sing-tun / throne-tun network adapters
	deps.CleanupZombieWintunAdapter(func(msg string) {
		actions = append(actions, msg)
	})

	// 5. Reset loopback proxy
	_ = deps.ResetLoopbackProxy(nil)
	actions = append(actions, "Настройки системного прокси сброшены на прямое подключение")

	return actions
}
