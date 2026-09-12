//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Запуск вместе с Windows.
//
// Через планировщик, а не через ветку Run в реестре: программа работает с
// правами администратора, и из Run она при каждом входе показывала бы окно
// согласия — сразу после загрузки, когда человек ещё ничего не нажимал.
// Задача планировщика с высшими правами запускается без вопросов, для этого
// планировщик и существует. Создать её может только администратор — мы им
// и работаем.
const autostartTask = "Marvia"

// autostartOn сообщает, стоит ли задача.
func autostartOn() bool {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", autostartTask)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run() == nil
}

// setAutostart ставит или снимает задачу. Задача зовёт нас с -tray: без окна,
// и с подключением, если ключ есть.
func setAutostart(on bool) error {
	if !on {
		cmd := exec.Command("schtasks.exe", "/Delete", "/TN", autostartTask, "/F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if out, err := cmd.CombinedOutput(); err != nil && autostartOn() {
			return errors.New(strings.TrimSpace(decodeConsole(out)))
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command("schtasks.exe", "/Create", "/F", "/TN", autostartTask,
		"/SC", "ONLOGON", "/RL", "HIGHEST", "/IT",
		"/TR", `"`+exe+`" -tray`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(decodeConsole(out)))
	}
	return nil
}

// decodeConsole переводит вывод консольной команды в текст.
//
// schtasks пишет в кодировке консоли, а не в UTF-8; в русской Windows это
// CP866, и без перевода ошибка выглядела бы как набор знаков вопроса.
// Проще всего — оставить как есть, если это уже UTF-8, и заменить остальное
// на общую фразу: сама причина обычно в правах, и она одна.
func decodeConsole(out []byte) string {
	s := string(out)
	for _, r := range s {
		if r == '�' {
			return say("autostartFailed")
		}
	}
	return s
}
