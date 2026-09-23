//go:build windows

package deps

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modDnsApi                 = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = modDnsApi.NewProc("DnsFlushResolverCache")
)

// CleanupZombieWintunAdapter removes phantom WarLink-Tun wintun interface via netsh.
func CleanupZombieWintunAdapter(logFn func(string)) {
	if logFn != nil {
		logFn("[INFO] Очистка остаточного сетевого адаптера WarLink-Tun...")
	}

	cmd := exec.Command("netsh", "interface", "delete", "interface", "name=WarLink-Tun")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err == nil {
		if logFn != nil {
			logFn("[OK] Сетевой адаптер WarLink-Tun успешно удален")
		}
		return
	} else {
		_ = out
	}

	cmd2 := exec.Command("netsh", "interface", "delete", "interface", "WarLink-Tun")
	cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out2, err2 := cmd2.CombinedOutput(); err2 == nil {
		if logFn != nil {
			logFn("[OK] Сетевой адаптер WarLink-Tun успешно удален")
		}
	} else {
		_ = out2
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

	HealWinDivertService(logFn)
}

// HealWinDivertService detects and repairs disabled or corrupted WinDivert services
// left by external software (such as zapret-discord-youtube or goodbyedpi) that
// set StartType=4 (DISABLED) or point ImagePath to broken directories.
func HealWinDivertService(logFn func(string)) {
	zapretDir := GetZapretDir()
	driverPath := filepath.Join(zapretDir, "bin", "WinDivert64.sys")
	absDriverPath, err := filepath.Abs(driverPath)
	if err != nil {
		absDriverPath = driverPath
	}
	ntDriverPath := `\??\` + absDriverPath

	for _, svcName := range []string{"WinDivert", "WinDivert14"} {
		// 1. Direct Registry inspection and repair (HKLM\SYSTEM\CurrentControlSet\Services\<svcName>)
		regPath := `SYSTEM\CurrentControlSet\Services\` + svcName
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.SET_VALUE|registry.QUERY_VALUE); err == nil {
			startVal, _, errStart := k.GetIntegerValue("Start")
			imgPath, _, _ := k.GetStringValue("ImagePath")

			needsFix := false
			if errStart == nil && startVal == 4 { // SERVICE_DISABLED
				needsFix = true
			}
			if imgPath != "" && !strings.EqualFold(imgPath, ntDriverPath) && !strings.EqualFold(imgPath, absDriverPath) {
				needsFix = true
			}

			if needsFix {
				if logFn != nil {
					logFn(fmt.Sprintf("[WARN] Обнаружена некорректная служба %s (Start=%d, путь: %s). Восстановление...", svcName, startVal, imgPath))
				}
				_ = k.SetDWordValue("Start", 3) // SERVICE_DEMAND_START
				_ = k.SetStringValue("ImagePath", ntDriverPath)
				_ = k.DeleteValue("DeleteFlag")
				if logFn != nil {
					logFn(fmt.Sprintf("[OK] Служба %s восстановлена в реестре: Start=3 (по требованию), путь: %s", svcName, ntDriverPath))
				}

				// Synchronize SCM without stopping or deleting the service (prevents ERROR_BAD_DEVICE / STOP_PENDING)
				if scm, errSCM := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_ALL_ACCESS); errSCM == nil {
					pName, _ := windows.UTF16PtrFromString(svcName)
					if hSvc, errSvc := windows.OpenService(scm, pName, windows.SERVICE_CHANGE_CONFIG); errSvc == nil {
						pBin, _ := windows.UTF16PtrFromString(ntDriverPath)
						_ = windows.ChangeServiceConfig(
							hSvc,
							windows.SERVICE_NO_CHANGE,
							windows.SERVICE_DEMAND_START,
							windows.SERVICE_NO_CHANGE,
							pBin,
							nil,
							nil,
							nil,
							nil,
							nil,
							nil,
						)
						_ = windows.CloseServiceHandle(hSvc)
					}
					_ = windows.CloseServiceHandle(scm)
				}
			}
			k.Close()
		}
	}
}
