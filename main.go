package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/engine"
	"warlink/internal/scanner"
	"warlink/internal/singbox"
	"warlink/internal/tray"
	"warlink/internal/updater"
	"warlink/internal/watcher"
)

var AppVersion = "v2.0.4"

//go:embed ui/*
var uiFS embed.FS

var (
	modUser32            = syscall.NewLazyDLL("user32.dll")
	modKernel32          = syscall.NewLazyDLL("kernel32.dll")
	modGdi32             = syscall.NewLazyDLL("gdi32.dll")
	procMsgBox           = modUser32.NewProc("MessageBoxW")
	procCreateMtx        = modKernel32.NewProc("CreateMutexW")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procSetForeground    = modUser32.NewProc("SetForegroundWindow")
	procFindWindow       = modUser32.NewProc("FindWindowW")
	procSetWindowLongPtr = modUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr = modUser32.NewProc("GetWindowLongPtrW")
	procCallWindowProc   = modUser32.NewProc("CallWindowProcW")
	procDefWindowProc    = modUser32.NewProc("DefWindowProcW")
	procActivateKeyboardLayout = modUser32.NewProc("ActivateKeyboardLayout")
	procReleaseCapture   = modUser32.NewProc("ReleaseCapture")
	procSendMessage      = modUser32.NewProc("SendMessageW")
	procPostMessage      = modUser32.NewProc("PostMessageW")
	procGetWindowRect    = modUser32.NewProc("GetWindowRect")
	procFlashWindow      = modUser32.NewProc("FlashWindow")
	procSetWindowPos     = modUser32.NewProc("SetWindowPos")
	procSetWindowsHookEx = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx   = modUser32.NewProc("CallNextHookEx")
	procGetCurrentThreadId = modKernel32.NewProc("GetCurrentThreadId")
	procCreateSolidBrush   = modGdi32.NewProc("CreateSolidBrush")
	procFillRect           = modUser32.NewProc("FillRect")
	procGetSystemMetrics   = modUser32.NewProc("GetSystemMetrics")
	procGetForegroundWindow = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput  = modUser32.NewProc("AttachThreadInput")
	procExtractIconEx      = modShell32.NewProc("ExtractIconExW")
	procLoadIcon           = modUser32.NewProc("LoadIconW")
	procSetClassLongPtr    = modUser32.NewProc("SetClassLongPtrW")
	procGetModuleHandle    = modKernel32.NewProc("GetModuleHandleW")
	modShell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecute       = modShell32.NewProc("ShellExecuteW")
	procIsUserAnAdmin      = modShell32.NewProc("IsUserAnAdmin")
)

const (
	WM_GETICON   = 0x007F
	WM_SETICON   = 0x0080
	ICON_SMALL   = 0
	ICON_BIG     = 1
	ICON_SMALL2  = 2
	GCLP_HICON   = -14
	GCLP_HICONSM = -34
)

var (
	cachedLargeIcon uintptr
	cachedSmallIcon uintptr
)

func isRunningAsAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}

func ensureAdminElevation() {
	if isRunningAsAdmin() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	pVerb, _ := syscall.UTF16PtrFromString("runas")
	pExe, _ := syscall.UTF16PtrFromString(exe)
	cwd, _ := os.Getwd()
	pCwd, _ := syscall.UTF16PtrFromString(cwd)

	var pArgs *uint16
	if len(os.Args) > 1 {
		args := strings.Join(os.Args[1:], " ")
		pArgs, _ = syscall.UTF16PtrFromString(args)
	}

	procShellExecute.Call(0, uintptr(unsafe.Pointer(pVerb)), uintptr(unsafe.Pointer(pExe)), uintptr(unsafe.Pointer(pArgs)), uintptr(unsafe.Pointer(pCwd)), uintptr(SW_SHOWNORMAL))
	os.Exit(0)
}

const (
	MB_OK                = 0x00000000
	MB_OKCANCEL          = 0x00000001
	MB_ICONEXCLAMATION   = 0x00000030
	MB_ICONINFORMATION   = 0x00000040
	IDOK                 = 1
	ERROR_ALREADY_EXISTS = 183

	SW_HIDE        = 0
	SW_SHOWNORMAL  = 1
	SW_SHOW        = 5
	SW_MINIMIZE    = 6
	SW_RESTORE     = 9

	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2
	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_SHOWWINDOW = 0x0040

	GWLP_WNDPROC   = -4
	WM_CLOSE       = 0x0010
	WM_ERASEBKGND  = 0x0014
	WM_SHOWWINDOW  = 0x0018
	WH_CBT         = 5
	HCBT_CREATEWND = 3

	WS_VISIBLE     = 0x10000000
	WS_BORDER      = 0x00800000
	WS_CAPTION     = 0x00C00000
	WS_THICKFRAME  = 0x00040000
	WS_MAXIMIZEBOX = 0x00010000

	SM_CXSCREEN    = 0
	SM_CYSCREEN    = 1
)

type CREATESTRUCTW struct {
	LpCreateParams uintptr
	HInstance      uintptr
	HMenu          uintptr
	HwndParent     uintptr
	Cy             int32
	Cx             int32
	Y              int32
	X              int32
	Style          int32
	LpszName       *uint16
	LpszClass      *uint16
	ExStyle        uint32
}

type CBT_CREATEWND struct {
	Lpcs            *CREATESTRUCTW
	HwndInsertAfter uintptr
}

var (
	oldWndProc    uintptr
	darkBrush     uintptr
	isWindowReady bool
)

func initWindowIcons() {
	if cachedLargeIcon != 0 && cachedSmallIcon != 0 {
		return
	}

	if exePath, err := os.Executable(); err == nil {
		exePtr, _ := syscall.UTF16PtrFromString(exePath)
		var hL, hS uintptr
		res, _, _ := procExtractIconEx.Call(
			uintptr(unsafe.Pointer(exePtr)),
			0,
			uintptr(unsafe.Pointer(&hL)),
			uintptr(unsafe.Pointer(&hS)),
			1,
		)
		if res > 0 {
			if hL != 0 {
				cachedLargeIcon = hL
			}
			if hS != 0 {
				cachedSmallIcon = hS
			}
		}
	}

	if cachedLargeIcon == 0 {
		hInst, _, _ := procGetModuleHandle.Call(0)
		h, _, _ := procLoadIcon.Call(hInst, uintptr(1))
		if h == 0 {
			h, _, _ = procLoadIcon.Call(0, uintptr(32512)) // IDI_APPLICATION
		}
		cachedLargeIcon = h
	}
	if cachedSmallIcon == 0 {
		cachedSmallIcon = cachedLargeIcon
	}
}

func applyWindowIcons(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	initWindowIcons()

	// 1. Send WM_SETICON for both big (Alt-Tab) and small (taskbar hover preview & titlebar)
	if cachedLargeIcon != 0 {
		procSendMessage.Call(hwnd, uintptr(WM_SETICON), uintptr(ICON_BIG), cachedLargeIcon)
	}
	if cachedSmallIcon != 0 {
		procSendMessage.Call(hwnd, uintptr(WM_SETICON), uintptr(ICON_SMALL), cachedSmallIcon)
	}

	// 2. Set Class Long icons for the window class
	nIconIndex := int32(GCLP_HICON)
	nIconSmIndex := int32(GCLP_HICONSM)
	idxHIcon := uintptr(uint32(nIconIndex))
	idxHIconSm := uintptr(uint32(nIconSmIndex))
	if procSetClassLongPtr.Find() == nil {
		if cachedLargeIcon != 0 {
			procSetClassLongPtr.Call(hwnd, idxHIcon, cachedLargeIcon)
		}
		if cachedSmallIcon != 0 {
			procSetClassLongPtr.Call(hwnd, idxHIconSm, cachedSmallIcon)
		}
	} else {
		procSetClassLong := modUser32.NewProc("SetClassLongW")
		if cachedLargeIcon != 0 {
			procSetClassLong.Call(hwnd, idxHIcon, cachedLargeIcon)
		}
		if cachedSmallIcon != 0 {
			procSetClassLong.Call(hwnd, idxHIconSm, cachedSmallIcon)
		}
	}

	// 3. Ensure WS_EX_APPWINDOW in extended style so Alt-Tab and Taskbar thumbnail treat it properly
	nExIndex := int32(-20) // GWL_EXSTYLE
	exStyle, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(uint32(nExIndex)))
	exStyle |= 0x00040000 // WS_EX_APPWINDOW
	exStyle &^= 0x00000080 // remove WS_EX_TOOLWINDOW
	procSetWindowLongPtr.Call(hwnd, uintptr(uint32(nExIndex)), exStyle)
}

func revealMainWindow(hwnd uintptr) {
	applyWindowIcons(hwnd)
	if isWindowReady {
		return
	}
	isWindowReady = true

	// Calculate center screen coordinates
	sw, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	sh, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	var winW int32 = 520
	var winH int32 = 370
	posX := (int32(sw) - winW) / 2
	posY := (int32(sh) - winH) / 2

	// Position and show window seamlessly
	procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(posX),
		uintptr(posY),
		uintptr(winW),
		uintptr(winH),
		0x0040, // SWP_SHOWWINDOW
	)
	procShowWindow.Call(hwnd, uintptr(SW_SHOWNORMAL))
	procSetForeground.Call(hwnd)
}

type AppState struct {
	mu                sync.Mutex
	cfg               *config.Config
	eng               *engine.Engine
	logs              []string
	isBusy            bool
	isDownloadingDeps bool
	depsMsg           string
	isInitializing    bool
	initPct           int
	initTitle         string
	initMsg           string
	isUpdating        bool
	updatePct         int
	updateMsg         string
	updateInfo        *updater.UpdateCheckResult
	gatewayStatus     *singbox.GatewayStatus
	gatewayRealPing   int
	lastConnectError  string
}

func resolveFromLocalSteam(appID string) (title, iconURL string) {
	if appID == "" {
		return "", ""
	}
	manifestName := "appmanifest_" + appID + ".acf"
	for _, lib := range watcher.GetSteamLibraryPaths() {
		manifestPath := filepath.Join(lib, "steamapps", manifestName)
		if data, err := os.ReadFile(manifestPath); err == nil {
			content := string(data)
			reName := regexp.MustCompile(`"name"\s+"([^"]+)"`)
			if m := reName.FindStringSubmatch(content); len(m) > 1 {
				title = strings.TrimSpace(m[1])
				break
			}
		}
	}
	return title, iconURL
}

func resolveSteamIcon(input string) (title, iconURL string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}

	// 1. If input is direct image URL, return it
	lower := strings.ToLower(input)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.HasSuffix(lower, ".ico") || strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".webp") {
			return "", input
		}
	}

	// Extract numeric Steam App ID if present
	appID := ""
	re := regexp.MustCompile(`\b\d{3,9}\b`)
	if m := re.FindString(input); m != "" {
		appID = m
	}

	client := &http.Client{Timeout: 3500 * time.Millisecond}

	// 2. Tier 1 (Highest Quality): Steam Metadata API (api.steamcmd.net) for official name and clienticon .ico hash
	if appID != "" {
		cmdReq, err := http.NewRequest("GET", fmt.Sprintf("https://api.steamcmd.net/v1/info/%s", appID), nil)
		if err == nil {
			cmdReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
			cmdReq.Header.Set("Accept", "application/json")
			cmdReq.Header.Set("Connection", "close")
			cmdClient := &http.Client{Timeout: 4500 * time.Millisecond}
			cmdResp, err := cmdClient.Do(cmdReq)
			if err == nil && cmdResp.StatusCode == http.StatusOK {
				var cRes struct {
					Status string `json:"status"`
					Data   map[string]struct {
						Common struct {
							Name       string `json:"name"`
							ClientIcon string `json:"clienticon"`
							Icon       string `json:"icon"`
						} `json:"common"`
					} `json:"data"`
				}
				if err := json.NewDecoder(cmdResp.Body).Decode(&cRes); err == nil {
					if item, ok := cRes.Data[appID]; ok {
						if title == "" && item.Common.Name != "" {
							title = item.Common.Name
						}
						// Priority 1: High-resolution Windows .ico from Fastly CDN
						if item.Common.ClientIcon != "" {
							iconURL = fmt.Sprintf("https://shared.fastly.steamstatic.com/community_assets/images/apps/%s/%s.ico", appID, item.Common.ClientIcon)
						} else if item.Common.Icon != "" {
							iconURL = fmt.Sprintf("https://shared.fastly.steamstatic.com/community_assets/images/apps/%s/%s.ico", appID, item.Common.Icon)
						}
					}
				}
				cmdResp.Body.Close()
			}
		}

		if title != "" && iconURL != "" && strings.HasSuffix(strings.ToLower(iconURL), ".ico") {
			return title, iconURL
		}
	}

	// 3. Tier 2: Steam Community App Hub (official title + community icon)
	if appID != "" && (title == "" || iconURL == "") {
		hubReq, err := http.NewRequest("GET", fmt.Sprintf("https://steamcommunity.com/app/%s", appID), nil)
		if err == nil {
			hubReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			hubReq.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
			hubReq.Header.Set("Cookie", "birthtime=568022401; wants_mature_content=1; mature_content=1")
			hubResp, err := client.Do(hubReq)
			if err == nil && hubResp.StatusCode == http.StatusOK {
				bodyBytes, _ := io.ReadAll(hubResp.Body)
				hubResp.Body.Close()
				htmlStr := string(bodyBytes)

				// Extract Name: <div class="apphub_AppName ellipsis">...</div>
				if title == "" {
					reName := regexp.MustCompile(`<div[^>]*class="apphub_AppName[^"]*"[^>]*>([^<]+)</div>`)
					if match := reName.FindStringSubmatch(htmlStr); len(match) > 1 {
						title = html.UnescapeString(strings.TrimSpace(match[1]))
					}
				}

				// Extract Icon: <div class="apphub_AppIcon"><img src="...">
				if iconURL == "" {
					reIcon := regexp.MustCompile(`<div[^>]*class="apphub_AppIcon"[^>]*>\s*<img[^>]*src="([^"]+)"`)
					if match := reIcon.FindStringSubmatch(htmlStr); len(match) > 1 {
						rawIcon := strings.TrimSpace(match[1])
						rawIcon = strings.Replace(rawIcon, "http://", "https://", 1)
						iconURL = rawIcon
					}
				}
			}
		}

		if title != "" && iconURL != "" {
			return title, iconURL
		}
	}

	// 3. Secondary: Official Steam Store API (reliable for title)
	if appID != "" {
		storeReq, err := http.NewRequest("GET", fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&l=russian", appID), nil)
		if err == nil {
			storeReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			storeReq.Header.Set("Cookie", "birthtime=568022401; wants_mature_content=1; mature_content=1")
			storeResp, err := client.Do(storeReq)
			if err == nil && storeResp.StatusCode == http.StatusOK {
				var sRes map[string]struct {
					Success bool `json:"success"`
					Data    struct {
						Name string `json:"name"`
					} `json:"data"`
				}
				if err := json.NewDecoder(storeResp.Body).Decode(&sRes); err == nil {
					if app, ok := sRes[appID]; ok && app.Success {
						if title == "" {
							title = app.Data.Name
						}
					}
				}
				storeResp.Body.Close()
			}
		}
	}

	// 4. Tertiary: SearchApps endpoint on steamcommunity.com
	searchQuery := title
	if searchQuery == "" {
		searchQuery = input
	}
	if searchQuery != "" {
		searchReq, err := http.NewRequest("GET", fmt.Sprintf("https://steamcommunity.com/actions/SearchApps/%s", url.PathEscape(searchQuery)), nil)
		if err == nil {
			searchReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			sResp, err := client.Do(searchReq)
			if err == nil && sResp.StatusCode == http.StatusOK {
				var items []struct {
					AppID string `json:"appid"`
					Name  string `json:"name"`
					Icon  string `json:"icon"`
				}
				if err := json.NewDecoder(sResp.Body).Decode(&items); err == nil && len(items) > 0 {
					for _, it := range items {
						if appID != "" && it.AppID == appID {
							if title == "" {
								title = it.Name
							}
							if iconURL == "" && it.Icon != "" {
								iconURL = it.Icon
							}
							break
						}
					}
					if (title == "" || iconURL == "") && len(items) > 0 {
						if title == "" {
							title = items[0].Name
						}
						if iconURL == "" && items[0].Icon != "" {
							iconURL = items[0].Icon
						}
					}
				}
				sResp.Body.Close()
			}
		}
	}

	// 5. Local Steam discovery
	if appID != "" && (title == "" || iconURL == "") {
		locTitle, locIcon := resolveFromLocalSteam(appID)
		if title == "" && locTitle != "" {
			title = locTitle
		}
		if iconURL == "" && locIcon != "" {
			iconURL = locIcon
		}
	}

	// 6. Transparent logo.png or capsule fallback (no ugly header banners)
	if appID != "" && iconURL == "" {
		logoCandidate := fmt.Sprintf("https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/%s/logo.png", appID)
		headResp, err := client.Head(logoCandidate)
		if err == nil && headResp.StatusCode == http.StatusOK {
			iconURL = logoCandidate
		} else {
			capsuleCandidate := fmt.Sprintf("https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/%s/capsule_231x87.jpg", appID)
			iconURL = capsuleCandidate
		}
	}

	return title, iconURL
}

func showNativeDialog(title, text string, style uint32) int {
	tPtr, _ := syscall.UTF16PtrFromString(title)
	mPtr, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := procMsgBox.Call(0, uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), uintptr(style))
	return int(r)
}

func validatePath() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}

	dir := filepath.Dir(exePath)
	hasCyrillicOrSpecial := false
	for _, r := range dir {
		if unicode.Is(unicode.Cyrillic, r) || strings.ContainsRune("!#$%^&*~`'\"", r) {
			hasCyrillicOrSpecial = true
			break
		}
	}

	if hasCyrillicOrSpecial {
		msg := fmt.Sprintf("ВНИМАНИЕ:\nПуть к программе содержит кириллицу или специальные символы:\n\n%s\n\nЭто может привести к сбою сетевого драйвера WinDivert.\nРекомендуется переместить папку программы в корень диска, например:\nC:\\WarLink\n\nПродолжить запуск?", dir)
		res := showNativeDialog("WarLink: Предупреждение о пути", msg, MB_OKCANCEL|MB_ICONEXCLAMATION)
		if res != IDOK {
			os.Exit(0)
		}
	}
}

func acquireSingleInstance() uintptr {
	namePtr, _ := syscall.UTF16PtrFromString("Local\\WarLink_SingleInstance_Mutex")
	h, _, err := procCreateMtx.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if err.(syscall.Errno) == ERROR_ALREADY_EXISTS {
		// Attempt to find and restore existing WarLink window
		wName, _ := syscall.UTF16PtrFromString("WarLink")
		existingHwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(wName)))
		if existingHwnd != 0 {
			procShowWindow.Call(existingHwnd, uintptr(SW_RESTORE))
			procSetForeground.Call(existingHwnd)
			os.Exit(0)
		}

		// If window is not visible (ghost or hidden background process), offer to terminate it
		res := showNativeDialog(
			"WarLink",
			"Программа WarLink уже запущена в фоновом режиме, но её окно скрыто.\n\nЗавершить фоновый процесс и перезапустить программу?",
			MB_OKCANCEL|MB_ICONEXCLAMATION,
		)
		if res == IDOK {
			_ = scanner.KillProcess("WarLink.exe")
			time.Sleep(500 * time.Millisecond)
			h2, _, _ := procCreateMtx.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
			return h2
		}
		os.Exit(0)
	}
	return h
}

func getTrayStatusInfo(state *AppState) (bool, string) {
	if state == nil {
		return false, ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()

	connected := state.eng.IsConnected()
	freeNet := state.eng.IsFreeInternetActive()

	if !connected && !freeNet {
		return false, ""
	}

	var title string
	if connected && state.cfg.SelectedGameID != "" {
		for _, g := range state.cfg.Games {
			if g.ID == state.cfg.SelectedGameID {
				title = g.Title
				if title == "" {
					title = g.SteamAppID
				}
				break
			}
		}
	}

	if title == "" && freeNet {
		title = "Свободный интернет"
	} else if title != "" && freeNet {
		title = title + " + Свободный интернет"
	} else if title == "" && connected {
		title = "Игровой профиль"
	}

	if len([]rune(title)) > 35 {
		title = string([]rune(title)[:32]) + "..."
	}
	return true, title
}

func updateTrayStatus(appTray *tray.Tray, state *AppState) {
	if appTray == nil || state == nil {
		return
	}
	connected, title := getTrayStatusInfo(state)
	appTray.UpdateStatus(connected, title)
}

func minimizeToTray(appTray *tray.Tray, wv webview2.WebView) {
	if wv != nil {
		wv.Dispatch(func() {
			procShowWindow.Call(uintptr(wv.Window()), uintptr(SW_HIDE))
		})
	}
	if appTray != nil {
		appTray.Show()
	}
}

func forceForegroundWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	// 1. Ensure window is not stuck offscreen
	var rect struct {
		Left, Top, Right, Bottom int32
	}
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if rect.Left < -1000 || rect.Top < -1000 {
		sw, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
		sh, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
		posX := (int32(sw) - 520) / 2
		posY := (int32(sh) - 370) / 2
		procSetWindowPos.Call(hwnd, 0, uintptr(posX), uintptr(posY), 520, 370, SWP_SHOWWINDOW)
	}

	// 2. Restore window from minimized or hidden state
	procShowWindow.Call(hwnd, uintptr(SW_RESTORE))
	procShowWindow.Call(hwnd, uintptr(SW_SHOW))

	// 3. Force to top and bring to foreground (standard bypass for Windows foreground lock)
	foreWnd, _, _ := procGetForegroundWindow.Call()
	if foreWnd != 0 && foreWnd != hwnd {
		foreThread, _, _ := procGetWindowThreadProcessId.Call(foreWnd, 0)
		curThread, _, _ := procGetCurrentThreadId.Call()
		if foreThread != curThread && foreThread != 0 {
			procAttachThreadInput.Call(curThread, foreThread, 1)
			procSetWindowPos.Call(hwnd, uintptr(HWND_TOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetWindowPos.Call(hwnd, uintptr(HWND_NOTOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetForeground.Call(hwnd)
			procAttachThreadInput.Call(curThread, foreThread, 0)
		} else {
			procSetWindowPos.Call(hwnd, uintptr(HWND_TOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetWindowPos.Call(hwnd, uintptr(HWND_NOTOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetForeground.Call(hwnd)
		}
	} else {
		procSetWindowPos.Call(hwnd, uintptr(HWND_TOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
		procSetWindowPos.Call(hwnd, uintptr(HWND_NOTOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
		procSetForeground.Call(hwnd)
	}

	// 4. Flash window / taskbar demanding user attention
	procFlashWindow.Call(hwnd, 1)
}

func restoreFromTray(appTray *tray.Tray, wv webview2.WebView) {
	if wv != nil {
		hwnd := uintptr(wv.Window())
		applyWindowIcons(hwnd)
		forceForegroundWindow(hwnd)
		wv.Dispatch(func() {
			applyWindowIcons(hwnd)
			forceForegroundWindow(hwnd)
		})
	}
}

func hookWindowClose(hwnd uintptr, onInterceptClose func() bool) {
	newWndProc := syscall.NewCallback(func(h uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == WM_GETICON {
			initWindowIcons()
			if wParam == uintptr(ICON_SMALL) || wParam == uintptr(ICON_SMALL2) {
				if cachedSmallIcon != 0 {
					return cachedSmallIcon
				}
				return cachedLargeIcon
			}
			if cachedLargeIcon != 0 {
				return cachedLargeIcon
			}
			return cachedSmallIcon
		} else if msg == WM_SHOWWINDOW {
			// Prevent showing the window until DOM is completely painted and revealWindow is called
			if !isWindowReady && wParam != 0 {
				return 0
			}
		} else if msg == WM_CLOSE {
			if onInterceptClose() {
				return 0 // Intercepted, do not destroy window!
			}
		} else if msg == WM_ERASEBKGND {
			if darkBrush != 0 {
				var rect struct {
					Left, Top, Right, Bottom int32
				}
				rect.Right = 520
				rect.Bottom = 370
				procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rect)), darkBrush)
				return 1 // Erased with dark brush!
			}
		} else if msg == 0x0050 { // WM_INPUTLANGCHANGEREQUEST
			r, _, _ := procDefWindowProc.Call(h, uintptr(msg), wParam, lParam)
			return r
		}
		r, _, _ := procCallWindowProc.Call(oldWndProc, h, uintptr(msg), wParam, lParam)
		return r
	})
	nIndex := int32(-4)
	r, _, _ := procSetWindowLongPtr.Call(hwnd, uintptr(uint32(nIndex)), newWndProc)
	oldWndProc = r
}

func main() {
	// 0. Ensure Admin privileges for WinDivert and WinTun kernel drivers
	ensureAdminElevation()

	// 0b. Initialize Windows Job Object to guarantee child daemon termination on exit
	_ = deps.InitGlobalJobObject()

	// 1. Path check
	validatePath()

	// 2. Single instance
	_ = acquireSingleInstance()

	// 3. Config & State
	cfg := config.Load()

	// Auto-heal existing games that have numeric titles, broken header icons or low-res non-.ico icons
	reNumeric := regexp.MustCompile(`^\d{3,9}$`)
	gamesModified := false
	for i, g := range cfg.Games {
		isNumeric := reNumeric.MatchString(g.Title)
		isHeader := strings.Contains(g.IconURL, "header.jpg")
		isNotIco := !strings.HasSuffix(strings.ToLower(g.IconURL), ".ico")
		if g.SteamAppID != "" && (isNumeric || g.Title == g.SteamAppID || g.IconURL == "" || isHeader || isNotIco) {
			sTitle, sIcon := resolveSteamIcon(g.SteamAppID)
			if sTitle != "" && (isNumeric || g.Title == g.SteamAppID) {
				cfg.Games[i].Title = sTitle
				gamesModified = true
			}
			if sIcon != "" && (g.IconURL == "" || isHeader || (isNotIco && strings.HasSuffix(strings.ToLower(sIcon), ".ico"))) {
				cfg.Games[i].IconURL = sIcon
				gamesModified = true
			}
		}
	}
	if gamesModified {
		_ = cfg.Save()
	}

	// All logs saved into warlink_core/warlink.log (1 run = 1 clean log file, clearing previous run)
	_ = deps.EnsureCoreDir()
	logFilePath := filepath.Join(deps.GetCoreDir(), "warlink.log")
	logFile, _ := os.OpenFile(logFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)

	state := &AppState{
		cfg:  cfg,
		logs: []string{},
	}

	var logsMu sync.Mutex
	appendLog := func(msg string) {
		formattedTime := time.Now().Format("2006-01-02 15:04:05")
		line := fmt.Sprintf("[%s] %s\n", formattedTime, msg)
		fmt.Print(line)
		if logFile != nil {
			_, _ = logFile.WriteString(line)
			_ = logFile.Sync()
		}
		logsMu.Lock()
		state.logs = append(state.logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
		if len(state.logs) > 400 {
			state.logs = state.logs[len(state.logs)-400:]
		}
		logsMu.Unlock()
	}

	state.eng = engine.New(cfg, appendLog)

	// Clean termination on OS signals (SIGINT / SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		if state.eng != nil {
			state.eng.Shutdown()
		}
		deps.RestoreWindowsNetworkStack(appendLog)
		deps.CloseGlobalJobObject()
		os.Exit(0)
	}()

	appendLog("[INFO] Инициализация WarLink " + AppVersion + "...")
	appendLog("[OK] Проверка пути и рабочего окружения пройдена")
	appendLog(fmt.Sprintf("[OK] Журнал работы сохраняется в: %s", logFilePath))

	// Ensure core embedded desync components exist on disk synchronously (~15ms)
	if !deps.HasZapret() {
		appendLog("[INFO] Первичная инициализация компонентов сетевого фильтра...")
		if err := deps.PrepareZapret(appendLog); err != nil {
			appendLog(fmt.Sprintf("[ERROR] Ошибка распаковки компонентов: %v", err))
		}
	}

	appendLog("[INFO] Очистка автозапуска и сетевых настроек...")
	deps.SanitizeStartupAndNetwork(appendLog)

	// Background startup orchestrator:
	// 1. Check for updates (blocking in-place updater)
	// 2. Initial component check & setup (if missing WinDivert/singbox/Cloudflare WARP) with blocking UI overlay
	// 3. Free Internet sync (if enabled)
	go func() {
		time.Sleep(300 * time.Millisecond)

		// 1. Check for updates
		checkRes, err := updater.CheckForUpdate(AppVersion)
		if err == nil && checkRes != nil && checkRes.UpdateAvailable {
			appendLog(fmt.Sprintf("[UPDATE] Обнаружена новая версия: %s (текущая: %s)", checkRes.LatestVersion, AppVersion))
			state.mu.Lock()
			state.updateInfo = checkRes
			state.isUpdating = true
			state.updatePct = 5
			state.updateMsg = fmt.Sprintf("Обнаружена версия %s. Подготовка к загрузке...", checkRes.LatestVersion)
			state.mu.Unlock()

			tempExe := filepath.Join(deps.GetCoreDir(), "WarLink_update.tmp")
			defer os.Remove(tempExe)

			dlErr := updater.DownloadWithProgress(checkRes.ExeURL, tempExe, checkRes.AssetSize, func(pct int, msg string) {
				state.mu.Lock()
				state.updatePct = pct
				state.updateMsg = msg
				state.mu.Unlock()
			})
			if dlErr != nil {
				appendLog(fmt.Sprintf("[ERROR] Ошибка загрузки обновления: %v", dlErr))
				state.mu.Lock()
				state.isUpdating = false
				state.mu.Unlock()
			} else {
				ok, vErr := updater.VerifySha256(tempExe, checkRes.Sha256URL)
				if !ok || vErr != nil {
					appendLog(fmt.Sprintf("[ERROR] Ошибка проверки целостности обновления: %v", vErr))
					state.mu.Lock()
					state.isUpdating = false
					state.mu.Unlock()
				} else {
					state.mu.Lock()
					state.updatePct = 100
					state.updateMsg = "Обновление проверено. Сохранение настроек и перезапуск..."
					state.mu.Unlock()

					appendLog("[UPDATE] Верификация пройдена! Сохранение config.json и бесшовная замена...")
					time.Sleep(1200 * time.Millisecond)
					if state.eng != nil {
						state.eng.Shutdown()
					}
					_ = updater.ApplyUpdateAndRestart(tempExe)
					return // Application restarted
				}
			}
		}

		// 2. Initial component check & setup with blocking UI overlay
		needsInit := !deps.HasZapret()
		needsBenchmark := !state.cfg.BenchmarkCompleted
		if needsInit || needsBenchmark {
			appendLog("[INIT] Обнаружен первый запуск или неинициализированные компоненты...")
			state.mu.Lock()
			state.isInitializing = true
			state.initTitle = "Первичная настройка WarLink..."
			state.initPct = 5
			state.initMsg = "Подготовка системных компонентов..."
			state.mu.Unlock()

			// 2.1 Prepare WinDivert / zapret files
			if !deps.HasZapret() {
				state.mu.Lock()
				state.initPct = 10
				state.initMsg = "Распаковка сетевого фильтра WinDivert..."
				state.mu.Unlock()
				if err := deps.PrepareZapret(appendLog); err != nil {
					appendLog(fmt.Sprintf("[ERROR] Ошибка распаковки WinDivert: %v", err))
				}
			}

			// 2.2 Prepare sing-box & wintun
			state.mu.Lock()
			state.initPct = 30
			state.initMsg = "Подготовка игрового роутера sing-box..."
			state.mu.Unlock()
			if err := deps.EnsureSingBoxFiles(appendLog); err != nil {
				appendLog(fmt.Sprintf("[ERROR] Ошибка проверки компонентов sing-box: %v", err))
			}

			// 2.3 Check Stockholm Gateway connectivity
			state.mu.Lock()
			state.initPct = 60
			state.initMsg = "Проверка связи со шлюзом Стокгольм (27 мс)..."
			state.mu.Unlock()
			if gw, err := singbox.GetServerGatewayStatus(); err == nil && gw != nil {
				state.mu.Lock()
				state.gatewayStatus = gw
				state.mu.Unlock()
				appendLog(fmt.Sprintf("[OK] Шлюз Стокгольм доступен (слоты: %d/%d, аренда: %d дн.)", gw.ActiveSessions, gw.MaxSessions, gw.DaysLeft))
			}

			// 2.4 Run full 22-profile DPI benchmark on first run
			if !state.cfg.BenchmarkCompleted && deps.HasZapret() {
				state.mu.Lock()
				state.initTitle = "Глобальное тестирование профилей DPI..."
				state.initPct = 75
				state.initMsg = "Запуск автотестирования 22 профилей под вашу сеть..."
				state.mu.Unlock()

				winner, _, err := scanner.RunFullBenchmark(
					deps.GetZapretDir(),
					func(curr, total int, presetName, logLine string) {
						pct := 75 + int(float64(curr)/float64(total)*24.0)
						state.mu.Lock()
						state.initPct = pct
						state.initMsg = logLine
						state.mu.Unlock()
					},
					appendLog,
				)

				state.mu.Lock()
				if err == nil && winner != "" {
					state.cfg.SelectedAlt = winner
					state.cfg.BenchmarkCompleted = true
					_ = state.cfg.Save()
				} else {
					state.cfg.BenchmarkCompleted = true
					_ = state.cfg.Save()
				}
				state.mu.Unlock()

				if err == nil && winner != "" {
					state.eng.SetSelectedAlt(winner)
					appendLog(fmt.Sprintf("[OK] Установлен лучший профиль обхода: %s", winner))
				}
			}

			state.mu.Lock()
			state.initPct = 100
			state.initMsg = "Все компоненты готовы к работе!"
			state.mu.Unlock()
			appendLog("[OK] Первичная настройка завершена. Все компоненты готовы")
			time.Sleep(600 * time.Millisecond)

			state.mu.Lock()
			state.isInitializing = false
			state.mu.Unlock()
		}

		// 3. Synchronize Free Internet background daemons on startup if enabled in config
		if cfg.FreeInternetEnabled {
			time.Sleep(300 * time.Millisecond)
			if err := state.eng.ToggleFreeInternet(true); err != nil {
				appendLog(fmt.Sprintf("[ERROR] Ошибка запуска режима «Свободный интернет»: %v", err))
			}
		}
	}()


	measureGatewayRTT := func(targetIP string) int {
		if targetIP == "" {
			return 0
		}
		// First try standard ICMP ping (using Windows built-in ping.exe)
		// This bypasses any TCP interception by TUN / local proxies and measures true physical round-trip.
		cmd := exec.Command("ping", targetIP, "-n", "1", "-w", "800")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000,
		}
		if out, err := cmd.Output(); err == nil {
			outStr := string(out)
			// Parse: time=42ms or время=42мс
			idx := strings.Index(strings.ToLower(outStr), "time=")
			if idx == -1 {
				idx = strings.Index(strings.ToLower(outStr), "время=")
			}
			if idx != -1 {
				rem := outStr[idx:]
				eqIdx := strings.Index(rem, "=")
				if eqIdx != -1 {
					rem = strings.TrimSpace(rem[eqIdx+1:])
					var rttVal int
					if _, scanErr := fmt.Sscanf(rem, "%dms", &rttVal); scanErr == nil && rttVal > 0 {
						return rttVal
					}
					if _, scanErr := fmt.Sscanf(rem, "%dмс", &rttVal); scanErr == nil && rttVal > 0 {
						return rttVal
					}
					if _, scanErr := fmt.Sscanf(rem, "%d", &rttVal); scanErr == nil && rttVal > 0 {
						return rttVal
					}
				}
			}
		}

		// Fallback to TCP handshake if ICMP was blocked
		start := time.Now()
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(targetIP, "443"), 1200*time.Millisecond)
		if err != nil {
			conn, err = net.DialTimeout("tcp", net.JoinHostPort(targetIP, "80"), 1200*time.Millisecond)
		}
		if err != nil {
			return 0
		}
		_ = conn.Close()
		ms := int(time.Since(start).Milliseconds())
		if ms <= 5 {
			// <= 5 ms indicates local TUN adapter or loopback interception, not real RTT to Stockholm
			return 0
		}
		return ms
	}

	// Background periodic poller for Stockholm Gateway status & real ping
	go func() {
		pollGateway := func() {
			serverIP := singbox.GetServerIP()
			if serverIP != "" {
				rtt := measureGatewayRTT(serverIP)
				state.mu.Lock()
				if rtt > 5 {
					state.gatewayRealPing = rtt
				} else {
					state.gatewayRealPing = 0
				}
				state.mu.Unlock()
			}
			st, err := singbox.GetServerGatewayStatus()
			state.mu.Lock()
			if err == nil && st != nil {
				state.gatewayStatus = st
			} else {
				state.gatewayStatus = nil
			}
			state.mu.Unlock()
		}
		pollGateway()
		ticker := time.NewTicker(4 * time.Second)
		for range ticker.C {
			pollGateway()
		}
	}()

	// 4. Setup Embedded Web Server for local UI
	subFS, _ := fs.Sub(uiFS, "ui")
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.FS(subFS)))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		pipelineProg := state.eng.GetPipelineProgress()
		matchServer, gamePing, _, gameActive := state.eng.GetGamePing()
		_, lossPct := state.eng.GetTelemetry()
		gwPing := state.gatewayRealPing
		if gwPing <= 1 && state.gatewayStatus != nil && state.gatewayStatus.PingHintMs > 1 {
			gwPing = state.gatewayStatus.PingHintMs
		}

		totalPing := gwPing
		pingLabel := "— мс"
		if gwPing > 0 {
			pingLabel = fmt.Sprintf("%d мс", gwPing)
		}

		gwSlots := "—"
		gwDays := 0
		gwLocation := "Стокгольм, Швеция"
		gwDonateAmount := 98
		if state.gatewayStatus != nil {
			gwSlots = fmt.Sprintf("%d/%d", state.gatewayStatus.ActiveSessions, state.gatewayStatus.MaxSessions)
			gwDays = state.gatewayStatus.DaysLeft
			gwLocation = state.gatewayStatus.Location
			if state.gatewayStatus.DonateAmountRub > 0 {
				gwDonateAmount = state.gatewayStatus.DonateAmountRub
			}
		}
		enableDonate := true
		enableVoting := true
		if state.gatewayStatus != nil {
			if state.gatewayStatus.EnableDonate != nil {
				enableDonate = *state.gatewayStatus.EnableDonate
			}
			if state.gatewayStatus.EnableVoting != nil {
				enableVoting = *state.gatewayStatus.EnableVoting
			}
		}

		resp := map[string]interface{}{
			"version":             AppVersion,
			"is_connected":        state.eng.IsConnected(),
			"ping_ms":             totalPing,
			"gateway_ping":        gwPing,
			"game_ping":           gamePing,
			"total_ping":          totalPing,
			"ping_label":          pingLabel,
			"match_server":        matchServer,
			"game_active":         gameActive,
			"packet_loss":         lossPct,
			"is_busy":             state.isBusy,
			"is_downloading_deps": state.isDownloadingDeps,
			"deps_msg":            state.depsMsg,
			"is_initializing":     state.isInitializing,
			"init_pct":            state.initPct,
			"init_title":          state.initTitle,
			"init_msg":            state.initMsg,
			"profile":             state.eng.GetBestAlt(),
			"available_profiles":  state.eng.FindAvailableAlts(),
			"autolaunch_game":     state.cfg.AutolaunchGame,
			"free_internet":       state.eng.IsFreeInternetActive(),
			"games":               state.cfg.Games,
			"selected_game_id":    state.cfg.SelectedGameID,
			"selected_game":       state.cfg.GetSelectedGame(),
			"is_updating":         state.isUpdating,
			"update_pct":          state.updatePct,
			"update_msg":          state.updateMsg,
			"progress":            pipelineProg,
			"gateway_slots":       gwSlots,
			"gateway_days":        gwDays,
			"gateway_location":    gwLocation,
			"donate_amount_rub":   gwDonateAmount,
			"enable_donate":       enableDonate,
			"enable_voting":       enableVoting,
			"last_error":          state.lastConnectError,
		}
		state.mu.Unlock()

		resp["logs"] = []string{}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/open-log", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			pLog, _ := syscall.UTF16PtrFromString(logFilePath)
			pOpen, _ := syscall.UTF16PtrFromString("open")
			procShellExecute.Call(0, uintptr(unsafe.Pointer(pOpen)), uintptr(unsafe.Pointer(pLog)), 0, 0, uintptr(SW_SHOWNORMAL))
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"opened": true})
	})

	mux.HandleFunc("/api/open-singbox-log", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			sbLogPath := filepath.Join(deps.GetCoreDir(), "singbox", "singbox.log")
			pLog, _ := syscall.UTF16PtrFromString(sbLogPath)
			pOpen, _ := syscall.UTF16PtrFromString("open")
			procShellExecute.Call(0, uintptr(unsafe.Pointer(pOpen)), uintptr(unsafe.Pointer(pLog)), 0, 0, uintptr(SW_SHOWNORMAL))
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"opened": true})
	})

	mux.HandleFunc("/api/server-donate", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			payURL, err := singbox.RequestServerDonate()
			if err != nil || payURL == "" {
				payURL = "https://my.aeza.net/"
			}
			appendLog(fmt.Sprintf("[DONATE] Открытие официальной страницы пополнения сервера (Aeza): %s", payURL))
			pURL, _ := syscall.UTF16PtrFromString(payURL)
			pOpen, _ := syscall.UTF16PtrFromString("open")
			procShellExecute.Call(0, uintptr(unsafe.Pointer(pOpen)), uintptr(unsafe.Pointer(pURL)), 0, 0, uintptr(SW_SHOWNORMAL))
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"opened": true})
	})

	var globalWV webview2.WebView
	var appTray *tray.Tray

	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.isBusy || state.isDownloadingDeps || state.isInitializing || state.isUpdating {
			state.mu.Unlock()
			return
		}
		state.isBusy = true
		state.mu.Unlock()

		go func() {
			defer func() {
				state.mu.Lock()
				state.isBusy = false
				state.mu.Unlock()
			}()

			state.mu.Lock()
			state.lastConnectError = ""
			if state.cfg.SelectedGameID != "" {
				state.cfg.IncrementLaunchCount(state.cfg.SelectedGameID)
			}
			state.mu.Unlock()

			// Run unified 5-stage pipeline
			err := state.eng.ConnectPipeline(func() {
				state.mu.Lock()
				autolaunch := state.cfg.GetSelectedGame().Autolaunch
				state.mu.Unlock()

				updateTrayStatus(appTray, state)

				if autolaunch {
					// On success with autolaunch: minimize to tray after short delay so game takes foreground
					time.Sleep(1500 * time.Millisecond)
					minimizeToTray(appTray, globalWV)
				} else {
					appendLog("[INFO] Сеть оптимизирована и подключена. Окно остается открытым.")
				}
			})
			if err != nil {
				state.mu.Lock()
				state.lastConnectError = err.Error()
				state.mu.Unlock()
				appendLog(fmt.Sprintf("[ERROR] Ошибка подключения: %v", err))
				updateTrayStatus(appTray, state)
			}
		}()

		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.isBusy {
			state.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		state.isBusy = true
		state.lastConnectError = ""
		state.mu.Unlock()

		go func() {
			defer func() {
				state.mu.Lock()
				state.isBusy = false
				state.mu.Unlock()
				updateTrayStatus(appTray, state)
			}()
			_ = state.eng.Disconnect()
		}()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Profile != "" {
			state.mu.Lock()
			if state.isBusy || state.eng.IsConnected() || state.isDownloadingDeps || state.isInitializing || state.isUpdating {
				state.mu.Unlock()
				http.Error(w, "Нельзя изменять профиль при активном подключении или тестировании", http.StatusConflict)
				return
			}
			state.mu.Unlock()
			state.eng.SetSelectedAlt(body.Profile)
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AutolaunchGame bool `json:"autolaunch_game"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			state.mu.Lock()
			state.cfg.AutolaunchGame = body.AutolaunchGame
			_ = state.cfg.Save()
			state.mu.Unlock()
			appendLog(fmt.Sprintf("[INFO] Настройка автозапуска игры обновлена: %v", body.AutolaunchGame))
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/free-internet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			state.mu.Lock()
			if state.isBusy || state.isUpdating || state.isDownloadingDeps || state.isInitializing {
				state.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "Операция уже выполняется",
					"enabled": state.eng.IsFreeInternetActive(),
				})
				return
			}
			state.isBusy = true
			state.mu.Unlock()

			defer func() {
				state.mu.Lock()
				state.isBusy = false
				state.mu.Unlock()
				updateTrayStatus(appTray, state)
			}()

			var body struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				err := state.eng.ToggleFreeInternet(body.Enabled)
				if err != nil {
					appendLog(fmt.Sprintf("[ERROR] Ошибка режима «Свободный интернет»: %v", err))
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": state.eng.IsFreeInternetActive(),
		})
	})

	mux.HandleFunc("/api/run-benchmark", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.isBusy || state.eng.IsConnected() || state.isInitializing {
			state.mu.Unlock()
			http.Error(w, "busy", http.StatusConflict)
			return
		}
		state.isInitializing = true
		state.initTitle = "Тестирование профилей DPI..."
		state.initPct = 5
		state.initMsg = "Подготовка к тестированию..."
		state.mu.Unlock()

		go func() {
			winner, _, err := scanner.RunFullBenchmark(
				deps.GetZapretDir(),
				func(curr, total int, presetName, logLine string) {
					pct := 5 + int(float64(curr)/float64(total)*90.0)
					state.mu.Lock()
					state.initPct = pct
					state.initMsg = logLine
					state.mu.Unlock()
				},
				appendLog,
			)
			state.mu.Lock()
			if err == nil && winner != "" {
				state.cfg.SelectedAlt = winner
				state.cfg.BenchmarkCompleted = true
				_ = state.cfg.Save()
			}
			state.initPct = 100
			state.initMsg = "Тестирование завершено!"
			state.mu.Unlock()

			if err == nil && winner != "" {
				state.eng.SetSelectedAlt(winner)
			}
			time.Sleep(600 * time.Millisecond)
			state.mu.Lock()
			state.isInitializing = false
			state.mu.Unlock()
		}()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
	})

	mux.HandleFunc("/api/games", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"games":            state.cfg.Games,
			"selected_game_id": state.cfg.SelectedGameID,
			"selected_game":    state.cfg.GetSelectedGame(),
		})
	})

	mux.HandleFunc("/api/select-game", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.ID != "" {
			state.mu.Lock()
			state.cfg.UpdateLastPlayed(body.ID)
			state.mu.Unlock()
			updateTrayStatus(appTray, state)
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/resolve-steam", func(w http.ResponseWriter, r *http.Request) {
		appID := strings.TrimSpace(r.URL.Query().Get("appid"))
		if appID == "" {
			http.Error(w, "missing appid", http.StatusBadRequest)
			return
		}
		title, iconURL := resolveSteamIcon(appID)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"title":    title,
			"icon_url": iconURL,
		})
	})

	mux.HandleFunc("/api/add-game", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Title      string `json:"title"`
			ExePath    string `json:"exe_path"`
			SteamAppID string `json:"steam_app_id"`
			IconURL    string `json:"icon_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			title := strings.TrimSpace(body.Title)
			steamAppID := strings.TrimSpace(body.SteamAppID)
			iconURL := strings.TrimSpace(body.IconURL)

			// If title was passed as a pure number, it's actually an App ID
			isNumericTitle := regexp.MustCompile(`^\d{3,9}$`).MatchString(title)
			if steamAppID == "" && isNumericTitle {
				steamAppID = title
			}

			// If we have an App ID, and the title is missing/numeric or icon is missing/header/not .ico, resolve it!
			isNotIco := !strings.HasSuffix(strings.ToLower(iconURL), ".ico")
			if steamAppID != "" && (title == "" || title == steamAppID || isNumericTitle || iconURL == "" || strings.Contains(iconURL, "header.jpg") || isNotIco) {
				sTitle, sIcon := resolveSteamIcon(steamAppID)
				if (title == "" || title == steamAppID || isNumericTitle) && sTitle != "" {
					title = sTitle
				}
				if (iconURL == "" || strings.Contains(iconURL, "header.jpg") || (isNotIco && strings.HasSuffix(strings.ToLower(sIcon), ".ico"))) && sIcon != "" {
					iconURL = sIcon
				}
			}

			if title == "" {
				if body.ExePath != "" {
					base := filepath.Base(body.ExePath)
					title = strings.TrimSuffix(base, filepath.Ext(base))
				} else if steamAppID != "" {
					title = "Steam " + steamAppID
				} else {
					title = "Игра"
				}
			}

			state.mu.Lock()
			newID := fmt.Sprintf("game_%d", time.Now().UnixNano())
			newGame := config.GameProfile{
				ID:           newID,
				Title:        title,
				ExePath:      strings.TrimSpace(body.ExePath),
				SteamAppID:   steamAppID,
				IconURL:      iconURL,
				LastPlayed:   time.Now().Unix(),
				IsDefault:    false,
				LaunchCount:  0,
				PreferredAlt: "general (ALT13)",
			}
			state.cfg.Games = append(state.cfg.Games, newGame)
			state.cfg.SelectedGameID = newID
			_ = state.cfg.Save()
			state.mu.Unlock()
			updateTrayStatus(appTray, state)
			appendLog(fmt.Sprintf("[SHOWCASE] Добавлен ярлык игры: %s", newGame.Title))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(newGame)
			return
		}
		http.Error(w, "invalid game data", http.StatusBadRequest)
	})

	mux.HandleFunc("/api/toggle-autolaunch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.ID != "" {
			state.mu.Lock()
			newVal := state.cfg.ToggleGameAutolaunch(body.ID)
			state.mu.Unlock()
			appendLog(fmt.Sprintf("[CONFIG] Автозапуск игры (%s) изменен: %v", body.ID, newVal))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":         body.ID,
				"autolaunch": newVal,
			})
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
	})

	mux.HandleFunc("/api/increment-launch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.ID != "" {
			state.mu.Lock()
			state.cfg.IncrementLaunchCount(body.ID)
			state.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/delete-game", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.ID != "" {
			state.mu.Lock()
			var remaining []config.GameProfile
			for _, g := range state.cfg.Games {
				if g.ID == body.ID && g.IsDefault {
					remaining = append(remaining, g) // Cannot delete default game
					continue
				}
				if g.ID != body.ID {
					remaining = append(remaining, g)
				}
			}
			state.cfg.Games = remaining
			if state.cfg.SelectedGameID == body.ID && len(remaining) > 0 {
				state.cfg.SelectedGameID = remaining[0].ID
			}
			_ = state.cfg.Save()
			state.mu.Unlock()
			updateTrayStatus(appTray, state)
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/votes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		deviceID := singbox.GetMachineGUID()

		switch r.Method {
		case http.MethodGet:
			res, err := singbox.FetchVotes(deviceID)
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   err.Error(),
				})
				return
			}
			_ = json.NewEncoder(w).Encode(res)

		case http.MethodPost:
			var req struct {
				SteamAppID int    `json:"steam_app_id"`
				Title      string `json:"title"`
				IconURL    string `json:"icon_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Некорректный запрос"})
				return
			}
			ok, errMsg, err := singbox.SubmitVote(deviceID, req.SteamAppID, req.Title, req.IconURL)
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
				return
			}
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": errMsg})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

		case http.MethodDelete:
			var req struct {
				SteamAppID int `json:"steam_app_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Некорректный запрос"})
				return
			}
			ok, err := singbox.RetractVote(deviceID, req.SteamAppID)
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": ok})
		}
	})

	mux.HandleFunc("/api/search-steam", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		term := strings.TrimSpace(r.URL.Query().Get("term"))
		if term == "" || len(term) < 2 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}})
			return
		}

		client := &http.Client{Timeout: 4 * time.Second}

		// 1. Primary: Steam Community SearchApps provides genuine square game icons
		commURL := fmt.Sprintf("https://steamcommunity.com/actions/SearchApps/%s", url.PathEscape(term))
		if resp, err := client.Get(commURL); err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var rawItems []struct {
				AppID string `json:"appid"`
				Name  string `json:"name"`
				Icon  string `json:"icon"`
				Logo  string `json:"logo"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&rawItems); err == nil && len(rawItems) > 0 {
				type ResultItem struct {
					ID        int    `json:"id"`
					Name      string `json:"name"`
					Icon      string `json:"icon"`
					TinyImage string `json:"tiny_image"`
				}
				results := make([]ResultItem, 0, len(rawItems))
				for _, it := range rawItems {
					id, _ := strconv.Atoi(it.AppID)
					if id > 0 {
						icon := it.Icon
						if icon == "" {
							icon = it.Logo
						}
						results = append(results, ResultItem{
							ID:        id,
							Name:      it.Name,
							Icon:      icon,
							TinyImage: icon,
						})
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": results})
				return
			}
		}

		// 2. Fallback: Steam Store Search
		steamURL := fmt.Sprintf("https://store.steampowered.com/api/storesearch/?term=%s&l=russian&cc=US", url.QueryEscape(term))
		resp, err := client.Get(steamURL)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": []interface{}{}})
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(w, resp.Body)
	})

	// Bind loopback port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		showNativeDialog("WarLink: Ошибка", "Не удалось запустить локальный сервер интерфейса.", MB_OK)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()

	appURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	_ = os.WriteFile(filepath.Join(deps.GetCoreDir(), "port.txt"), []byte(fmt.Sprintf("%d", port)), 0644)
	appendLog(fmt.Sprintf("[INFO] Локальный сервер запущен: %s", appURL))

	// 5. Tray setup
	appendLog("[INFO] Настройка системного трея...")
	appTray = tray.New(
		func() { // On tray icon click -> restore window
			restoreFromTray(appTray, globalWV)
		},
		func() { // On toggle connect/disconnect from tray
			state.mu.Lock()
			connected := state.eng.IsConnected()
			freeNet := state.eng.IsFreeInternetActive()
			busy := state.isBusy || state.isDownloadingDeps
			state.mu.Unlock()

			if busy {
				return
			}

			if connected || freeNet {
				appendLog("[INFO] Отключение сети через контекстное меню трея...")
				state.mu.Lock()
				state.isBusy = true
				state.mu.Unlock()

				go func() {
					defer func() {
						state.mu.Lock()
						state.isBusy = false
						state.mu.Unlock()
						updateTrayStatus(appTray, state)
					}()
					if state.eng.IsConnected() {
						_ = state.eng.Disconnect()
					}
					if state.eng.IsFreeInternetActive() {
						_ = state.eng.ToggleFreeInternet(false)
					}
				}()
			} else {
				appendLog("[INFO] Подключение сети через контекстное меню трея...")
				state.mu.Lock()
				state.isBusy = true
				state.mu.Unlock()

				go func() {
					defer func() {
						state.mu.Lock()
						state.isBusy = false
						state.mu.Unlock()
						updateTrayStatus(appTray, state)
					}()

					err := state.eng.ConnectPipeline(func() {
						updateTrayStatus(appTray, state)
						time.Sleep(1500 * time.Millisecond)
						minimizeToTray(appTray, globalWV)
					})
					if err != nil {
						appendLog(fmt.Sprintf("[ERROR] Ошибка подключения из трея: %v", err))
					}
				}()
			}
		},
		func() { // On exit menu
			go func() {
				_ = state.eng.Disconnect()
				if state.eng.IsFreeInternetActive() {
					_ = state.eng.ToggleFreeInternet(false)
				}
			}()
			if globalWV != nil {
				globalWV.Dispatch(func() {
					globalWV.Terminate()
				})
			}
		},
		func() bool { // isConnected
			return state.eng.IsConnected() || state.eng.IsFreeInternetActive()
		},
		func() (bool, string) { // getStatus
			return getTrayStatusInfo(state)
		},
	)
	updateTrayStatus(appTray, state)
	_ = appTray.Start()
	appTray.Show()
	appendLog("[INFO] Системный трей запущен")

	// 6. Game watcher: dynamic active game monitoring with Steam manifest auto-discovery
	appendLog("[INFO] Запуск мониторинга процессов игр...")
	gameWatcher := watcher.New(
		func() watcher.TargetInfo {
			state.mu.Lock()
			g := state.cfg.GetSelectedGame()
			state.mu.Unlock()
			return watcher.ResolveGameProcessNames(g.ID, g.SteamAppID, g.Title, g.ExePath, g.ProcessNames)
		},
		func(name string) {
			appendLog(fmt.Sprintf("[GAME] Обнаружен запуск %s. Сетевая оптимизация активна.", name))
		},
		func(name string) {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "launcher") || strings.Contains(lower, "setup") || strings.Contains(lower, "update") {
				appendLog(fmt.Sprintf("[GAME] Лаунчер %s завершил работу, ожидание запуска игрового клиента...", name))
				// Debounce: if user simply closed the launcher without starting the game client,
				// check after 12 seconds if any game process is active. If not, restore window and disconnect cleanly!
				go func() {
					time.Sleep(12 * time.Second)
					state.mu.Lock()
					g := state.cfg.GetSelectedGame()
					state.mu.Unlock()
					running, _ := watcher.IsAnyProcessRunning(g.ProcessNames, watcher.NormalizeGameToken(g.Title))
					if !running && state.eng.IsConnected() {
						appendLog("[GAME] Игровой клиент не был запущен после закрытия лаунчера. Завершение сессии...")
						restoreFromTray(appTray, globalWV)
						_ = state.eng.Disconnect()
						updateTrayStatus(appTray, state)
					}
				}()
				return
			}
			appendLog(fmt.Sprintf("[GAME] Игра %s закрыта. Возврат интерфейса WarLink...", name))
			// 1. Force window to foreground instantly
			restoreFromTray(appTray, globalWV)

			// 2. Disconnect game tunnel if Free Internet is not active
			go func() {
				_ = state.eng.Disconnect()
				updateTrayStatus(appTray, state)
			}()
		},
		func(gameID, procName string) {
			if strings.Contains(strings.ToLower(procName), "launcher") {
				return // Do not register launcher executables as game process targets
			}
			state.mu.Lock()
			state.cfg.AddGameProcess(gameID, procName)
			state.mu.Unlock()
			if state.eng.IsConnected() {
				_ = state.eng.AddGameProcess(procName)
			}
		},
	)
	gameWatcher.Start()
	defer gameWatcher.Stop()

	// 7. Initialize Native WebView2 Window (Embedded directly in WarLink.exe process)
	// Create dark background brush (#161616) to eliminate white background flash
	b, _, _ := procCreateSolidBrush.Call(0x00161616) // RGB(0x16, 0x16, 0x16)
	darkBrush = b

	// Install WH_CBT hook to intercept window creation BEFORE it is shown or painted
	// Strip WS_VISIBLE, position offscreen (-32000, -32000), strip captions
	tid, _, _ := procGetCurrentThreadId.Call()
	var hHook uintptr
	hookCb := syscall.NewCallback(func(nCode int32, wParam uintptr, lParam unsafe.Pointer) uintptr {
		if nCode == HCBT_CREATEWND {
			cbt := (*CBT_CREATEWND)(lParam)
			if cbt != nil && cbt.Lpcs != nil {
				// Strip caption, thick frame, maximize box, and visible flag
				cbt.Lpcs.Style &^= (WS_CAPTION | WS_THICKFRAME | WS_MAXIMIZEBOX | WS_VISIBLE)
				cbt.Lpcs.Style |= WS_BORDER
				cbt.Lpcs.X = -32000
				cbt.Lpcs.Y = -32000
				cbt.Lpcs.Cx = 520
				cbt.Lpcs.Cy = 370
			}
		}
		r, _, _ := procCallNextHookEx.Call(hHook, uintptr(nCode), wParam, uintptr(lParam))
		return r
	})
	hHook, _, _ = procSetWindowsHookEx.Call(uintptr(WH_CBT), hookCb, 0, tid)

	dataDir := filepath.Join(deps.GetCoreDir(), "webview_data")
	appendLog("[INFO] Инициализация WebView2 оконного модуля...")
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "WarLink",
			Width:  520,
			Height: 370,
			Center: false, // Positioned offscreen until revealed
		},
	})

	if hHook != 0 {
		procUnhookWindowsHookEx.Call(hHook)
	}

	if wv == nil {
		appendLog("[ERROR] webview2.NewWithOptions вернул nil!")
		showNativeDialog("WarLink: Ошибка", "Не удалось создать нативное окно интерфейса (требуется WebView2 Runtime).", MB_OK)
		return
	}
	appendLog("[INFO] WebView2 успешно создан, переход к привязке событий...")
	globalWV = wv

	// Disable Edge/Chromium browser accelerator keys so Alt/Shift layout switching reaches the window
	func() {
		defer func() {
			_ = recover()
		}()
		val := reflect.ValueOf(wv).Elem()
		f := val.FieldByName("browser")
		if !f.IsValid() {
			return
		}
		ptr := unsafe.Pointer(f.UnsafeAddr())
		type rawInterface struct {
			tab  unsafe.Pointer
			data unsafe.Pointer
		}
		raw := (*rawInterface)(ptr)
		if raw.data == nil {
			return
		}
		chrom := (*edge.Chromium)(raw.data)
		if settings, err := chrom.GetSettings(); err == nil && settings != nil {
			_ = settings.PutAreBrowserAcceleratorKeysEnabled(false)
		}
		chrom.AcceleratorKeyCallback = func(vKey uint) bool {
			return false
		}
	}()

	hwnd := uintptr(wv.Window())

	// Ensure window stays hidden initially and set to exact size
	procShowWindow.Call(hwnd, uintptr(SW_HIDE))

	// Ensure frameless fixed-size properties
	nIndex := int32(-16) // GWL_STYLE
	style, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(uint32(nIndex)))
	style &^= (WS_CAPTION | WS_THICKFRAME | WS_MAXIMIZEBOX | WS_VISIBLE)
	style |= WS_BORDER
	procSetWindowLongPtr.Call(hwnd, uintptr(uint32(nIndex)), style)
	var offscreenVal int32 = -32000
	offscreenCoord := uintptr(uint32(offscreenVal))
	procSetWindowPos.Call(hwnd, 0, offscreenCoord, offscreenCoord, 520, 370, 0x0004|0x0020|0x0080) // SWP_NOZORDER | SWP_FRAMECHANGED | SWP_HIDEWINDOW
	wv.SetSize(520, 370, webview2.HintFixed)
	applyWindowIcons(hwnd)

	// Hook window close [X] and WM_SHOWWINDOW early to intercept any premature show
	hookWindowClose(hwnd, func() bool {
		state.mu.Lock()
		active := state.eng.IsConnected() || state.eng.IsFreeInternetActive()
		state.mu.Unlock()

		if active {
			minimizeToTray(appTray, globalWV)
			return true // Intercepted, keep running in background tray!
		}
		return false // Clean exit when disconnected
	})

	// Bind custom titlebar actions to Go
	_ = wv.Bind("revealWindow", func() error {
		globalWV.Dispatch(func() {
			revealMainWindow(hwnd)
		})
		return nil
	})

	_ = wv.Bind("dragWindow", func() error {
		procReleaseCapture.Call()
		procPostMessage.Call(hwnd, uintptr(0x00A1), uintptr(2), 0) // WM_NCLBUTTONDOWN, HTCAPTION via non-blocking PostMessage
		return nil
	})

	_ = wv.Bind("minimizeWindow", func() error {
		procShowWindow.Call(hwnd, uintptr(SW_MINIMIZE))
		return nil
	})

	_ = wv.Bind("closeWindow", func() error {
		state.mu.Lock()
		active := state.eng.IsConnected() || state.eng.IsFreeInternetActive()
		state.mu.Unlock()

		if active {
			minimizeToTray(appTray, globalWV)
			return nil
		}

		// Clean exit: disconnect network filters and terminate webview
		go func() {
			_ = state.eng.Disconnect()
			if state.eng.IsFreeInternetActive() {
				_ = state.eng.ToggleFreeInternet(false)
			}
		}()
		if globalWV != nil {
			globalWV.Dispatch(func() {
				globalWV.Terminate()
			})
		}
		return nil
	})

	_ = wv.Bind("openLogFileNative", func() error {
		pLog, _ := syscall.UTF16PtrFromString(logFilePath)
		pOpen, _ := syscall.UTF16PtrFromString("open")
		procShellExecute.Call(0, uintptr(unsafe.Pointer(pOpen)), uintptr(unsafe.Pointer(pLog)), 0, 0, uintptr(SW_SHOWNORMAL))
		return nil
	})

	_ = wv.Bind("switchKeyboardLayout", func() error {
		globalWV.Dispatch(func() {
			procActivateKeyboardLayout.Call(1, 0) // HKL_NEXT = 1
		})
		return nil
	})

	wv.Navigate(appURL)

	// Failsafe: guarantee window is revealed even if JS or WebView2 DOM is delayed
	go func() {
		time.Sleep(600 * time.Millisecond)
		if globalWV != nil {
			globalWV.Dispatch(func() {
				revealMainWindow(hwnd)
			})
		}
	}()

	// Run message loop on main thread
	wv.Run()

	// Teardown when window is closed
	// 1. Hide window immediately so no dead window ever remains on screen
	procShowWindow.Call(hwnd, uintptr(SW_HIDE))
	appTray.Hide()

	// 2. Full unconditional teardown of all network tunnels, processes and WinDivert drivers on application exit
	state.eng.Shutdown()
	deps.RestoreWindowsNetworkStack(appendLog)
	deps.CloseGlobalJobObject()

	_ = server.Close()
	wv.Destroy()
	os.Exit(0)
}
