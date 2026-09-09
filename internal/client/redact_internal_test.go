package client

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// Секрет не должен попадать в текст ошибки.
//
// Проверяем не «красиво ли написано», а что конкретных строк в выводе нет:
// ошибка отсюда уезжает и на экран, и в журнал, а журнал человек пересылает
// продавцу, когда просит помочь.

const (
	testKey   = "TBumTZ6x_oiA38AE7QfyDVodxg0PamSql514tPLu3Lk"
	testToken = "0XWt7C7KexXcIVSYLvIAU354W1UyP5IFlEqc2WOvdTs"
)

func TestWithoutSecretDropsToken(t *testing.T) {
	err := withoutSecret(&url.Error{
		Op:  "Get",
		URL: "https://panel.example.com/sub/" + testToken + "?format=json",
		Err: errors.New("dial tcp: connection refused"),
	})

	got := err.Error()
	if strings.Contains(got, testToken) {
		t.Fatalf("токен подписки остался в ошибке: %q", got)
	}
	// Хост оставляем намеренно: без имени панели «не отвечает» не отличить от
	// «интернета нет».
	if !strings.Contains(got, "panel.example.com") {
		t.Errorf("хост потерян, ошибка перестала помогать: %q", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("причина потеряна: %q", got)
	}
}

func TestWithoutLinkDropsWholeLink(t *testing.T) {
	link := "marvia://" + testKey + "@panel.example.com/sub/" + testToken

	err := withoutLink(&url.Error{
		Op:  "parse",
		URL: link,
		Err: errors.New("invalid port \":oops\" after host"),
	})

	got := err.Error()
	if strings.Contains(got, testKey) {
		t.Fatalf("приватный ключ остался в ошибке: %q", got)
	}
	if strings.Contains(got, testToken) {
		t.Fatalf("токен подписки остался в ошибке: %q", got)
	}
	if !strings.Contains(got, "invalid port") {
		t.Errorf("причина потеряна: %q", got)
	}
}

// Ошибки не про адреса проходят как есть: подменять их пустышкой значило бы
// лечить не ту болезнь и заодно прятать полезное.
func TestRedactionLeavesOtherErrorsAlone(t *testing.T) {
	plain := errors.New("нода не отвечает")

	if got := withoutSecret(plain); got != plain {
		t.Errorf("withoutSecret подменил обычную ошибку: %v", got)
	}
	if got := withoutLink(plain); got != plain {
		t.Errorf("withoutLink подменил обычную ошибку: %v", got)
	}
}

// ParseAccountLink — главный вход для чужой строки, и именно её человек
// вставляет руками, а значит и опечатывается в ней чаще всего.
func TestParseAccountLinkKeepsSecretsOutOfErrors(t *testing.T) {
	bad := []string{
		// Неверная процентная последовательность и пробел в имени —
		// то, что получается при копировании ссылки из мессенджера.
		"marvia://" + testKey + "@panel.example.com/sub/%zz" + testToken,
		"marvia://" + testKey + "@panel example.com/sub/" + testToken,
	}

	for _, link := range bad {
		_, err := ParseAccountLink(link)
		if err == nil {
			t.Errorf("битая ссылка принята: %q", link)
			continue
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("приватный ключ в ошибке разбора: %q", err)
		}
		if strings.Contains(err.Error(), testToken) {
			t.Errorf("токен подписки в ошибке разбора: %q", err)
		}
	}
}
