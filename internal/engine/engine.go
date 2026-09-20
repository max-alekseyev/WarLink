package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/desync"
	"warlink/internal/hostlist"
	"warlink/internal/pingmeter"
	"warlink/internal/scanner"
	"warlink/internal/singbox"
)

type PipelineProgress struct {
	IsRunning  bool   `json:"is_running"`
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Percent    int    `json:"percent"`
	Config     string `json:"config"`
	BestConfig string `json:"best_config"`
	Message    string `json:"message"`
}

type Engine struct {
	mu                 sync.Mutex
	transitionMu       sync.Mutex
	cfg                *config.Config
	zapretCmd          *exec.Cmd
	isConnected        bool
	isConnecting       bool
	selectedAlt        string
	freeInternetActive bool
	logCallback        func(string)
	pipelineProgress   PipelineProgress
	hostlistMgr        *hostlist.Manager
	singboxMgr         *singbox.Manager
	telemetry          *TelemetryMonitor
	pingMeter          *pingmeter.Meter
	watchdogStop       chan struct{}
}

func New(cfg *config.Config, logCb func(string)) *Engine {
	eng := &Engine{
		cfg:                cfg,
		selectedAlt:        cfg.SelectedAlt,
		logCallback:        logCb,
		hostlistMgr:        hostlist.NewManager(logCb),
		freeInternetActive: cfg.FreeInternetEnabled,
		singboxMgr:         singbox.NewManager(deps.GetCoreDir()),
		telemetry:          NewTelemetryMonitor(),
		pingMeter:          pingmeter.New(filepath.Join(deps.GetCoreDir(), "zapret", "bin")),
	}
	if eng.pingMeter != nil {
		eng.pingMeter.SetUpdateCallback(func(server string, wireRttMs int, inGameEstMs int) {
			eng.log(fmt.Sprintf("[PING] Матч WARDOGS (%s): Сеть = %d мс | Оверлей игры ~ %d мс", server, wireRttMs, inGameEstMs))
		})
	}
	// Sync upstream lists in background
	eng.hostlistMgr.SyncUpstream()
	return eng
}

func (e *Engine) log(msg string) {
	if e.logCallback != nil {
		e.logCallback(msg)
	}
}

func (e *Engine) IsConnected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isConnected
}

func (e *Engine) IsFreeInternetActive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.freeInternetActive
}

func (e *Engine) ToggleFreeInternet(enable bool) error {
	e.transitionMu.Lock()
	defer e.transitionMu.Unlock()

	e.mu.Lock()
	isConn := e.isConnected
	e.mu.Unlock()

	if enable {
		e.log("[FREE NET] Включение режима «Свободный интернет» (YouTube, Discord, Telegram, WhatsApp, web)...")
		if !deps.HasZapret() {
			if err := deps.PrepareZapret(e.log); err != nil {
				return fmt.Errorf("ошибка загрузки компонентов: %w", err)
			}
		}
		if _, err := e.hostlistMgr.ExportFreeInternetList(); err != nil {
			e.log(fmt.Sprintf("[WARN] Ошибка экспорта списка доменов: %v", err))
		}
		if isConn {
			_ = e.stopWinws()
		}
		if err := e.EnsureWinwsRunning(); err != nil {
			return err
		}

		// Ensure sing-box is active with Stockholm Hysteria 2 for IP-blocked services
		if err := deps.EnsureSingBoxFiles(e.log); err != nil {
			return fmt.Errorf("ошибка подготовки sing-box: %w", err)
		}

		if e.singboxMgr != nil {
			var targets []string
			if isConn {
				targetGameProcess := "WardogsClient-Win64-Shipping.exe"
				selectedGame := e.cfg.GetSelectedGame()
				if selectedGame.ExePath != "" {
					targetGameProcess = filepath.Base(selectedGame.ExePath)
				}
				targets = []string{targetGameProcess}
				for _, p := range selectedGame.ProcessNames {
					if p != "" && p != targetGameProcess && !strings.Contains(strings.ToLower(p), "launcher") {
						targets = append(targets, p)
					}
				}
			}
			selectedGameID := "free_internet"
			if isConn {
				selectedGameID = e.cfg.GetSelectedGame().ID
				if selectedGameID == "" {
					selectedGameID = "wardogs"
				}
			}
			if err := e.singboxMgr.Start(targets, true, e.log, selectedGameID); err != nil {
				// Rollback and release session immediately on failure
				if !isConn {
					_ = e.stopWinws()
					_ = singbox.ReleaseSession()
				}
				return fmt.Errorf("ошибка запуска шлюза Стокгольм: %w", err)
			}
		}

		e.mu.Lock()
		e.freeInternetActive = true
		e.cfg.FreeInternetEnabled = true
		_ = e.cfg.Save()
		e.mu.Unlock()

		e.startWatchdog()
		if e.telemetry != nil {
			serverIP := singbox.GetServerIP()
			if serverIP != "" {
				e.telemetry.SetDirectTarget(serverIP)
			}
			e.telemetry.Start(false)
		}
		if e.pingMeter != nil {
			_ = e.pingMeter.Start()
		}
		return nil
	} else {
		e.log("[FREE NET] Отключение режима «Свободный интернет» (браузер возвращен на прямой интернет)...")
		e.mu.Lock()
		e.freeInternetActive = false
		e.cfg.FreeInternetEnabled = false
		_ = e.cfg.Save()
		e.mu.Unlock()

		if isConn {
			_ = e.stopWinws()
			_ = e.EnsureWinwsRunning()
			if e.singboxMgr != nil {
				targetGameProcess := "WardogsClient-Win64-Shipping.exe"
				selectedGame := e.cfg.GetSelectedGame()
				if selectedGame.ExePath != "" {
					targetGameProcess = filepath.Base(selectedGame.ExePath)
				}
				targets := []string{targetGameProcess}
				for _, p := range selectedGame.ProcessNames {
					if p != "" && p != targetGameProcess && !strings.Contains(strings.ToLower(p), "launcher") {
						targets = append(targets, p)
					}
				}
				_ = e.singboxMgr.Start(targets, false, e.log)
			}
			return nil
		}
		e.stopWatchdog()
		if e.singboxMgr != nil {
			_ = e.singboxMgr.Stop()
		}
		// Release slot immediately when Free Internet is disabled
		go func() {
			_ = singbox.ReleaseSession()
		}()
		return e.stopWinws()
	}
}

func (e *Engine) EnsureWinwsRunning() error {
	e.mu.Lock()
	if e.zapretCmd != nil && e.zapretCmd.Process != nil {
		pid := e.zapretCmd.Process.Pid
		e.mu.Unlock()
		check := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV")
		check.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := check.Output()
		if err == nil && strings.Contains(string(out), strconv.Itoa(pid)) {
			return nil // Already running!
		}
	} else {
		e.mu.Unlock()
	}

	bestAlt := e.GetBestAlt()
	zapretDir := deps.GetZapretDir()
	winwsPath := filepath.Join(zapretDir, "bin", "winws2.exe")

	if gwIP := singbox.GetServerIP(); gwIP != "" {
		_ = deps.AddGatewayToZapretExclude(gwIP)
	}

	_, _ = e.hostlistMgr.ExportFreeInternetList()

	e.mu.Lock()
	isFreeNet := e.freeInternetActive
	isConn := e.isConnected
	e.mu.Unlock()

	selectedGame := e.cfg.GetSelectedGame()
	if isConn && selectedGame.PreferredAlt != "" {
		bestAlt = selectedGame.PreferredAlt
	}

	preset := desync.GetPreset(bestAlt)
	if preset == nil {
		preset = desync.GetPreset("general (ALT13)")
	}

	args := preset.BuildModularArgs(zapretDir, isFreeNet)
	if isFreeNet && isConn {
		e.log(fmt.Sprintf("[INFO] Запуск winws2.exe (%s) в композитном режиме (Свободный интернет + Игра)...", preset.Name))
	} else if isFreeNet {
		e.log(fmt.Sprintf("[INFO] Запуск winws2.exe (%s) в режиме «Свободный интернет» (YouTube/Discord/Web)...", preset.Name))
	} else {
		e.log(fmt.Sprintf("[INFO] Запуск winws2.exe (%s) в селективном игровом режиме...", preset.Name))
	}

	zCmd := exec.Command(winwsPath, args...)
	zCmd.Dir = filepath.Dir(winwsPath)
	zCmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	if logFile, err := os.OpenFile(filepath.Join(zapretDir, "winws2.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		zCmd.Stdout = logFile
		zCmd.Stderr = logFile
	}
	if err := zCmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска winws2: %w", err)
	}

	e.mu.Lock()
	e.zapretCmd = zCmd
	e.mu.Unlock()

	winwsReady := false
	for i := 0; i < 8; i++ {
		time.Sleep(100 * time.Millisecond)
		check := exec.Command("tasklist", "/FI", "IMAGENAME eq winws2.exe", "/NH", "/FO", "CSV")
		check.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := check.Output()
		if err == nil && strings.Contains(strings.ToLower(string(out)), "winws2") {
			winwsReady = true
			break
		}
	}
	if !winwsReady {
		return fmt.Errorf("winws2.exe не отвечает. Запустите от имени администратора")
	}

	e.log("[OK] Сетевой фильтр WinDivert и winws2 активны")
	return nil
}

func (e *Engine) stopWinws() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.log("[INFO] Завершение сетевого фильтра (winws2.exe)...")
	_ = scanner.KillProcess("winws2.exe")
	_ = scanner.KillProcess("winws.exe")

	if e.zapretCmd != nil && e.zapretCmd.Process != nil {
		pid := e.zapretCmd.Process.Pid
		_ = e.zapretCmd.Process.Kill()
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = killCmd.Run()
		e.zapretCmd = nil
	}

	scanner.StopWinDivertService()
	return nil
}

func (e *Engine) setProgress(percent int, current, total int, cfgName, msg string, isRunning bool) {
	e.mu.Lock()
	if isRunning && percent > 99 {
		percent = 99
	} else if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	e.pipelineProgress.Percent = percent
	e.pipelineProgress.Current = current
	e.pipelineProgress.Total = total
	e.pipelineProgress.Config = cfgName
	e.pipelineProgress.Message = msg
	e.pipelineProgress.IsRunning = isRunning
	e.mu.Unlock()
}

// FindAvailableAlts returns the naturally sorted list of all builtin presets.
func (e *Engine) FindAvailableAlts() []string {
	return desync.GetAvailablePresetNames()
}

// SetSelectedAlt updates the chosen DPI desync profile in memory and reloads winws2 if running.
func (e *Engine) SetSelectedAlt(alt string) {
	cleanAlt := strings.TrimSuffix(strings.TrimSpace(alt), ".bat")
	e.mu.Lock()
	e.selectedAlt = cleanAlt
	e.cfg.SelectedAlt = cleanAlt
	_ = e.cfg.Save()
	isRunning := e.zapretCmd != nil && e.zapretCmd.Process != nil
	isFreeNet := e.freeInternetActive
	isConn := e.isConnected
	e.mu.Unlock()

	e.log(fmt.Sprintf("[CONFIG] Профиль обхода DPI изменен: %s", cleanAlt))
	if isRunning || isFreeNet || isConn {
		_ = e.stopWinws()
		_ = e.EnsureWinwsRunning()
	}
}

// GetBestAlt returns the selected alt or chooses the default optimal profile.
func (e *Engine) GetBestAlt() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getBestAltLocked()
}

func (e *Engine) getBestAltLocked() string {
	if e.selectedAlt != "" {
		return strings.TrimSuffix(e.selectedAlt, ".bat")
	}
	return "general (ALT6)"
}

// ConnectPipeline runs the full 5-stage unified connect pipeline.
func (e *Engine) ConnectPipeline(onSuccess func()) error {
	e.transitionMu.Lock()
	defer e.transitionMu.Unlock()

	e.mu.Lock()
	if e.isConnecting || e.isConnected {
		e.mu.Unlock()
		return fmt.Errorf("соединение уже устанавливается или установлено")
	}
	e.isConnecting = true
	e.mu.Unlock()

	// Ensure no stale winws is running from a previous crash/manual run
	_ = scanner.KillProcess("winws2.exe")
	_ = scanner.KillProcess("winws.exe")

	defer func() {
		e.mu.Lock()
		e.isConnecting = false
		e.mu.Unlock()
	}()

	// ==========================================
	// STAGE 1: ПРЕДСТАРТОВАЯ ДИАГНОСТИКА (0% - 15%)
	// ==========================================
	// 1.0 Ensure desync files are downloaded and extracted
	if !deps.HasZapret() {
		e.setProgress(1, 0, 0, "", "Загрузка компонентов сетевой оптимизации...", true)
		e.log("[INFO] Компоненты сетевого фильтра отсутствуют на диске. Первичная загрузка с GitHub...")
		if err := deps.PrepareZapret(e.log); err != nil {
			e.setProgress(0, 0, 0, "", "Ошибка загрузки компонентов", false)
			return fmt.Errorf("ошибка загрузки компонентов сетевой оптимизации: %w", err)
		}
	}

	e.setProgress(3, 0, 0, "", "Диагностика: зачистка конфликтов...", true)
	e.log("[INFO] Старт предстартовой диагностики системы...")

	// 1.1 Kill conflicts and sanitize startup / broken proxies
	killed, _ := scanner.KillAllConflicts()
	if len(killed) > 0 {
		e.log(fmt.Sprintf("[WARN] Завершены конфликтующие процессы: %s", strings.Join(killed, ", ")))
	} else {
		e.log("[OK] Конфликтующие процессы не обнаружены")
	}
	deps.SanitizeStartupAndNetwork(e.log)

	// 1.2 Stop leftover WinDivert driver
	scanner.StopWinDivertService()

	// 1.3 Restore ipset-all.txt if damaged
	deps.RestoreIpsetIfNeeded(e.log)

	// 1.4 Check basic internet connectivity
	e.setProgress(8, 0, 0, "", "Диагностика: проверка доступа к сети...", true)
	if !deps.CheckInternetConnection() {
		e.log("[WARN] Базовая сеть недоступна или нет ответа от DNS. Попытка продолжить...")
	} else {
		e.log("[OK] Сетевой адаптер и интернет активны")
	}

	// 1.5 Ensure sing-box and WinTun components
	e.setProgress(15, 0, 0, "", "Диагностика: проверка компонентов sing-box...", true)
	if err := deps.EnsureSingBoxFiles(e.log); err != nil {
		e.setProgress(0, 0, 0, "", "Ошибка компонентов sing-box", false)
		return fmt.Errorf("ошибка компонентов sing-box: %w", err)
	}
	e.log("[OK] Игровой селективный роутер sing-box и WinTun готовы")

	// ==========================================
	// STAGE 2: АНАЛИЗ И ВЫБОР ПРОФИЛЯ ОБХОДА DPI (15% - 60%)
	// ==========================================
	bestAlt := e.GetBestAlt()
	e.log(fmt.Sprintf("[INFO] Выбран оптимальный профиль десинхронизации: %s", bestAlt))
	e.setProgress(60, 0, 0, bestAlt, fmt.Sprintf("Профиль: %s", bestAlt), true)
	time.Sleep(100 * time.Millisecond)

	// ==========================================
	// STAGE 3: ЗАПУСК СЕТЕВОГО ФИЛЬТРА (ТИХИЙ РЕЖИМ)
	// ==========================================
	e.setProgress(65, 0, 0, bestAlt, fmt.Sprintf("Запуск сетевого фильтра (%s)...", bestAlt), true)
	e.log(fmt.Sprintf("[INFO] Тихий запуск сетевого фильтра WinDivert: %s...", bestAlt))

	startErr := e.EnsureWinwsRunning()

	if startErr != nil {
		_ = e.disconnectInternal()
		e.setProgress(0, 0, 0, "", "winws2.exe не отвечает", false)
		return fmt.Errorf("winws2.exe не запустился: %w", startErr)
	}
	e.setProgress(75, 0, 0, bestAlt, "Сетевой фильтр winws2 и WinDivert активны", true)
	e.log("[OK] winws2.exe успешно обнаружен в процессах, драйвер WinDivert активен")

	// ==========================================
	// STAGE 4: ЗАПУСК ИГРОВОГО СЕЛЕКТИВНОГО РОУТЕРА SING-BOX (75% - 92%)
	// ==========================================
	e.setProgress(80, 0, 0, bestAlt, "Запуск игрового шлюза Стокгольм (Hysteria 2)...", true)
	e.log("[INFO] Инициализация нативного игрового L3 туннеля Hysteria 2 (Стокгольм, 27 мс)...")

	targetGameProcess := "WardogsClient-Win64-Shipping.exe"
	selectedGame := e.cfg.GetSelectedGame()
	if selectedGame.ExePath != "" {
		targetGameProcess = filepath.Base(selectedGame.ExePath)
	}

	targets := []string{targetGameProcess}
	for _, p := range selectedGame.ProcessNames {
		if p != "" && p != targetGameProcess && !strings.Contains(strings.ToLower(p), "launcher") {
			targets = append(targets, p)
		}
	}

	e.mu.Lock()
	isFreeNet := e.freeInternetActive
	e.mu.Unlock()

	selectedGameID := e.cfg.GetSelectedGame().ID
	if selectedGameID == "" {
		selectedGameID = "wardogs"
	}
	if err := e.singboxMgr.Start(targets, isFreeNet, e.log, selectedGameID); err != nil {
		_ = e.disconnectInternal()
		e.setProgress(0, 0, 0, "", "Ошибка запуска sing-box", false)
		return fmt.Errorf("ошибка запуска sing-box: %w", err)
	}

	e.setProgress(92, 0, 0, bestAlt, "Игровой туннель активен. Стабилизация сетевого адаптера...", true)
	time.Sleep(300 * time.Millisecond)

	e.mu.Lock()
	e.isConnected = true
	e.mu.Unlock()

	e.startWatchdog()
	if e.telemetry != nil {
		serverIP := singbox.GetServerIP()
		if serverIP != "" {
			e.telemetry.SetDirectTarget(serverIP)
		}
		e.telemetry.Start(false)
	}
	if e.pingMeter != nil {
		_ = e.pingMeter.Start()
	}

	e.setProgress(100, 0, 0, bestAlt, "Сетевой туннель полностью активен!", false)
	e.log("[OK] Сетевой туннель полностью активен! Готово к игре")

	selectedGame = e.cfg.GetSelectedGame()
	// Launch game if autolaunch is enabled for this game
	if selectedGame.Autolaunch {
		e.cfg.UpdateLastPlayed(selectedGame.ID)

		if selectedGame.SteamAppID != "" {
			e.log(fmt.Sprintf("[INFO] Автозапуск %s в Steam (steam://run/%s)...", selectedGame.Title, selectedGame.SteamAppID))
			steamCmd := exec.Command("cmd.exe", "/c", "start", fmt.Sprintf("steam://run/%s", selectedGame.SteamAppID))
			steamCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = steamCmd.Start()
		} else if selectedGame.ExePath != "" {
			e.log(fmt.Sprintf("[INFO] Автозапуск %s (%s)...", selectedGame.Title, selectedGame.ExePath))
			gameCmd := exec.Command("cmd.exe", "/c", "start", "", selectedGame.ExePath)
			gameCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = gameCmd.Start()
		}

	}

	if onSuccess != nil {
		onSuccess()
	}

	return nil
}

// Disconnect gracefully disconnects Cloudflare WARP and Zapret without holding the mutex during I/O.
func (e *Engine) Disconnect() error {
	e.transitionMu.Lock()
	defer e.transitionMu.Unlock()

	e.mu.Lock()
	e.isConnected = false
	e.isConnecting = false
	e.pipelineProgress = PipelineProgress{
		Percent: 0,
		Message: "",
	}
	e.mu.Unlock()

	e.log("[INFO] Остановка игрового туннеля...")
	return e.disconnectInternal()
}

func (e *Engine) disconnectInternal() error {
	e.mu.Lock()
	freeActive := e.freeInternetActive
	e.mu.Unlock()

	// 1. If Free Internet is NOT active, stop winws completely.
	// If Free Internet IS active, seamlessly restart winws in Web-Only mode!
	if !freeActive {
		e.stopWatchdog()
		e.log("[INFO] Завершение процесса Zapret (winws2.exe)...")
		_ = scanner.KillProcess("winws2.exe")
		_ = scanner.KillProcess("winws.exe")

		e.mu.Lock()
		if e.zapretCmd != nil && e.zapretCmd.Process != nil {
			pid := e.zapretCmd.Process.Pid
			_ = e.zapretCmd.Process.Kill()
			killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
			killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = killCmd.Run()
			e.zapretCmd = nil
		}
		e.mu.Unlock()

		scanner.StopWinDivertService()
	} else {
		e.log("[INFO] Игра закрыта. Режим «Свободный интернет» переведен в базовый веб-профиль")
		_ = e.stopWinws()
		_ = e.EnsureWinwsRunning()
	}

	// 2. Disconnect sing-box (unless Free Internet mode is active)
	if !freeActive {
		if e.singboxMgr != nil {
			e.log("[INFO] Остановка игрового туннеля sing-box...")
			_ = e.singboxMgr.Stop()
		}
		go func() {
			_ = singbox.ReleaseSession()
		}()
	} else {
		if e.singboxMgr != nil {
			// Seamlessly transition sing-box to Web-Only mode (remove game process)
			_ = e.singboxMgr.Start(nil, true, e.log, "free_internet")
		}
		e.log("[INFO] Селективный туннель Cloudflare MASQUE остается активным для Telegram и WhatsApp")
	}

	e.mu.Lock()
	e.isConnected = false
	e.pipelineProgress = PipelineProgress{
		Percent: 0,
		Message: "",
	}
	e.mu.Unlock()

	if e.telemetry != nil {
		e.telemetry.Stop()
	}
	if e.pingMeter != nil {
		e.pingMeter.Stop()
	}

	e.log("[OK] Игровой туннель отключен, сеть в исходном состоянии")
	return nil
}

func (e *Engine) GetTelemetry() (int, int) {
	if e.telemetry != nil {
		return e.telemetry.Get()
	}
	return 0, 0
}

// GetGamePing returns the active live match server and wire latency.
func (e *Engine) GetGamePing() (server string, wireRttMs int, inGameEstMs int, active bool) {
	if e.pingMeter != nil {
		return e.pingMeter.GetActivePing()
	}
	return "", 0, 0, false
}

// IsSingboxAlive reports whether the sing-box process is currently alive.
// Used by the silent reconnect monitor in main.go.
func (e *Engine) IsSingboxAlive() bool {
	if e.singboxMgr == nil {
		return false
	}
	return e.singboxMgr.IsProcessAlive()
}

func (e *Engine) GetPipelineProgress() PipelineProgress {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pipelineProgress
}

// Shutdown performs an unconditional full cleanup of all processes and kernel drivers on application exit.
func (e *Engine) Shutdown() {
	e.stopWatchdog()

	if e.telemetry != nil {
		e.telemetry.Stop()
	}
	if e.pingMeter != nil {
		e.pingMeter.Stop()
	}

	e.mu.Lock()
	e.isConnected = false
	e.isConnecting = false
	e.mu.Unlock()

	if e.singboxMgr != nil {
		_ = e.singboxMgr.Stop()
	}
	_ = singbox.ReleaseSession()

	_ = scanner.KillProcess("winws2.exe")
	_ = scanner.KillProcess("winws.exe")
	scanner.StopWinDivertService()
}

// AddGameProcess dynamically adds an executable name to the running sing-box routing rules.
func (e *Engine) AddGameProcess(procName string) error {
	if e.singboxMgr != nil {
		return e.singboxMgr.AddTargetProcess(procName, e.log)
	}
	return nil
}

func (e *Engine) startWatchdog() {
	e.mu.Lock()
	if e.watchdogStop != nil {
		e.mu.Unlock()
		return
	}
	stopChan := make(chan struct{})
	e.watchdogStop = stopChan
	e.mu.Unlock()

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				e.checkProcessHealth()
			}
		}
	}()
}

func (e *Engine) stopWatchdog() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.watchdogStop != nil {
		close(e.watchdogStop)
		e.watchdogStop = nil
	}
}

func (e *Engine) checkProcessHealth() {
	e.mu.Lock()
	isConn := e.isConnected
	isFreeNet := e.freeInternetActive
	e.mu.Unlock()

	if !isConn && !isFreeNet {
		return
	}

	// 1. Check winws.exe
	winwsAlive := false
	e.mu.Lock()
	if e.zapretCmd != nil && e.zapretCmd.Process != nil {
		pid := e.zapretCmd.Process.Pid
		e.mu.Unlock()
		h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
		if err == nil {
			var code uint32
			if syscall.GetExitCodeProcess(h, &code) == nil && code == 259 {
				winwsAlive = true
			}
			syscall.CloseHandle(h)
		}
	} else {
		e.mu.Unlock()
	}

	if !winwsAlive && (isConn || isFreeNet) {
		e.log("[WARN] Обнаружено неожиданное завершение winws2.exe. Автоматическое восстановление сетевого фильтра...")
		_ = e.EnsureWinwsRunning()
	}

	// 2. Check sing-box.exe if connected or free internet
	if (isConn || isFreeNet) && e.singboxMgr != nil && !e.singboxMgr.IsProcessAlive() {
		e.log("[WARN] Обнаружено неожиданное завершение sing-box.exe. Перезапуск туннеля...")
		var targets []string
		if isConn {
			selectedGame := e.cfg.GetSelectedGame()
			targetGameProcess := "WardogsClient-Win64-Shipping.exe"
			if selectedGame.ExePath != "" {
				targetGameProcess = filepath.Base(selectedGame.ExePath)
			}
			targets = []string{targetGameProcess}
			for _, p := range selectedGame.ProcessNames {
				if p != "" && p != targetGameProcess && !strings.Contains(strings.ToLower(p), "launcher") {
					targets = append(targets, p)
				}
			}
		}
		// Stop first to reset isRunning flag — Manager.Start is a no-op if isRunning==true.
		// Invalidate cached token so AcquireSession fetches a fresh one with the correct
		// client IP (stale token was issued to 127.0.0.1 and would be rejected by Hysteria2).
		_ = e.singboxMgr.Stop()
		singbox.InvalidateSession()
		if startErr := e.singboxMgr.Start(targets, isFreeNet, e.log); startErr != nil {
			e.log(fmt.Sprintf("[ERROR] Не удалось перезапустить туннель: %v", startErr))
		} else {
			e.log("[OK] Туннель автоматически восстановлен")
		}
	}
}
