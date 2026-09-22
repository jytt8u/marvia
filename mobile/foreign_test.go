package mobile

import "testing"

// TestAnyKeyIsAcceptedButNonsenseIsNot: «+» принимает ключ Marvia, ссылку
// чужой ноды любого вида и адрес чужой подписки, а мусор и незнакомые схемы —
// нет, с причиной.
func TestAnyKeyIsAcceptedButNonsenseIsNot(t *testing.T) {
	for _, ok := range []string{
		"vless://b831381d-6324-4d53-ad4f-8cda48b30811@nl.example.com:443?security=reality&pbk=K&sni=x#NL",
		"trojan://p@t.example.com:443",
		"hy2://a@h.example.com:443",
		"https://panel.example.com/sub/abcdef",
	} {
		if err := CheckAccountLink(ok); err != nil {
			t.Errorf("%s отвергнута: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"tuic://u:p@h:443",
		"vless://@h:443",
		"просто текст",
		"marvia://испорчено",
	} {
		if err := CheckAccountLink(bad); err == nil {
			t.Errorf("%s принята", bad)
		}
	}
}

// TestMarviaKeysStayOnTheirOwnPath: ключ Marvia не уходит в чужой путь —
// у него свой протокол, своя панель и свой российский список.
func TestMarviaKeysStayOnTheirOwnPath(t *testing.T) {
	if isForeign("marvia://key@panel.example.com/sub/t") || isForeign("veil-account://key@panel/sub/t") {
		t.Fatal("ключ Marvia принят за чужой")
	}
	if !isForeign("https://panel.example.com/sub/t") || !isForeign("ss://abc@h:1") {
		t.Fatal("чужая ссылка принята за ключ Marvia")
	}
}
