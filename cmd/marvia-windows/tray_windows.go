//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Значок в трее.
//
// Без него закрытое окно означало выключенный туннель: свернуть программу
// было некуда, и человек держал окно открытым весь день или терял связь,
// закрыв его по привычке. Теперь окно закрывается в трей, туннель живёт,
// а значок — единственное место, откуда всё видно и выключается.
//
// Реализовано напрямую через user32 и shell32, без сторонней библиотеки: та
// потянула бы свой цикл сообщений, а он здесь уже есть — у окна WebView2.
// Скрытое окно-приёмник живёт в том же потоке, и его сообщения раздаёт тот
// же цикл.

//go:embed ui/tray.png
var trayPNG []byte

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy   = 0x0002
	wmClose     = 0x0010
	wmActivate  = 0x0006
	wmCommand   = 0x0111
	wmNull      = 0x0000
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmApp       = 0x8000
	wmTrayEvent = wmApp + 1
	waInactive  = 0

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfGrayed    = 0x0001

	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	hwndMessage = ^uintptr(2) // HWND_MESSAGE: окно только для сообщений, без экрана
)

// Пункты меню по правой кнопке.
const (
	menuOpen = iota + 1
	menuConnect
	menuDisconnect
	menuExit
)

// notifyIconData — NOTIFYICONDATAW. Порядок и типы полей — как в заголовке
// Windows, иначе shell32 прочтёт чужие байты.
type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct{ X, Y int32 }

// tray — значок и его скрытое окно-приёмник.
type tray struct {
	hwnd uintptr
	icon uintptr
	data notifyIconData

	// Что делать по нажатиям. Ставит окно: значок про окно ничего не знает.
	onClick   func()
	onMenu    func(id int)
	menuState func() (connected bool, hasAccount bool)
}

// newTray вешает значок. Зовётся из потока цикла сообщений.
func newTray() (*tray, error) {
	t := &tray{}

	className, _ := syscall.UTF16PtrFromString("MarviaTray")
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(t.wndProc),
		Instance:  inst,
		ClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, fmt.Errorf("класс окна трея: %w", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0,
		hwndMessage, 0, inst, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("окно трея: %w", err)
	}
	t.hwnd = hwnd

	// Иконка — из PNG, прямо из байтов: с Vista user32 умеет читать PNG как
	// ресурс иконки, и отдельный .ico ради этого не нужен.
	icon, _, err := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&trayPNG[0])), uintptr(len(trayPNG)), 1, 0x30000, 0, 0, 0)
	if icon == 0 {
		return nil, fmt.Errorf("иконка трея: %w", err)
	}
	t.icon = icon

	t.data = notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             hwnd,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayEvent,
		Icon:            icon,
	}
	t.setTip("Marvia")
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.data))); r == 0 {
		return nil, fmt.Errorf("значок в трее: %w", err)
	}
	return t, nil
}

func (t *tray) setTip(text string) {
	u, _ := syscall.UTF16FromString(text)
	for i := range t.data.Tip {
		t.data.Tip[i] = 0
	}
	copy(t.data.Tip[:len(t.data.Tip)-1], u)
}

// Tip меняет подсказку под курсором: «Подключено · Финляндия» и тому подобное.
func (t *tray) Tip(text string) {
	t.setTip(text)
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.data)))
}

// Remove снимает значок. Обязательно перед выходом: иначе он висит до
// первого наведения, как у половины программ на свете.
func (t *tray) Remove() {
	if t.hwnd == 0 {
		return
	}
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.data)))
	if t.icon != 0 {
		_, _, _ = procDestroyIcon.Call(t.icon)
		t.icon = 0
	}
	if t.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(t.hwnd)
		t.hwnd = 0
	}
}

func (t *tray) wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == wmTrayEvent {
		switch lp & 0xffff {
		case wmLButtonUp:
			if t.onClick != nil {
				t.onClick()
			}
		case wmRButtonUp:
			t.showMenu()
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// showMenu показывает меню по правой кнопке и сразу исполняет выбор.
func (t *tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	connected, hasAccount := false, false
	if t.menuState != nil {
		connected, hasAccount = t.menuState()
	}
	add := func(id int, text string, grayed bool) {
		flags := uintptr(mfString)
		if grayed {
			flags |= mfGrayed
		}
		s, _ := syscall.UTF16PtrFromString(text)
		_, _, _ = procAppendMenuW.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(s)))
	}
	d := trayWords()
	add(menuOpen, d.open, false)
	if connected {
		add(menuDisconnect, d.disconnect, false)
	} else {
		add(menuConnect, d.connect, !hasAccount)
	}
	_, _, _ = procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	add(menuExit, d.exit, false)

	// Без SetForegroundWindow меню не закрывается по клику мимо, а без
	// пустого сообщения после — открывается не с первого раза. Обе странности
	// описаны у Microsoft и лечатся ровно так.
	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	id, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)

	if id != 0 && t.onMenu != nil {
		t.onMenu(int(id))
	}
}

// trayWords — надписи меню на языке окна. Словарь окна живёт в странице, а
// меню рисует Windows, поэтому эти четыре слова — здесь.
type trayDict struct{ open, connect, disconnect, exit string }

func trayWords() trayDict {
	if lang, _ := uiLang.Load().(string); lang == "en" {
		return trayDict{"Open Marvia", "Connect", "Disconnect", "Quit"}
	}
	return trayDict{"Открыть Marvia", "Подключиться", "Отключиться", "Выйти"}
}
