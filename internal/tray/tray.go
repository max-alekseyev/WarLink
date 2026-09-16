package tray

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	modShell32 = syscall.NewLazyDLL("shell32.dll")
	modUser32  = syscall.NewLazyDLL("user32.dll")

	procShellNotifyIcon = modShell32.NewProc("Shell_NotifyIconW")
	procExtractIconEx   = modShell32.NewProc("ExtractIconExW")
	procRegisterClassEx = modUser32.NewProc("RegisterClassExW")
	procCreateWindowEx  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProc   = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow   = modUser32.NewProc("DestroyWindow")
	procGetMessage      = modUser32.NewProc("GetMessageW")
	procTranslateMsg    = modUser32.NewProc("TranslateMessage")
	procDispatchMsg     = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage = modUser32.NewProc("PostQuitMessage")
	procCreatePopupMenu = modUser32.NewProc("CreatePopupMenu")
	procAppendMenu      = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu  = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu     = modUser32.NewProc("DestroyMenu")
	procGetCursorPos    = modUser32.NewProc("GetCursorPos")
	procSetForeground   = modUser32.NewProc("SetForegroundWindow")
	procPostMessage     = modUser32.NewProc("PostMessageW")
	procLoadIcon        = modUser32.NewProc("LoadIconW")
	modKernel32         = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandle = modKernel32.NewProc("GetModuleHandleW")
)

const (
	NIM_ADD        = 0x00000000
	NIM_MODIFY     = 0x00000001
	NIM_DELETE     = 0x00000002
	NIF_MESSAGE    = 0x00000001
	NIF_ICON       = 0x00000002
	NIF_TIP        = 0x00000004
	WM_USER        = 0x0400
	WM_TRAYICON    = WM_USER + 1
	WM_COMMAND     = 0x0111
	WM_LBUTTONUP   = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP   = 0x0205
	MF_STRING      = 0x00000000
	MF_SEPARATOR   = 0x00000800
	TPM_RIGHTBUTTON = 0x0002
	TPM_BOTTOMALIGN = 0x0020
	IDI_APPLICATION = 32512

	ID_OPEN = 1001
	ID_EXIT = 1002
)

type POINT struct {
	X int32
	Y int32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type Tray struct {
	hwnd     uintptr
	hicon    uintptr
	nid      NOTIFYICONDATAW
	onOpen   func()
	onExit   func()
	isActive bool
}

var globalTray *Tray

func New(onOpen, onExit func()) *Tray {
	t := &Tray{
		onOpen: onOpen,
		onExit: onExit,
	}
	globalTray = t
	return t
}

func (t *Tray) Start() error {
	ready := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		classNamePtr, _ := syscall.UTF16PtrFromString("WarLink_Tray_Class")

		wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
			switch msg {
			case WM_TRAYICON:
				switch lParam {
				case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
					if globalTray != nil && globalTray.onOpen != nil {
						globalTray.onOpen()
					}
					return 0
				case WM_RBUTTONUP:
					globalTray.showContextMenu()
					return 0
				}
			case WM_COMMAND:
				cmdId := uint16(wParam & 0xFFFF)
				switch cmdId {
				case ID_OPEN:
					if globalTray != nil && globalTray.onOpen != nil {
						globalTray.onOpen()
					}
				case ID_EXIT:
					if globalTray != nil && globalTray.onExit != nil {
						globalTray.onExit()
					}
				}
				return 0
			}
			r, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wParam, lParam)
			return r
		})

		wc := WNDCLASSEXW{
			CbSize:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			LpfnWndProc: wndProc,
			LpszClassName: classNamePtr,
		}

		atom, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			ready <- fmt.Errorf("register class error: %v", err)
			return
		}

		// Hidden top-level window (WS_POPUP). Shell_NotifyIcon requires a valid top-level window, not HWND_MESSAGE.
		hwnd, _, err := procCreateWindowEx.Call(
			0,
			uintptr(unsafe.Pointer(classNamePtr)),
			0,
			0x80000000, // WS_POPUP
			0, 0, 0, 0,
			0, // Top-level window
			0, 0, 0,
		)
		if hwnd == 0 {
			ready <- fmt.Errorf("create window error: %v", err)
			return
		}

		t.hwnd = hwnd

		// Load crisp 16x16 icon from current executable using ExtractIconEx
		var hicon uintptr
		if exePath, err := os.Executable(); err == nil {
			exePtr, _ := syscall.UTF16PtrFromString(exePath)
			var hLarge, hSmall uintptr
			res, _, _ := procExtractIconEx.Call(
				uintptr(unsafe.Pointer(exePtr)),
				0,
				uintptr(unsafe.Pointer(&hLarge)),
				uintptr(unsafe.Pointer(&hSmall)),
				1,
			)
			if res > 0 {
				if hSmall != 0 {
					hicon = hSmall
				} else if hLarge != 0 {
					hicon = hLarge
				}
			}
		}

		if hicon == 0 {
			hInst, _, _ := procGetModuleHandle.Call(0)
			hicon, _, _ = procLoadIcon.Call(hInst, uintptr(1))
			if hicon == 0 {
				hicon, _, _ = procLoadIcon.Call(0, uintptr(IDI_APPLICATION))
			}
		}
		t.hicon = hicon

		t.nid = NOTIFYICONDATAW{
			CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
			HWnd:             t.hwnd,
			UID:              1,
			UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
			UCallbackMessage: WM_TRAYICON,
			HIcon:            t.hicon,
		}
		copyUTF16("WarLink - Сетевая оптимизация", t.nid.SzTip[:])

		ready <- nil

		// Message loop
		var msg struct {
			Hwnd    uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      POINT
		}

		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()

	return <-ready
}

func (t *Tray) Show() {
	if t.hwnd == 0 {
		return
	}
	procShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.nid)))
	t.isActive = true
}

func (t *Tray) Hide() {
	if t.hwnd == 0 {
		return
	}
	procShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))
	t.isActive = false
}

func (t *Tray) SetTooltip(tip string) {
	copyUTF16(tip, t.nid.SzTip[:])
	if t.isActive {
		procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&t.nid)))
	}
}

func (t *Tray) showContextMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	openText, _ := syscall.UTF16PtrFromString("Развернуть WarLink")
	exitText, _ := syscall.UTF16PtrFromString("Отключить и выйти")

	procAppendMenu.Call(hMenu, MF_STRING, uintptr(ID_OPEN), uintptr(unsafe.Pointer(openText)))
	procAppendMenu.Call(hMenu, MF_SEPARATOR, 0, 0)
	procAppendMenu.Call(hMenu, MF_STRING, uintptr(ID_EXIT), uintptr(unsafe.Pointer(exitText)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForeground.Call(t.hwnd)
	procTrackPopupMenu.Call(
		hMenu,
		TPM_RIGHTBUTTON|TPM_BOTTOMALIGN,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		t.hwnd,
		0,
	)
	procPostMessage.Call(t.hwnd, 0, 0, 0)
}

func copyUTF16(s string, dst []uint16) {
	u16, _ := syscall.UTF16FromString(s)
	for i := 0; i < len(dst); i++ {
		if i < len(u16) {
			dst[i] = u16[i]
		} else {
			dst[i] = 0
		}
	}
}
