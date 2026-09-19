package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ReleaseInfo struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

type UpdateCheckResult struct {
	UpdateAvailable bool
	LatestVersion   string
	CurrentVersion  string
	ExeURL          string
	Sha256URL       string
	AssetSize       int64
	ReleaseNotes    string
}

// CompareSemVer returns 1 if v1 > v2, -1 if v1 < v2, 0 if v1 == v2
func CompareSemVer(v1, v2 string) int {
	clean := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		parts := strings.Split(v, ".")
		var res []int
		for _, p := range parts {
			// Strip any non-digit suffix (e.g. -beta)
			d := ""
			for _, r := range p {
				if r >= '0' && r <= '9' {
					d += string(r)
				} else {
					break
				}
			}
			num, _ := strconv.Atoi(d)
			res = append(res, num)
		}
		for len(res) < 3 {
			res = append(res, 0)
		}
		return res
	}

	p1 := clean(v1)
	p2 := clean(v2)

	for i := 0; i < 3; i++ {
		if p1[i] > p2[i] {
			return 1
		}
		if p1[i] < p2[i] {
			return -1
		}
	}
	return 0
}

var UpdateRepo = "max-alekseyev/WarLink"

func GetUpdateURL() string {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--update-url=") {
			return strings.TrimPrefix(arg, "--update-url=")
		}
	}
	if env := os.Getenv("WARLINK_UPDATE_URL"); env != "" {
		return env
	}
	exe, err := os.Executable()
	if err == nil {
		coreOverride := filepath.Join(filepath.Dir(exe), "warlink_core", "test_update_url.txt")
		if data, err := os.ReadFile(coreOverride); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				return trimmed
			}
		}
		overrideFile := filepath.Join(filepath.Dir(exe), "test_update_url.txt")
		if data, err := os.ReadFile(overrideFile); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				return trimmed
			}
		}
	}
	repo := strings.TrimSpace(UpdateRepo)
	if repo == "" {
		repo = "max-alekseyev/WarLink"
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
}

// CheckForUpdate polls GitHub Releases API with a fast timeout (3 seconds)
func CheckForUpdate(currentVersion string) (*UpdateCheckResult, error) {
	client := &http.Client{Timeout: 3500 * time.Millisecond}
	apiURL := GetUpdateURL()
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WarLink-Updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}

	res := &UpdateCheckResult{
		LatestVersion:  rel.TagName,
		CurrentVersion: currentVersion,
		ReleaseNotes:   rel.Body,
	}

	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "WarLink.exe") {
			res.ExeURL = a.BrowserDownloadURL
			res.AssetSize = a.Size
		} else if strings.EqualFold(a.Name, "WarLink.exe.sha256") {
			res.Sha256URL = a.BrowserDownloadURL
		}
	}

	if CompareSemVer(rel.TagName, currentVersion) > 0 && res.ExeURL != "" {
		res.UpdateAvailable = true
	}

	return res, nil
}

// DownloadWithProgress downloads a file to destPath and notifies callback with progress (0-100)
func DownloadWithProgress(url, destPath string, expectedSize int64, progressCb func(int, string)) error {
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error %s", resp.Status)
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		totalSize = expectedSize
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	lastReport := time.Now()

	for {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			_, wErr := out.Write(buf[:n])
			if wErr != nil {
				return wErr
			}
			downloaded += int64(n)

			if totalSize > 0 && time.Since(lastReport) > 100*time.Millisecond {
				pct := int((downloaded * 100) / totalSize)
				if pct > 98 {
					pct = 98
				}
				if progressCb != nil {
					mb := float64(downloaded) / (1024 * 1024)
					totalMb := float64(totalSize) / (1024 * 1024)
					progressCb(pct, fmt.Sprintf("Загрузка обновления: %.1f / %.1f МБ (%d%%)", mb, totalMb, pct))
				}
				lastReport = time.Now()
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			return rErr
		}
	}

	if progressCb != nil {
		progressCb(99, "Загрузка завершена. Проверка целостности...")
	}
	return nil
}

// VerifySha256 checks the SHA-256 hash of a file against expected hash string or remote sha256 url
func VerifySha256(filePath, sha256URL string) (bool, error) {
	if sha256URL == "" {
		// If no sha256 file was provided in release, verify minimum file size (> 5MB)
		fi, err := os.Stat(filePath)
		if err != nil || fi.Size() < 5*1024*1024 {
			return false, fmt.Errorf("размер файла слишком мал или файл поврежден")
		}
		return true, nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sha256URL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	expectedHash := strings.ToLower(strings.TrimSpace(strings.Split(string(body), " ")[0]))

	f, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, err
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if !strings.EqualFold(actualHash, expectedHash) {
		return false, fmt.Errorf("хеш не совпал: ожидался %s, получен %s", expectedHash, actualHash)
	}

	return true, nil
}

// ApplyUpdateAndRestart creates an in-place atomic update script, preserves config.json, and restarts WarLink
func ApplyUpdateAndRestart(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}

	currentDir := filepath.Dir(currentExe)
	coreDir := filepath.Join(currentDir, "warlink_core")
	_ = os.MkdirAll(coreDir, 0755)
	currentPID := os.Getpid()

	// 1. Double check and preserve config.json inside warlink_core
	configPath := filepath.Join(coreDir, "config.json")
	backupConfigPath := filepath.Join(coreDir, "config.json.bak")
	if cfgData, err := os.ReadFile(configPath); err == nil && len(cfgData) > 0 {
		_ = os.WriteFile(backupConfigPath, cfgData, 0644)
	}

	// 2. Prepare updater helper script inside warlink_core
	updaterScript := filepath.Join(coreDir, "warlink_self_update.cmd")
	scriptContent := fmt.Sprintf(`@echo off
setlocal enabledelayedexpansion

:: 1. Wait for current WarLink process to exit completely
:WAIT_LOOP
tasklist /FI "PID eq %d" 2>NUL | find /I "%d" >NUL
if not errorlevel 1 (
    timeout /t 1 /nobreak >NUL
    goto WAIT_LOOP
)

:: Extra safety sleep
timeout /t 1 /nobreak >NUL

:: 2. Replace WarLink.exe with verified binary
copy /Y "%s" "%s" >NUL
if errorlevel 1 (
    timeout /t 2 /nobreak >NUL
    copy /Y "%s" "%s" >NUL
)

:: 3. Restore config backup if config was somehow wiped
if not exist "%s" (
    if exist "%s" copy /Y "%s" "%s" >NUL
)

:: 4. Clean up temporary files
del /F /Q "%s" >NUL 2>&1

:: 5. Relaunch updated WarLink.exe
start "" "%s"

:: 6. Self-delete this script
del /F /Q "%%~f0" >NUL 2>&1
exit
`,
		currentPID, currentPID,
		newExePath, currentExe,
		newExePath, currentExe,
		configPath, backupConfigPath, backupConfigPath, configPath,
		newExePath,
		currentExe,
	)

	if err := os.WriteFile(updaterScript, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("не удалось создать скрипт обновления: %w", err)
	}

	// 3. Launch updater script detached
	cmd := exec.Command("cmd.exe", "/C", updaterScript)
	cmd.Dir = currentDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить процесс обновления: %w", err)
	}

	// 4. Terminate current process immediately
	os.Exit(0)
	return nil
}
