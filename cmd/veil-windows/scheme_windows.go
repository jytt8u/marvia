//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Ссылка veil-account:// из телеграма должна открывать программу.
//
// На телефоне это уже работает: покупатель нажимает ссылку, приложение
// подхватывает ключ, ему остаётся нажать одну кнопку. На компьютере он до сих
// пор копировал ссылку руками, искал, куда её вставить, и терял её по дороге —
// причём это первое, что он делает после оплаты, и самый обидный шаг, чтобы
// потерять человека.
//
// Схема регистрируется в HKCU, а не в HKLM: прав администратора у нас может и
// не быть, а в своей ветке реестра пользователь хозяин. Побочный плюс — при
// удалении программы чужим пользователям ничего не остаётся.

const schemeKey = `Software\Classes\veil-account`

// registerScheme прописывает схему на себя.
//
// Вызывается при каждом запуске: путь к программе меняется, когда её
// перекладывают из «Загрузок» на рабочий стол, а человек об этом программе не
// сообщит.
func registerScheme(exe string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("ветка реестра: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue("", "URL:Veil Account"); err != nil {
		return err
	}
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKey+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("команда запуска: %w", err)
	}
	defer cmd.Close()

	// Кавычки обязательны: путь почти всегда содержит пробелы, а без них
	// Windows передаст программе первое слово и потеряет остальное.
	return cmd.SetStringValue("", `"`+exe+`" "%1"`)
}

// accountFromArgs достаёт ссылку доступа из аргументов запуска.
//
// Windows зовёт программу с адресом одним аргументом. Флаги при этом не
// используются, поэтому берём первый аргумент, похожий на нашу ссылку, и не
// трогаем остальные: программу запускают и руками, с ключами.
func accountFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), "veil-account://") {
			return arg
		}
	}
	return ""
}

// setupScheme регистрирует схему и возвращает ссылку, с которой запустили.
func setupScheme(log *journal) string {
	exe, err := os.Executable()
	if err == nil {
		if err := registerScheme(exe); err != nil {
			// Не смертельно: ссылки просто не будут открываться, ключ можно
			// вставить руками. Ронять из-за этого программу нельзя.
			log.add("схема veil-account не зарегистрирована: %v", err)
		}
	}
	return accountFromArgs(os.Args[1:])
}
