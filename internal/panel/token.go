package panel

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenLen — длина токена в байтах. 32 байта случайности перебрать нельзя.
const tokenLen = 32

// NewToken выпускает случайный токен.
//
// Токены нужны трём сущностям: администратору (полный доступ к API), боту
// продавца (тот же API) и каждой ноде (только свой список пользователей и
// отправка статистики).
func NewToken() (string, error) {
	raw := make([]byte, tokenLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("генерация токена: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashToken считает хеш токена для хранения.
//
// В базе лежит только хеш. Слитая база панели не даёт доступа к нодам:
// восстановить из хеша сам токен нельзя.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokensEqual сравнивает токены за постоянное время.
//
// Обычное сравнение строк выходит из цикла на первом несовпавшем байте, и по
// времени ответа токен подбирается посимвольно. На локальной сети это
// измеримо.
func TokensEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
