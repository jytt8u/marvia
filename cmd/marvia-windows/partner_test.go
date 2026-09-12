//go:build windows

package main

import "testing"

// Ссылка-приглашение разбирается только в своём виде: схема, токен, адрес.
// Всё остальное — отказ, а не догадка: догадавшись неверно, программа
// отправила бы токен продавца не на ту панель.
func TestPanelLinkNeedsSchemeTokenAndHost(t *testing.T) {
	token, host, err := parsePanelLink("  marvia-panel://abc%2Fdef@panel.example.test/  ")
	if err != nil || token != "abc/def" || host != "panel.example.test" {
		t.Fatalf("хорошая ссылка: %q %q %v", token, host, err)
	}
	if _, host, err := parsePanelLink("marvia-panel://tok@panel.example.test:8443/"); err != nil || host != "panel.example.test:8443" {
		t.Fatalf("порт в адресе: %q %v", host, err)
	}

	for _, bad := range []string{
		"",
		"marvia://key@panel.example.test/sub/t",
		"https://panel.example.test/",
		"marvia-panel://panel.example.test/",
		"marvia-panel://@panel.example.test/",
		"marvia-panel://tok@/",
		"marvia-panel:tok",
	} {
		if _, _, err := parsePanelLink(bad); err == nil {
			t.Errorf("прошла плохая ссылка: %q", bad)
		}
	}
}

// Из аргументов запуска ссылка-приглашение и ссылка доступа берутся
// каждая своя, и одна не выдаёт себя за другую.
func TestArgsTellPanelLinkFromAccountLink(t *testing.T) {
	args := []string{"-tray", "MARVIA-PANEL://tok@panel.example.test/", "marvia://k@panel/sub/t"}
	if got := panelFromArgs(args); got != "MARVIA-PANEL://tok@panel.example.test/" {
		t.Errorf("приглашение: %q", got)
	}
	if got := accountFromArgs(args); got != "marvia://k@panel/sub/t" {
		t.Errorf("ключ доступа: %q", got)
	}
	if got := accountFromArgs([]string{"marvia-panel://tok@panel.example.test/"}); got != "" {
		t.Errorf("приглашение принято за ключ доступа: %q", got)
	}
}

// Адрес панели — только https: по нему ходит токен.
func TestPanelURLIsHTTPSOnly(t *testing.T) {
	if got := panelURL("panel.example.test:8443"); got != "https://panel.example.test:8443/" {
		t.Errorf("адрес: %q", got)
	}
}
