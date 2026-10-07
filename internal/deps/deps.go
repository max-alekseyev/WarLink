package deps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
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
		if cwd, cErr := os.Getwd(); cErr == nil {
			if _, wErr := os.Stat(filepath.Join(cwd, "warlink_core")); wErr == nil {
				return filepath.Join(cwd, "warlink_core")
			}
			parentCwd := filepath.Dir(cwd)
			if _, pwErr := os.Stat(filepath.Join(parentCwd, "warlink_core")); pwErr == nil {
				return filepath.Join(parentCwd, "warlink_core")
			}
		}
	}
	return filepath.Join(dir, "warlink_core")
}

func EnsureCoreDir() error {
	return os.MkdirAll(GetCoreDir(), 0755)
}

func GetLogsDir() string {
	return filepath.Join(GetCoreDir(), "logs")
}

func EnsureLogsDir() error {
	return os.MkdirAll(GetLogsDir(), 0755)
}

const (
	maxLogFileSize = 15 * 1024 * 1024 // 15 MB cap
	keepLogTail    = 10 * 1024 * 1024 // keep last 10 MB on cap
	logMaxAge      = 7 * 24 * time.Hour
)

// capFileSize ensures a log file does not exceed maxLogFileSize. If it does, keeps only the last keepLogTail bytes.
func capFileSize(filePath string) {
	fi, err := os.Stat(filePath)
	if err != nil || fi.Size() <= maxLogFileSize {
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer f.Close()

	seekPos := fi.Size() - keepLogTail
	if _, err := f.Seek(seekPos, io.SeekStart); err != nil {
		return
	}

	buf, err := io.ReadAll(f)
	if err != nil {
		return
	}
	_ = f.Close()

	_ = os.WriteFile(filePath, buf, 0644)
}

// RotateLogs performs two-session rotation for warlink.log (warlink.log -> warlink.prev.log),
// caps existing logs to 15 MB, migrates legacy logs, and cleans files older than 7 days.
func RotateLogs() error {
	_ = EnsureLogsDir()
	logsDir := GetLogsDir()

	// 1. Two-session rotation: warlink.log -> warlink.prev.log
	currLog := filepath.Join(logsDir, "warlink.log")
	prevLog := filepath.Join(logsDir, "warlink.prev.log")
	if _, err := os.Stat(currLog); err == nil {
		_ = os.Remove(prevLog)
		_ = os.Rename(currLog, prevLog)
	}

	// 2. Migrate legacy warlink_core/warlink.log if present
	legacyCoreLog := filepath.Join(GetCoreDir(), "warlink.log")
	if _, err := os.Stat(legacyCoreLog); err == nil {
		if _, errPrev := os.Stat(prevLog); errPrev != nil {
			_ = os.Rename(legacyCoreLog, prevLog)
		} else {
			_ = os.Remove(legacyCoreLog)
		}
	}

	// 3. Migrate & cap legacy singbox.log if in old dir
	oldSbLog := filepath.Join(GetCoreDir(), "singbox", "singbox.log")
	newSbLog := filepath.Join(logsDir, "singbox.log")
	if _, err := os.Stat(oldSbLog); err == nil {
		if _, errNew := os.Stat(newSbLog); errNew != nil {
			_ = os.Rename(oldSbLog, newSbLog)
		} else {
			_ = os.Remove(oldSbLog)
		}
	}

	// 4. Clean legacy winws2.log if present
	_ = os.Remove(filepath.Join(logsDir, "winws2.log"))

	// 5. Cap sizes of active logs
	logFiles := []string{
		filepath.Join(logsDir, "singbox.log"),
		filepath.Join(logsDir, "game.log"),
		filepath.Join(logsDir, "warlink.prev.log"),
	}
	for _, lf := range logFiles {
		capFileSize(lf)
	}

	// 5. TTL Cleanup: remove temp or dump files older than 7 days
	entries, err := os.ReadDir(logsDir)
	if err == nil {
		cutoff := time.Now().Add(-logMaxAge)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := strings.ToLower(e.Name())
			if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".dmp") || strings.HasSuffix(name, ".bak") {
				if info, iErr := e.Info(); iErr == nil && info.ModTime().Before(cutoff) {
					_ = os.Remove(filepath.Join(logsDir, e.Name()))
				}
			}
		}
	}

	return nil
}

// FetchWardogsGameLog locates WARDOGS game logs and copies the latest events to warlink_core/logs/game.log.
func FetchWardogsGameLog() error {
	_ = EnsureLogsDir()
	destLog := filepath.Join(GetLogsDir(), "game.log")

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return nil
	}

	candidates := []string{
		filepath.Join(localAppData, "Wardogs", "Saved", "Logs", "Wardogs.log"),
		filepath.Join(localAppData, "WardogsGame", "Saved", "Logs", "WardogsGame.log"),
	}

	var foundSource string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.Size() > 0 {
			foundSource = c
			break
		}
	}

	if foundSource == "" {
		return nil
	}

	fi, err := os.Stat(foundSource)
	if err != nil {
		return err
	}

	f, err := os.Open(foundSource)
	if err != nil {
		return err
	}
	defer f.Close()

	readStart := int64(0)
	if fi.Size() > keepLogTail {
		readStart = fi.Size() - keepLogTail
	}
	if _, err := f.Seek(readStart, io.SeekStart); err != nil {
		return err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	return os.WriteFile(destLog, data, 0644)
}

// ResetLoopbackProxy checks if a broken local loopback proxy (ProxyEnable=1 with 127.0.0.1 or localhost)
// was left behind by third-party VPN crashes, and resets it using the Windows registry API.
func ResetLoopbackProxy(logFn func(string)) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	proxyEnable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || proxyEnable != 1 {
		return nil
	}

	proxyServer, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return nil
	}

	strServer := strings.ToLower(proxyServer)
	if strings.Contains(strServer, "127.0.0.1") || strings.Contains(strServer, "localhost") {
		if logFn != nil {
			logFn("[WARN] Обнаружен зависший локальный системный прокси (ProxyEnable=1). Автоматический сброс...")
		}
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			return fmt.Errorf("failed to reset ProxyEnable: %w", err)
		}
		if logFn != nil {
			logFn("[OK] Системный прокси успешно сброшен на прямое подключение")
		}
	}
	return nil
}

// PurgeLegacyZapretArtifacts automatically purges any legacy winws, zapret, or WinDivert
// files, services, drivers, and directories left behind by older versions of WarLink.
func PurgeLegacyZapretArtifacts(logFn func(string)) {
	coreDir := GetCoreDir()
	logsDir := GetLogsDir()

	// 1. Stop and remove leftover WinDivert driver services
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, svc := range []string{"WinDivert", "WinDivert14"} {
		cStop := exec.CommandContext(ctx, "sc.exe", "stop", svc)
		cStop.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cStop.Run()

		cDel := exec.CommandContext(ctx, "sc.exe", "delete", svc)
		cDel.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cDel.Run()
	}

	// 2. Kill any stale winws/zapret processes
	for _, proc := range []string{"winws.exe", "winws2.exe", "zapret.exe"} {
		cKill := exec.CommandContext(ctx, "taskkill.exe", "/F", "/IM", proc)
		cKill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cKill.Run()
	}

	candidateCoreDirs := []string{coreDir}
	if cwd, err := os.Getwd(); err == nil {
		d := cwd
		for i := 0; i < 4; i++ {
			cCore := filepath.Join(d, "warlink_core")
			found := false
			for _, existing := range candidateCoreDirs {
				if strings.EqualFold(existing, cCore) {
					found = true
					break
				}
			}
			if !found {
				candidateCoreDirs = append(candidateCoreDirs, cCore)
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}

	removedAny := false

	// 3. Remove legacy files and directories from candidate core dirs
	for _, cDir := range candidateCoreDirs {
		legacyFiles := []string{
			filepath.Join(cDir, "winws.exe"),
			filepath.Join(cDir, "winws2.exe"),
			filepath.Join(cDir, "zapret.exe"),
			filepath.Join(cDir, "WinDivert.dll"),
			filepath.Join(cDir, "WinDivert64.sys"),
			filepath.Join(cDir, "WinDivert32.sys"),
			filepath.Join(cDir, "hosts.txt"),
			filepath.Join(cDir, "autohosts.txt"),
			filepath.Join(cDir, "ipset.txt"),
			filepath.Join(cDir, "zapret-discord.bat"),
			filepath.Join(cDir, "zapret-general.bat"),
			filepath.Join(cDir, "service_install.bat"),
			filepath.Join(cDir, "service_remove.bat"),
			filepath.Join(cDir, "zapret-discord.log"),
			filepath.Join(cDir, "winws2.log"),
			filepath.Join(cDir, "windivert.log"),
			filepath.Join(logsDir, "winws2.log"),
			filepath.Join(logsDir, "windivert.log"),
			filepath.Join(logsDir, "zapret.log"),
		}
		for _, f := range legacyFiles {
			if _, err := os.Stat(f); err == nil {
				if rErr := os.Remove(f); rErr == nil {
					removedAny = true
				}
			}
		}

		legacyDirs := []string{
			filepath.Join(cDir, "zapret"),
			filepath.Join(cDir, "zapret2"),
			filepath.Join(cDir, "lists"),
			filepath.Join(cDir, "bin"),
			filepath.Join(cDir, "lua"),
		}
		for _, d := range legacyDirs {
			if _, err := os.Stat(d); err == nil {
				if rErr := os.RemoveAll(d); rErr == nil {
					removedAny = true
				}
			}
		}
	}

	// Also clean up leftover previous executable if updated in-place (.old)
	if exe, err := os.Executable(); err == nil {
		oldExe := exe + ".old"
		if _, err := os.Stat(oldExe); err == nil {
			if rErr := os.Remove(oldExe); rErr == nil {
				removedAny = true
			}
		}
	}

	if removedAny && logFn != nil {
		logFn("[OK] Устаревшие компоненты WinDivert/Zapret удалены (система очищена)")
	}
}

// SanitizeStartupAndNetwork ensures any broken local loopback proxy is safely reset
// and legacy DPI bypass artifacts are completely purged.
func SanitizeStartupAndNetwork(logFn func(string)) {
	_ = ResetLoopbackProxy(logFn)
	PurgeLegacyZapretArtifacts(logFn)
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

// EnsureSingBoxFiles ensures sing-box.exe and wintun.dll are available in warlink_core/singbox.
func EnsureSingBoxFiles(logFn func(string)) error {
	_ = EnsureCoreDir()
	mgr := singbox.NewManager(GetCoreDir())
	return mgr.EnsureFiles(logFn)
}

