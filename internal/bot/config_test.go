package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Настройки проверяются на запуске, а не при первой продаже.
//
// Бот, поднятый с опечаткой в тарифе, узнает о ней от покупателя, а покупатель
// — от продавца, которому напишет. Поэтому здесь строго и вслух.

func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "veil-bot.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("файл настроек: %v", err)
	}
	return path
}

const goodConfig = `{
  "telegram_token": "123:abc",
  "panel": "https://panel.example.com",
  "panel_key": "vk_test",
  "payment": "stars",
  "tariffs": [{"id": "m1", "title": "1 месяц", "days": 30, "price": 150}]
}`

func TestConfigLoads(t *testing.T) {
	cfg, err := LoadConfig(write(t, goodConfig))
	if err != nil {
		t.Fatalf("настройки не прочитались: %v", err)
	}
	if len(cfg.Tariffs) != 1 || cfg.Tariffs[0].Days != 30 {
		t.Fatalf("тариф разобрался не так: %+v", cfg.Tariffs)
	}
	if cfg.Tariffs[0].currency() != "₽" {
		t.Errorf("валюта по умолчанию не рубль: %q", cfg.Tariffs[0].currency())
	}
}

func TestConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "нет тарифов",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k","tariffs":[]}`,
			want: "продавать нечего",
		},
		{
			name: "двоеточие в id",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k",
			        "tariffs":[{"id":"m:1","title":"т","days":30,"price":1}]}`,
			want: "двоеточие",
		},
		{
			name: "тариф без срока",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k",
			        "tariffs":[{"id":"m1","title":"т","days":0,"price":1}]}`,
			want: "больше нуля",
		},
		{
			name: "одинаковые id",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k",
			        "tariffs":[{"id":"m1","title":"а","days":30,"price":1},
			                   {"id":"m1","title":"б","days":60,"price":2}]}`,
			want: "дважды",
		},
		{
			name: "адрес панели без схемы",
			body: `{"telegram_token":"1","panel":"panel.example.com","panel_key":"k",
			        "tariffs":[{"id":"m1","title":"т","days":30,"price":1}]}`,
			want: "адрес панели",
		},
		{
			name: "оплата вручную без продавца",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k","payment":"manual",
			        "tariffs":[{"id":"m1","title":"т","days":30,"price":1}]}`,
			want: "подтверждать платежи",
		},
		{
			name: "неизвестное поле",
			body: `{"telegram_token":"1","panel":"https://p","panel_key":"k","тарифы":[]}`,
			want: "unknown field",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Переменные окружения не должны спасать заведомо плохой файл.
			t.Setenv("VEIL_BOT_TOKEN", "")
			t.Setenv("VEIL_PANEL_KEY", "")

			_, err := LoadConfig(write(t, c.body))
			if err == nil {
				t.Fatal("плохие настройки приняты молча")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("в ошибке не сказано о причине %q: %v", c.want, err)
			}
		})
	}
}

// TestSecretsFromEnv — токены можно не держать в файле.
//
// Файл настроек продавец редактирует, копирует и показывает; секреты в нём
// утекают тем же путём, что и он сам.
func TestSecretsFromEnv(t *testing.T) {
	t.Setenv("VEIL_BOT_TOKEN", "123:из-окружения")
	t.Setenv("VEIL_PANEL_KEY", "vk_из-окружения")

	cfg, err := LoadConfig(write(t, `{
	  "panel": "https://panel.example.com",
	  "tariffs": [{"id":"m1","title":"1 месяц","days":30,"price":150}]
	}`))
	if err != nil {
		t.Fatalf("настройки без секретов не прочитались: %v", err)
	}
	if cfg.Token != "123:из-окружения" || cfg.Key != "vk_из-окружения" {
		t.Fatalf("секреты не подхватились из окружения: %+v", cfg)
	}
}

// TestExampleConfigIsValid — образец из -example работает как есть.
//
// Кроме мест, которые продавец обязан заполнить сам: без них он бы и не понял,
// что именно от него нужно.
func TestExampleConfigIsValid(t *testing.T) {
	body := strings.ReplaceAll(ExampleConfig,
		"укажи токен от @BotFather или задай VEIL_BOT_TOKEN", "123:abc")

	t.Setenv("VEIL_BOT_TOKEN", "")
	t.Setenv("VEIL_PANEL_KEY", "")

	cfg, err := LoadConfig(write(t, body))
	if err != nil {
		t.Fatalf("образец настроек не проходит собственную проверку: %v", err)
	}
	if len(cfg.Tariffs) < 2 {
		t.Errorf("в образце меньше двух тарифов: продавцу нечего сравнивать")
	}
}
