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
)

// checkDriversAndServices inspects WinDivert, Wintun and conflicting processes on Windows.
func checkDriversAndServices() []CheckResult {
	results := make([]CheckResult, 0, 4)

	// 1. WinDivert Service & Registry Check
	wdResult := CheckResult{
		ID:       "driver_windivert",
		Category: "driver",
		Title:    "Служба WinDivert (Сетевой фильтр)",
		Status:   "ok",
		Message:  "Служба и драйвер в штатном состоянии",
	}

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\WinDivert`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		startVal, _, errVal := key.GetIntegerValue("Start")
		if errVal == nil && startVal == 4 {
			wdResult.Status = "error"
			wdResult.Message = "Служба WinDivert отключена антивирусом или сторонней утилитой (Start=4)"
			wdResult.Detail = "Требуется восстановление штатного запуска драйвера"
			wdResult.CanAutoFix = true
		}

		imagePath, _, errPath := key.GetStringValue("ImagePath")
		if errPath == nil && imagePath != "" {
			coreDir := strings.ToLower(filepath.Clean(deps.GetCoreDir()))
			cleanPath := strings.ToLower(imagePath)
			cleanPath = strings.TrimPrefix(cleanPath, `\??\`)
			cleanPath = strings.TrimPrefix(cleanPath, `\\?\`)
			cleanPath = filepath.Clean(cleanPath)
			if !strings.Contains(cleanPath, coreDir) && !strings.Contains(cleanPath, "warlink_core") && !strings.Contains(cleanPath, "system32\\drivers") {
				wdResult.Status = "warning"
				wdResult.Message = "Обнаружен путь драйвера WinDivert от сторонней утилиты"
				wdResult.Detail = "Служба WinDivert указывает на каталог сторонней программы"
				wdResult.CanAutoFix = true
			}
		}
	}
	results = append(results, wdResult)

	// Check third-party packet filter drivers that conflict with WinDivert/Wintun
	conflictDrivers := []struct {
		svc  string
		name string
	}{
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
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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

	// 1. Terminate conflicting standalone DPI tools
	dpiProcesses := []string{"goodbyedpi.exe", "ciadpi.exe", "byedpi.exe", "spoof-dpi.exe", "spoofdpi.exe"}
	for _, proc := range dpiProcesses {
		killCmd := exec.Command("taskkill", "/F", "/IM", proc)
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := killCmd.Run(); err == nil {
			actions = append(actions, fmt.Sprintf("Остановлен конфликтующий процесс: %s", proc))
		}
	}

	// 2. Heal WinDivert service registry
	deps.HealWinDivertService(nil)
	actions = append(actions, "Служба WinDivert проверена и восстановлена")

	// 3. Cleanup zombie Wintun interface if stuck
	cmdWintun := exec.Command("netsh", "interface", "delete", "interface", "WarLink-Tun")
	cmdWintun.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmdWintun.Run()
	actions = append(actions, "Сетевой адаптер WarLink-Tun сброшен")

	// 4. Reset loopback proxy
	_ = deps.ResetLoopbackProxy(nil)
	actions = append(actions, "Настройки системного прокси сброшены на прямое подключение")

	return actions
}
