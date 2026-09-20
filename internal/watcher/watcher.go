package watcher

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	modKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First           = modKernel32.NewProc("Process32FirstW")
	procProcess32Next            = modKernel32.NewProc("Process32NextW")
	procCloseHandle              = modKernel32.NewProc("CloseHandle")
)

const (
	TH32CS_SNAPPROCESS = 0x00000002
)

type PROCESSENTRY32W struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	DwFlags             uint32
	SzExeFile           [260]uint16
}

type TargetInfo struct {
	GameID          string
	ProcessNames    []string
	NormalizedTitle string
}

type GameWatcher struct {
	mu             sync.Mutex
	stopChan       chan struct{}
	isRunning      bool
	wasRunning     bool
	missCount      int
	activeTarget   string
	targetFn       func() TargetInfo
	onGameExit     func(string)
	onGameStarted  func(string)
	onProcessFound func(gameID string, procName string)
}

func New(targetFn func() TargetInfo, onGameStarted func(string), onGameExit func(string), onProcessFound func(gameID, procName string)) *GameWatcher {
	return &GameWatcher{
		stopChan:       make(chan struct{}),
		targetFn:       targetFn,
		onGameStarted:  onGameStarted,
		onGameExit:     onGameExit,
		onProcessFound: onProcessFound,
	}
}

// Built-in aliases for popular multiplayer titles
var BuiltinAliases = map[string][]string{
	"1867240": {"wardogsclient-win64-shipping.exe", "wardogslauncher-shipping.exe"},
	"730":     {"cs2.exe"},
	"570":     {"dota2.exe"},
	"1172470": {"r5apex.exe"},
	"578080":  {"tslgame.exe"},
	"252490":  {"rustclient.exe", "rust.exe"},
	"359550":  {"rainbowsix.exe", "rainbowsix_vulkan.exe"},
	"271590":  {"gta5.exe", "playgtav.exe"},
	"381210":  {"deadbydaylight-win64-shipping.exe"},
	"553850":  {"helldivers2.exe"},
	"1091500": {"cyberpunk2077.exe"},
	"1245620": {"eldenring.exe"},
}

// NormalizeGameToken strips common game engine suffixes to find the root name
func NormalizeGameToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".exe")

	// Remove common engine / packaging suffixes
	suffixes := []string{
		"-win64-shipping",
		"-win32-shipping",
		"_shipping",
		"-shipping",
		"client",
		"launcher",
		"game",
		"_data",
	}
	for _, suf := range suffixes {
		s = strings.TrimSuffix(s, suf)
	}

	// Remove non-alphanumeric chars
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'а' && r <= 'я') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// GetSteamLibraryPaths finds all Steam library folders on the system
func GetSteamLibraryPaths() []string {
	paths := make(map[string]bool)

	// 1. Check registry HKCU\Software\Valve\Steam\SteamPath
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if val, _, err := k.GetStringValue("SteamPath"); err == nil && val != "" {
			clean := filepath.Clean(val)
			paths[clean] = true
		}
		_ = k.Close()
	}

	// 2. Default standard Steam installation paths
	defaults := []string{
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
	}
	for _, d := range defaults {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			paths[filepath.Clean(d)] = true
		}
	}

	// 3. Inspect libraryfolders.vdf in each known Steam directory to discover other drives (D:, E:, etc.)
	pathRegex := regexp.MustCompile(`"path"\s+"([^"]+)"`)
	for p := range paths {
		vdfPath := filepath.Join(p, "steamapps", "libraryfolders.vdf")
		if data, err := os.ReadFile(vdfPath); err == nil {
			matches := pathRegex.FindAllStringSubmatch(string(data), -1)
			for _, m := range matches {
				if len(m) > 1 {
					cleanPath := filepath.Clean(strings.ReplaceAll(m[1], `\\`, `\`))
					if fi, err := os.Stat(cleanPath); err == nil && fi.IsDir() {
						paths[cleanPath] = true
					}
				}
			}
		}
	}

	result := make([]string, 0, len(paths))
	for p := range paths {
		result = append(result, p)
	}
	return result
}

// FindSteamGameExecutables reads steamapps/appmanifest_<appid>.acf and extracts real executables
func FindSteamGameExecutables(steamAppID string) []string {
	steamAppID = strings.TrimSpace(steamAppID)
	if steamAppID == "" {
		return nil
	}

	libraries := GetSteamLibraryPaths()
	manifestName := "appmanifest_" + steamAppID + ".acf"

	var foundDir string
	for _, lib := range libraries {
		manifestPath := filepath.Join(lib, "steamapps", manifestName)
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		// Read installdir
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.Contains(line, `"installdir"`) {
				parts := strings.Split(line, `"`)
				if len(parts) >= 4 {
					foundDir = filepath.Join(lib, "steamapps", "common", parts[3])
					break
				}
			}
		}
		if foundDir != "" {
			break
		}
	}

	if foundDir == "" {
		return nil
	}

	// Scan game folder for .exe binaries (up to 4 levels deep)
	var executables []string
	ignoreExes := map[string]bool{
		"crashreportclient.exe": true,
		"epicwebhelper.exe":     true,
		"crashpad_handler.exe":  true,
		"dxsetup.exe":           true,
		"installscript.exe":     true,
	}

	_ = filepath.Walk(foundDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, _ := filepath.Rel(foundDir, path)
		depth := strings.Count(rel, string(os.PathSeparator))
		if depth > 4 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".exe") {
			nameLower := strings.ToLower(info.Name())
			if !ignoreExes[nameLower] && !strings.Contains(nameLower, "crash") && !strings.Contains(nameLower, "helper") {
				executables = append(executables, info.Name())
			}
		}
		return nil
	})

	return executables
}

// ResolveGameProcessNames aggregates Steam manifest exes, explicit path, aliases, and title
func ResolveGameProcessNames(gameID, steamAppID, title, exePath string, cached []string) TargetInfo {
	targets := make(map[string]bool)

	// 1. Previously cached process names
	for _, p := range cached {
		p = strings.TrimSpace(p)
		if p != "" {
			targets[strings.ToLower(p)] = true
		}
	}

	// 2. Steam AppManifest discovery
	if steamAppID != "" {
		for _, e := range FindSteamGameExecutables(steamAppID) {
			targets[strings.ToLower(e)] = true
		}
		if aliases, ok := BuiltinAliases[steamAppID]; ok {
			for _, a := range aliases {
				targets[strings.ToLower(a)] = true
			}
		}
	}

	// 3. User-selected ExePath
	if exePath != "" {
		base := strings.ToLower(filepath.Base(exePath))
		if base != "" {
			targets[base] = true
		}
		// Also scan sibling Binaries/Win64 if present
		gameDir := filepath.Dir(exePath)
		shippingDir := filepath.Join(gameDir, "Binaries", "Win64")
		if entries, err := os.ReadDir(shippingDir); err == nil {
			for _, ent := range entries {
				if !ent.IsDir() && strings.EqualFold(filepath.Ext(ent.Name()), ".exe") {
					targets[strings.ToLower(ent.Name())] = true
				}
			}
		}
	}

	// 4. Built-in alias lookup by title
	normTitle := NormalizeGameToken(title)
	if aliases, ok := BuiltinAliases[normTitle]; ok {
		for _, a := range aliases {
			targets[strings.ToLower(a)] = true
		}
	}

	// 5. Fallback title base (e.g. "wardogs.exe")
	if normTitle != "" {
		targets[normTitle+".exe"] = true
	}

	result := make([]string, 0, len(targets))
	for t := range targets {
		result = append(result, t)
	}

	return TargetInfo{
		GameID:          gameID,
		ProcessNames:    result,
		NormalizedTitle: normTitle,
	}
}

// SelectBestProcess selects the most relevant game executable from matched processes,
// giving priority to game clients and shipping binaries over auxiliary launchers/updaters.
func SelectBestProcess(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var best string
	for _, n := range names {
		lower := strings.ToLower(n)
		isLauncher := strings.Contains(lower, "launcher") || strings.Contains(lower, "setup") || strings.Contains(lower, "update")
		isClient := strings.Contains(lower, "client") || strings.Contains(lower, "shipping")

		if best == "" {
			best = n
			continue
		}

		bestLower := strings.ToLower(best)
		bestIsLauncher := strings.Contains(bestLower, "launcher") || strings.Contains(bestLower, "setup") || strings.Contains(bestLower, "update")

		if bestIsLauncher && !isLauncher {
			best = n
		} else if isClient && !strings.Contains(bestLower, "client") {
			best = n
		}
	}
	return best
}

// IsAnyProcessRunning checks active processes against target list and normalized token
func IsAnyProcessRunning(targets []string, normalizedTitle string) (bool, string) {
	if len(targets) == 0 && normalizedTitle == "" {
		return false, ""
	}

	hSnap, _, _ := procCreateToolhelp32Snapshot.Call(uintptr(TH32CS_SNAPPROCESS), 0)
	if hSnap == 0 || hSnap == uintptr(syscall.InvalidHandle) {
		return false, ""
	}
	defer procCloseHandle.Call(hSnap)

	var entry PROCESSENTRY32W
	entry.DwSize = uint32(unsafe.Sizeof(entry))

	r, _, _ := procProcess32First.Call(hSnap, uintptr(unsafe.Pointer(&entry)))
	if r == 0 {
		return false, ""
	}

	var matched []string
	for {
		name := syscall.UTF16ToString(entry.SzExeFile[:])
		nameLower := strings.ToLower(name)
		isMatch := false

		// 1. Direct match with target list
		for _, t := range targets {
			if nameLower == t || strings.Contains(nameLower, t) {
				isMatch = true
				break
			}
		}

		// 2. Heuristic token match
		if !isMatch && normalizedTitle != "" && len(normalizedTitle) >= 4 {
			normProc := NormalizeGameToken(name)
			if normProc != "" && (normProc == normalizedTitle || strings.Contains(normProc, normalizedTitle)) {
				isMatch = true
			}
		}

		if isMatch {
			matched = append(matched, name)
		}

		r, _, _ = procProcess32Next.Call(hSnap, uintptr(unsafe.Pointer(&entry)))
		if r == 0 {
			break
		}
	}

	if len(matched) > 0 {
		return true, SelectBestProcess(matched)
	}
	return false, ""
}

// IsProcessRunning retains backward compatibility
func IsProcessRunning(targetName string) bool {
	running, _ := IsAnyProcessRunning([]string{strings.ToLower(targetName)}, NormalizeGameToken(targetName))
	return running
}

func (w *GameWatcher) Start() {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return
	}
	w.isRunning = true
	w.stopChan = make(chan struct{})
	w.missCount = 0
	w.mu.Unlock()

	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChan:
				return
			case <-ticker.C:
				w.mu.Lock()
				var info TargetInfo
				if w.targetFn != nil {
					info = w.targetFn()
				}
				w.mu.Unlock()

				if len(info.ProcessNames) == 0 && info.NormalizedTitle == "" {
					continue
				}

				running, procName := IsAnyProcessRunning(info.ProcessNames, info.NormalizedTitle)

				w.mu.Lock()
				if running {
					w.missCount = 0
					lowerProc := strings.ToLower(procName)
					isProcLauncher := strings.Contains(lowerProc, "launcher") || strings.Contains(lowerProc, "setup") || strings.Contains(lowerProc, "update")
					isProcClient := strings.Contains(lowerProc, "client")

					// Update activeTarget: if it was empty, or if activeTarget was a launcher and now we have a game client/main binary
					if w.activeTarget == "" || (!isProcLauncher && strings.Contains(strings.ToLower(w.activeTarget), "launcher")) || isProcClient {
						w.activeTarget = procName
					}

					if !w.wasRunning {
						w.wasRunning = true
						w.activeTarget = procName
						w.mu.Unlock()

						if w.onProcessFound != nil && info.GameID != "" {
							w.onProcessFound(info.GameID, procName)
						}
						if w.onGameStarted != nil {
							w.onGameStarted(procName)
						}
						continue
					} else {
						// Process was already running, but could be transition to main client
						if w.onProcessFound != nil && info.GameID != "" && !isProcLauncher {
							w.mu.Unlock()
							w.onProcessFound(info.GameID, procName)
							w.mu.Lock()
						}
					}
				} else {
					if w.wasRunning {
						w.missCount++
						// 10 consecutive misses (2.5s) = confirmed exit, allowing launcher -> client handoff without closing tunnel
						if w.missCount >= 10 {
							w.wasRunning = false
							w.missCount = 0
							closedProc := w.activeTarget
							w.activeTarget = ""
							w.mu.Unlock()

							if w.onGameExit != nil {
								w.onGameExit(closedProc)
							}
							continue
						}
					}
				}
				w.mu.Unlock()
			}
		}
	}()
}

func (w *GameWatcher) Reset() {
	w.mu.Lock()
	w.wasRunning = false
	w.missCount = 0
	w.activeTarget = ""
	w.mu.Unlock()
}

func (w *GameWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.isRunning {
		return
	}
	w.isRunning = false
	close(w.stopChan)
}
