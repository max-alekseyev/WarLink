package progression

import (
	"bufio"
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

const WardogsAppID = "1867240"

// FindSteamScreenshotsDirs searches for WARDOGS screenshots directories in Steam userdata across all disks.
func FindSteamScreenshotsDirs() []string {
	var results []string
	seen := make(map[string]bool)

	var candidateSteamRoots []string

	// 1. Try registry HKCU\Software\Valve\Steam\SteamPath
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if val, _, err := k.GetStringValue("SteamPath"); err == nil && val != "" {
			candidateSteamRoots = append(candidateSteamRoots, filepath.Clean(strings.ReplaceAll(val, "/", "\\")))
		}
		k.Close()
	}

	// 2. Try registry HKLM\SOFTWARE\WOW6432Node\Valve\Steam\InstallPath
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if val, _, err := k.GetStringValue("InstallPath"); err == nil && val != "" {
			candidateSteamRoots = append(candidateSteamRoots, filepath.Clean(strings.ReplaceAll(val, "/", "\\")))
		}
		k.Close()
	}

	// 3. Scan all logical drive letters C..Z for Steam directories
	driveLetters := []string{"C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
	for _, letter := range driveLetters {
		candidateSteamRoots = append(candidateSteamRoots,
			letter+`:\Program Files (x86)\Steam`,
			letter+`:\Program Files\Steam`,
			letter+`:\Steam`,
			letter+`:\SteamLibrary`,
			letter+`:\Games\Steam`,
			letter+`:\Games\SteamLibrary`,
		)
	}

	// 4. Also scan libraryfolders.vdf in found Steam roots to discover secondary library disks
	extraRoots := make(map[string]bool)
	for _, root := range candidateSteamRoots {
		for _, vdfRel := range []string{`steamapps\libraryfolders.vdf`, `config\libraryfolders.vdf`} {
			vdfPath := filepath.Join(root, vdfRel)
			f, err := os.Open(vdfPath)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, `"path"`) {
					parts := strings.Split(line, `"`)
					if len(parts) >= 4 {
						p := strings.ReplaceAll(parts[3], `\\`, `\`)
						p = strings.ReplaceAll(p, `/`, `\`)
						extraRoots[filepath.Clean(p)] = true
					}
				}
			}
			f.Close()
		}
	}
	for r := range extraRoots {
		candidateSteamRoots = append(candidateSteamRoots, r)
	}

	for _, root := range candidateSteamRoots {
		userdataDir := filepath.Join(root, "userdata")
		entries, err := os.ReadDir(userdataDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			shotsDir := filepath.Join(userdataDir, e.Name(), "760", "remote", WardogsAppID, "screenshots")
			if fi, err := os.Stat(shotsDir); err == nil && fi.IsDir() {
				norm := strings.ToLower(shotsDir)
				if !seen[norm] {
					seen[norm] = true
					results = append(results, shotsDir)
				}
			}
		}
	}

	return results
}

// SteamWatcher monitors Steam's screenshot folder for WARDOGS and triggers parsing when a new screenshot appears.
type SteamWatcher struct {
	mu             sync.Mutex
	dirs           []string
	knownFiles     map[string]int64
	onSuccess      func(res *ProgressionResult, path string)
	logger         func(format string, args ...interface{})
}

// NewSteamWatcher initializes a watcher with discovered Steam directories.
func NewSteamWatcher(onSuccess func(res *ProgressionResult, path string), logger func(format string, args ...interface{})) *SteamWatcher {
	w := &SteamWatcher{
		knownFiles: make(map[string]int64),
		onSuccess:  onSuccess,
		logger:     logger,
	}
	w.RefreshDirs()
	return w
}

// RefreshDirs discovers Steam screenshot directories.
func (w *SteamWatcher) RefreshDirs() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.dirs = FindSteamScreenshotsDirs()
	// Populate known files initially so existing old files aren't treated as new
	for _, d := range w.dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".png") {
				fPath := filepath.Join(d, e.Name())
				if fi, err := e.Info(); err == nil {
					w.knownFiles[fPath] = fi.ModTime().UnixNano()
				}
			}
		}
	}
}

// Start begins background polling.
func (w *SteamWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	refreshTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-refreshTicker.C:
			w.RefreshDirs()
		case <-ticker.C:
			w.mu.Lock()
			hasDirs := len(w.dirs) > 0
			w.mu.Unlock()
			if !hasDirs {
				w.RefreshDirs()
			}
			w.scanOnce()
		}
	}
}

func (w *SteamWatcher) scanOnce() {
	w.mu.Lock()
	dirs := append([]string(nil), w.dirs...)
	w.mu.Unlock()

	if len(dirs) == 0 {
		return
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if !strings.HasSuffix(lower, ".jpg") && !strings.HasSuffix(lower, ".png") {
				continue
			}

			fPath := filepath.Join(d, e.Name())
			fi, err := e.Info()
			if err != nil {
				continue
			}

			w.mu.Lock()
			prevMod, exists := w.knownFiles[fPath]
			currMod := fi.ModTime().UnixNano()
			if exists && prevMod == currMod {
				w.mu.Unlock()
				continue
			}
			// Mark as seen immediately so we don't double process
			w.knownFiles[fPath] = currMod
			w.mu.Unlock()

			// Only process if file has size > 20KB (Steam may still be writing)
			if fi.Size() < 20*1024 {
				continue
			}

			// Short pause to ensure file write is finished by Steam
			time.Sleep(150 * time.Millisecond)

			if w.logger != nil {
				w.logger("[STEAM-F12] Обнаружен новый снимок Steam (%s, %.1f КБ), запуск распознавания...", e.Name(), float64(fi.Size())/1024)
			}

			res, parseErr := ParseScreenshotFile(fPath)
			if parseErr != nil {
				if w.logger != nil {
					w.logger("[STEAM-F12] Снимок не является экраном прокачки WARDOGS: %v", parseErr)
				}
				continue
			}

			isAcceptable := res != nil && (res.Valid || (res.CareerLevel > 0 && res.SumRoles > 0 && math.Abs(float64(res.CareerLevel-res.SumRoles)) <= 2))
			if isAcceptable && res.CareerLevel > 0 {
				if w.logger != nil {
					w.logger("[STEAM-F12] Успешно распознан уровень WARDOGS: %d (ранги ролей: %d). Применяю...", res.CareerLevel, res.SumRoles)
				}
				if w.onSuccess != nil {
					w.onSuccess(res, fPath)
				}
			} else if res != nil && w.logger != nil {
				w.logger("[STEAM-F12] Контрольная сумма не сошлась (Карьера=%d, сумма ролей=%d). Пропуск.", res.CareerLevel, res.SumRoles)
			}
		}
	}
}
