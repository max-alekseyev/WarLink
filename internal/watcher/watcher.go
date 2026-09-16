package watcher

import (
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
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

type GameWatcher struct {
	mu            sync.Mutex
	stopChan      chan struct{}
	isRunning     bool
	wasRunning    bool
	missCount     int
	onGameExit    func()
	onGameStarted func()
}

func New(onGameStarted func(), onGameExit func()) *GameWatcher {
	return &GameWatcher{
		stopChan:      make(chan struct{}),
		onGameStarted: onGameStarted,
		onGameExit:    onGameExit,
	}
}

// IsGameRunning checks with sub-millisecond Toolhelp32 snapshot (zero overhead, 0 processes)
// whether any WARDOGS process is active on the system.
func IsGameRunning() bool {
	hSnap, _, _ := procCreateToolhelp32Snapshot.Call(uintptr(TH32CS_SNAPPROCESS), 0)
	if hSnap == 0 || hSnap == uintptr(syscall.InvalidHandle) {
		return false
	}
	defer procCloseHandle.Call(hSnap)

	var entry PROCESSENTRY32W
	entry.DwSize = uint32(unsafe.Sizeof(entry))

	r, _, _ := procProcess32First.Call(hSnap, uintptr(unsafe.Pointer(&entry)))
	if r == 0 {
		return false
	}

	for {
		name := syscall.UTF16ToString(entry.SzExeFile[:])
		nameLower := strings.ToLower(name)
		if strings.Contains(nameLower, "wardogs") {
			return true
		}

		r, _, _ = procProcess32Next.Call(hSnap, uintptr(unsafe.Pointer(&entry)))
		if r == 0 {
			break
		}
	}
	return false
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
		// 400ms ticker for near-instant game start and game exit detection
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChan:
				return
			case <-ticker.C:
				running := IsGameRunning()

				w.mu.Lock()
				if running {
					w.missCount = 0
					if !w.wasRunning {
						w.wasRunning = true
						w.mu.Unlock()
						if w.onGameStarted != nil {
							w.onGameStarted()
						}
						continue
					}
				} else {
					if w.wasRunning {
						w.missCount++
						// 2 consecutive misses at 400ms = 800ms confirmed exit
						if w.missCount >= 2 {
							w.wasRunning = false
							w.missCount = 0
							w.mu.Unlock()
							if w.onGameExit != nil {
								w.onGameExit()
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
