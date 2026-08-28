package panel_test

import (
	"strings"
	"testing"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/panel"
)

// TestAccountSchemeMatchesClient — панель и клиент называют схему одинаково.
//
// Панель держит схему литералом, чтобы не тащить клиентский транспорт в свой
// бинарник. Разъедутся — покупатель получит ссылку, которую его же приложение
// откажется понимать, и заметит это первым он, а не мы.
func TestAccountSchemeMatchesClient(t *testing.T) {
	link := panel.AccountLink("https://panel.example.com", "ключ", "токен", "метка", nil)

	if !strings.HasPrefix(link, client.AccountScheme+"://") {
		t.Fatalf("панель выдала ссылку не той схемы: %s", link)
	}
}

// TestOldSchemeStillWorks — ключи, выданные до переименования, не отвалились.
//
// Ссылка живёт в переписке покупателя и в памяти его телефона, а не в нашей
// базе. Перестать понимать старую схему — значит в один день выключить доступ
// всем, кто получил ключ раньше, молча и без возможности что-то поправить на
// их стороне.
func TestOldSchemeStillWorks(t *testing.T) {
	const key = "6Ay1r4cCsDNrDnpFaBnuEcYQNGqQ_KJDdCJHqJdrRVc"

	for _, link := range []string{
		client.AccountScheme + "://" + key + "@panel.example.com/sub/tok#телефон",
		client.LegacyAccountScheme + "://" + key + "@panel.example.com/sub/tok#телефон",
	} {
		account, err := client.ParseAccountLink(link)
		if err != nil {
			t.Fatalf("ссылка не разобралась: %s\n%v", link, err)
		}
		if account.SubscriptionURL != "https://panel.example.com/sub/tok" {
			t.Errorf("адрес подписки разобрался не так: %s", account.SubscriptionURL)
		}
	}
}
