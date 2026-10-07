package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/desync"
	"warlink/internal/hostlist"
	"warlink/internal/pingmeter"
	"warlink/internal/scanner"
	"warlink/internal/singbox"
	"warlink/internal/system"
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
	isConnected        bool
	isConnecting       bool
	singboxStopping    atomic.Bool
	selectedAlt        string
	freeInternetActive bool
	logCallback        func(string)
	pipelineProgress   PipelineProgress
	hostlistMgr        *hostlist.Manager
	singboxMgr             *singbox.Manager
	telemetry              *TelemetryMonitor
	pingMeter              *pingmeter.Meter
	beacon                 *AutoBeacon
	watchdogStop           chan struct{}
	singboxRestartAttempts int
	singboxLastRestart     time.Time
	singboxStartedAt       time.Time
	lastCrashReportTime    time.Time
	crashReportsThisHour   int
	crashReportHourStart   time.Time
}

func (e *Engine) shouldSendCrashReport() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if now.Sub(e.crashReportHourStart) > time.Hour {
		e.crashReportHourStart = now
		e.crashReportsThisHour = 0
	}
	if e.crashReportsThisHour >= 3 {
		return false
	}
	if !e.lastCrashReportTime.IsZero() && now.Sub(e.lastCrashReportTime) < 30*time.Second {
		return false
	}
	e.lastCrashReportTime = now
	e.crashReportsThisHour++
	return true
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
		pingMeter:          pingmeter.New(),
	}
	eng.beacon = NewAutoBeacon(eng, logCb)
	if eng.singboxMgr != nil {
		eng.singboxMgr.OnProcessStart = func(pid int) {
			_ = deps.AssignProcessToJob(pid)
		}
	}
	if eng.pingMeter != nil {
		eng.pingMeter.SetUpdateCallback(func(server string, wireRttMs int, inGameEstMs int) {
			eng.log(fmt.Sprintf("[PING] Матч WARDOGS (%s): Сеть = %d мс | Оверлей игры ~ %d мс", server, wireRttMs, inGameEstMs))
		})
	}
	// Sync upstream lists in background
	eng.hostlistMgr.SyncUpstream()
	// Start auto-discovered hostlist watcher
	eng.hostlistMgr.StartAutoListWatcher(context.Background(), 30*time.Second)
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
		e.log("[FREE NET] Включение режима «Свободный интернет» (маршрутизация Discord, YouTube, Telegram через Hysteria 2)...")
		e.mu.Lock()
		e.freeInternetActive = true
		e.mu.Unlock()

		if err := deps.EnsureSingBoxFiles(e.log); err != nil {
			e.mu.Lock()
			e.freeInternetActive = false
			e.mu.Unlock()
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
					if p != "" && !strings.EqualFold(p, targetGameProcess) {
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
				selectedGameID = selectedGameID + " (гибрид)"
			}
			e.singboxStopping.Store(true)
			startErr := e.singboxMgr.Start(targets, true, e.log, selectedGameID)
			e.singboxStopping.Store(false)
			if startErr != nil {
				e.mu.Lock()
				e.freeInternetActive = false
				e.mu.Unlock()
				if !isConn {
					_ = singbox.ReleaseSession()
				}
				errMsg := startErr.Error()
				if idx := strings.Index(errMsg, "техобслуживани"); idx != -1 {
					return errors.New(errMsg[idx:])
				}
				if idx := strings.Index(errMsg, "все слоты шлюза заняты"); idx != -1 {
					return errors.New(errMsg[idx:])
				}
				return fmt.Errorf("ошибка подключения к шлюзу: %w", startErr)
			}
		}

		e.mu.Lock()
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
		if e.beacon != nil {
			e.beacon.Start("free_internet", "browser")
		}
		return nil
	} else {
		e.log("[FREE NET] Отключение режима «Свободный интернет»...")
		e.mu.Lock()
		e.freeInternetActive = false
		e.cfg.FreeInternetEnabled = false
		_ = e.cfg.Save()
		e.mu.Unlock()

		if isConn {
			if e.singboxMgr != nil {
				targetGameProcess := "WardogsClient-Win64-Shipping.exe"
				selectedGame := e.cfg.GetSelectedGame()
				if selectedGame.ExePath != "" {
					targetGameProcess = filepath.Base(selectedGame.ExePath)
				}
				targets := []string{targetGameProcess}
				for _, p := range selectedGame.ProcessNames {
					if p != "" && !strings.EqualFold(p, targetGameProcess) {
						targets = append(targets, p)
					}
				}
				selectedGameID := selectedGame.ID
				if selectedGameID == "" {
					selectedGameID = "wardogs"
				}
				e.singboxStopping.Store(true)
				_ = e.singboxMgr.Start(targets, false, e.log, selectedGameID)
				e.singboxStopping.Store(false)
			}
			return nil
		}
		e.stopWatchdog()
		if e.beacon != nil {
			e.beacon.SendMatchSummary()
			e.beacon.Stop()
		}
		if e.singboxMgr != nil {
			e.singboxStopping.Store(true)
			_ = e.singboxMgr.Stop()
			e.singboxStopping.Store(false)
		}
		go func() {
			_ = singbox.ReleaseSession()
		}()
		return nil
	}
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

// SetSelectedAlt updates the chosen profile in memory.
func (e *Engine) SetSelectedAlt(alt string) {
	cleanAlt := strings.TrimSuffix(strings.TrimSpace(alt), ".bat")
	e.mu.Lock()
	e.selectedAlt = cleanAlt
	e.cfg.SelectedAlt = cleanAlt
	_ = e.cfg.Save()
	e.mu.Unlock()

	e.log(fmt.Sprintf("[CONFIG] Профиль: %s", cleanAlt))
}

// GetBestAlt returns the selected alt or chooses the default optimal profile.
func (e *Engine) GetBestAlt() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.getBestAltLocked()
}

func (e *Engine) getBestAltLocked() string {
	if e.selectedAlt != "" && e.selectedAlt != "general" {
		return strings.TrimSuffix(e.selectedAlt, ".bat")
	}
	if e.cfg != nil {
		selectedGame := e.cfg.GetSelectedGame()
		if selectedGame.PreferredAlt != "" && selectedGame.PreferredAlt != "general" {
			return strings.TrimSuffix(selectedGame.PreferredAlt, ".bat")
		}
	}
	if e.selectedAlt != "" {
		return strings.TrimSuffix(e.selectedAlt, ".bat")
	}
	return "Автокалибровка (Circular Adaptive)"
}

// ConnectPipeline runs the unified connect pipeline.
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

	defer func() {
		e.mu.Lock()
		e.isConnecting = false
		e.mu.Unlock()
	}()

	// ==========================================
	// STAGE 1: ПРЕДСТАРТОВАЯ ДИАГНОСТИКА СИСТЕМЫ (0% - 25%)
	// ==========================================
	e.setProgress(5, 0, 0, "", "Диагностика: зачистка сетевых конфликтов...", true)
	e.log("[INFO] Старт предстартовой диагностики системы...")

	// 1.1 Kill conflicts and sanitize startup / broken proxies
	killed, _ := scanner.KillAllConflicts()
	if len(killed) > 0 {
		e.log(fmt.Sprintf("[WARN] Завершены конфликтующие процессы: %s", strings.Join(killed, ", ")))
	} else {
		e.log("[OK] Конфликтующие процессы не обнаружены")
	}
	deps.SanitizeStartupAndNetwork(e.log)

	// 1.2 Check basic internet connectivity
	e.setProgress(15, 0, 0, "", "Диагностика: проверка доступа к сети...", true)
	if !deps.CheckInternetConnection() {
		e.log("[WARN] Базовая сеть недоступна или нет ответа от DNS. Попытка продолжить...")
	} else {
		e.log("[OK] Сетевой адаптер и интернет активны")
	}

	// 1.4 Ensure sing-box and WinTun components
	e.setProgress(25, 0, 0, "", "Диагностика: проверка компонентов sing-box...", true)
	if err := deps.EnsureSingBoxFiles(e.log); err != nil {
		e.setProgress(0, 0, 0, "", "Ошибка компонентов sing-box", false)
		return fmt.Errorf("ошибка компонентов sing-box: %w", err)
	}
	e.log("[OK] Игровой селективный роутер sing-box и WinTun готовы")

	// ==========================================
	// STAGE 2: ЗАПУСК ИГРОВОГО ШЛЮЗА HYSTERIA 2 (25% - 85%)
	// ==========================================
	e.setProgress(50, 0, 0, "", "Подключение к игровому шлюзу (Hysteria 2)...", true)
	e.log("[INFO] Инициализация нативного игрового L3 туннеля Hysteria 2...")

	targetGameProcess := "WardogsClient-Win64-Shipping.exe"
	selectedGame := e.cfg.GetSelectedGame()
	if selectedGame.ExePath != "" {
		targetGameProcess = filepath.Base(selectedGame.ExePath)
	}

	targets := []string{targetGameProcess}
	for _, p := range selectedGame.ProcessNames {
		if p != "" && !strings.EqualFold(p, targetGameProcess) {
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
	if isFreeNet {
		selectedGameID = selectedGameID + " (гибрид)"
	}

	// Clean zombie Wintun adapter if left by previous unclean termination
	deps.CleanupZombieWintunAdapter(e.log)

	if err := e.singboxMgr.Start(targets, isFreeNet, e.log, selectedGameID); err != nil {
		_ = e.disconnectInternal()
		e.setProgress(0, 0, 0, "", "Ошибка запуска sing-box", false)
		errMsg := err.Error()
		if idx := strings.Index(errMsg, "техобслуживани"); idx != -1 {
			return errors.New(errMsg[idx:])
		}
		if idx := strings.Index(errMsg, "Шлюз находится на техобслуживании"); idx != -1 {
			return errors.New(errMsg[idx:])
		}
		if idx := strings.Index(errMsg, "проводятся плановые технические работы"); idx != -1 {
			return errors.New(errMsg[idx:])
		}
		if idx := strings.Index(errMsg, "все слоты шлюза заняты"); idx != -1 {
			return errors.New(errMsg[idx:])
		}
		return fmt.Errorf("ошибка запуска sing-box: %w", err)
	}

	// ==========================================
	// STAGE 3: СТАБИЛИЗАЦИЯ И ФИНАЛЬНАЯ НАСТРОЙКА (85% - 100%)
	// ==========================================
	e.setProgress(90, 0, 0, "", "Игровой туннель активен. Стабилизация сетевого адаптера...", true)
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
	if e.beacon != nil {
		g := e.cfg.GetSelectedGame()
		pName := ""
		if g.ExePath != "" {
			pName = filepath.Base(g.ExePath)
		}
		e.beacon.Start(g.ID, pName)
	}

	e.setProgress(100, 0, 0, "", "Сетевой туннель полностью активен!", false)
	e.log("[OK] Сетевой туннель полностью активен! Готово к игре")

	// Apply Windows network stack and DSCP 46 optimizations for competitive low latency
	go system.ApplyCompetitiveGamingTweaks(e.log)

	selectedGame = e.cfg.GetSelectedGame()
	// Launch game if autolaunch is enabled for this game
	if selectedGame.Autolaunch {
		e.cfg.UpdateLastPlayed(selectedGame.ID)

		if selectedGame.SteamAppID != "" {
			e.log(fmt.Sprintf("[INFO] Автозапуск %s в Steam (steam://run/%s)...", selectedGame.Title, selectedGame.SteamAppID))
			steamCmd := exec.Command("explorer.exe", fmt.Sprintf("steam://run/%s", selectedGame.SteamAppID))
			steamCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = steamCmd.Start()
		} else if selectedGame.ExePath != "" {
			e.log(fmt.Sprintf("[INFO] Автозапуск %s (%s)...", selectedGame.Title, selectedGame.ExePath))
			gameCmd := exec.Command("explorer.exe", selectedGame.ExePath)
			gameCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			_ = gameCmd.Start()
		}
	}

	if onSuccess != nil {
		onSuccess()
	}

	return nil
}

// Disconnect gracefully disconnects Hysteria 2 without holding the mutex during I/O.
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

	if !freeActive {
		e.stopWatchdog()
		e.singboxStopping.Store(true)
		if e.singboxMgr != nil {
			e.log("[INFO] Остановка игрового туннеля sing-box...")
			_ = e.singboxMgr.Stop()
		}
		go func() {
			_ = singbox.ReleaseSession()
			e.singboxStopping.Store(false)
		}()
	} else {
		e.singboxStopping.Store(true)
		if e.singboxMgr != nil {
			// Seamlessly transition sing-box to Web-Only mode (remove game process)
			_ = e.singboxMgr.Start(nil, true, e.log, "free_internet")
		}
		e.singboxStopping.Store(false)
		e.log("[INFO] Селективный туннель Hysteria 2 остается активным для Discord, YouTube и мессенджеров")
	}

	e.mu.Lock()
	e.isConnected = false
	e.pipelineProgress = PipelineProgress{
		Percent: 0,
		Message: "",
	}
	e.mu.Unlock()

	if e.beacon != nil {
		e.beacon.SendMatchSummary()
		e.beacon.Stop()
	}
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


func (e *Engine) GetPipelineProgress() PipelineProgress {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pipelineProgress
}

// Shutdown performs an unconditional full cleanup of all processes and kernel drivers on application exit.
func (e *Engine) Shutdown() {
	e.stopWatchdog()

	if e.beacon != nil {
		e.beacon.SendMatchSummary()
		e.beacon.Stop()
	}
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

	deps.RestoreWindowsNetworkStack(e.log)
}

// OnGameStart informs the engine and auto-beacon that a game process was launched.
func (e *Engine) OnGameStart(gameID, procName string) {
	if e.beacon != nil {
		e.beacon.SetGame(gameID, procName)
	}
}

// OnGameExit sends the match summary report and resets match telemetry accumulators.
func (e *Engine) OnGameExit(gameTitle string) {
	if e.beacon != nil {
		e.beacon.SendMatchSummary()
	}
}


// AddGameProcess dynamically adds an executable name to the running sing-box routing rules.
func (e *Engine) AddGameProcess(procName string) error {
	if e.singboxMgr != nil {
		e.singboxStopping.Store(true)
		defer e.singboxStopping.Store(false)
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

	// Check sing-box.exe if connected or free internet
	needRestart := false
	if (isConn || isFreeNet) && e.singboxMgr != nil && !e.singboxStopping.Load() {
		if !e.singboxMgr.IsProcessAlive() {
			exitCode, stderrTail := e.singboxMgr.GetLastExitInfo()
			e.log(fmt.Sprintf("[WARN] Обнаружено неожиданное завершение sing-box.exe (код: %d). Перезапуск туннеля...", exitCode))
			if e.shouldSendCrashReport() {
				ReportEngineCrash("singbox_crash", fmt.Sprintf("Неожиданное завершение sing-box.exe во время активного подключения (код: %d)", exitCode), map[string]interface{}{
					"is_connected": isConn,
					"is_free_net":  isFreeNet,
					"exit_code":    exitCode,
					"stderr":       stderrTail,
				})
			}
			needRestart = true
		} else if e.singboxMgr.HasAuthError() {
			e.log("[WARN] Обнаружен сбой авторизации шлюза Hysteria 2 (404/expired). Обновление сессии и перезапуск туннеля...")
			needRestart = true
		}
	}

	if needRestart {
		backoffDelays := []time.Duration{0, 3 * time.Second, 8 * time.Second, 15 * time.Second}
		delay := 15 * time.Second
		if e.singboxRestartAttempts < len(backoffDelays) {
			delay = backoffDelays[e.singboxRestartAttempts]
		}
		if e.singboxRestartAttempts > 0 && time.Since(e.singboxLastRestart) < delay {
			return
		}
		e.singboxLastRestart = time.Now()
		e.singboxStopping.Store(true)
		defer e.singboxStopping.Store(false)
		var targets []string
		if isConn {
			selectedGame := e.cfg.GetSelectedGame()
			targetGameProcess := "WardogsClient-Win64-Shipping.exe"
			if selectedGame.ExePath != "" {
				targetGameProcess = filepath.Base(selectedGame.ExePath)
			}
			targets = []string{targetGameProcess}
			for _, p := range selectedGame.ProcessNames {
				if p != "" && !strings.EqualFold(p, targetGameProcess) {
					targets = append(targets, p)
				}
			}
		}

		_ = e.singboxMgr.Stop()
		deps.CleanupZombieWintunAdapter(e.log)
		time.Sleep(1000 * time.Millisecond)
		singbox.InvalidateSession()
		selectedGameID := "free_internet"
		if isConn {
			selectedGameID = e.cfg.GetSelectedGame().ID
			if selectedGameID == "" {
				selectedGameID = "wardogs"
			}
			if isFreeNet {
				selectedGameID = selectedGameID + " (гибрид)"
			}
		}

		startErr := e.singboxMgr.Start(targets, isFreeNet, e.log, selectedGameID)
		if startErr != nil && strings.Contains(startErr.Error(), "already exists") {
			e.log("[WARN] Обнаружена задержка освобождения Wintun-интерфейса Windows. Принудительный сброс адаптера...")
			if e.shouldSendCrashReport() {
				ReportEngineCrash("wintun_driver", "Задержка освобождения Wintun-интерфейса: "+startErr.Error(), map[string]interface{}{
					"game_id": selectedGameID,
				})
			}
			deps.CleanupZombieWintunAdapter(e.log)
			time.Sleep(2000 * time.Millisecond)
			startErr = e.singboxMgr.Start(targets, isFreeNet, e.log, selectedGameID)
		}

		if startErr != nil {
			e.singboxRestartAttempts++
			e.log(fmt.Sprintf("[ERROR] Не удалось перезапустить туннель (попытка %d/3): %v", e.singboxRestartAttempts, startErr))
			if e.shouldSendCrashReport() {
				ReportEngineCrash("singbox_restart_failed", fmt.Sprintf("Ошибка перезапуска sing-box: %v", startErr), map[string]interface{}{
					"attempts": e.singboxRestartAttempts,
				})
			}
			if e.singboxRestartAttempts >= 3 {
				e.log("[ERROR] Превышен лимит попыток восстановления туннеля (шлюз перегружен или недоступен). Сессия отключена.")
				e.singboxRestartAttempts = 0
				go func() {
					_ = e.Disconnect()
				}()
			}
		} else {
			e.singboxRestartAttempts = 0
			e.singboxMgr.ResetAuthErrorOffset()
			e.log("[OK] Туннель автоматически восстановлен")
		}
	}
}

// GetAutoDiscoveredCount returns the number of runtime auto-discovered domains.
func (e *Engine) GetAutoDiscoveredCount() int {
	if e.hostlistMgr != nil {
		return e.hostlistMgr.GetAutoDiscoveredCount()
	}
	return 0
}

// ResetAutoList resets the runtime auto-discovered domains and files.
func (e *Engine) ResetAutoList() error {
	if e.hostlistMgr != nil {
		return e.hostlistMgr.ResetAutoList()
	}
	return nil
}

