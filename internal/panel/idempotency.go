package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Повторное уведомление от платёжной системы — не редкость, а норма.
//
// ЮKassa шлёт уведомление заново, пока не получит ответ; Telegram Stars тоже;
// у Marzban в теле вебхука есть счётчик попыток — это прямое признание, что
// доставка ровно-один-раз никем не обещана. Значит бот продавца рано или
// поздно позовёт нас дважды за одну оплату.
//
// Для создания подписчика защита уже есть: повтор с тем же external_id
// возвращает того же и ничего не создаёт. А вот продление такой защиты не
// имело, и повтор добавлял не тридцать дней, а шестьдесят. Ошибка тихая:
// покупатель доволен, продавец недосчитается денег через месяц и не поймёт,
// почему.
//
// Лечится ключом идемпотентности. Бот кладёт в заголовок идентификатор
// платежа, панель запоминает ответ и на повтор отдаёт тот же, ничего не делая.

const (
	// idempotencyHeader — заголовок с ключом.
	idempotencyHeader = "Idempotency-Key"

	// idempotencyTTL — сколько помним ответ.
	//
	// Платёжные системы повторяют уведомления часами, редко дольше суток.
	// Неделя берётся с запасом и стоит копейки: строка на платёж.
	idempotencyTTL = 7 * 24 * time.Hour

	// maxIdempotencyKey — разумная граница длины.
	maxIdempotencyKey = 200
)

// ErrIdempotencyScope означает, что ключ уже использован для другой операции.
//
// Это не повтор, а ошибка бота: один и тот же идентификатор платежа отправлен
// и на продажу, и на продление. Молча выполнить — значит сделать не то, что
// просили; молча вернуть чужой ответ — тем более.
var ErrIdempotencyScope = errors.New("этот ключ идемпотентности уже использован для другой операции")

// RememberedResponse ищет сохранённый ответ по ключу.
//
// found = false означает, что операцию надо выполнить впервые.
func (s *Store) RememberedResponse(ctx context.Context, key, scope string) (string, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false, nil
	}

	var storedScope, response, created string
	err := s.db.QueryRowContext(ctx,
		`SELECT scope, response, created_at FROM idempotency WHERE key = ?`, key,
	).Scan(&storedScope, &response, &created)

	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	// Протухший ключ — то же самое, что отсутствующий: платёжная система так
	// долго не повторяет, а живой ключ недельной давности означает, что это
	// уже другой платёж.
	if at, parseErr := time.Parse(time.RFC3339Nano, created); parseErr == nil && time.Since(at) > idempotencyTTL {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM idempotency WHERE key = ?`, key)
		return "", false, nil
	}

	if storedScope != scope {
		return "", false, fmt.Errorf("%w: раньше это была %q", ErrIdempotencyScope, storedScope)
	}
	return response, true, nil
}

// RememberResponse запоминает ответ операции под ключом.
//
// Гонку двух одновременных повторов разрешает сама база: ключ — первичный
// ключ таблицы, второй вставке достанется конфликт, и мы его проглотим —
// сохранённый ответ там уже правильный.
func (s *Store) RememberResponse(ctx context.Context, key, scope, response string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO idempotency (key, scope, response, created_at) VALUES (?, ?, ?, ?)`,
		key, scope, response, format(time.Now().UTC()),
	)
	return err
}

// ForgetStaleIdempotency чистит старые ключи.
//
// Зовётся при запуске: без уборки таблица растёт со скоростью продаж и живёт
// вечно, хотя нужна была сутки.
func (s *Store) ForgetStaleIdempotency(ctx context.Context) error {
	cutoff := format(time.Now().UTC().Add(-idempotencyTTL))
	_, err := s.db.ExecContext(ctx, `DELETE FROM idempotency WHERE created_at < ?`, cutoff)
	return err
}
