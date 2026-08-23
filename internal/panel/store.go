// Package panel — управляющий слой: пользователи, подписки, ноды, статистика.
//
// Панель живёт отдельно от нод и никогда не пропускает через себя трафик
// пользователей. Нода — расходник, её теряют и блокируют; панель должна
// пережить любое число потерянных нод.
package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"

	_ "modernc.org/sqlite" // чистый Go, без cgo: нода и панель кросс-компилируются одной командой
)

// ErrNotFound — запрошенной записи нет.
var ErrNotFound = errors.New("запись не найдена")

// Виды учётных данных.
//
// Один пользователь может иметь несколько наборов: ключ для нашего протокола,
// UUID для VLESS, пароль для Trojan. Так продавец переводит покупателей на
// Veil, не заставляя их менять приложение, — а это самое дорогое в переезде.
const (
	CredVP1    = "vp1"
	CredVLESS  = "vless"
	CredTrojan = "trojan"
)

// User — подписчик в терминах панели.
type User struct {
	ID           int64      `json:"id"`
	Label        string     `json:"label"`
	Enabled      bool       `json:"enabled"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	TrafficLimit int64      `json:"traffic_limit"`
	MaxIPs       int        `json:"max_ips"`
	MaxConns     int        `json:"max_conns"`
	SubToken     string     `json:"sub_token"`
	CreatedAt    time.Time  `json:"created_at"`

	// Used — суммарный расход по всем нодам, заполняется при чтении.
	Used int64 `json:"used"`

	// Credentials — наборы доступа. Секреты здесь публичные (публичный ключ,
	// UUID): приватную часть панель не хранит.
	Credentials []Credential `json:"credentials,omitempty"`
}

// Credential — один набор доступа пользователя.
type Credential struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Secret    string    `json:"secret"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// Node — точка входа.
type Node struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Address   string     `json:"address"`
	SNI       string     `json:"sni"`
	PublicKey string     `json:"public_key"`
	Enabled   bool       `json:"enabled"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Store — хранилище панели поверх SQLite.
type Store struct {
	db *sql.DB
}

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    label         TEXT    NOT NULL DEFAULT '',
    enabled       INTEGER NOT NULL DEFAULT 1,
    expires_at    TEXT,
    traffic_limit INTEGER NOT NULL DEFAULT 0,
    max_ips       INTEGER NOT NULL DEFAULT 0,
    max_conns     INTEGER NOT NULL DEFAULT 0,
    sub_token     TEXT    NOT NULL UNIQUE,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    secret     TEXT    NOT NULL,
    label      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL,
    UNIQUE (kind, secret)
);

CREATE INDEX IF NOT EXISTS credentials_user ON credentials(user_id);

CREATE TABLE IF NOT EXISTS nodes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    address    TEXT    NOT NULL,
    sni        TEXT    NOT NULL DEFAULT '',
    public_key TEXT    NOT NULL,
    token_hash TEXT    NOT NULL UNIQUE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    last_seen  TEXT,
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS usage (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    up         INTEGER NOT NULL DEFAULT 0,
    down       INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, node_id)
);
`

// Open открывает или создаёт базу панели.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("открытие базы %s: %w", path, err)
	}
	// SQLite не любит параллельных писателей, а выигрыш от пула здесь нулевой.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("создание схемы: %w", err)
	}
	return &Store{db: db}, nil
}

// Close закрывает базу.
func (s *Store) Close() error { return s.db.Close() }

// CreateUserParams — что нужно, чтобы завести подписчика.
type CreateUserParams struct {
	Label        string     `json:"label"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	TrafficLimit int64      `json:"traffic_limit"`
	MaxIPs       int        `json:"max_ips"`
	MaxConns     int        `json:"max_conns"`
}

// CreateUser заводит подписчика и выдаёт ему ключ для нашего протокола.
//
// Приватный ключ возвращается ровно один раз и нигде не сохраняется — как у
// WireGuard. Если продавец его потеряет, он выпустит новый: это дешевле, чем
// хранить приватные ключи всех клиентов в одной базе, которую однажды сольют.
func (s *Store) CreateUser(ctx context.Context, p CreateUserParams) (User, string, error) {
	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		return User{}, "", fmt.Errorf("генерация ключа: %w", err)
	}
	subToken, err := NewToken()
	if err != nil {
		return User{}, "", err
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (label, enabled, expires_at, traffic_limit, max_ips, max_conns, sub_token, created_at)
		 VALUES (?, 1, ?, ?, ?, ?, ?, ?)`,
		p.Label, nullTime(p.ExpiresAt), p.TrafficLimit, p.MaxIPs, p.MaxConns, subToken, format(now))
	if err != nil {
		return User{}, "", fmt.Errorf("создание пользователя: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, "", err
	}

	pub := vp1.EncodeKey(pair.Public)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO credentials (user_id, kind, secret, label, created_at) VALUES (?, ?, ?, '', ?)`,
		id, CredVP1, pub, format(now)); err != nil {
		return User{}, "", fmt.Errorf("создание ключа: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}

	user := User{
		ID: id, Label: p.Label, Enabled: true, ExpiresAt: p.ExpiresAt,
		TrafficLimit: p.TrafficLimit, MaxIPs: p.MaxIPs, MaxConns: p.MaxConns,
		SubToken: subToken, CreatedAt: now,
		Credentials: []Credential{{Kind: CredVP1, Secret: pub, CreatedAt: now}},
	}
	return user, vp1.EncodeKey(pair.Private), nil
}

// UpdateUserParams — изменяемые поля. nil означает «не трогать».
type UpdateUserParams struct {
	Label        *string    `json:"label,omitempty"`
	Enabled      *bool      `json:"enabled,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	TrafficLimit *int64     `json:"traffic_limit,omitempty"`
	MaxIPs       *int       `json:"max_ips,omitempty"`
	MaxConns     *int       `json:"max_conns,omitempty"`
}

// UpdateUser меняет заданные поля подписчика.
func (s *Store) UpdateUser(ctx context.Context, id int64, p UpdateUserParams) (User, error) {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)

	if p.Label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *p.Label)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolInt(*p.Enabled))
	}
	if p.ExpiresAt != nil {
		sets = append(sets, "expires_at = ?")
		args = append(args, format(p.ExpiresAt.UTC()))
	}
	if p.TrafficLimit != nil {
		sets = append(sets, "traffic_limit = ?")
		args = append(args, *p.TrafficLimit)
	}
	if p.MaxIPs != nil {
		sets = append(sets, "max_ips = ?")
		args = append(args, *p.MaxIPs)
	}
	if p.MaxConns != nil {
		sets = append(sets, "max_conns = ?")
		args = append(args, *p.MaxConns)
	}
	if len(sets) == 0 {
		return s.GetUser(ctx, id)
	}

	args = append(args, id)
	query := "UPDATE users SET " + join(sets, ", ") + " WHERE id = ?"
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return User{}, fmt.Errorf("обновление пользователя: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrNotFound
	}
	return s.GetUser(ctx, id)
}

// DeleteUser удаляет подписчика вместе с его ключами и статистикой.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление пользователя: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUser читает подписчика вместе с ключами и расходом.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	list, err := s.queryUsers(ctx, `WHERE u.id = ?`, id)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// UserBySubToken находит подписчика по токену подписки.
func (s *Store) UserBySubToken(ctx context.Context, token string) (User, error) {
	list, err := s.queryUsers(ctx, `WHERE u.sub_token = ?`, token)
	if err != nil {
		return User{}, err
	}
	if len(list) == 0 {
		return User{}, ErrNotFound
	}
	return list[0], nil
}

// ListUsers возвращает всех подписчиков.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	return s.queryUsers(ctx, `ORDER BY u.id`)
}

func (s *Store) queryUsers(ctx context.Context, where string, args ...any) ([]User, error) {
	query := `
		SELECT u.id, u.label, u.enabled, u.expires_at, u.traffic_limit, u.max_ips,
		       u.max_conns, u.sub_token, u.created_at,
		       COALESCE((SELECT SUM(up + down) FROM usage WHERE user_id = u.id), 0)
		FROM users u ` + where

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("чтение пользователей: %w", err)
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var (
			u         User
			enabled   int
			expires   sql.NullString
			createdAt string
		)
		if err := rows.Scan(&u.ID, &u.Label, &enabled, &expires, &u.TrafficLimit,
			&u.MaxIPs, &u.MaxConns, &u.SubToken, &createdAt, &u.Used); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		u.ExpiresAt = parseNullTime(expires)
		u.CreatedAt = parse(createdAt)
		list = append(list, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range list {
		creds, err := s.credentials(ctx, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Credentials = creds
	}
	return list, nil
}

func (s *Store) credentials(ctx context.Context, userID int64) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, secret, label, created_at FROM credentials WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("чтение ключей: %w", err)
	}
	defer rows.Close()

	var list []Credential
	for rows.Next() {
		var (
			c         Credential
			createdAt string
		)
		if err := rows.Scan(&c.ID, &c.Kind, &c.Secret, &c.Label, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = parse(createdAt)
		list = append(list, c)
	}
	return list, rows.Err()
}

// CreateNodeParams — что нужно, чтобы завести ноду.
type CreateNodeParams struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	SNI       string `json:"sni"`
	PublicKey string `json:"public_key"`
}

// CreateNode регистрирует ноду и выдаёт ей токен.
// Токен возвращается один раз, в базе лежит только его хеш.
func (s *Store) CreateNode(ctx context.Context, p CreateNodeParams) (Node, string, error) {
	if p.PublicKey != "" {
		if _, err := vp1.DecodeKey(p.PublicKey); err != nil {
			return Node{}, "", fmt.Errorf("публичный ключ ноды: %w", err)
		}
	}

	token, err := NewToken()
	if err != nil {
		return Node{}, "", err
	}
	now := time.Now().UTC()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes (name, address, sni, public_key, token_hash, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?)`,
		p.Name, p.Address, p.SNI, p.PublicKey, HashToken(token), format(now))
	if err != nil {
		return Node{}, "", fmt.Errorf("создание ноды: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Node{}, "", err
	}

	return Node{ID: id, Name: p.Name, Address: p.Address, SNI: p.SNI,
		PublicKey: p.PublicKey, Enabled: true, CreatedAt: now}, token, nil
}

// ListNodes возвращает все ноды.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, address, sni, public_key, enabled, last_seen, created_at FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("чтение нод: %w", err)
	}
	defer rows.Close()

	var list []Node
	for rows.Next() {
		var (
			n         Node
			enabled   int
			lastSeen  sql.NullString
			createdAt string
		)
		if err := rows.Scan(&n.ID, &n.Name, &n.Address, &n.SNI, &n.PublicKey, &enabled, &lastSeen, &createdAt); err != nil {
			return nil, err
		}
		n.Enabled = enabled != 0
		n.LastSeen = parseNullTime(lastSeen)
		n.CreatedAt = parse(createdAt)
		list = append(list, n)
	}
	return list, rows.Err()
}

// DeleteNode удаляет ноду вместе с её статистикой.
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("удаление ноды: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AuthenticateNode находит ноду по её токену и отмечает, что она на связи.
func (s *Store) AuthenticateNode(ctx context.Context, token string) (Node, error) {
	var (
		n         Node
		enabled   int
		lastSeen  sql.NullString
		createdAt string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, address, sni, public_key, enabled, last_seen, created_at
		 FROM nodes WHERE token_hash = ?`, HashToken(token)).
		Scan(&n.ID, &n.Name, &n.Address, &n.SNI, &n.PublicKey, &enabled, &lastSeen, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}

	n.Enabled = enabled != 0
	n.LastSeen = parseNullTime(lastSeen)
	n.CreatedAt = parse(createdAt)

	if _, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen = ? WHERE id = ?`,
		format(time.Now().UTC()), n.ID); err != nil {
		return Node{}, err
	}
	return n, nil
}

// NodeUsers собирает список пользователей для конкретной ноды в том же виде,
// в каком нода читает его из файла.
//
// Главная тонкость — общая квота при нескольких нодах. Каждая нода считает
// только свой трафик, поэтому лимит для неё уменьшается на то, что человек уже
// израсходовал на других. Иначе квоту в 100 ГБ можно было бы потратить на
// каждой ноде отдельно.
func (s *Store) NodeUsers(ctx context.Context, nodeID int64) ([]users.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.label, u.enabled, u.expires_at, u.traffic_limit, u.max_ips, u.max_conns,
		       c.secret,
		       COALESCE((SELECT SUM(up + down) FROM usage WHERE user_id = u.id AND node_id <> ?), 0)
		FROM users u
		JOIN credentials c ON c.user_id = u.id AND c.kind = ?
		ORDER BY u.id`, nodeID, CredVP1)
	if err != nil {
		return nil, fmt.Errorf("сборка списка для ноды: %w", err)
	}
	defer rows.Close()

	var list []users.User
	for rows.Next() {
		var (
			userID    int64
			label     string
			enabled   int
			expires   sql.NullString
			limit     int64
			maxIPs    int
			maxConns  int
			secret    string
			elsewhere int64
		)
		if err := rows.Scan(&userID, &label, &enabled, &expires, &limit, &maxIPs, &maxConns, &secret, &elsewhere); err != nil {
			return nil, err
		}

		u := users.User{
			PublicKey: secret,
			Label:     label,
			Enabled:   enabled != 0,
			MaxIPs:    maxIPs,
			MaxConns:  maxConns,
			// Все ключи одного подписчика попадают в один аккаунт: квота,
			// срок и лимит устройств у телефона и ноутбука общие.
			Account: strconv.FormatInt(userID, 10),
		}
		if t := parseNullTime(expires); t != nil {
			u.ExpiresAt = *t
		}

		if limit > 0 {
			remaining := limit - elsewhere
			if remaining <= 0 {
				// Ноль в поле лимита означает «без ограничений», поэтому
				// исчерпавшего квоту выключаем целиком, а не шлём лимит 0.
				u.Enabled = false
				u.TrafficLimit = 0
			} else {
				u.TrafficLimit = remaining
			}
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// ReportUsage принимает от ноды её счётчики, разложенные по аккаунтам.
//
// Нода присылает накопленные с её стороны итоги, а не приращения: они
// переживают её перезапуск, и повторная доставка того же отчёта ничего не
// испортит. Панель просто складывает итоги всех нод.
func (s *Store) ReportUsage(ctx context.Context, nodeID int64, report map[string]users.Usage) error {
	if len(report) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := format(time.Now().UTC())
	for account, usage := range report {
		userID, err := strconv.ParseInt(account, 10, 64)
		if err != nil {
			// Отчёт не от нашей панели: нода работала по локальному файлу,
			// там идентификатор аккаунта — сам публичный ключ.
			continue
		}

		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			// Пользователя удалили, пока нода копила отчёт. Не ошибка.
			continue
		}
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO usage (user_id, node_id, up, down, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, node_id) DO UPDATE SET up = excluded.up, down = excluded.down, updated_at = excluded.updated_at`,
			userID, nodeID, usage.Up, usage.Down, now); err != nil {
			return fmt.Errorf("запись расхода: %w", err)
		}
	}
	return tx.Commit()
}

func format(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parse(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseNullTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parse(v.String)
	return &t
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return format(*t)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
