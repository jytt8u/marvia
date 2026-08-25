//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// Программа собирается оконной, а не консольной: чёрное окно с текстом рядом
// с настоящим окном выглядит недоделкой, и людям оно ничего не говорит.
//
// Цена в том, что печатать больше некуда: stdout уходит в никуда. Поэтому всё,
// что раньше печаталось, теперь показывается системным окном сообщения — либо
// попадает в журнал внутри программы.

// alert показывает системное окно с сообщением.
func alert(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}

// notice показывает системное окно без значка ошибки.
func notice(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_SETFOREGROUND)
}
