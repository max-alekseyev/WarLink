//go:build windows

package deps

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	modDnsApi                 = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = modDnsApi.NewProc("DnsFlushResolverCache")
	reWintunID                = regexp.MustCompile(`(?i)SWD\\WINTUN\\[^\s\r\n]+`)
)

// CleanupZombieWintunAdapter removes phantom WarLink-Tun / Wintun interface via pnputil and netsh.
func CleanupZombieWintunAdapter(logFn func(string)) {
	if logFn != nil {
		logFn("[INFO] Очистка остаточного сетевого адаптера WarLink-Tun...")
	}

	// 1. Remove device node via Windows PnP utility (language-invariant regexp)
	cmdEnum := exec.Command("pnputil", "/enum-devices", "/class", "Net")
	cmdEnum.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmdEnum.Output(); err == nil {
		matches := reWintunID.FindAllString(string(out), -1)
		seen := make(map[string]bool)
		for _, devID := range matches {
			devID = strings.TrimSpace(devID)
			if devID == "" || seen[devID] {
				continue
			}
			seen[devID] = true
			cmdRm := exec.Command("pnputil", "/remove-device", devID)
			cmdRm.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = cmdRm.Run()
			if logFn != nil {
				logFn(fmt.Sprintf("[OK] Устройство сетевого адаптера %s удалено", devID))
			}
		}
	}

	// 2. Fallback to netsh for WarLink TUN interface
	for _, iface := range []string{"WarLink-Tun", "throne-tun", "sing-tun"} {
		cmd := exec.Command("netsh", "interface", "delete", "interface", "name="+iface)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cmd.Run()
	}
}

// FlushDNSResolverCache flushes the Windows DNS resolver cache using dnsapi.dll!DnsFlushResolverCache
// or falls back to ipconfig /flushdns.
func FlushDNSResolverCache() error {
	if err := procDnsFlushResolverCache.Find(); err == nil {
		r1, _, _ := procDnsFlushResolverCache.Call()
		if r1 != 0 {
			return nil
		}
	}

	cmd := exec.Command("ipconfig", "/flushdns")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ipconfig /flushdns failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RestoreWindowsNetworkStack orchestrates full network cleanup:
// removing zombie Wintun adapters, flushing DNS resolver cache, and resetting loopback proxy.
func RestoreWindowsNetworkStack(logFn func(string)) {
	if logFn != nil {
		logFn("[INFO] Восстановление сетевого стека Windows...")
	}

	CleanupZombieWintunAdapter(logFn)

	if err := FlushDNSResolverCache(); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[WARN] Ошибка очистки DNS кэша: %v", err))
		}
	} else if logFn != nil {
		logFn("[OK] Кэш DNS успешно очищен")
	}

	if err := ResetLoopbackProxy(logFn); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("[WARN] Ошибка сброса системного прокси: %v", err))
		}
	}
}
