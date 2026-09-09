//go:build windows

package main

import (
	"fmt"
	"os/exec"

	"github.com/jchv/go-webview2"
)

// showWindow открывает окно программы и держит его до закрытия.
//
// Внутри окна — движок WebView2, который в Windows 11 встроен, а в десятке
// доезжает с обновлениями. Своего набора виджетов мы не тащим: он потребовал
// бы компилятора C, весил бы больше самого ядра и выглядел бы одинаково чужим
// на всех системах.
//
// Если движка не оказалось, открываем ту же страницу в браузере. Это хуже —
// окно с адресной строкой вместо программы, — но лучше, чем отказ работать.
func showWindow(url string) error {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Marvia",
			Width:  980,
			Height: 640,
			Center: true,
		},
	})
	if w == nil {
		return openInBrowser(url)
	}
	defer w.Destroy()

	w.Navigate(url)
	w.Run()
	return nil
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
