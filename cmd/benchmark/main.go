package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
	"warlink/internal/deps"
	"warlink/internal/scanner"
)

var (
	modShell32       = syscall.NewLazyDLL("shell32.dll")
	procShellExecute  = modShell32.NewProc("ShellExecuteW")
	procIsUserAnAdmin = modShell32.NewProc("IsUserAnAdmin")
)

const SW_SHOWNORMAL = 1

func isRunningAsAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}

func ensureAdminElevation() {
	for _, arg := range os.Args {
		if arg == "--no-elevate" {
			return
		}
	}
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

	ret, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(pVerb)), uintptr(unsafe.Pointer(pExe)), uintptr(unsafe.Pointer(pArgs)), uintptr(unsafe.Pointer(pCwd)), uintptr(SW_SHOWNORMAL))
	if ret > 32 {
		os.Exit(0)
	}

	fmt.Println("[WARN] Не удалось автоматически повысить права до Администратора.")
	fmt.Println("[WARN] Пожалуйста, запустите терминал или файл от имени Администратора (ПКМ -> Запуск от имени администратора).")
	pause()
	os.Exit(1)
}

func pause() {
	fmt.Println("\nНажмите Enter для выхода...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadBytes('\n')
}

func main() {
	ensureAdminElevation()

	fmt.Println("================================================================")
	fmt.Println("   WARLINK: ТЕСТИРОВАНИЕ 22 ПРОФИЛЕЙ ОБХОДА DPI (LIVE MATRIX)")
	fmt.Println("================================================================")

	zapretDir := deps.GetZapretDir()
	if !deps.HasZapret() {
		fmt.Println("[INFO] Распаковка сетевого фильтра WinDivert / winws...")
		_ = deps.PrepareZapret(func(msg string) {
			fmt.Println(" ", msg)
		})
	}

	winner, bestScore, err := scanner.RunFullBenchmark(
		zapretDir,
		func(curr, total int, presetName, logLine string) {
			// Realtime step progress
		},
		func(msg string) {
			fmt.Println(msg)
		},
	)

	if err != nil {
		fmt.Printf("\n[ERROR] Ошибка выполнения бенчмарка: %v\n", err)
		pause()
		os.Exit(1)
	}

	fmt.Println("\n================================================================")
	fmt.Printf(" [ИТОГ] ЛУЧШИЙ ПРОФИЛЬ ДЛЯ ВАШЕГО ПРОВАЙДЕРА: %s\n", winner)
	if bestScore != nil {
		fmt.Printf(" Пройдено проверок: %d/%d\n", bestScore.PassedChecks, bestScore.TotalChecks)
		fmt.Printf(" Средняя задержка (Ping RTT): %d ms\n", bestScore.AvgPingMs)
	}
	fmt.Println("================================================================")
	pause()
}
