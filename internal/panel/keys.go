package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ключи доступа к API — отдельно от админского токена.
//
// До этого токен был один на всё: им продавец входил в панель, им же ходил его
// телеграм-бот. Три беды сразу. Бот получал право удалить всех покупателей и
// увести базу с секретами чужих протоколов. Отозвать утёкший токен означало
// выкинуть из панели самого продавца. И понять, кто именно им пользовался,
// было нельзя — обращения выглядели одинаково.
//
// Теперь админский токен остаётся ключом от дома: им входят в панель и им
// выпускают ключи. Ключ выпустить ключ не может — иначе разделение ничего не
// стоило бы.

// Права ключа.
//
// Их намеренно мало. Права, которые нельзя объяснить одной строкой, продавец
// раздаёт наугад — а наугад он раздаст все.
const (
	// ScopeUsers — завести покупателя, продлить, отключить, выдать ссылки.
	// Это всё, что нужно боту, и ничего сверх.
	ScopeUsers = "users"

	// ScopeNodes — добавить ноду, удалить, посмотреть состояние.
	ScopeNodes = "nodes"

	// ScopeRead — только смотреть. Для доски с показателями или второго
	// человека, которому не надо ничего менять.
	ScopeRead = "read"
)

// keyPrefix отличает ключ API от админского токена с одного взгляда.
//
// Оба — случайные строки одинаковой длины, и в переписке с поддержкой их
// путают. Приставка стоит копейку и снимает целый класс недоразумений; заодно
// по ней ключи находят автоматические искалки секретов в чужих репозиториях.
const keyPrefix = "vk_"

// APIKey — выпущенный ключ. Секрета здесь нет: он показывается один раз.
type APIKey struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
}

// ErrKeyNotFound — ключа с таким именем или номером нет.
var ErrKeyNotFound = errors.New("ключ не найден")

// knownScopes проверяет права и убирает повторы.
func knownScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, errors.New("не заданы права: укажи хотя бы одно из users, nodes, read")
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.ToLower(strings.TrimSpace(s))
		switch s {
		case ScopeUsers, ScopeNodes, ScopeRead:
		default:
			return nil, fmt.Errorf("неизвестное право %q: бывают users, nodes, read", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, nil
}

// CreateAPIKey выпускает ключ и возвращает его секрет — единственный раз.
func (s *Store) CreateAPIKey(ctx context.Context, name string, scopes []string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, "", errors.New("у ключа должно быть имя: по нему его потом отзывают")
	}

	clean, err := knownScopes(scopes)
	if err != nil {
		return APIKey{}, "", err
	}

	raw, err := NewToken()
	if err != nil {
		return APIKey{}, "", err
	}
	secret := keyPrefix + raw

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (name, token_hash, scopes, created_at) VALUES (?, ?, ?, ?)`,
		name, HashToken(secret), strings.Join(clean, ","), format(now))
	if err != nil {
		return APIKey{}, "", fmt.Errorf("выпуск ключа: %w", err)
	}

	id, _ := res.LastInsertId()
	return APIKey{ID: id, Name: name, Scopes: clean, CreatedAt: now}, secret, nil
}

// ListAPIKeys перечисляет живые ключи.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, scopes, created_at, last_used_at
		 FROM api_keys WHERE revoked_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []APIKey{}
	for rows.Next() {
		var (
			k        APIKey
			scopes   string
			created  string
			lastUsed sql.NullString
		)
		if err := rows.Scan(&k.ID, &k.Name, &scopes, &created, &lastUsed); err != nil {
			return nil, err
		}
		k.Scopes = strings.Split(scopes, ",")
		k.CreatedAt = parse(created)
		if lastUsed.Valid {
			t := parse(lastUsed.String)
			k.LastUsed = &t
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey отзывает ключ.
//
// Помечаем, а не удаляем: запись «был такой ключ, отозван тогда-то» — это
// единственный след, по которому потом разбирают, что происходило.
func (s *Store) RevokeAPIKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		format(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// AuthenticateAPIKey опознаёт ключ и отдаёт его права.
func (s *Store) AuthenticateAPIKey(ctx context.Context, secret string) (APIKey, error) {
	if !strings.HasPrefix(secret, keyPrefix) {
		return APIKey{}, ErrKeyNotFound
	}

	var (
		k       APIKey
		scopes  string
		created string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, scopes, created_at FROM api_keys
		 WHERE token_hash = ? AND revoked_at IS NULL`, HashToken(secret)).
		Scan(&k.ID, &k.Name, &scopes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrKeyNotFound
	}
	if err != nil {
		return APIKey{}, err
	}

	k.Scopes = strings.Split(scopes, ",")
	k.CreatedAt = parse(created)

	// Отметку о последнем использовании ставим и молчим об ошибке: она нужна
	// продавцу, чтобы понять, каким ключом ещё пользуются, и ронять из-за неё
	// работающий запрос было бы глупо.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, format(time.Now().UTC()), k.ID)

	return k, nil
}

// Allows проверяет, даёт ли ключ нужное право.
//
// Право read входит в любое другое: кто может менять, тот может и смотреть.
// Обратное неверно.
func (k APIKey) Allows(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
		if scope == ScopeRead {
			return true
		}
	}
	return false
}
