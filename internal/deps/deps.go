package deps

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"warlink/internal/embedded"
	"warlink/internal/singbox"
)

func GetCoreDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "warlink_core"
	}
	dir := filepath.Dir(exePath)
	base := strings.ToLower(filepath.Base(dir))
	if base == "test" || base == "cmd" || base == "bin" {
		parentDir := filepath.Dir(dir)
		if _, pErr := os.Stat(filepath.Join(parentDir, "warlink_core")); pErr == nil {
			return filepath.Join(parentDir, "warlink_core")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "warlink_core")); err != nil {
		parentDir := filepath.Dir(dir)
		if _, pErr := os.Stat(filepath.Join(parentDir, "warlink_core")); pErr == nil {
			return filepath.Join(parentDir, "warlink_core")
		}
	}
	return filepath.Join(dir, "warlink_core")
}

func EnsureCoreDir() error {
	return os.MkdirAll(GetCoreDir(), 0755)
}

func GetZapretDir() string {
	return filepath.Join(GetCoreDir(), "zapret")
}

func HasZapret() bool {
	zapretDir := GetZapretDir()
	binFile := filepath.Join(zapretDir, "bin", "winws2.exe")
	divertFile := filepath.Join(zapretDir, "bin", "WinDivert64.sys")
	luaFile := filepath.Join(zapretDir, "lua", "zapret-lib.lua")
	if _, err := os.Stat(binFile); err == nil {
		if _, err := os.Stat(divertFile); err == nil {
			if _, err := os.Stat(luaFile); err == nil {
				return true
			}
		}
	}
	return false
}


// RestoreIpsetIfNeeded checks if ipset-all.txt is corrupted/too small (like the 18-byte stub) and restores it.
// Also ensures all required user list files exist so winws.exe doesn't abort.
func RestoreIpsetIfNeeded(logFn func(string)) bool {
	zapretDir := GetZapretDir()
	listsDir := filepath.Join(zapretDir, "lists")
	_ = os.MkdirAll(listsDir, 0755)

	// Ensure user lists exist (fresh download from Flowseal doesn't include user files until service.bat runs)
	userFiles := map[string]string{
		"ipset-exclude-user.txt": "203.0.113.113/32\n",
		"list-general-user.txt":  "domain.example.abc\n",
		"list-exclude-user.txt":  "domain.example.abc\n",
	}
	for fname, defaultContent := range userFiles {
		fpath := filepath.Join(listsDir, fname)
		if _, err := os.Stat(fpath); os.IsNotExist(err) {
			_ = os.WriteFile(fpath, []byte(defaultContent), 0644)
			if logFn != nil {
				logFn(fmt.Sprintf("[OK] Создан обязательный список: %s", fname))
			}
		}
	}

	ipsetPath := filepath.Join(listsDir, "ipset-all.txt")
	info, err := os.Stat(ipsetPath)
	if err != nil || info.Size() < 10000 { // less than 10KB means corrupted or stub
		if data, readErr := embedded.AssetsFS.ReadFile("assets/lists/ipset-all.txt"); readErr == nil {
			if logFn != nil {
				logFn("[WARN] Обнаружен повреждённый ipset-all.txt. Восстановление из встроенных ресурсов...")
			}
			if writeErr := os.WriteFile(ipsetPath, data, 0644); writeErr == nil {
				if logFn != nil {
					logFn(fmt.Sprintf("[OK] ipset-all.txt успешно восстановлен (%d байт)", len(data)))
				}
				return true
			}
		}
	}
	return false
}

// AddGatewayToZapretExclude ensures ONLY the active Stockholm gateway IP is placed in ipset-exclude-user.txt
// so that WinDivert and winws never intercept or desync Hysteria UDP tunnel traffic.
// Any previous / stale WarLink gateway IPs are safely replaced.
func AddGatewayToZapretExclude(serverIP string) error {
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		return nil
	}
	if strings.Contains(serverIP, ":") {
		serverIP = strings.Split(serverIP, ":")[0]
	}
	zapretDir := GetZapretDir()
	listsDir := filepath.Join(zapretDir, "lists")
	_ = os.MkdirAll(listsDir, 0755)
	excludePath := filepath.Join(listsDir, "ipset-exclude-user.txt")

	content, err := os.ReadFile(excludePath)
	var preservedLines []string
	if err == nil {
		isNextGateway := false
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if trimmed == "# warlink-gateway" {
				isNextGateway = true
				continue
			}
			if isNextGateway {
				isNextGateway = false
				continue
			}
			if strings.Contains(trimmed, "# warlink-gateway") {
				continue
			}
			if trimmed == serverIP || trimmed == serverIP+"/32" {
				continue
			}
			preservedLines = append(preservedLines, trimmed)
		}
	}
	preservedLines = append(preservedLines, "# warlink-gateway", serverIP)
	newContent := strings.Join(preservedLines, "\n") + "\n"
	return os.WriteFile(excludePath, []byte(newContent), 0644)
}

// SanitizeStartupAndNetwork ensures any broken local loopback proxy (ProxyEnable=1 with 127.0.0.1)
// left by third-party VPN crashes is safely disabled.
func SanitizeStartupAndNetwork(logFn func(string)) {

	// 2. Check for broken loopback system proxy
	checkProxy := exec.Command("reg", "query", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable")
	checkProxy.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := checkProxy.CombinedOutput()
	if err == nil && strings.Contains(string(out), "0x1") {
		checkServer := exec.Command("reg", "query", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyServer")
		checkServer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		outServer, errS := checkServer.CombinedOutput()
		if errS == nil {
			strServer := strings.ToLower(string(outServer))
			if strings.Contains(strServer, "127.0.0.1") || strings.Contains(strServer, "localhost") {
				if logFn != nil {
					logFn("[WARN] Обнаружен зависший локальный системный прокси (ProxyEnable=1). Автоматический сброс...")
				}
				fixProxy := exec.Command("reg", "add", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f")
				fixProxy.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
				_ = fixProxy.Run()
				if logFn != nil {
					logFn("[OK] Системный прокси успешно сброшен на прямое подключение")
				}
			}
		}
	}
}

// CheckInternetConnection tests if basic internet/DNS is reachable.
func CheckInternetConnection() bool {
	client := &http.Client{Timeout: 2500 * time.Millisecond}
	resp, err := client.Get("http://1.1.1.1")
	if err == nil && resp != nil {
		_ = resp.Body.Close()
		return true
	}
	resp2, err2 := client.Get("http://8.8.8.8")
	if err2 == nil && resp2 != nil {
		_ = resp2.Body.Close()
		return true
	}
	return false
}

// PrepareZapret ensures DPI desync components are unpacked from embedded assets into warlink_core/zapret.
func PrepareZapret(logFn func(string)) error {
	if HasZapret() {
		if logFn != nil {
			logFn("[OK] Компоненты десинхронизации готовы к работе")
		}
		return nil
	}

	_ = EnsureCoreDir()

	if logFn != nil {
		logFn("[INFO] Извлечение встроенных компонентов сетевого фильтра WinDivert / winws...")
	}
	if err := embedded.EnsureCoreFiles(GetZapretDir()); err != nil {
		return fmt.Errorf("ошибка распаковки компонентов сетевого фильтра: %w", err)
	}

	RestoreIpsetIfNeeded(logFn)

	if logFn != nil {
		logFn("[OK] Сетевой фильтр успешно инициализирован в warlink_core")
	}
	return nil
}

// EnsureSingBoxFiles ensures sing-box.exe and wintun.dll are available in warlink_core/singbox.
func EnsureSingBoxFiles(logFn func(string)) error {
	_ = EnsureCoreDir()
	mgr := singbox.NewManager(GetCoreDir())
	return mgr.EnsureFiles(logFn)
}

