package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/veilproject/veil/internal/vp1"
)

// AddCredential выпускает подписчику ещё один ключ — под новое устройство.
//
// Отдельный ключ на устройство даёт три вещи, которых нет у схемы «один
// конфиг на человека»: видно, сколько устройств реально пользуется; любое
// отключается по отдельности; потерянный телефон не требует менять доступ
// на остальных. Квота при этом общая — ключи связаны одним аккаунтом.
//
// Приватная часть возвращается один раз и в базу не попадает.
func (s *Store) AddCredential(ctx context.Context, userID int64, label string) (Credential, string, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, "", ErrNotFound
	}
	if err != nil {
		return Credential{}, "", err
	}

	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		return Credential{}, "", fmt.Errorf("генерация ключа: %w", err)
	}

	now := time.Now().UTC()
	pub := vp1.EncodeKey(pair.Public)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials (user_id, kind, secret, label, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, CredVP1, pub, label, format(now))
	if err != nil {
		return Credential{}, "", fmt.Errorf("создание ключа: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Credential{}, "", err
	}

	cred := Credential{ID: id, Kind: CredVP1, Secret: pub, Label: label, CreatedAt: now}
	return cred, vp1.EncodeKey(pair.Private), nil
}

// DeleteCredential отзывает один ключ.
//
// Отзыв мгновенный настолько, насколько быстро ноды перечитают список: до
// нескольких секунд. Расход аккаунта при этом сохраняется — статистика
// привязана к аккаунту, а не к ключу.
func (s *Store) DeleteCredential(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление ключа: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
