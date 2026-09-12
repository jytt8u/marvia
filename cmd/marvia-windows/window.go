//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
)

var (
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procShowWindow           = user32.NewProc("ShowWindow")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
)

const (
	// Индексы для SetWindowLongPtr: отрицательные, в регистр уходят как 32-битные.
	gwlStyle    = uintptr(0xFFFFFFF0) // GWL_STYLE = -16
	gwlpWndProc = uintptr(0xFFFFFFFC) // GWLP_WNDPROC = -4

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000

	swHide = 0
	swShow = 5

	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010

	spiGetWorkArea = 0x0030

	// Размеры из макета: полное окно и трей-виджет 380×520.
	fullWidth    = 1120
	fullHeight   = 700
	widgetWidth  = 380
	widgetHeight = 520
	widgetGap    = 12
)

type rect struct{ Left, Top, Right, Bottom int32 }

// shell — окно программы и значок в трее.
//
// Окно одно, но у него два вида. Полное — как раньше, с рамкой и вкладками.
// Виджет — макет D01: 380×520 без рамки у трея, кнопка, состояние, нода и
// три ссылки; открывается по значку и прячется, как только человек нажал
// куда-то ещё. Второго окна с движком нет намеренно: два WebView2 — это два
// набора памяти ради двух шкур одной страницы.
//
// Крестик и «закрыть» больше не выключают программу: окно уходит в трей, а
// туннель живёт. Выход — только из меню значка.
type shell struct {
	w       webview2.WebView
	hwnd    uintptr
	url     string
	widget  bool
	oldProc uintptr
	tray    *tray
	ctl     *Controller
	log     *journal
}

// showWindow открывает окно программы и держит её до выхода из меню трея.
//
// Внутри окна — движок WebView2, который в Windows 11 встроен, а в десятке
// доезжает с обновлениями. Своего набора виджетов мы не тащим: он потребовал
// бы компилятора C, весил бы больше самого ядра и выглядел бы одинаково чужим
// на всех системах.
//
// Если движка не оказалось, открываем ту же страницу в браузере. Это хуже —
// окно с адресной строкой вместо программы, — но лучше, чем отказ работать.
// hidden — начать в трее, без окна: так программа стартует вместе с Windows.
func showWindow(url string, ctl *Controller, log *journal, hidden bool, onWindow *func(mode, tab string)) error {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Marvia",
			Width:  fullWidth,
			Height: fullHeight,
			Center: true,
		},
	})
	if w == nil {
		return openInBrowser(url)
	}
	defer w.Destroy()

	s := &shell{w: w, hwnd: uintptr(w.Window()), url: url, ctl: ctl, log: log}
	s.subclass()

	t, err := newTray()
	if err != nil {
		// Без значка программа теряет способ выйти из трея, поэтому ведём
		// себя как раньше: крестик выключает. Об этом — в журнал.
		log.add("значок в трее не встал: %v — окно закрывается вместе с туннелем", err)
	} else {
		s.tray = t
		t.onClick = func() { s.toggleWidget() }
		t.onMenu = s.menu
		t.menuState = func() (bool, bool) {
			st := ctl.Status()
			return st.State == StateConnected || st.State == StateConnecting || st.State == StateStalled, st.HasAccount
		}
		defer t.Remove()
	}

	// Страница просит переключить вид: из виджета — в полное окно на нужную
	// вкладку, крестиком виджета — спрятаться. Приходит из обработчика HTTP,
	// то есть из другого потока, а окно трогать можно только из своего.
	*onWindow = func(mode, tab string) {
		w.Dispatch(func() {
			switch mode {
			case "full":
				s.showFull(tab)
			case "hide":
				s.hide()
			}
		})
	}

	if hidden && s.tray != nil {
		w.Navigate(url)
		s.hide()
	} else {
		s.showFull("")
	}

	w.Run()
	return nil
}

// subclass перехватывает сообщения окна: крестик прячет, а не разрушает,
// потеря фокуса прячет виджет.
func (s *shell) subclass() {
	proc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		switch msg {
		case wmClose:
			if s.tray != nil {
				s.hide()
				return 0
			}
		case wmActivate:
			if s.widget && wp&0xffff == waInactive {
				s.hide()
			}
		}
		r, _, _ := procCallWindowProcW.Call(s.oldProc, hwnd, msg, wp, lp)
		return r
	})
	s.oldProc, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlpWndProc, proc)
}

func (s *shell) hide() {
	_, _, _ = procShowWindow.Call(s.hwnd, swHide)
}

func (s *shell) visible() bool {
	r, _, _ := procIsWindowVisible.Call(s.hwnd)
	return r != 0
}

// showFull показывает полное окно; tab — какую вкладку открыть.
func (s *shell) showFull(tab string) {
	s.widget = false
	_, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlStyle, wsOverlappedWindow|wsVisible)
	var wa rect
	_, _, _ = procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x := int(wa.Left) + (int(wa.Right-wa.Left)-fullWidth)/2
	y := int(wa.Top) + (int(wa.Bottom-wa.Top)-fullHeight)/2
	_, _, _ = procSetWindowPos.Call(s.hwnd, 0, uintptr(x), uintptr(y), fullWidth, fullHeight, swpNoZOrder|swpFrameChanged)
	page := s.url
	if tab != "" {
		page += "?tab=" + tab
	}
	s.w.Navigate(page)
	_, _, _ = procShowWindow.Call(s.hwnd, swShow)
	_, _, _ = procSetForegroundWindow.Call(s.hwnd)
}

// toggleWidget — нажатие на значок: виджет показать или спрятать.
func (s *shell) toggleWidget() {
	if s.visible() {
		s.hide()
		return
	}
	s.widget = true
	_, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlStyle, wsPopup|wsVisible)
	// В угол рабочей области — туда, где трей. Панель задач может стоять и
	// сбоку, и сверху; рабочая область это учитывает, а угол справа внизу
	// остаётся ближайшим к значку в подавляющем числе случаев.
	var wa rect
	_, _, _ = procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x := int(wa.Right) - widgetWidth - widgetGap
	y := int(wa.Bottom) - widgetHeight - widgetGap
	_, _, _ = procSetWindowPos.Call(s.hwnd, 0, uintptr(x), uintptr(y), widgetWidth, widgetHeight, swpNoZOrder|swpFrameChanged)
	s.w.Navigate(s.url + "?mode=widget")
	_, _, _ = procShowWindow.Call(s.hwnd, swShow)
	_, _, _ = procSetForegroundWindow.Call(s.hwnd)
}

// menu исполняет выбор из меню значка.
func (s *shell) menu(id int) {
	switch id {
	case menuOpen:
		s.showFull("")
	case menuConnect:
		go func() {
			if err := s.ctl.Connect(); err != nil {
				s.log.add("из трея: %v", err)
			}
		}()
	case menuDisconnect:
		go s.ctl.Disconnect()
	case menuExit:
		s.ctl.Disconnect()
		if s.tray != nil {
			s.tray.Remove()
			s.tray = nil
		}
		_, _, _ = procPostQuitMessage.Call(0)
	}
}

// openInBrowser показывает страницу обычным браузером.
//
// Запускаем через проводник, а не напрямую: программа работает с правами
// администратора, и браузер, запущенный из неё, унаследовал бы их. Chrome и
// Edge в таком виде просто отказываются открываться, а если бы открылись — это
// был бы браузер с правами администратора, чего быть не должно. Проводник же
// работает от обычного пользователя и откроет браузер таким же.
func openInBrowser(url string) error {
	fmt.Printf("окно не открылось — показываю страницу в браузере:\n  %s\n", url)
	fmt.Printf("\nдля выхода нажми Ctrl+C — маршруты снимутся сами\n")

	if err := exec.Command("explorer.exe", url).Start(); err != nil {
		return fmt.Errorf("не удалось открыть браузер: %w", err)
	}

	// Держим программу живой, пока человек не остановит её сам: туннель живёт
	// в этом процессе, и выйти отсюда означало бы его выключить. Выход —
	// через Ctrl+C, его ловит main.
	select {}
}
