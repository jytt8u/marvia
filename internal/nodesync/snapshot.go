package nodesync

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jytt8u/marvia/internal/users"
)

// OfflineTTL ограничивает задержку отзыва доступа при потере панели.
const OfflineTTL = 24 * time.Hour

type accessSnapshot struct {
	Version int                    `json:"version"`
	At      time.Time              `json:"at"`
	Users   []users.User           `json:"users"`
	Usage   map[string]users.Usage `json:"usage"`
}

// WithSnapshot включает восстановление доступа после перезапуска. Файл
// шифруется токеном этой ноды, не содержит имён и привязан к адресу панели.
func (c *Client) WithSnapshot(path string) *Client { c.snapshotPath = path; return c }

func (c *Client) SnapshotError() error { return c.snapshotErr }

func (c *Client) snapshotCipher() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("marvia/node-access/v1\x00" + c.token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (c *Client) saveSnapshot(list []users.User, at time.Time) error {
	if c.snapshotPath == "" {
		return nil
	}
	clean := append([]users.User(nil), list...)
	for i := range clean {
		clean[i].Label = ""
	}
	plain, err := json.Marshal(accessSnapshot{1, at.UTC(), clean, c.snapshotUsage})
	if err != nil {
		return err
	}
	if len(plain) > maxResponse {
		return errors.New("снимок доступа слишком велик")
	}
	aead, err := c.snapshotCipher()
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	raw := aead.Seal(nonce, nonce, plain, []byte(c.base))
	f, err := os.CreateTemp(filepath.Dir(c.snapshotPath), ".access-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.snapshotPath)
}

func (c *Client) loadSnapshot(now time.Time) (accessSnapshot, error) {
	var s accessSnapshot
	f, err := os.Open(c.snapshotPath)
	if err != nil {
		return s, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxResponse+128))
	if err != nil {
		return s, err
	}
	aead, err := c.snapshotCipher()
	if err != nil {
		return s, err
	}
	if len(raw) < aead.NonceSize()+aead.Overhead() || len(raw) >= maxResponse+128 {
		return s, errors.New("повреждён снимок доступа")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(c.base))
	if err != nil {
		return s, errors.New("снимок доступа не прошёл проверку подлинности")
	}
	if err := json.Unmarshal(plain, &s); err != nil {
		return s, err
	}
	if s.Version != 1 || s.At.After(now) || !now.Before(s.At.Add(OfflineTTL)) {
		return s, errors.New("срок снимка доступа истёк или время неверно")
	}
	if _, err := users.NewRegistry(s.Users); err != nil {
		return s, errors.New("неверные доступы в снимке")
	}
	for _, usage := range s.Usage {
		if usage.Up < 0 || usage.Down < 0 {
			return s, errors.New("неверный расход в снимке")
		}
	}
	s.Users = lease(s.Users, s.At.Add(OfflineTTL))
	return s, nil
}

func lease(list []users.User, until time.Time) []users.User {
	list = append([]users.User(nil), list...)
	for i := range list {
		list[i].Label = ""
		if list[i].ExpiresAt.IsZero() || list[i].ExpiresAt.After(until) {
			list[i].ExpiresAt = until
		}
	}
	return list
}

// Bootstrap сначала спрашивает панель. Только её недоступность допускает
// снимок; явный отзыв токена или отключение ноды всегда имеют приоритет.
// Счётчики объединяются по максимуму, чтобы перезапуск не подарил квоту.
func (c *Client) Bootstrap(ctx context.Context, saved map[string]users.Usage) ([]users.User, map[string]users.Usage, bool, error) {
	usage := make(map[string]users.Usage, len(saved))
	for id, value := range saved {
		usage[id] = value
	}
	snapshot, snapshotErr := c.loadSnapshot(time.Now())
	if snapshotErr == nil {
		for id, value := range snapshot.Usage {
			old := usage[id]
			old.Up, old.Down = max(old.Up, value.Up), max(old.Down, value.Down)
			usage[id] = old
		}
	}
	c.snapshotUsage = usage
	list, err := c.FetchUsers(ctx)
	if err == nil || errors.Is(err, ErrNodeDisabled) || errors.Is(err, ErrNodeUnauthorized) {
		return list, usage, false, err
	}
	if snapshotErr != nil {
		return nil, nil, false, fmt.Errorf("%w; нет пригодного снимка доступа: %v", err, snapshotErr)
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, nil, false, ctx.Err()
	}
	return snapshot.Users, usage, true, nil
}

func (c *Client) forgetSnapshot() {
	if c.snapshotPath == "" {
		return
	}
	c.snapshotErr = os.Remove(c.snapshotPath)
	if os.IsNotExist(c.snapshotErr) {
		c.snapshotErr = nil
	}
}
