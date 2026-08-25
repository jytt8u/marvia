// Package client — общая логика клиентской стороны: разбор ссылки аккаунта,
// получение списка нод и дозвон до цели через туннель.
//
// Пакет намеренно не знает ни про SOCKS, ни про TUN, ни про Android. Всё это
// разные оболочки вокруг одного и того же: настольный клиент подставляет
// SOCKS, телефон — сетевой интерфейс, а внутри работает один код. Иначе
// пришлось бы чинить каждую ошибку по три раза.
package client

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/veilproject/veil/internal/vp1"
)

// AccountScheme — схема ссылки, которую бот отправляет покупателю.
const AccountScheme = "veil-account"

// Account — то, что покупатель импортирует один раз.
//
// Разделение неслучайное: ключ выдаётся один раз и живёт долго, а список нод
// меняется постоянно и подтягивается по адресу подписки. Если бы ноды были
// прописаны в самой ссылке, каждая блокировка требовала бы выдавать людям
// новый конфиг.
type Account struct {
	// PrivateKey — личный ключ покупателя, 32 байта.
	PrivateKey []byte

	// SubscriptionURL — откуда брать список нод.
	SubscriptionURL string

	// PanelIPs — адреса панели, вписанные прямо в ссылку параметром ip.
	//
	// Нужны, чтобы не спрашивать имя домена подписки у резолвера провайдера.
	// С августа 2026 это перестало быть безобидным: после блокировки DoH у
	// Google и DoT у Cloudflare запросы имён откатились на открытый резолвер
	// провайдера. Домен, к которому ходят тысячи покупателей одного продавца,
	// стал виден как на ладони — а такой домен признают средством обхода и
	// блокируют целиком.
	//
	// Имя при этом никуда не девается: оно уходит в SNI, и по нему проверяется
	// сертификат. Меняется только то, откуда мы узнали адрес.
	PanelIPs []netip.Addr

	// Label — человекочитаемая пометка из ссылки. На работу не влияет.
	Label string
}

// ParseAccountLink разбирает ссылку вида
// veil-account://<приватный ключ>@<хост>/sub/<токен>#<метка>
func ParseAccountLink(link string) (Account, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return Account{}, errors.New("пустая ссылка")
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return Account{}, fmt.Errorf("ссылка не разбирается: %w", err)
	}
	if parsed.Scheme != AccountScheme {
		return Account{}, fmt.Errorf("ожидалась схема %s://, получено %q", AccountScheme, parsed.Scheme)
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return Account{}, errors.New("в ссылке нет приватного ключа")
	}
	if parsed.Host == "" {
		return Account{}, errors.New("в ссылке нет адреса панели")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return Account{}, errors.New("в ссылке нет пути подписки")
	}

	key, err := vp1.DecodeKey(parsed.User.Username())
	if err != nil {
		return Account{}, fmt.Errorf("приватный ключ: %w", err)
	}

	// Схему подписки берём https всегда: по этому адресу ходит токен, и
	// отдавать его открытым текстом нельзя даже ради удобства отладки.
	sub := (&url.URL{Scheme: "https", Host: parsed.Host, Path: parsed.Path}).String()

	ips, err := parsePanelIPs(parsed.Query().Get("ip"))
	if err != nil {
		return Account{}, err
	}

	label, _ := url.PathUnescape(parsed.Fragment)

	return Account{PrivateKey: key, SubscriptionURL: sub, PanelIPs: ips, Label: label}, nil
}

// parsePanelIPs разбирает список адресов панели, перечисленных через запятую.
//
// Несколько адресов не для красоты: у панели за CDN их всегда несколько, и
// первый может не отвечать. Порядок сохраняем — продавец ставит первым тот,
// что ближе к его покупателям.
func parsePanelIPs(raw string) ([]netip.Addr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	out := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("адрес панели %q в ссылке: нужен IP, а не имя", part)
		}
		out = append(out, addr)
	}
	return out, nil
}
