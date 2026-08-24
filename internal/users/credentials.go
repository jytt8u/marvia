package users

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Виды учётных данных, которые понимает нода.
//
// VP1 — наш протокол: секрет клиента остаётся у клиента, в списке лежит только
// публичный ключ. У VLESS и Trojan так не получится: там секрет — это строка,
// которую клиент предъявляет целиком, и её приходится знать обеим сторонам.
// Это объективно слабее, и это цена совместимости с чужими приложениями.
const (
	KindVP1    = "vp1"
	KindVLESS  = "vless"
	KindTrojan = "trojan"
)

// identityLen — длина опознавательной части для каждого вида.
const (
	uuidLen         = 16
	trojanDigestLen = 28 // SHA-224
)

// Identity превращает секрет из списка в то, что нода сравнивает с проводом.
//
//	vp1    — публичный ключ, 32 байта
//	vless  — UUID, 16 байт
//	trojan — SHA-224 от пароля, 28 байт (клиент присылает его же в hex)
func Identity(kind, secret string) ([]byte, error) {
	switch kind {
	case KindVP1, "":
		return DecodePublicKey(secret)
	case KindVLESS:
		return ParseUUID(secret)
	case KindTrojan:
		if secret == "" {
			return nil, fmt.Errorf("пустой пароль")
		}
		return TrojanDigest(secret), nil
	default:
		return nil, fmt.Errorf("неизвестный вид учётных данных %q", kind)
	}
}

// TrojanDigest считает то, что клиент Trojan шлёт первым делом.
//
// В Trojan это SHA-224 от пароля в шестнадцатеричном виде. Хеш здесь не защита
// пароля, а просто способ получить строку фиксированной длины: зная хеш, можно
// подключиться, не зная пароля. Поэтому список пользователей ноды с записями
// Trojan — такой же секрет, как сами пароли.
func TrojanDigest(password string) []byte {
	sum := sha256.Sum224([]byte(password))
	return sum[:]
}

// TrojanDigestHex — то же самое в том виде, в каком оно едет по проводу.
func TrojanDigestHex(password string) string {
	return hex.EncodeToString(TrojanDigest(password))
}

// ParseUUID разбирает UUID в каноническом виде 8-4-4-4-12.
//
// Своя реализация вместо зависимости: нам нужно ровно одно направление и
// ровно один формат, а тащить пакет ради тридцати строк незачем.
func ParseUUID(s string) ([]byte, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(clean) != 32 {
		return nil, fmt.Errorf("UUID должен состоять из 32 шестнадцатеричных цифр, получено %d", len(clean))
	}
	raw, err := hex.DecodeString(strings.ToLower(clean))
	if err != nil {
		return nil, fmt.Errorf("UUID содержит недопустимые символы: %w", err)
	}
	return raw, nil
}

// FormatUUID приводит 16 байт к каноническому виду.
func FormatUUID(raw []byte) string {
	if len(raw) != uuidLen {
		return ""
	}
	h := hex.EncodeToString(raw)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// credentialKey — ключ, по которому реестр ищет аккаунт.
// Вид входит в ключ: одинаковые байты в разных протоколах — разные люди.
func credentialKey(kind string, identity []byte) string {
	if kind == "" {
		kind = KindVP1
	}
	return kind + "|" + string(identity)
}
