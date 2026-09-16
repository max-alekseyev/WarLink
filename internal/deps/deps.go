package deps

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func GetCoreDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "warlink_core"
	}
	return filepath.Join(filepath.Dir(exePath), "warlink_core")
}

func EnsureCoreDir() error {
	return os.MkdirAll(GetCoreDir(), 0755)
}

func GetZapretDir() string {
	return filepath.Join(GetCoreDir(), "zapret")
}

func HasZapret() bool {
	zapretDir := GetZapretDir()
	checkFile := filepath.Join(zapretDir, "service.bat")
	if _, err := os.Stat(checkFile); err == nil {
		return true
	}
	// Check for winws.exe in bin or root
	binFile := filepath.Join(zapretDir, "bin", "winws.exe")
	if _, err := os.Stat(binFile); err == nil {
		return true
	}
	return false
}

func HasWarpInstalled() bool {
	// Check via Service Manager without executing warp-cli
	cmd := exec.Command("sc.exe", "query", "CloudflareWARP")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := cmd.Run(); err == nil {
		return true
	}
	// Fallback to program files path
	targetExe := filepath.Join(os.Getenv("ProgramFiles"), "Cloudflare", "Cloudflare WARP", "warp-cli.exe")
	if _, err := os.Stat(targetExe); err == nil {
		return true
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
	backupPath := filepath.Join(listsDir, "ipset-all.txt.backup")

	info, err := os.Stat(ipsetPath)
	if err != nil || info.Size() < 10000 { // less than 10KB means corrupted or stub
		if bInfo, bErr := os.Stat(backupPath); bErr == nil && bInfo.Size() > 10000 {
			if logFn != nil {
				logFn("[WARN] Обнаружен повреждённый ipset-all.txt (размер мал). Восстановление из бэкапа...")
			}
			data, readErr := os.ReadFile(backupPath)
			if readErr == nil {
				if writeErr := os.WriteFile(ipsetPath, data, 0644); writeErr == nil {
					if logFn != nil {
						logFn(fmt.Sprintf("[OK] ipset-all.txt успешно восстановлен (%d байт)", len(data)))
					}
					return true
				}
			}
		}
	}
	return false
}

// SanitizeStartupAndNetwork ensures:
// 1. Cloudflare WARP GUI does not auto-start with Windows (which causes broken internet upon reboot without Zapret).
// 2. Any broken local loopback proxy (ProxyEnable=1 with 127.0.0.1) left by third-party VPN crashes is safely disabled.
func SanitizeStartupAndNetwork(logFn func(string)) {
	// 1. Remove CloudflareWARP GUI from Windows startup (HKLM & HKCU)
	delHklm := exec.Command("reg", "delete", "HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "CloudflareWARP", "/f")
	delHklm.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = delHklm.Run()

	delHkcu := exec.Command("reg", "delete", "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "CloudflareWARP", "/f")
	delHkcu.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = delHkcu.Run()

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

// PatchTestScriptIfNeeded ensures test zapret.ps1 supports NON_INTERACTIVE environment variable
// PatchTestScriptIfNeeded ensures test zapret.ps1 supports NON_INTERACTIVE environment variable
// so that automated benchmarking runs completely unattended.
func PatchTestScriptIfNeeded(logFn func(string)) {
	scriptPath := filepath.Join(GetZapretDir(), "utils", "test zapret.ps1")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, "NON_INTERACTIVE") {
		return // already patched
	}

	isCRLF := strings.Contains(content, "\r\n")
	normalized := strings.ReplaceAll(content, "\r\n", "\n")

	// 1. Read-TestType
	reTestType := regexp.MustCompile(`function\s+Read-TestType\s*\{\s*while\s*\(\$true\)\s*\{`)
	normalized = reTestType.ReplaceAllString(normalized, "function Read-TestType {\n    if ($env:NON_INTERACTIVE -eq \"1\" -or $env:TEST_TYPE) {\n        if ($env:TEST_TYPE -eq 'dpi') { return 'dpi' }\n        return 'standard'\n    }\n    while ($true) {")

	// 2. Read-ModeSelection
	reMode := regexp.MustCompile(`function\s+Read-ModeSelection\s*\{\s*while\s*\(\$true\)\s*\{`)
	normalized = reMode.ReplaceAllString(normalized, "function Read-ModeSelection {\n    if ($env:NON_INTERACTIVE -eq \"1\" -or $env:TEST_MODE) {\n        return 'all'\n    }\n    while ($true) {")

	// 3. Read-ConfigSelection
	reCfg := regexp.MustCompile(`function\s+Read-ConfigSelection\s*\{\s*param\(\[array\]\$allFiles\)\s*while\s*\(\$true\)\s*\{`)
	normalized = reCfg.ReplaceAllString(normalized, "function Read-ConfigSelection {\n    param([array]$allFiles)\n    if ($env:NON_INTERACTIVE -eq \"1\") { return $allFiles }\n    while ($true) {")

	// 4. Wrap all [System.Console]::ReadKey($true) calls safely
	reReadKey := regexp.MustCompile(`\[void\]\[System\.Console\]::ReadKey\(\$true\)`)
	normalized = reReadKey.ReplaceAllString(normalized, "if ($env:NON_INTERACTIVE -ne \"1\" -and [Environment]::UserInteractive) { try { [void][System.Console]::ReadKey($true) } catch {} }")

	if isCRLF {
		normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
	}

	if writeErr := os.WriteFile(scriptPath, []byte(normalized), 0644); writeErr == nil && logFn != nil {
		logFn("[OK] Скрипт тестирования Zapret адаптирован для автоматического фонового бенчмарка")
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


// PrepareZapret ensures Zapret is extracted into warlink_core/zapret from GitHub.
func PrepareZapret(logFn func(string)) error {
	if HasZapret() {
		logFn("[OK] Компоненты Zapret уже присутствуют в warlink_core/zapret")
		return nil
	}

	_ = EnsureCoreDir()

	logFn("[INFO] Загрузка последней официальной версии Zapret с GitHub...")
	zipPath := filepath.Join(GetCoreDir(), "zapret.zip")
	defer func() {
		_ = os.Remove(zipPath)
	}()

	downloadURL, err := getLatestZapretURL()
	if err != nil {
		return fmt.Errorf("ошибка получения ссылки на Zapret: %w", err)
	}

	logFn(fmt.Sprintf("[INFO] Скачивание архива: %s", downloadURL))
	if err := downloadFile(downloadURL, zipPath); err != nil {
		return fmt.Errorf("ошибка скачивания Zapret: %w", err)
	}

	logFn("[INFO] Распаковка архива в warlink_core/zapret...")
	if err := unzip(zipPath, GetZapretDir()); err != nil {
		return fmt.Errorf("ошибка распаковки Zapret: %w", err)
	}

	RestoreIpsetIfNeeded(logFn)
	PatchTestScriptIfNeeded(logFn)

	logFn("[OK] Zapret успешно установлен в warlink_core/zapret")
	return nil
}

// InstallWarp downloads and saves official Cloudflare WARP installer in warlink_core and installs it if needed.
func InstallWarp(logFn func(string)) error {
	_ = EnsureCoreDir()
	msiPath := filepath.Join(GetCoreDir(), "Cloudflare_WARP.msi")

	// 1. Ensure installer is downloaded and kept in warlink_core
	if _, err := os.Stat(msiPath); err != nil {
		logFn("[INFO] Скачивание официального установщика Cloudflare WARP в warlink_core...")
		msiURL := "https://1111-releases.cloudflareclient.com/windows/Cloudflare_WARP_Release-x64.msi"
		if err := downloadFile(msiURL, msiPath); err != nil {
			return fmt.Errorf("ошибка скачивания Cloudflare WARP: %w", err)
		}
		logFn("[OK] Файл Cloudflare_WARP.msi успешно сохранен в warlink_core")
	}

	// 2. Install if not already active in system
	if HasWarpInstalled() {
		logFn("[OK] Клиент Cloudflare WARP активен в системе")
		return nil
	}

	logFn("[INFO] Запуск тихой установки Cloudflare WARP (msiexec)...")
	// Launch msiexec silently without auto-launching UI
	cmd := exec.Command("msiexec", "/i", msiPath, "/qn")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка установки Cloudflare WARP: %w", err)
	}

	// Wait up to 30 seconds for warp-cli to become available
	logFn("[INFO] Ожидание инициализации службы WARP...")
	for i := 0; i < 15; i++ {
		time.Sleep(2 * time.Second)
		if HasWarpInstalled() {
			logFn("[OK] Cloudflare WARP успешно установлен и инициализирован")
			// Close the popup GUI window if Cloudflare launched it
			killGUI := exec.Command("taskkill", "/F", "/IM", "Cloudflare WARP.exe")
			killGUI.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = killGUI.Run()

			// Remove autostart of WARP GUI from registry so WarLink controls the lifecycle
			delReg := exec.Command("reg", "delete", "HKLM\\Software\\Microsoft\\Windows\\CurrentVersion\\Run", "/v", "CloudflareWARP", "/f")
			delReg.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = delReg.Run()
			return nil
		}
	}

	return fmt.Errorf("служба Cloudflare WARP не ответила после установки")
}

func getLatestZapretURL() (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/Flowseal/zapret-discord-youtube/releases/latest", nil)
	req.Header.Set("User-Agent", "WarLink-Optimizer")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}

	for _, asset := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") {
			return asset.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("в релизе %s не найден zip-архив", rel.TagName)
}

func downloadFile(url string, destPath string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("сервер вернул статус: %s", resp.Status)
	}

	_, err = io.Copy(out, resp.Body)
	return err
}

func unzip(src string, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	// Detect if all files share a common single root directory (e.g. zapret-discord-youtube-1.10.2/)
	var rootDir string
	for _, f := range r.File {
		cleanName := strings.ReplaceAll(f.Name, "\\", "/")
		parts := strings.Split(strings.Trim(cleanName, "/"), "/")
		if len(parts) > 1 {
			if rootDir == "" {
				rootDir = parts[0] + "/"
			} else if !strings.HasPrefix(cleanName, rootDir) {
				rootDir = ""
				break
			}
		}
	}

	for _, f := range r.File {
		entryName := strings.ReplaceAll(f.Name, "\\", "/")
		if rootDir != "" && strings.HasPrefix(entryName, rootDir) {
			entryName = strings.TrimPrefix(entryName, rootDir)
		}
		if entryName == "" {
			continue
		}

		fpath := filepath.Join(dest, filepath.FromSlash(entryName))
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("недопустимый путь к файлу в архиве: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ParseBatWinwsArgs extracts and formats winws command-line arguments directly from a Zapret .bat preset.
// This bypasses service.bat update checks and input redirection issues while fully maintaining the exact bypass strategy.
func ParseBatWinwsArgs(batPath string) ([]string, string, error) {
	content, err := os.ReadFile(batPath)
	if err != nil {
		return nil, "", err
	}

	zapretDir := filepath.Dir(batPath)
	binDir := filepath.Join(zapretDir, "bin")
	listsDir := filepath.Join(zapretDir, "lists")

	var fullCmdLine string
	lines := strings.Split(string(content), "\n")
	capturing := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "winws.exe") {
			capturing = true
		}
		if capturing {
			// strip trailing caret and spaces
			line = strings.TrimSuffix(line, "^")
			line = strings.TrimSpace(line)
			fullCmdLine += " " + line
			if !strings.HasSuffix(strings.TrimSpace(line), "^") {
				// if original line didn't end with caret, might be end of command
			}
		}
	}

	// Extract everything after winws.exe" or winws.exe
	idx := strings.Index(fullCmdLine, "winws.exe")
	if idx == -1 {
		return nil, "", fmt.Errorf("winws.exe не найден в %s", batPath)
	}

	rawArgs := fullCmdLine[idx+len("winws.exe"):]
	if strings.HasPrefix(rawArgs, "\"") {
		rawArgs = rawArgs[1:]
	}

	// Replace batch variable macros
	rawArgs = strings.ReplaceAll(rawArgs, "%BIN%", binDir+string(os.PathSeparator))
	rawArgs = strings.ReplaceAll(rawArgs, "%LISTS%", listsDir+string(os.PathSeparator))
	rawArgs = strings.ReplaceAll(rawArgs, "%~dp0bin\\", binDir+string(os.PathSeparator))
	rawArgs = strings.ReplaceAll(rawArgs, "%~dp0lists\\", listsDir+string(os.PathSeparator))
	rawArgs = strings.ReplaceAll(rawArgs, "%~dp0", zapretDir+string(os.PathSeparator))
	rawArgs = strings.ReplaceAll(rawArgs, "%GameFilterTCP%", "12")
	rawArgs = strings.ReplaceAll(rawArgs, "%GameFilterUDP%", "12")
	rawArgs = strings.ReplaceAll(rawArgs, "%GameFilter%", "12")

	// Tokenize arguments respecting quotes
	var args []string
	var cur strings.Builder
	inQuotes := false

	for i := 0; i < len(rawArgs); i++ {
		c := rawArgs[i]
		if c == '"' {
			inQuotes = !inQuotes
		} else if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			if inQuotes {
				cur.WriteByte(c)
			} else if cur.Len() > 0 {
				arg := cur.String()
				if arg != "^" && arg != "" {
					args = append(args, arg)
				}
				cur.Reset()
			}
		} else {
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		arg := cur.String()
		if arg != "^" && arg != "" {
			args = append(args, arg)
		}
	}

	winwsPath := filepath.Join(binDir, "winws.exe")
	return args, winwsPath, nil
}
