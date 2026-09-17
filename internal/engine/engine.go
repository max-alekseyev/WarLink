package engine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"warlink/internal/config"
	"warlink/internal/deps"
	"warlink/internal/scanner"
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
	mu               sync.Mutex
	cfg              *config.Config
	zapretCmd        *exec.Cmd
	isConnected      bool
	isConnecting     bool
	isTesting        bool
	logCallback      func(string)
	pipelineProgress PipelineProgress
}

func New(cfg *config.Config, logCb func(string)) *Engine {
	return &Engine{
		cfg:         cfg,
		logCallback: logCb,
	}
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

func (e *Engine) IsTesting() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isTesting
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

// FindAvailableAlts returns a naturally sorted list of available .bat config files in zapret directory.
func (e *Engine) FindAvailableAlts() []string {
	zapretDir := deps.GetZapretDir()
	entries, err := os.ReadDir(zapretDir)
	if err != nil {
		return nil
	}

	var alts []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(strings.ToLower(name), ".bat") && strings.HasPrefix(strings.ToLower(name), "general") {
			alts = append(alts, name)
		}
	}

	reNum := regexp.MustCompile(`(?i)alt\s*(\d+)`)
	sort.Slice(alts, func(i, j int) bool {
		m1 := reNum.FindStringSubmatch(alts[i])
		m2 := reNum.FindStringSubmatch(alts[j])
		num1 := 0
		num2 := 0
		if len(m1) > 1 {
			num1, _ = strconv.Atoi(m1[1])
		}
		if len(m2) > 1 {
			num2, _ = strconv.Atoi(m2[1])
		}
		if num1 != num2 {
			return num1 < num2
		}
		return alts[i] < alts[j]
	})

	return alts
}

// SetSelectedAlt updates and saves the chosen Zapret configuration profile.
func (e *Engine) SetSelectedAlt(alt string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg.SelectedAlt = alt
	_ = e.cfg.Save()
	e.log(fmt.Sprintf("[CONFIG] Профиль Zapret изменен пользователем: %s", alt))
}

// GetBestAlt returns the configured alt or attempts to read from official Zapret test results.
func (e *Engine) GetBestAlt() string {
	if e.cfg.SelectedAlt != "" {
		return e.cfg.SelectedAlt
	}

	// Try reading from official Zapret test results (service 12)
	if reportPath, err := FindLatestZapretReport(); err == nil {
		if summary, err := ParseZapretReport(reportPath); err == nil && summary.BestConfig != "" {
			return summary.BestConfig
		}
	}

	// Default fallback: general (ALT9).bat (top performer in Zapret suite) or available
	available := e.FindAvailableAlts()
	for _, a := range available {
		if strings.Contains(strings.ToLower(a), "alt9") {
			return a
		}
	}
	for _, a := range available {
		if strings.Contains(strings.ToLower(a), "alt") {
			return a
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return "general (ALT).bat"
}

// ConnectPipeline runs the full 5-stage unified connect pipeline.
func (e *Engine) ConnectPipeline(onSuccess func()) error {
	e.mu.Lock()
	if e.isConnecting || e.isConnected {
		e.mu.Unlock()
		return fmt.Errorf("соединение уже устанавливается или установлено")
	}
	e.isConnecting = true
	e.mu.Unlock()

	// Ensure no stale Zapret is running from a previous crash/manual run
	_ = scanner.KillProcess("winws.exe")

	defer func() {
		e.mu.Lock()
		e.isConnecting = false
		e.mu.Unlock()
	}()

	// ==========================================
	// STAGE 1: ПРЕДСТАРТОВАЯ ДИАГНОСТИКА (0% - 15%)
	// ==========================================
	// 1.0 Ensure Zapret files are downloaded and extracted
	if !deps.HasZapret() {
		e.setProgress(1, 0, 0, "", "Загрузка компонентов сетевой оптимизации...", true)
		e.log("[INFO] Компоненты Zapret отсутствуют на диске. Первичная загрузка с GitHub...")
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

	// 1.5 Check / Prepare WARP & Zapret
	e.setProgress(12, 0, 0, "", "Диагностика: проверка службы Cloudflare WARP...", true)
	if !deps.HasWarpInstalled() {
		e.log("[INFO] Клиент Cloudflare WARP не обнаружен. Установка из warlink_core...")
		if err := deps.InstallWarp(e.log); err != nil {
			e.setProgress(0, 0, 0, "", "Ошибка установки WARP", false)
			return fmt.Errorf("ошибка установки Cloudflare WARP: %w", err)
		}
	} else {
		e.log("[OK] Служба Cloudflare WARP найдена")
		// Close standalone GUI if it was auto-started by Windows or installer
		killGUI := exec.Command("taskkill", "/F", "/IM", "Cloudflare WARP.exe")
		killGUI.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = killGUI.Run()
	}

	// 1.6 Ensure local WARP adapter is disconnected so network stack is clean for Zapret/tests
	e.setProgress(15, 0, 0, "", "Диагностика: сброс сетевого адаптера WARP...", true)
	discCmd := exec.Command("warp-cli", "disconnect")
	discCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	outDisc, _ := discCmd.CombinedOutput()
	if txt := strings.TrimSpace(string(outDisc)); txt != "" {
		e.log("[WARP] локальное отключение перед тестами/запуском: " + txt)
	}

	// ==========================================
	// STAGE 2: АНАЛИЗ И ВЫБОР ПРОФИЛЯ ZAPRET (15% - 60%)
	// ==========================================
	bestAlt := e.GetBestAlt()
	zapretDir := deps.GetZapretDir()

	// Check if benchmark report exists; if not (first clean launch), automatically run the benchmark
	reportPath, rErr := FindLatestZapretReport()
	summary, sErr := ParseZapretReport(reportPath)
	if rErr != nil || sErr != nil || len(summary.Scores) == 0 {
		e.log("[INFO] Отчет калибровки профилей не обнаружен (первый запуск).")
		e.log("[INFO] Запуск первичной калибровки 22 профилей Zapret под вашего провайдера...")
		e.setProgress(15, 0, 22, "", "Первичная калибровка: тест 22 профилей под вашего провайдера...", true)

		if testErr := e.runFullZapretTestInternal(true); testErr != nil {
			e.log(fmt.Sprintf("[WARN] Первичная калибровка завершилась с предупреждением: %v", testErr))
		}

		// Re-read newly generated report
		if newReport, nErr := FindLatestZapretReport(); nErr == nil {
			if newSummary, nsErr := ParseZapretReport(newReport); nsErr == nil && len(newSummary.Scores) > 0 {
				summary = newSummary
				reportPath = newReport
				rErr = nil
				sErr = nil
			}
		}
	}

	if rErr == nil && sErr == nil && len(summary.Scores) > 0 {
		e.log(fmt.Sprintf("[ZAPRET TEST] Загружен отчет официального тестирования (service 12): %s", summary.ReportFile))
		e.log("[ZAPRET TEST] Топ проверенных конфигураций:")
		for i := 0; i < len(summary.Scores) && i < 3; i++ {
			sc := summary.Scores[i]
			e.log(fmt.Sprintf("[ZAPRET TEST]   #%d: %s (OK: %d, ERR: %d, Ping: %d ms)", i+1, sc.ConfigName, sc.OkCount, sc.ErrCount, sc.PingAvgMs))
		}
		if e.cfg.SelectedAlt == "" {
			bestAlt = summary.BestConfig
			e.cfg.SelectedAlt = bestAlt
			_ = e.cfg.Save()
			e.log(fmt.Sprintf("[OK] Автоматически выбран лучший профиль из отчета: %s", bestAlt))
		} else {
			bestAlt = e.cfg.SelectedAlt
			e.log(fmt.Sprintf("[OK] Используется профиль, выбранный пользователем: %s", bestAlt))
		}
	} else {
		e.log(fmt.Sprintf("[INFO] Выбран рабочий профиль по умолчанию: %s", bestAlt))
	}

	e.setProgress(60, 0, 0, bestAlt, fmt.Sprintf("Профиль: %s", bestAlt), true)
	time.Sleep(200 * time.Millisecond)

	// ==========================================
	// STAGE 3: ЗАПУСК ZAPRET (ТИХИЙ РЕЖИМ)
	// ==========================================
	e.setProgress(62, 0, 0, bestAlt, fmt.Sprintf("Запуск сетевого фильтра Zapret (%s)...", bestAlt), true)
	e.log(fmt.Sprintf("[INFO] Тихий запуск сетевого фильтра Zapret: %s...", bestAlt))

	bestPath := filepath.Join(zapretDir, bestAlt)
	args, winwsPath, parseErr := deps.ParseBatWinwsArgs(bestPath)

	var zCmd *exec.Cmd
	if parseErr == nil && len(args) > 0 {
		e.log(fmt.Sprintf("[INFO] Запуск winws.exe напрямую в фоновом режиме (%d параметров, без окон и статус-бара)...", len(args)))
		zCmd = exec.Command(winwsPath, args...)
		zCmd.Dir = filepath.Dir(winwsPath)
		zCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	} else {
		// Fallback: execute bat in hidden mode
		e.log(fmt.Sprintf("[WARN] Не удалось разобрать параметры bat (%v), запуск в скрытом режиме...", parseErr))
		zCmd = exec.Command("cmd.exe", "/c", bestPath)
		zCmd.Dir = zapretDir
		zCmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	}

	if err := zCmd.Start(); err != nil {
		e.setProgress(0, 0, 0, "", "Ошибка запуска Zapret", false)
		return fmt.Errorf("ошибка запуска Zapret: %w", err)
	}
	e.zapretCmd = zCmd

	winwsReady := false
	for i := 0; i < 15; i++ {
		time.Sleep(300 * time.Millisecond)
		check := exec.Command("tasklist", "/FI", "IMAGENAME eq winws.exe", "/NH", "/FO", "CSV")
		check.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := check.Output()
		if err == nil && strings.Contains(strings.ToLower(string(out)), "winws") {
			winwsReady = true
			break
		}
	}

	if !winwsReady {
		_ = e.disconnectInternal()
		e.setProgress(0, 0, 0, "", "winws.exe не отвечает", false)
		return fmt.Errorf("winws.exe не запустился. Запустите от имени администратора")
	}
	e.setProgress(74, 0, 0, bestAlt, "Сетевой фильтр winws и WinDivert активны", true)
	e.log("[OK] winws.exe успешно обнаружен в процессах, драйвер WinDivert активен")

	// ==========================================
	// STAGE 4: ПОДКЛЮЧЕНИЕ CLOUDFLARE WARP (75% - 92%)
	// ==========================================
	e.setProgress(75, 0, 0, bestAlt, "Настройка и запуск Cloudflare WARP...", true)
	e.log("[INFO] Инициализация подключения Cloudflare WARP через десинхронизированный канал...")

	// 4.0 Ensure previous sessions are disconnected
	discCmd = exec.Command("warp-cli", "disconnect")
	discCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = discCmd.Run()

	// 4.1 Ensure Cloudflare WARP registration is valid and active
	regCheck := exec.Command("warp-cli", "registration", "show")
	regCheck.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := regCheck.CombinedOutput()
	outLower := strings.ToLower(string(out))
	if err != nil || !strings.Contains(outLower, "account type") {
		e.log("[INFO] Регистрация Cloudflare WARP не обнаружена. Создание новой учетной записи клиента...")
		regNew := exec.Command("warp-cli", "registration", "new")
		regNew.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = regNew.Run()
	} else {
		e.log("[OK] Обнаружена активная регистрация Cloudflare WARP")
	}

	// Mode WARP
	outMode, _ := exec.Command("warp-cli", "mode", "warp").CombinedOutput()
	if txt := strings.TrimSpace(string(outMode)); txt != "" {
		e.log("[WARP] mode warp: " + txt)
	}



	// Connect via MASQUE protocol
	e.setProgress(80, 0, 0, bestAlt, "Установка туннеля через сетевой фильтр...", true)
	e.log("[INFO] Подключение Cloudflare WARP...")

	connCmd := exec.Command("warp-cli", "connect")
	connCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	outConn, _ := connCmd.CombinedOutput()
	if txt := strings.TrimSpace(string(outConn)); txt != "" {
		e.log("[WARP] connect: " + txt)
	}

	warpConnected := false
	for i := 0; i < 20; i++ {
		pct := 82 + int((float64(i) / 20.0) * 10.0)
		if pct > 92 {
			pct = 92
		}
		e.setProgress(pct, 0, 0, bestAlt, fmt.Sprintf("Ожидание туннеля MASQUE (%d сек)...", i+1), true)
		time.Sleep(1 * time.Second)

		stCmd := exec.Command("warp-cli", "status")
		stCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := stCmd.CombinedOutput()
		outStr := strings.TrimSpace(string(out))
		if err == nil && strings.Contains(strings.ToLower(outStr), "connected") {
			warpConnected = true
			for _, line := range strings.Split(outStr, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					e.log("[WARP] " + line)
				}
			}
			break
		}
	}

	// Fallback: if MASQUE fails (ISP blocks it entirely), try WireGuard (UDP/2408)
	if !warpConnected {
		e.log("[WARN] MASQUE не подключился. Попытка через WireGuard (UDP/2408)...")
		e.setProgress(88, 0, 0, bestAlt, "Резервный протокол WireGuard (UDP/2408)...", true)

		wgCmd := exec.Command("warp-cli", "tunnel", "protocol", "set", "WireGuard")
		wgCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = wgCmd.Run()

		conn2 := exec.Command("warp-cli", "connect")
		conn2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = conn2.Run()

		for i := 0; i < 12; i++ {
			time.Sleep(1 * time.Second)
			st2 := exec.Command("warp-cli", "status")
			st2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			out2, err2 := st2.CombinedOutput()
			if err2 == nil && strings.Contains(strings.ToLower(strings.TrimSpace(string(out2))), "connected") {
				warpConnected = true
				e.log("[WARP] Подключено через WireGuard (резерв, UDP/2408)")
				break
			}
		}
	}

	if !warpConnected {
		_ = e.disconnectInternal()
		e.setProgress(0, 0, 0, "", "Ошибка туннеля Cloudflare WARP", false)
		e.log("[ERROR] Cloudflare WARP не смог установить связь. Проверьте профиль Zapret")
		return fmt.Errorf("Cloudflare WARP не смог подключиться. Сеть возвращена в исходное состояние")
	}

	e.log("[OK] Cloudflare WARP успешно подключен")

	// ==========================================
	// STAGE 5: ФИНАЛИЗАЦИЯ И СТАРТ (92% - 100%)
	// ==========================================
	// Wait for routing tables and health probes to stabilize before launching game.
	e.setProgress(92, 0, 0, bestAlt, "Стабилизация маршрутов и проверка шлюза...", true)
	e.log("[INFO] Ожидание перехода WARP в статус Network: healthy...")

	for i := 0; i < 10; i++ {
		stCmd := exec.Command("warp-cli", "status")
		stCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, _ := stCmd.CombinedOutput()
		outLower := strings.ToLower(string(out))

		if strings.Contains(outLower, "healthy") {
			e.log("[OK] WARP перешел в стабильный режим: Network healthy")
			break
		}
		pct := 92 + int((float64(i) / 10.0) * 6.0) // 92% to 98%
		e.setProgress(pct, 0, 0, bestAlt, fmt.Sprintf("Ожидание стабилизации шлюза (%d сек)...", i+1), true)
		time.Sleep(1 * time.Second)
	}

	e.mu.Lock()
	e.isConnected = true
	e.mu.Unlock()

	e.setProgress(100, 0, 0, bestAlt, "Сетевой туннель полностью активен!", false)
	e.log("[OK] Сетевой туннель полностью активен! Готово к игре")

	// Launch game if checkbox enabled
	if e.cfg.AutolaunchGame {
		e.log("[INFO] Автозапуск WARDOGS в Steam (steam://run/1867240)...")
		steamCmd := exec.Command("cmd.exe", "/c", "start", "steam://run/1867240")
		steamCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = steamCmd.Start()
	}

	if onSuccess != nil {
		onSuccess()
	}

	return nil
}

// Disconnect gracefully disconnects Cloudflare WARP and Zapret without holding the mutex during I/O.
func (e *Engine) Disconnect() error {
	e.mu.Lock()
	e.isConnected = false
	e.isConnecting = false
	e.pipelineProgress = PipelineProgress{
		Percent: 0,
		Message: "",
	}
	e.mu.Unlock()

	e.log("[INFO] Остановка сетевого туннеля...")
	return e.disconnectInternal()
}

// runWarpCli runs warp-cli with a strict context timeout and channel protection to prevent any deadlock or hang.
func runWarpCli(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "warp-cli", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}

	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		ch <- result{out: out, err: err}
	}()

	select {
	case res := <-ch:
		return strings.TrimSpace(string(res.out)), res.err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = scanner.KillProcess("warp-cli.exe")
		return "", ctx.Err()
	}
}

func (e *Engine) disconnectInternal() error {
	// 1. Kill Zapret process immediately so sockets and WinDivert are released fast
	e.log("[INFO] Завершение процесса Zapret (winws.exe)...")
	_ = scanner.KillProcess("winws.exe")

	e.mu.Lock()
	if e.zapretCmd != nil && e.zapretCmd.Process != nil {
		pid := e.zapretCmd.Process.Pid
		_ = e.zapretCmd.Process.Kill()
		// Kill process tree by PID forcibly without blocking Wait
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		killCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = killCmd.Run()
		e.zapretCmd = nil
	}
	e.mu.Unlock()

	// 2. Stop driver service (non-blocking with 2s timeout)
	scanner.StopWinDivertService()

	// 3. Disconnect WARP with a strict 2.5s timeout so it never blocks or hangs
	e.log("[INFO] Отключение Cloudflare WARP...")
	outDisc, _ := runWarpCli(2500*time.Millisecond, "disconnect")
	if outDisc != "" {
		e.log("[WARP] " + outDisc)
	}

	e.mu.Lock()
	e.isConnected = false
	e.pipelineProgress = PipelineProgress{
		Percent: 0,
		Message: "",
	}
	e.mu.Unlock()

	e.log("[OK] Все соединения отключены, сеть восстановлена")
	return nil
}

func (e *Engine) GetPipelineProgress() PipelineProgress {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pipelineProgress
}

// RunFullZapretTest runs the authentic Zapret test suite (service 12) with real-time output logging
func (e *Engine) RunFullZapretTest() error {
	e.mu.Lock()
	if e.isConnecting || e.isTesting {
		e.mu.Unlock()
		return fmt.Errorf("операция уже выполняется")
	}
	e.isTesting = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.isTesting = false
		e.mu.Unlock()
		time.Sleep(2 * time.Second)
		e.setProgress(0, 0, 0, "", "", false)
	}()

	return e.runFullZapretTestInternal(false)
}

func (e *Engine) runFullZapretTestInternal(isPipeline bool) error {
	zapretDir := deps.GetZapretDir()
	deps.PatchTestScriptIfNeeded(e.log)
	testScript := filepath.Join(zapretDir, "utils", "test zapret.ps1")
	if _, err := os.Stat(testScript); err != nil {
		return fmt.Errorf("скрипт тестирования %s не найден", testScript)
	}

	e.log("============================================================")
	e.log("[ZAPRET TEST] ЗАПУСК ОФИЦИАЛЬНОГО ТЕСТИРОВАНИЯ ZAPRET (service 12)")
	e.log("[ZAPRET TEST] Проверка 22 конфигураций по 16 целевым сервисам...")
	e.log("============================================================")

	if isPipeline {
		e.setProgress(15, 0, 22, "", "Калибровка: подготовка тестов 22 профилей Zapret...", true)
	} else {
		e.setProgress(1, 0, 22, "", "Инициализация официального теста Zapret...", true)
	}

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", fmt.Sprintf("& '%s'", testScript))
	cmd.Dir = zapretDir
	cmd.Env = append(os.Environ(),
		"NON_INTERACTIVE=1",
		"TEST_TYPE=standard",
		"TEST_MODE=all",
	)
	cmd.Stdin = strings.NewReader("1\r\n1\r\n0\r\n\r\n")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		e.log(fmt.Sprintf("[ERROR] Ошибка запуска тестов Zapret: %v", err))
		return err
	}

	reConfigProgress := regexp.MustCompile(`\[(\d+)/(\d+)\]\s+([^\r\n]+)`)

	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				e.log("[ZAPRET TEST] " + line)
				matches := reConfigProgress.FindStringSubmatch(line)
				if len(matches) > 3 {
					cur, _ := strconv.Atoi(matches[1])
					tot, _ := strconv.Atoi(matches[2])
					cfgName := strings.TrimSpace(matches[3])
					var pct int
					var msg string
					if isPipeline {
						pct = 15 + int((float64(cur) / float64(tot)) * 45.0)
						msg = fmt.Sprintf("Первичная калибровка [%d/%d]: %s", cur, tot, cfgName)
					} else {
						pct = int((float64(cur) / float64(tot)) * 100.0)
						msg = fmt.Sprintf("Тест [%d/%d]: %s", cur, tot, cfgName)
					}
					e.setProgress(pct, cur, tot, cfgName, msg, true)
				}
			}
		}
	}()

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				e.log("[ZAPRET TEST ERR] " + line)
			}
		}
	}()

	err := cmd.Wait()

	// Parse new results report
	if reportPath, rErr := FindLatestZapretReport(); rErr == nil {
		if summary, sErr := ParseZapretReport(reportPath); sErr == nil {
			e.log(fmt.Sprintf("[ZAPRET TEST] Тестирование завершено! Создан отчет: %s", summary.ReportFile))
			if len(summary.Scores) > 0 {
				e.log(fmt.Sprintf("[ZAPRET TEST] Победитель тестирования: %s (OK: %d, ERR: %d)", summary.BestConfig, summary.Scores[0].OkCount, summary.Scores[0].ErrCount))
			}
			e.cfg.SelectedAlt = summary.BestConfig
			_ = e.cfg.Save()
			if isPipeline {
				e.setProgress(60, 22, 22, summary.BestConfig, "Калибровка завершена! Выбран: "+summary.BestConfig, true)
			} else {
				e.setProgress(100, 22, 22, summary.BestConfig, "Тестирование завершено! Победитель: "+summary.BestConfig, false)
			}
		}
	}

	return err
}
