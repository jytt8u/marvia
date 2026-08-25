package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Кэш подписки.
//
// 21 августа 2026 Роскомнадзор взялся за DoH Google и DoT Cloudflare, и запрос
// имени на большинстве устройств снова уходит к резолверу провайдера открытым
// текстом. Значит каждый поход в панель — это строчка в логах провайдера: вот
// столько-то человек спрашивают один и тот же непубличный домен. Именно по
// такому следу домен подписки и находят, а найдя — закрывают.
//
// Поэтому список нод живёт на диске рядом со ссылкой доступа, и в панель мы
// ходим только тогда, когда без неё не обойтись: кэш протух или ни одна нода
// из него не отвечает. Пока кэш работает, домен подписки в запросах имён не
// появляется вовсе.
//
// Секретов в кэше нет: адреса нод и их публичные ключи покупатель и так узнаёт
// при первом подключении. Адрес подписки, наоборот, не пишем даже сюда —
// только его отпечаток, чтобы отличить чужой кэш от своего.

const (
	// CacheTTL — сколько кэш считается свежим.
	//
	// Сутки — компромисс. Меньше — и домен снова светится каждый день у
	// каждого покупателя. Больше — и человек с давно заменённым списком нод
	// будет слишком долго стучаться в мёртвые адреса, прежде чем панель
	// расскажет ему про новые. Впрочем, мёртвые ноды и так уводят нас в
	// панель раньше срока, так что цена ошибки здесь невелика.
	CacheTTL = 24 * time.Hour

	// cacheVersion — версия формата файла. Меняется, когда старый файл
	// перестаёт годиться: тогда его проще выбросить, чем разбирать.
	cacheVersion = 1
)

// cacheFile — то, что лежит на диске.
type cacheFile struct {
	Version int `json:"version"`

	// Subscription — отпечаток адреса подписки. Нужен, чтобы после смены
	// ссылки доступа не подсунуть человеку ноды прошлого продавца.
	Subscription string `json:"subscription"`

	FetchedAt time.Time    `json:"fetched_at"`
	Payload   Subscription `json:"payload"`
}

// CachedSubscription — подписка, поднятая с диска, вместе со временем, когда
// её забирали у панели.
type CachedSubscription struct {
	Subscription Subscription
	FetchedAt    time.Time
}

// Nodes отдаёт ноды из кэша. У пустого кэша их нет — это не ошибка.
func (c CachedSubscription) Nodes() []Node { return c.Subscription.Nodes }

// Fresh сообщает, можно ли обойтись кэшем, не тревожа панель.
//
// Время из будущего считаем негодным: часы на телефоне переводят руками, и
// кэш с датой на месяц вперёд иначе не протух бы никогда.
func (c CachedSubscription) Fresh() bool {
	if len(c.Nodes()) == 0 || c.FetchedAt.IsZero() {
		return false
	}
	age := time.Since(c.FetchedAt)
	return age >= 0 && age < CacheTTL
}

// subscriptionFingerprint — отпечаток адреса подписки.
//
// Хешируем, а не пишем как есть: в адресе токен покупателя, и раскладывать
// его по лишним файлам ни к чему.
func subscriptionFingerprint(subURL string) string {
	sum := sha256.Sum256([]byte(subURL))
	return hex.EncodeToString(sum[:])
}

// LoadCache читает кэш подписки для указанного адреса.
//
// Отсутствие файла возвращается как os.ErrNotExist — обычное дело при первом
// запуске. Кэш от другой подписки считается отсутствующим.
func LoadCache(path, subURL string) (CachedSubscription, error) {
	if path == "" {
		return CachedSubscription{}, os.ErrNotExist
	}

	f, err := os.Open(path)
	if err != nil {
		return CachedSubscription{}, err
	}
	defer f.Close()

	var stored cacheFile
	if err := json.NewDecoder(io.LimitReader(f, maxSubscription)).Decode(&stored); err != nil {
		return CachedSubscription{}, fmt.Errorf("разбор кэша: %w", err)
	}
	if stored.Version != cacheVersion {
		return CachedSubscription{}, fmt.Errorf("кэш версии %d, ожидалась %d", stored.Version, cacheVersion)
	}
	if stored.Subscription != subscriptionFingerprint(subURL) {
		// Человек сменил ссылку доступа. Ноды прошлого продавца ему не
		// подойдут, и стучаться в них незачем.
		return CachedSubscription{}, fmt.Errorf("кэш от другой подписки: %w", os.ErrNotExist)
	}
	if len(stored.Payload.Nodes) == 0 {
		return CachedSubscription{}, errors.New("в кэше нет ни одной ноды")
	}

	return CachedSubscription{Subscription: stored.Payload, FetchedAt: stored.FetchedAt}, nil
}

// SaveCache сохраняет подписку рядом со ссылкой доступа.
//
// Пишем через временный файл: обрыв питания посреди записи оставил бы
// половину JSON, и следующий запуск потерял бы кэш целиком — ровно тогда,
// когда он нужнее всего.
func SaveCache(path, subURL string, sub Subscription) error {
	if path == "" {
		return errors.New("не задан путь кэша")
	}
	if len(sub.Nodes) == 0 {
		return errors.New("нечего сохранять: в подписке нет нод")
	}

	raw, err := json.Marshal(cacheFile{
		Version:      cacheVersion,
		Subscription: subscriptionFingerprint(subURL),
		FetchedAt:    time.Now().UTC(),
		Payload:      sub,
	})
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".subscription-*")
	if err != nil {
		return err
	}
	// Пока файл не встал на место, он наш и его надо убрать. После удачного
	// переименования убирать уже нечего — os.Remove промахнётся молча.
	defer func() {
		_ = os.Remove(tmp.Name())
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
