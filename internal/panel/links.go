package panel

import (
	"net/url"
	"strings"
)

// Ссылки, которые бот отправляет покупателю.
//
// Форматы vless:// и trojan:// придуманы не нами — их понимают v2rayNG,
// Hiddify, NekoBox и почти всё остальное, что уже стоит у людей. Именно
// поэтому мы их и выдаём: покупателю не нужно ничего переставлять.
//
// Ссылка marvia:// наша: в ней личный ключ и адрес, откуда брать список
// нод. Её понимает только наш клиент.

// nodeQuery собирает общие для чужих протоколов параметры.
//
// fp=chrome просит клиент подделать отпечаток браузера — без этого он
// представится своим, а такой отпечаток DPI отличает.
//
// Дальше развилка по виду маскировки ноды.
//
// Под REALITY нода не предъявляет своего сертификата: клиенту нужны публичный
// ключ ноды (pbk) и короткий идентификатор (sid), а sni указывает на чужой
// сайт прикрытия. Своего домена у ноды при этом нет вовсе.
//
// Без REALITY это обычный TLS: sni — домен самой ноды, и сертификат должен
// быть на него выписан.
func nodeQuery(n Node) url.Values {
	q := url.Values{}
	q.Set("fp", "chrome")

	if n.WSPath != "" {
		// Нода за CDN. Адрес в ссылке ведёт на CDN, а не на саму ноду —
		// её настоящий адрес в конфиг не попадает вовсе, и ковровая
		// блокировка диапазонов хостингов такую ноду не задевает.
		//
		// REALITY здесь невозможен: CDN расшифровывает TLS у себя.
		q.Set("type", "ws")
		q.Set("path", n.WSPath)
		q.Set("security", "tls")
		if n.SNI != "" {
			q.Set("sni", n.SNI)
			q.Set("host", n.SNI)
		}
		return q
	}

	q.Set("type", "tcp")

	if n.RealityPublicKey != "" {
		q.Set("security", "reality")
		q.Set("pbk", n.RealityPublicKey)
		q.Set("sid", n.RealityShortID)
		if n.SNI != "" {
			q.Set("sni", n.SNI)
		}
		return q
	}

	q.Set("security", "tls")
	if n.SNI != "" {
		q.Set("sni", n.SNI)
		q.Set("host", n.SNI)
	}
	return q
}

// VLESSLink собирает ссылку для чужого клиента по VLESS.
func VLESSLink(n Node, uuid, name string) string {
	q := nodeQuery(n)
	// VLESS не шифрует сам: шифрование целиком на внешнем TLS. Параметр
	// обязателен, клиенты без него ссылку не принимают.
	q.Set("encryption", "none")

	return (&url.URL{
		Scheme:   "vless",
		User:     url.User(uuid),
		Host:     n.Address,
		RawQuery: q.Encode(),
		Fragment: name,
	}).String()
}

// TrojanLink собирает ссылку для чужого клиента по Trojan.
func TrojanLink(n Node, password, name string) string {
	return (&url.URL{
		Scheme:   "trojan",
		User:     url.User(password),
		Host:     n.Address,
		RawQuery: nodeQuery(n).Encode(),
		Fragment: name,
	}).String()
}

// VeilNodeLink собирает ссылку на ноду для нашего клиента.
func VeilNodeLink(n Node) string {
	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	q.Set("fp", "chrome")

	return (&url.URL{
		Scheme:   "veil",
		User:     url.User(n.PublicKey),
		Host:     n.Address,
		RawQuery: q.Encode(),
		Fragment: NodeTitle(n),
	}).String()
}

// NodeTitle — как нода подписана в клиенте.
//
// Страна идёт первой, потому что чужие клиенты — Happ, Hiddify, v2rayNG —
// подбирают флажок по тексту названия, передать его отдельно им нельзя. Имя
// сервера от хостера покупателю не говорит ничего, а «Нидерланды» говорит.
// Пустая страна ничего не портит: остаётся одно имя, как было.
func NodeTitle(n Node) string {
	switch {
	case n.Country == "":
		return n.Name
	case n.Name == "":
		return n.Country
	default:
		return n.Country + " · " + n.Name
	}
}

// StockLinks собирает ссылки для приложений, которые уже стоят у покупателя.
//
// Наши ссылки сюда намеренно не попадают: чужие клиенты не знают схемы veil://
// и на незнакомой строке в подписке некоторые из них спотыкаются целиком.
// Смешивать форматы в одном списке — верный способ сломать подписку тем, ради
// кого мы всё это и делаем.
func StockLinks(nodes []Node, creds []Credential, label string) []string {
	links := make([]string, 0, len(nodes)*len(creds))

	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		name := NodeTitle(n)
		if label != "" {
			name += " · " + label
		}

		for _, c := range creds {
			switch c.Kind {
			case CredVLESS:
				links = append(links, VLESSLink(n, c.Secret, name))
			case CredTrojan:
				links = append(links, TrojanLink(n, c.Secret, name))
			}
		}
	}
	return links
}

// AccountLink — то, что бот отправляет покупателю для нашего клиента.
//
// В ссылке личный ключ и адрес подписки: клиент импортирует её один раз, а
// список нод потом обновляет сам. Ноды меняются часто, ключ — почти никогда.
// ips — адреса самой панели. Когда они заданы, клиент идёт прямо по ним и не
// спрашивает имя домена подписки у резолвера провайдера. После блокировки DoH
// в августе 2026 такой запрос виден провайдеру открытым текстом, а домен, к
// которому ходят все покупатели одного продавца, на этом и попадается.
//
// Имя при этом остаётся в ссылке и проверяется в сертификате: подсказка
// говорит, куда идти, а не кому верить.
func AccountLink(base, privateKey, subToken, label string, ips []string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(base, "/"), "https://"), "http://")
	if host == "" {
		host = "ПОДСТАВЬ-АДРЕС-ПАНЕЛИ"
	}

	link := &url.URL{
		Scheme:   accountScheme,
		User:     url.User(privateKey),
		Host:     host,
		Path:     "/sub/" + subToken,
		Fragment: label,
	}

	if len(ips) > 0 {
		link.RawQuery = "ip=" + strings.Join(ips, ",")
	}

	return link.String()
}

// accountScheme — схема ссылки доступа.
//
// Держим литералом, а не берём из internal/client: панель не должна тащить в
// свой бинарник весь клиентский транспорт ради одной строки. Значение обязано
// совпадать с client.AccountScheme, и это проверяется тестом.
const accountScheme = "marvia"
