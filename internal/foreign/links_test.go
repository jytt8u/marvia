package foreign

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"os/exec"
	"strings"
	"testing"
)

// TestLinksFromOtherAppsAreUnderstood: ссылки в том виде, в каком их выдают
// 3x-ui, Marzban, Hiddify и v2rayN, разбираются в ноду с адресом, именем и
// нужной маскировкой.
func TestLinksFromOtherAppsAreUnderstood(t *testing.T) {
	cases := []struct {
		link, protocol, host, name string
		port                       int
	}{
		{"vless://b831381d-6324-4d53-ad4f-8cda48b30811@nl.example.com:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&flow=xtls-rprx-vision#%F0%9F%87%B3%F0%9F%87%B1%20Amsterdam", "vless", "nl.example.com", "🇳🇱 Amsterdam", 443},
		{"vless://id@[2001:db8::1]:8443?type=grpc&serviceName=api&security=tls&sni=cdn.example.com", "vless", "2001:db8::1", "", 8443},
		{"vless://id@x.example.com:443?type=xhttp&path=%2Fx&mode=auto&security=tls", "vless", "x.example.com", "", 443},
		{"trojan://pass@t.example.com:443?type=ws&path=%2Fws&host=cdn.example.com#Trojan", "trojan", "t.example.com", "Trojan", 443},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:a2V5a2V5a2V5a2V5a2V5aw==")) + "@s.example.com:8388#SS", "shadowsocks", "s.example.com", "SS", 8388},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw@legacy.example.com:1234")) + "#Old", "shadowsocks", "legacy.example.com", "Old", 1234},
		{"hysteria2://secret@h.example.com:443?sni=h.example.com&obfs=salamander&obfs-password=x#Hy2", "hysteria2", "h.example.com", "Hy2", 443},
		{"hy2://user:pass@h.example.com:8443/", "hysteria2", "h.example.com", "", 8443},
		{"wireguard://cHJpdmF0ZWtleQ%3D%3D@wg.example.com:51820?publickey=cHVibGlja2V5&address=172.16.0.2/32,fd00::2/128&mtu=1280&reserved=1,2,3#WG", "wireguard", "wg.example.com", "WG", 51820},
	}
	for _, c := range cases {
		l, err := Parse(c.link)
		if err != nil {
			t.Errorf("%s: %v", c.link, err)
			continue
		}
		if l.Protocol != c.protocol || l.Host != c.host || l.Port != c.port || l.Name != c.name {
			t.Errorf("%s: получено %s %s:%d «%s»", c.link, l.Protocol, l.Host, l.Port, l.Name)
		}
		if l.Outbound["protocol"] == nil {
			t.Errorf("%s: нет настроек исходящего", c.link)
		}
	}
}

// TestRealityKeepsItsKeys: у REALITY без публичного ключа не подключиться, и
// ключ, короткий id и имя прикрытия доходят до настроек как есть.
func TestRealityKeepsItsKeys(t *testing.T) {
	l, err := Parse("vless://id@h:443?security=reality&sni=www.microsoft.com&pbk=KEY&sid=ab&spx=%2F&fp=firefox")
	if err != nil {
		t.Fatal(err)
	}
	r := l.Outbound["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	if r["publicKey"] != "KEY" || r["shortId"] != "ab" || r["serverName"] != "www.microsoft.com" || r["fingerprint"] != "firefox" {
		t.Fatalf("настройки reality: %v", r)
	}
	if _, err := Parse("vless://id@h:443?security=reality&sni=x"); err == nil {
		t.Fatal("reality без ключа принят")
	}
}

// TestInsecureIsNotHonoured: «не проверять сертификат» из ссылки не
// выполняется — вместо этого нужна привязка к отпечатку.
func TestInsecureIsNotHonoured(t *testing.T) {
	l, _ := Parse("trojan://p@h:443?allowInsecure=1&pcs=AABB")
	tls := l.Outbound["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
	if _, ok := tls["allowInsecure"]; ok {
		t.Fatal("проверка сертификата выключена по ссылке")
	}
	if tls["pinnedPeerCertSha256"] != "AABB" {
		t.Fatal("отпечаток сертификата потерян")
	}
}

// TestWhatCannotWorkIsRefusedNotGuessed: то, что клиент исполнить не может,
// отвергается с причиной, а не превращается в неработающую ноду.
func TestWhatCannotWorkIsRefusedNotGuessed(t *testing.T) {
	for _, link := range []string{
		"ss://YWVzLTI1Ni1nY206cHc@h:1?plugin=obfs-local%3Bobfs%3Dhttp",
		"tuic://u:p@h:443",
		"vless://@h:443",
		"vless://id@h",
		"hysteria2://a@h:443?obfs=gfw",
		"vless://id@h:443?type=kcp",
		"https://example.com/sub",
	} {
		if _, err := Parse(link); err == nil {
			t.Errorf("%s принята", link)
		}
	}
	if _, err := Parse("tuic://u:p@h:443"); !errors.Is(err, ErrUnsupported) {
		t.Error("незнакомая схема не названа незнакомой")
	}
}

// TestSubscriptionBodyInAnyForm: подписка разбирается и в base64, и
// текстом; непонятные строки считаются, а не роняют весь список.
func TestSubscriptionBodyInAnyForm(t *testing.T) {
	plain := "vless://id@a:443#A\ntrojan://p@b:443#B\ntuic://x@c:1\n\n# комментарий\n"
	for _, body := range []string{plain, base64.StdEncoding.EncodeToString([]byte(plain)), base64.RawURLEncoding.EncodeToString([]byte(plain))} {
		sub := ParseList([]byte(body))
		if len(sub.Links) != 2 || sub.Skipped != 1 {
			t.Errorf("нод %d, пропущено %d", len(sub.Links), sub.Skipped)
		}
	}
	var s Subscription
	s.ParseUserinfo("upload=100; download=900; total=10000; expire=1893456000")
	if s.Remaining() != 9000 || s.Expire.Unix() != 1893456000 {
		t.Fatalf("остаток %d, срок %v", s.Remaining(), s.Expire)
	}
	if (Subscription{}).Remaining() != -1 {
		t.Fatal("подписка без предела показана с нулём")
	}
}

// TestEveryParsedLinkStartsTheEngine: разобранная ссылка любого вида
// собирается в настройки, которые Xray принимает. Иначе ошибка всплыла бы
// только у человека на телефоне, при нажатии «подключить».
func TestEveryParsedLinkStartsTheEngine(t *testing.T) {
	for _, link := range []string{
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?security=reality&sni=www.microsoft.com&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2&flow=xtls-rprx-vision",
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?type=grpc&serviceName=api&security=tls&sni=x",
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?type=xhttp&path=%2Fx&security=tls&sni=x",
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?type=httpupgrade&path=%2Fu",
		"hysteria2://secret@127.0.0.1:443?sni=h.example.com&obfs=salamander&obfs-password=x",
		"wireguard://" + "yAnz5TF%2BlXXJte14tji3zlMNq%2BhN2f8iw7xZVmXzn2E%3D" + "@127.0.0.1:51820?publickey=HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw%3D&address=172.16.0.2/32",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:a2V5a2V5a2V5a2V5a2V5aw==")) + "@127.0.0.1:8388",
	} {
		l, err := Parse(link)
		if err != nil {
			t.Errorf("%s: разбор: %v", link, err)
			continue
		}
		e, err := Start(l)
		if err != nil {
			t.Errorf("%s: %v", l.Protocol, err)
			continue
		}
		_ = e.Close()
	}
}

// TestPinnedNodeKeepsItsNameWhereItIsChecked: закреплённая за адресом нода
// звонит по адресу, но имя остаётся в SNI и в Host — за CDN без него не
// пустят. Номер ноды от закрепления не меняется: выбор руками не теряется.
func TestPinnedNodeKeepsItsNameWhereItIsChecked(t *testing.T) {
	l, _ := Parse("vless://id@cdn.example.com:443?type=ws&path=%2Fw&security=tls#W")
	p := Pin(l, netip.MustParseAddr("203.0.113.7"))
	vnext := p.Outbound["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
	stream := p.Outbound["streamSettings"].(map[string]any)
	if vnext["address"] != "203.0.113.7" {
		t.Fatalf("адрес не закреплён: %v", vnext["address"])
	}
	if stream["wsSettings"].(map[string]any)["host"] != "cdn.example.com" {
		t.Fatal("Host потерял имя ноды")
	}
	if stream["tlsSettings"].(map[string]any)["serverName"] != "cdn.example.com" {
		t.Fatal("SNI потерял имя ноды")
	}
	if NodeID(p) != NodeID(l) {
		t.Fatal("номер ноды сменился от закрепления")
	}
	if l.Outbound["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["address"] != "cdn.example.com" {
		t.Fatal("закрепление испортило исходную ноду")
	}

	h, _ := Parse("hy2://a@h.example.com:443?sni=h.example.com")
	hp := Pin(h, netip.MustParseAddr("198.51.100.1"))
	if hp.Outbound["settings"].(map[string]any)["address"] != "198.51.100.1" {
		t.Fatal("hysteria2 не закреплена")
	}
	if _, err := Start(hp); err != nil {
		t.Fatalf("закреплённая нода не запускается: %v", err)
	}
}

// TestEngineStartsWithoutTestImports: движок запускается в сборке, где нет
// ничего из тестов. Тесты пакета подключают серверную часть Xray для своих
// серверов, и она заодно регистрирует то, без чего Xray не создаётся, — так
// однажды всё зелёное здесь не запустилось на телефоне. Эта проверка
// собирает отдельную программу только с тем, что уходит в приложение.
func TestEngineStartsWithoutTestImports(t *testing.T) {
	if testing.Short() {
		t.Skip("собирает отдельную программу")
	}
	link := "vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:443?type=ws&path=%2Fv&security=none"
	out, err := exec.Command("go", "run", "./testdata/bare", link).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("движок не запустился без тестовых импортов: %v\n%s", err, out)
	}
}

// TestKeysSurviveTheirUsualSpelling: панели пишут ссылки по-разному, и
// разбор обязан понять обычные варианты — иначе нода молча не проходит
// замер, а человек не понимает, что не так со ссылкой.
func TestKeysSurviveTheirUsualSpelling(t *testing.T) {
	// base64 в userinfo с «=» в хвосте, закодированным как %3D.
	userinfo := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:пароль1"))
	ss, err := Parse("ss://" + strings.ReplaceAll(userinfo, "=", "%3D") + "@s.example.com:8388#SS")
	if err != nil {
		t.Fatalf("ss с %%3D: %v", err)
	}
	server := ss.Outbound["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)
	if server["password"] != "пароль1" || server["method"] != "chacha20-ietf-poly1305" {
		t.Fatalf("ss разобран как %v", server)
	}

	// Ключ WireGuard с «+», записанным как есть.
	wg, err := Parse("wireguard://cHJpdmF0ZWtleQ%3D%3D@wg.example.com:51820?publickey=ab+cd/ef==&address=172.16.0.2/32")
	if err != nil {
		t.Fatalf("wireguard с «+»: %v", err)
	}
	peer := wg.Outbound["settings"].(map[string]any)["peers"].([]any)[0].(map[string]any)
	if peer["publicKey"] != "ab+cd/ef==" {
		t.Fatalf("ключ пира %q", peer["publicKey"])
	}
}
