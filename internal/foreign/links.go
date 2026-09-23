// Package foreign — чужие протоколы: VLESS, VMess, Trojan, Shadowsocks,
// Hysteria2, WireGuard.
//
// Зачем они клиенту Marvia. Человек, у которого есть доступ у чужого
// продавца — ключ из Happ, Hiddify, v2rayNG, — хочет держать всё в одном
// приложении, а не в трёх. И наоборот: продавец, переезжающий на Marvia с
// Xray-панели, какое-то время живёт с обоими видами ключей. Свой протокол VP1
// остаётся своим; чужие ссылки клиент понимает так же, как понимают их
// приложения, откуда эти ссылки пришли.
//
// Сами протоколы не пишем: их исполняет Xray-core, тот же, что стоит на
// другой стороне у продавцов. Своих криптопримитивов в проекте нет, и
// переписывать VMess или Shadowsocks-2022 заново значило бы завести их
// десяток — с ошибками, которых у Xray уже нет. Лицензия Xray — MPL-2.0, как у
// REALITY, который проект уже берёт у XTLS; sing-box не взят из-за GPL-3.0,
// которая распространилась бы на всё приложение.
//
// Здесь — только перевод ссылки в настройки исходящего соединения Xray.
package foreign

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Link — одна чужая нода: откуда взята, как называется, как до неё дойти.
type Link struct {
	// Name — подпись из ссылки (то, что после #). Пусто — хост.
	Name string
	// Protocol — vless, vmess, trojan, shadowsocks, hysteria2, wireguard.
	Protocol string
	Host     string
	Port     int
	// Outbound — настройки исходящего соединения Xray, готовые к JSON.
	Outbound map[string]any
	// Raw — ссылка как есть: по ней узнаётся та же нода в новой подписке.
	Raw string
}

// Title — как ноду показывать человеку.
func (l Link) Title() string {
	if l.Name != "" {
		return l.Name
	}
	return net.JoinHostPort(l.Host, strconv.Itoa(l.Port))
}

// ErrUnsupported — схема ссылки не из тех, что клиент умеет.
var ErrUnsupported = errors.New("такой вид ссылки клиент не знает")

// Schemes — какие схемы понимает Parse. Приложению нужно, чтобы отличить
// чужую ссылку от адреса подписки, не разбирая её.
var Schemes = []string{"vless", "vmess", "trojan", "ss", "hysteria2", "hy2", "wireguard", "wg"}

// IsLink говорит, похожа ли строка на ссылку одной ноды.
func IsLink(s string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(s), "://")
	if !ok {
		return false
	}
	scheme = strings.ToLower(scheme)
	for _, known := range Schemes {
		if scheme == known {
			return true
		}
	}
	return false
}

// Parse разбирает ссылку одной ноды.
func Parse(raw string) (Link, error) {
	raw = strings.TrimSpace(raw)
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return Link{}, ErrUnsupported
	}
	var (
		l   Link
		err error
	)
	switch strings.ToLower(scheme) {
	case "vless":
		l, err = parseVLESS(raw)
	case "vmess":
		l, err = parseVMess(raw)
	case "trojan":
		l, err = parseTrojan(raw)
	case "ss":
		l, err = parseShadowsocks(raw)
	case "hysteria2", "hy2":
		l, err = parseHysteria2(raw)
	case "wireguard", "wg":
		l, err = parseWireGuard(raw)
	default:
		return Link{}, fmt.Errorf("%w: %s://", ErrUnsupported, scheme)
	}
	if err != nil {
		return Link{}, err
	}
	l.Raw = raw
	return l, nil
}

// ---------------------------------------------------------------- VLESS

func parseVLESS(raw string) (Link, error) {
	u, host, port, err := hostPort(raw)
	if err != nil {
		return Link{}, err
	}
	id := u.User.Username()
	if id == "" {
		return Link{}, errors.New("в ссылке vless нет идентификатора")
	}
	q := u.Query()
	user := map[string]any{"id": id, "encryption": first(q.Get("encryption"), "none")}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}
	stream, err := streamOf(q, host, "none")
	if err != nil {
		return Link{}, err
	}
	return Link{
		Name: fragment(u), Protocol: "vless", Host: host, Port: port,
		Outbound: map[string]any{
			"protocol": "vless",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"address": host, "port": port, "users": []any{user},
			}}},
			"streamSettings": stream,
		},
	}, nil
}

// ---------------------------------------------------------------- Trojan

func parseTrojan(raw string) (Link, error) {
	u, host, port, err := hostPort(raw)
	if err != nil {
		return Link{}, err
	}
	password := u.User.Username()
	if password == "" {
		return Link{}, errors.New("в ссылке trojan нет пароля")
	}
	// Trojan без TLS не бывает: пустое security в его ссылках означает tls.
	stream, err := streamOf(u.Query(), host, "tls")
	if err != nil {
		return Link{}, err
	}
	return Link{
		Name: fragment(u), Protocol: "trojan", Host: host, Port: port,
		Outbound: map[string]any{
			"protocol": "trojan",
			"settings": map[string]any{"servers": []any{map[string]any{
				"address": host, "port": port, "password": password,
			}}},
			"streamSettings": stream,
		},
	}, nil
}

// ---------------------------------------------------------------- VMess

// parseVMess разбирает vmess://base64(JSON) — формат v2rayN, другого у VMess
// в ходу нет.
func parseVMess(raw string) (Link, error) {
	body, err := decodeBase64(strings.TrimPrefix(raw[strings.Index(raw, "://")+3:], "/"))
	if err != nil {
		return Link{}, errors.New("ссылка vmess не в base64")
	}
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return Link{}, errors.New("внутри ссылки vmess не JSON")
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	host := str("add")
	port, err := strconv.Atoi(str("port"))
	if host == "" || err != nil || port <= 0 || port > 65535 {
		return Link{}, errors.New("в ссылке vmess нет адреса или порта")
	}
	id := str("id")
	if id == "" {
		return Link{}, errors.New("в ссылке vmess нет идентификатора")
	}
	// Переводим в вид «как у vless», чтобы транспорт собирался одним кодом.
	q := url.Values{}
	q.Set("type", first(str("net"), "tcp"))
	q.Set("host", str("host"))
	q.Set("path", str("path"))
	q.Set("sni", str("sni"))
	q.Set("alpn", str("alpn"))
	q.Set("fp", str("fp"))
	q.Set("headerType", str("type"))
	if str("net") == "grpc" {
		q.Set("serviceName", str("path"))
		q.Set("mode", str("type"))
	}
	security := "none"
	if t := str("tls"); t == "tls" || t == "reality" {
		security = t
	}
	q.Set("security", security)
	stream, err := streamOf(q, host, "none")
	if err != nil {
		return Link{}, err
	}
	aid, _ := strconv.Atoi(str("aid"))
	return Link{
		Name: str("ps"), Protocol: "vmess", Host: host, Port: port,
		Outbound: map[string]any{
			"protocol": "vmess",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"address": host, "port": port,
				"users": []any{map[string]any{"id": id, "alterId": aid, "security": first(str("scy"), "auto")}},
			}}},
			"streamSettings": stream,
		},
	}, nil
}

// ---------------------------------------------------------------- Shadowsocks

// parseShadowsocks понимает оба вида: SIP002 ss://base64(метод:пароль)@хост:порт
// и старый ss://base64(метод:пароль@хост:порт). Плагины (obfs, v2ray-plugin)
// не поддерживаются — Xray их не исполняет, и молча подключиться без них
// значило бы не подключиться вовсе.
func parseShadowsocks(raw string) (Link, error) {
	rest := raw[strings.Index(raw, "://")+3:]
	name := ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		name, _ = url.PathUnescape(rest[i+1:])
		rest = rest[:i]
	}
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	rest = strings.TrimSuffix(rest, "/")
	if q, _ := url.ParseQuery(query); q.Get("plugin") != "" {
		return Link{}, errors.New("ссылки shadowsocks с плагином клиент не поддерживает")
	}

	var userinfo, hostport string
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		userinfo, hostport = rest[:at], rest[at+1:]
		// Сначала снимаем %-кодирование: часть панелей кодирует «=» в хвосте
		// base64 как %3D, и такой userinfo как base64 не разбирался вовсе.
		plain, perr := url.PathUnescape(userinfo)
		if perr != nil {
			plain = userinfo
		}
		if dec, err := decodeBase64(plain); err == nil && strings.Contains(string(dec), ":") {
			userinfo = string(dec)
		} else {
			userinfo = plain
		}
	} else {
		dec, err := decodeBase64(rest)
		if err != nil {
			return Link{}, errors.New("ссылка shadowsocks не разбирается")
		}
		at := strings.LastIndexByte(string(dec), '@')
		if at < 0 {
			return Link{}, errors.New("в ссылке shadowsocks нет адреса")
		}
		userinfo, hostport = string(dec[:at]), string(dec[at+1:])
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok || method == "" || password == "" {
		return Link{}, errors.New("в ссылке shadowsocks нет метода или пароля")
	}
	host, portText, err := net.SplitHostPort(hostport)
	if err != nil {
		return Link{}, errors.New("в ссылке shadowsocks нет порта")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return Link{}, errors.New("кривой порт в ссылке shadowsocks")
	}
	return Link{
		Name: name, Protocol: "shadowsocks", Host: host, Port: port,
		Outbound: map[string]any{
			"protocol": "shadowsocks",
			"settings": map[string]any{"servers": []any{map[string]any{
				"address": host, "port": port, "method": strings.ToLower(method), "password": password,
			}}},
		},
	}, nil
}

// ---------------------------------------------------------------- Hysteria2

func parseHysteria2(raw string) (Link, error) {
	u, host, port, err := hostPort(raw)
	if err != nil {
		return Link{}, err
	}
	auth := u.User.Username()
	if p, ok := u.User.Password(); ok {
		auth += ":" + p
	}
	q := u.Query()
	tls := map[string]any{"serverName": first(q.Get("sni"), host), "alpn": []string{"h3"}}
	pin(tls, q)
	stream := map[string]any{
		"network":          "hysteria",
		"security":         "tls",
		"tlsSettings":      tls,
		"hysteriaSettings": map[string]any{"version": 2, "auth": auth},
	}
	if obfs := q.Get("obfs"); obfs != "" {
		if obfs != "salamander" {
			return Link{}, fmt.Errorf("обфускацию %q в hysteria2 клиент не знает", obfs)
		}
		stream["finalmask"] = map[string]any{"udp": []any{map[string]any{
			"type": "salamander", "settings": map[string]any{"password": q.Get("obfs-password")},
		}}}
	}
	return Link{
		Name: fragment(u), Protocol: "hysteria2", Host: host, Port: port,
		Outbound: map[string]any{
			"protocol":       "hysteria",
			"settings":       map[string]any{"version": 2, "address": host, "port": port},
			"streamSettings": stream,
		},
	}, nil
}

// ---------------------------------------------------------------- WireGuard

// parseWireGuard — wireguard://приватный@хост:порт?publickey=…&address=…
// Так ссылки пишут Hiddify и v2rayN; своего стандарта у WireGuard нет.
func parseWireGuard(raw string) (Link, error) {
	u, host, port, err := hostPort(raw)
	if err != nil {
		return Link{}, err
	}
	secret, _ := url.PathUnescape(u.User.Username())
	q := u.Query()
	// Ключи WireGuard — base64, и «+» в них пишут как есть, не кодируя. Разбор
	// строки запроса превращает его в пробел, и нода молча не проходила
	// замер. Пробела в base64 не бывает, так что вернуть «+» безопасно.
	key := func(s string) string { return strings.ReplaceAll(s, " ", "+") }
	peer := key(first(q.Get("publickey"), q.Get("peer_public_key"), q.Get("pbk")))
	if secret == "" || peer == "" {
		return Link{}, errors.New("в ссылке wireguard нет ключей")
	}
	addresses := splitList(first(q.Get("address"), q.Get("ip")))
	if len(addresses) == 0 {
		return Link{}, errors.New("в ссылке wireguard нет адреса внутри туннеля")
	}
	p := map[string]any{"publicKey": peer, "endpoint": net.JoinHostPort(host, strconv.Itoa(port))}
	if psk := key(q.Get("presharedkey")); psk != "" {
		p["preSharedKey"] = psk
	}
	settings := map[string]any{"secretKey": secret, "address": addresses, "peers": []any{p}}
	if mtu, err := strconv.Atoi(q.Get("mtu")); err == nil && mtu > 0 {
		settings["mtu"] = mtu
	}
	if r := splitList(q.Get("reserved")); len(r) == 3 {
		var bytes []int
		for _, s := range r {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 || n > 255 {
				bytes = nil
				break
			}
			bytes = append(bytes, n)
		}
		if bytes != nil {
			settings["reserved"] = bytes
		}
	}
	return Link{
		Name: fragment(u), Protocol: "wireguard", Host: host, Port: port,
		Outbound: map[string]any{"protocol": "wireguard", "settings": settings},
	}, nil
}

// ---------------------------------------------------------------- транспорт

// streamOf собирает streamSettings из параметров ссылки в стиле v2rayN:
// type — транспорт, security — tls, reality или none.
func streamOf(q url.Values, host, defaultSecurity string) (map[string]any, error) {
	network := strings.ToLower(first(q.Get("type"), "tcp"))
	stream := map[string]any{}
	switch network {
	case "tcp", "raw":
		network = "raw"
		if q.Get("headerType") == "http" {
			return nil, errors.New("tcp с http-маскировкой клиент не поддерживает")
		}
	case "ws":
		stream["wsSettings"] = map[string]any{"path": first(q.Get("path"), "/"), "host": q.Get("host")}
	case "grpc":
		stream["grpcSettings"] = map[string]any{
			"serviceName": first(q.Get("serviceName"), q.Get("path")),
			"multiMode":   q.Get("mode") == "multi",
		}
	case "httpupgrade":
		stream["httpupgradeSettings"] = map[string]any{"path": first(q.Get("path"), "/"), "host": q.Get("host")}
	case "xhttp", "splithttp":
		network = "xhttp"
		x := map[string]any{"path": first(q.Get("path"), "/"), "host": q.Get("host")}
		if mode := q.Get("mode"); mode != "" {
			x["mode"] = mode
		}
		stream["xhttpSettings"] = x
	default:
		return nil, fmt.Errorf("транспорт %q клиент не знает", network)
	}
	stream["network"] = network

	security := strings.ToLower(first(q.Get("security"), defaultSecurity))
	sni := first(q.Get("sni"), q.Get("peer"), q.Get("host"), host)
	switch security {
	case "none", "":
		stream["security"] = "none"
	case "tls":
		t := map[string]any{"serverName": sni, "fingerprint": first(q.Get("fp"), "chrome")}
		if alpn := splitList(q.Get("alpn")); len(alpn) > 0 {
			t["alpn"] = alpn
		}
		pin(t, q)
		stream["security"] = "tls"
		stream["tlsSettings"] = t
	case "reality":
		pbk := q.Get("pbk")
		if pbk == "" {
			return nil, errors.New("в ссылке reality нет публичного ключа")
		}
		stream["security"] = "reality"
		stream["realitySettings"] = map[string]any{
			"serverName":  sni,
			"fingerprint": first(q.Get("fp"), "chrome"),
			"publicKey":   pbk,
			"shortId":     q.Get("sid"),
			"spiderX":     q.Get("spx"),
		}
	default:
		return nil, fmt.Errorf("защиту %q клиент не знает", security)
	}
	return stream, nil
}

// pin переносит привязку к сертификату из ссылки.
//
// «allowInsecure=1» — проверку сертификата выключить — не выполняется, и это
// намеренно: Xray этот ключ убрал, а подключение без проверки — приглашение
// подменить ноду кому угодно на пути. Вместо него продавцы с самоподписанным
// сертификатом дают его отпечаток: pcs у Xray, pinSHA256 у Hysteria2.
func pin(t map[string]any, q url.Values) {
	if pcs := first(q.Get("pcs"), q.Get("pinSHA256"), q.Get("pinnedPeerCertSha256")); pcs != "" {
		t["pinnedPeerCertSha256"] = pcs
	}
	if vcn := q.Get("vcn"); vcn != "" {
		t["verifyPeerCertByName"] = vcn
	}
}

// ---------------------------------------------------------------- мелочи

func hostPort(raw string) (*url.URL, string, int, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, "", 0, errors.New("ссылка не разбирается")
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port <= 0 || port > 65535 {
		return nil, "", 0, errors.New("в ссылке нет адреса или порта")
	}
	return u, host, port, nil
}

func fragment(u *url.URL) string {
	name, err := url.PathUnescape(u.Fragment)
	if err != nil {
		return u.Fragment
	}
	return strings.TrimSpace(name)
}

func first(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func truthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// decodeBase64 понимает все четыре вида, какие встречаются в ссылках: с
// дополнением и без, обычный и для адресов.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("не base64")
}
