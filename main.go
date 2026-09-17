package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/jchv/go-webview2"
	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/engine"
	"warlink/internal/scanner"
	"warlink/internal/tray"
	"warlink/internal/watcher"
)

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
	modShell32             = syscall.NewLazyDLL("shell32.dll")
	procShellExecute       = modShell32.NewProc("ShellExecuteW")
)

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

func revealMainWindow(hwnd uintptr) {
	if isWindowReady {
		return
	}
	isWindowReady = true

	// Calculate center screen coordinates
	sw, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	sh, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	var winW int32 = 420
	var winH int32 = 270
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

func minimizeToTray(appTray *tray.Tray, wv webview2.WebView) {
	if wv != nil {
		wv.Dispatch(func() {
			procShowWindow.Call(uintptr(wv.Window()), uintptr(SW_HIDE))
		})
	}
	appTray.Show()
	appTray.SetTooltip("WarLink [АКТИВЕН] - Кликните для открытия")
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
		posX := (int32(sw) - 420) / 2
		posY := (int32(sh) - 270) / 2
		procSetWindowPos.Call(hwnd, 0, uintptr(posX), uintptr(posY), 420, 270, SWP_SHOWWINDOW)
	}

	// 2. Restore window from minimized or hidden state
	procShowWindow.Call(hwnd, uintptr(SW_RESTORE))
	procShowWindow.Call(hwnd, uintptr(SW_SHOW))

	// 3. Force to top and bring to foreground (standard bypass for Windows foreground lock)
	procSetWindowPos.Call(hwnd, uintptr(HWND_TOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
	procSetWindowPos.Call(hwnd, uintptr(HWND_NOTOPMOST), 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
	procSetForeground.Call(hwnd)

	// 4. Flash window / taskbar demanding user attention
	procFlashWindow.Call(hwnd, 1)
}

func restoreFromTray(appTray *tray.Tray, wv webview2.WebView) {
	if wv != nil {
		wv.Dispatch(func() {
			forceForegroundWindow(uintptr(wv.Window()))
		})
	}
}

func hookWindowClose(hwnd uintptr, onInterceptClose func() bool) {
	newWndProc := syscall.NewCallback(func(h uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == WM_SHOWWINDOW {
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
				rect.Right = 420
				rect.Bottom = 270
				procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rect)), darkBrush)
				return 1 // Erased with dark brush!
			}
		}
		r, _, _ := procCallWindowProc.Call(oldWndProc, h, uintptr(msg), wParam, lParam)
		return r
	})
	nIndex := int32(-4)
	r, _, _ := procSetWindowLongPtr.Call(hwnd, uintptr(uint32(nIndex)), newWndProc)
	oldWndProc = r
}

func main() {
	// 1. Path check
	validatePath()

	// 2. Single instance
	_ = acquireSingleInstance()

	// 3. Config & State
	cfg := config.Load()

	// All logs saved into warlink_core/warlink.log (1 run = 1 clean log file, clearing previous run)
	_ = deps.EnsureCoreDir()
	logFilePath := filepath.Join(deps.GetCoreDir(), "warlink.log")
	logFile, _ := os.OpenFile(logFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)

	state := &AppState{
		cfg:  cfg,
		logs: []string{},
	}

	if !deps.HasZapret() {
		state.isDownloadingDeps = true
		state.depsMsg = "Загрузка компонентов сетевой оптимизации с GitHub..."
	}

	appendLog := func(msg string) {
		state.mu.Lock()
		defer state.mu.Unlock()
		formattedTime := time.Now().Format("2006-01-02 15:04:05")
		line := fmt.Sprintf("[%s] %s\n", formattedTime, msg)
		if logFile != nil {
			_, _ = logFile.WriteString(line)
		}
		state.logs = append(state.logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
		if len(state.logs) > 400 {
			state.logs = state.logs[len(state.logs)-400:]
		}
	}

	state.eng = engine.New(cfg, appendLog)

	appendLog("[INFO] Инициализация WarLink v1.0.1...")
	appendLog("[OK] Проверка пути и рабочего окружения пройдена")
	appendLog(fmt.Sprintf("[OK] Журнал работы сохраняется в: %s", logFilePath))

	// Ensure clean system network state and remove WARP from Windows startup
	deps.SanitizeStartupAndNetwork(appendLog)

	// Check and log official Zapret test report if available
	if reportPath, err := engine.FindLatestZapretReport(); err == nil {
		if summary, err := engine.ParseZapretReport(reportPath); err == nil && len(summary.Scores) > 0 {
			appendLog(fmt.Sprintf("[ZAPRET TEST] Обнаружен отчет тестирования: %s", summary.ReportFile))
			top := summary.Scores[0]
			appendLog(fmt.Sprintf("[ZAPRET TEST] Лучшая проверенная конфигурация: %s (%d OK, %d ERR)", top.ConfigName, top.OkCount, top.ErrCount))
			if cfg.SelectedAlt == "" {
				cfg.SelectedAlt = summary.BestConfig
				_ = cfg.Save()
			}
		}
	}

	// Ensure core dependencies in background
	go func() {
		if !deps.HasZapret() {
			err := deps.PrepareZapret(appendLog)
			state.mu.Lock()
			state.isDownloadingDeps = false
			if err != nil {
				state.depsMsg = "Ошибка загрузки: " + err.Error()
				appendLog(fmt.Sprintf("[ERROR] Ошибка загрузки компонентов Zapret: %v", err))
			} else {
				state.depsMsg = ""
				appendLog("[OK] Загрузка компонентов Zapret успешно завершена")
			}
			state.mu.Unlock()
		}
	}()

	// 4. Setup Embedded Web Server for local UI
	subFS, _ := fs.Sub(uiFS, "ui")
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.FS(subFS)))

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		pipelineProg := state.eng.GetPipelineProgress()
		resp := map[string]interface{}{
			"is_connected":        state.eng.IsConnected(),
			"is_busy":             state.isBusy,
			"is_testing":          state.eng.IsTesting(),
			"is_downloading_deps": state.isDownloadingDeps,
			"deps_msg":            state.depsMsg,
			"profile":             state.eng.GetBestAlt(),
			"available_profiles":  state.eng.FindAvailableAlts(),
			"autolaunch_game":     state.cfg.AutolaunchGame,
			"logs":                state.logs,
			"progress":            pipelineProg,
			"benchmark":           pipelineProg,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/run-test", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.isBusy || state.eng.IsConnected() || state.eng.IsTesting() || state.isDownloadingDeps {
			state.mu.Unlock()
			http.Error(w, "Операция уже выполняется или компоненты еще загружаются", http.StatusConflict)
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
			_ = state.eng.RunFullZapretTest()
		}()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"started": true})
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

	var globalWV webview2.WebView
	var appTray *tray.Tray

	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		if state.isBusy || state.isDownloadingDeps {
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

			// Run unified 5-stage pipeline
			err := state.eng.ConnectPipeline(func() {
				state.mu.Lock()
				autolaunch := state.cfg.AutolaunchGame
				state.mu.Unlock()

				if autolaunch {
					// On success with autolaunch: minimize to tray after short delay so game takes foreground
					time.Sleep(1500 * time.Millisecond)
					minimizeToTray(appTray, globalWV)
				} else {
					appendLog("[INFO] Сеть оптимизирована и подключена. Окно остается открытым.")
				}
			})
			if err != nil {
				appendLog(fmt.Sprintf("[ERROR] Ошибка подключения: %v", err))
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
		state.mu.Unlock()

		go func() {
			defer func() {
				state.mu.Lock()
				state.isBusy = false
				state.mu.Unlock()
			}()
			_ = state.eng.Disconnect()
			if appTray != nil {
				appTray.SetTooltip("WarLink [ГОТОВ] - Кликните для открытия")
			}
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
			if state.isBusy || state.eng.IsConnected() || state.eng.IsTesting() || state.isDownloadingDeps {
				state.mu.Unlock()
				http.Error(w, "Нельзя изменять профиль при активном подключении или тестировании", http.StatusConflict)
				return
			}
			state.eng.SetSelectedAlt(body.Profile)
			state.mu.Unlock()
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

	// 5. Tray setup
	appTray = tray.New(
		func() { // On tray icon click -> restore window
			restoreFromTray(appTray, globalWV)
		},
		func() { // On toggle connect/disconnect from tray
			state.mu.Lock()
			connected := state.eng.IsConnected()
			busy := state.isBusy || state.isDownloadingDeps || state.eng.IsTesting()
			state.mu.Unlock()

			if busy {
				return
			}

			if connected {
				appendLog("[INFO] Отключение сети через контекстное меню трея...")
				state.mu.Lock()
				state.isBusy = true
				state.mu.Unlock()

				go func() {
					defer func() {
						state.mu.Lock()
						state.isBusy = false
						state.mu.Unlock()
					}()
					_ = state.eng.Disconnect()
					if appTray != nil {
						appTray.SetTooltip("WarLink [ГОТОВ] - Кликните для открытия")
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
					}()

					err := state.eng.ConnectPipeline(func() {
						time.Sleep(1500 * time.Millisecond)
						minimizeToTray(appTray, globalWV)
					})
					if err != nil {
						appendLog(fmt.Sprintf("[ERROR] Ошибка подключения из трея: %v", err))
					} else if appTray != nil {
						appTray.SetTooltip("WarLink [ПОДКЛЮЧЕНО] - Кликните для открытия")
					}
				}()
			}
		},
		func() { // On exit menu
			if globalWV != nil {
				globalWV.Dispatch(func() {
					globalWV.Terminate()
				})
			}
		},
		func() bool { // isConnected
			return state.eng.IsConnected()
		},
	)
	_ = appTray.Start()
	appTray.Show()

	// 6. Game watcher: when WARDOGS exits, restore WarLink and disconnect
	gameWatcher := watcher.New(
		func() {
			appendLog("[INFO] Обнаружен запуск WARDOGS. Сетевая оптимизация активна.")
		},
		func() {
			appendLog("[INFO] Игра WARDOGS закрыта. Моментальный возврат приложения и отключение...")
			// 1. Immediately force window to foreground demanding user attention!
			if globalWV != nil {
				globalWV.Dispatch(func() {
					forceForegroundWindow(uintptr(globalWV.Window()))
				})
			}
			// 2. Disconnect network in background so UI pops up with zero delay
			go func() {
				_ = state.eng.Disconnect()
			}()
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
	hookCb := syscall.NewCallback(func(nCode int32, wParam, lParam uintptr) uintptr {
		if nCode == HCBT_CREATEWND {
			cbt := (*CBT_CREATEWND)(unsafe.Pointer(lParam))
			if cbt != nil && cbt.Lpcs != nil {
				// Strip caption, thick frame, maximize box, and visible flag
				cbt.Lpcs.Style &^= (WS_CAPTION | WS_THICKFRAME | WS_MAXIMIZEBOX | WS_VISIBLE)
				cbt.Lpcs.Style |= WS_BORDER
				cbt.Lpcs.X = -32000
				cbt.Lpcs.Y = -32000
				cbt.Lpcs.Cx = 420
				cbt.Lpcs.Cy = 270
			}
		}
		r, _, _ := procCallNextHookEx.Call(hHook, uintptr(nCode), wParam, lParam)
		return r
	})
	hHook, _, _ = procSetWindowsHookEx.Call(uintptr(WH_CBT), hookCb, 0, tid)

	dataDir := filepath.Join(deps.GetCoreDir(), "webview_data")
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "WarLink",
			Width:  420,
			Height: 270,
			Center: false, // Positioned offscreen until revealed
		},
	})

	if hHook != 0 {
		procUnhookWindowsHookEx.Call(hHook)
	}

	if wv == nil {
		showNativeDialog("WarLink: Ошибка", "Не удалось создать нативное окно интерфейса (требуется WebView2 Runtime).", MB_OK)
		return
	}
	globalWV = wv

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
	procSetWindowPos.Call(hwnd, 0, offscreenCoord, offscreenCoord, 420, 270, 0x0004|0x0020|0x0080) // SWP_NOZORDER | SWP_FRAMECHANGED | SWP_HIDEWINDOW
	wv.SetSize(420, 270, webview2.HintFixed)

	// Hook window close [X] and WM_SHOWWINDOW early to intercept any premature show
	hookWindowClose(hwnd, func() bool {
		state.mu.Lock()
		connected := state.eng.IsConnected()
		busy := state.isBusy
		state.mu.Unlock()

		if connected || busy {
			minimizeToTray(appTray, globalWV)
			return true // Intercept close!
		}
		// Disconnected -> allow exit
		return false
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
		connected := state.eng.IsConnected()
		busy := state.isBusy
		state.mu.Unlock()

		if connected || busy {
			minimizeToTray(appTray, globalWV)
		} else {
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

	// 2. Safe timeout for network disconnect on exit (max 2.5s)
	done := make(chan struct{})
	go func() {
		_ = state.eng.Disconnect()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2500 * time.Millisecond):
		_ = scanner.KillProcess("winws.exe")
	}

	_ = server.Close()
	wv.Destroy()
	os.Exit(0)
}
