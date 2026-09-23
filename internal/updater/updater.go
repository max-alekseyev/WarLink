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

// CheckForUpdate polls GitHub Releases API with a fast timeout (3.5 seconds)
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

	if totalSize > 0 && downloaded < totalSize {
		return fmt.Errorf("загрузка файла прервана: получено %d из %d байт", downloaded, totalSize)
	}

	if progressCb != nil {
		progressCb(99, "Загрузка завершена. Проверка целостности...")
	}
	return nil
}

// VerifySha256 checks the SHA-256 hash of a file against remote sha256 url
func VerifySha256(filePath, sha256URL string) (bool, error) {
	if strings.TrimSpace(sha256URL) == "" {
		return false, fmt.Errorf("sha256 URL не указан, проверка обязательна")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sha256URL)
	if err != nil {
		return false, fmt.Errorf("ошибка запроса sha256: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ошибка загрузки sha256: статус %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("ошибка чтения тела sha256: %w", err)
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return false, fmt.Errorf("пустой файл контрольной суммы sha256")
	}
	expectedHash := strings.ToLower(fields[0])

	f, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("ошибка открытия файла для проверки хеша: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, fmt.Errorf("ошибка вычисления хеша: %w", err)
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if !strings.EqualFold(actualHash, expectedHash) {
		return false, fmt.Errorf("хеш не совпал: ожидался %s, получен %s", expectedHash, actualHash)
	}

	return true, nil
}

// ApplyUpdateAndRestart renames running binary to .old, moves new binary into place, and restarts WarLink
func ApplyUpdateAndRestart(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не удалось определить путь исполняемого файла: %w", err)
	}

	currentDir := filepath.Dir(currentExe)
	coreDir := filepath.Join(currentDir, "warlink_core")
	_ = os.MkdirAll(coreDir, 0755)

	// Backup config.json if present
	configPath := filepath.Join(coreDir, "config.json")
	backupConfigPath := filepath.Join(coreDir, "config.json.bak")
	if cfgData, err := os.ReadFile(configPath); err == nil && len(cfgData) > 0 {
		_ = os.WriteFile(backupConfigPath, cfgData, 0644)
	}

	// 1. Rename running binary to .old
	oldExe := currentExe + ".old"
	_ = os.Remove(oldExe)
	if err := os.Rename(currentExe, oldExe); err != nil {
		return fmt.Errorf("не удалось переименовать текущий бинарник: %w", err)
	}

	// 2. Move new binary to currentExe location
	if err := os.Rename(newExePath, currentExe); err != nil {
		// Fallback: copy file if across different volumes
		if copyErr := copyFile(newExePath, currentExe); copyErr != nil {
			_ = os.Rename(oldExe, currentExe)
			return fmt.Errorf("не удалось переместить новый бинарник: %w", copyErr)
		}
		_ = os.Remove(newExePath)
	}

	// 3. Launch updated binary
	cmd := exec.Command(currentExe)
	cmd.Dir = currentDir
	if err := cmd.Start(); err != nil {
		_ = os.Remove(currentExe)
		_ = os.Rename(oldExe, currentExe)
		return fmt.Errorf("не удалось запустить обновленный бинарник: %w", err)
	}

	// 4. Terminate current process immediately
	os.Exit(0)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
