package vp1

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/flynn/noise"
	"golang.org/x/crypto/curve25519"
)

// KeyLen — длина X25519-ключа в байтах.
const KeyLen = 32

// CipherSuite — набор криптопримитивов протокола: X25519 + ChaCha20-Poly1305 + BLAKE2s.
// Ровно этот набор использует WireGuard. Своих шифров мы не изобретаем: цена ошибки
// в криптографии — полная компрометация, а выигрыша нет никакого.
var CipherSuite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

// KeyPair — статическая пара ключей узла. Публичный ключ сервера клиент знает
// заранее (он лежит в конфиге), приватный не покидает машину.
type KeyPair struct {
	Private []byte
	Public  []byte
}

// GenerateKeyPair создаёт новую статическую пару ключей.
func GenerateKeyPair() (KeyPair, error) {
	dh, err := noise.DH25519.GenerateKeypair(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("генерация ключей: %w", err)
	}
	return KeyPair{Private: dh.Private, Public: dh.Public}, nil
}

// KeyPairFromPrivate восстанавливает пару по приватному ключу.
func KeyPairFromPrivate(priv []byte) (KeyPair, error) {
	if len(priv) != KeyLen {
		return KeyPair{}, fmt.Errorf("длина приватного ключа %d байт, ожидается %d", len(priv), KeyLen)
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return KeyPair{}, fmt.Errorf("вывод публичного ключа: %w", err)
	}
	return KeyPair{Private: append([]byte(nil), priv...), Public: pub}, nil
}

func (k KeyPair) noiseKey() noise.DHKey {
	return noise.DHKey{Private: k.Private, Public: k.Public}
}

// EncodeKey кодирует ключ в base64 без паддинга (url-safe: такую строку
// безопасно класть в ссылку подписки).
func EncodeKey(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

// DecodeKey разбирает ключ из base64 и проверяет длину.
func DecodeKey(s string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Допускаем и обычный base64 с паддингом — так удобнее копипастить.
		raw, err = base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errors.New("ключ не является корректным base64")
		}
	}
	if len(raw) != KeyLen {
		return nil, fmt.Errorf("длина ключа %d байт, ожидается %d", len(raw), KeyLen)
	}
	return raw, nil
}
