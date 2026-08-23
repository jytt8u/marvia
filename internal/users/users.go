// Package users ведёт учёт клиентов ноды: кого пускать, сколько ему можно и
// сколько он уже израсходовал.
//
// Нода намеренно не знает, кто такой пользователь. У неё есть публичный ключ,
// лимиты к нему и счётчики. Ни имени, ни телефона, ни истории посещений:
// изъятие сервера не должно выдавать людей.
package users

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Причины отказа. Наружу они не уходят: клиент в любом случае получает
// сайт-прикрытие, иначе разница в реакции сама становится признаком.
var (
	ErrUnknown       = errors.New("ключ не зарегистрирован")
	ErrDisabled      = errors.New("доступ отключён")
	ErrExpired       = errors.New("срок подписки истёк")
	ErrQuotaExceeded = errors.New("исчерпана квота трафика")
	ErrTooManyIPs    = errors.New("превышено число устройств")
	ErrTooManyConns  = errors.New("превышено число соединений")
)

// User — запись о клиенте, как её задаёт владелец ноды или панель.
type User struct {
	// PublicKey — публичный ключ клиента в base64. Он же идентификатор ключа.
	PublicKey string `json:"public_key"`

	// Label — человекочитаемая пометка для владельца ноды. На работу не влияет.
	Label string `json:"label,omitempty"`

	// Enabled — выключенный пользователь не пускается, но остаётся в списке
	// вместе со статистикой. Для «приостановить до оплаты».
	Enabled bool `json:"enabled"`

	// ExpiresAt — конец подписки. Нулевое значение означает «бессрочно».
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// TrafficLimit — сколько байт всего можно. 0 означает «без лимита».
	TrafficLimit int64 `json:"traffic_limit,omitempty"`

	// MaxIPs — со скольких разных адресов можно подключаться в пределах окна.
	// 0 означает «без ограничения».
	//
	// Это ответ на главную боль продавца: один купил, раздал конфиг десяти
	// друзьям. Считать устройства напрямую нельзя — все они предъявляют один
	// и тот же ключ, — а вот разные IP видно.
	MaxIPs int `json:"max_ips,omitempty"`

	// MaxConns — сколько одновременных соединений до ноды разрешено.
	// 0 означает «без ограничения». С мультиплексированием одно устройство
	// открывает до четырёх, так что ставить сюда единицу нельзя.
	MaxConns int `json:"max_conns,omitempty"`

	// Account связывает несколько ключей в один аккаунт.
	//
	// У человека обычно телефон и ноутбук, и у каждого свой ключ: так их видно
	// по отдельности и любой отзывается отдельно. Но квота, срок и лимит
	// устройств у них общие — иначе достаточно выпустить второй ключ, чтобы
	// удвоить себе трафик. Пусто означает «ключ сам себе аккаунт».
	Account string `json:"account,omitempty"`
}

// AccountID возвращает идентификатор аккаунта, к которому относится ключ.
func (u User) AccountID() string {
	if u.Account != "" {
		return u.Account
	}
	return u.PublicKey
}

// Usage — накопленный расход.
type Usage struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// Total — сколько всего израсходовано.
func (u Usage) Total() int64 { return u.Up + u.Down }

// ipWindow — сколько помним адрес, с которого подключались.
//
// Час выбран как компромисс: меньше — и человек, переехавший с Wi-Fi на
// мобильную сеть, упрётся в лимит на ровном месте; больше — и раздача
// конфига друзьям слишком долго остаётся незамеченной.
const ipWindow = time.Hour

// account — состояние одного аккаунта в памяти ноды. Ключей у аккаунта может
// быть несколько, а счётчики, соединения и адреса общие.
type account struct {
	id    string
	user  User
	usage Usage
	conns int
	ips   map[string]time.Time
}

// Registry хранит аккаунты и их расход.
type Registry struct {
	mu sync.Mutex

	// byKey — публичный ключ клиента (сырые байты) → аккаунт. Несколько
	// ключей одного человека указывают на одну и ту же запись.
	byKey map[string]*account

	// byAccount — идентификатор аккаунта → та же запись. Нужен, чтобы
	// восстанавливать расход и отдавать статистику.
	byAccount map[string]*account

	now func() time.Time
}

// NewRegistry собирает реестр из списка пользователей.
func NewRegistry(list []User) (*Registry, error) {
	r := &Registry{
		byKey:     make(map[string]*account, len(list)),
		byAccount: make(map[string]*account, len(list)),
		now:       time.Now,
	}
	if err := r.Replace(list); err != nil {
		return nil, err
	}
	return r, nil
}

// Replace заменяет список пользователей, сохраняя накопленную статистику тех,
// кто остался. Вызывается при перечитывании файла и при обновлении с панели.
func (r *Registry) Replace(list []User) error {
	nextKeys := make(map[string]*account, len(list))
	nextAccounts := make(map[string]*account, len(list))

	for i, u := range list {
		key, err := DecodePublicKey(u.PublicKey)
		if err != nil {
			return fmt.Errorf("пользователь %d (%s): %w", i+1, u.Label, err)
		}

		id := u.AccountID()
		acc, ok := nextAccounts[id]
		if !ok {
			acc = &account{id: id, user: u, ips: make(map[string]time.Time)}
			nextAccounts[id] = acc
		}
		nextKeys[string(key)] = acc
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Переносим расход, живые соединения и известные адреса к новым записям.
	for id, old := range r.byAccount {
		if fresh, ok := nextAccounts[id]; ok {
			fresh.usage = old.usage
			fresh.conns = old.conns
			fresh.ips = old.ips
		}
	}
	r.byKey = nextKeys
	r.byAccount = nextAccounts
	return nil
}

// Session — принятое соединение конкретного пользователя.
type Session struct {
	registry *Registry
	id       string
	closed   bool
}

// Admit решает, пускать ли соединение с этим ключом, и открывает сессию.
func (r *Registry) Admit(pub []byte, remote net.Addr) (*Session, error) {

	r.mu.Lock()
	defer r.mu.Unlock()

	acc, ok := r.byKey[string(pub)]
	if !ok {
		return nil, ErrUnknown
	}
	if !acc.user.Enabled {
		return nil, ErrDisabled
	}

	now := r.now()
	if !acc.user.ExpiresAt.IsZero() && now.After(acc.user.ExpiresAt) {
		return nil, ErrExpired
	}
	if acc.user.TrafficLimit > 0 && acc.usage.Total() >= acc.user.TrafficLimit {
		return nil, ErrQuotaExceeded
	}
	if acc.user.MaxConns > 0 && acc.conns >= acc.user.MaxConns {
		return nil, ErrTooManyConns
	}

	if acc.user.MaxIPs > 0 {
		host := hostOf(remote)
		acc.pruneIPs(now)
		if _, seen := acc.ips[host]; !seen && len(acc.ips) >= acc.user.MaxIPs {
			return nil, ErrTooManyIPs
		}
		acc.ips[host] = now
	}

	acc.conns++
	return &Session{registry: r, id: acc.id}, nil
}

// Add записывает израсходованные байты и сообщает, не пора ли отключать.
func (s *Session) Add(up, down int64) (overQuota bool) {
	if s == nil {
		return false
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()

	acc, ok := s.registry.byAccount[s.id]
	if !ok {
		// Пользователя удалили из списка, пока он был подключён.
		return true
	}
	acc.usage.Up += up
	acc.usage.Down += down

	return acc.user.TrafficLimit > 0 && acc.usage.Total() >= acc.user.TrafficLimit
}

// Label возвращает пометку пользователя — для журнала.
func (s *Session) Label() string {
	if s == nil {
		return ""
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if acc, ok := s.registry.byAccount[s.id]; ok {
		return acc.user.Label
	}
	return ""
}

// Valid сообщает, можно ли продолжать обслуживать уже открытую сессию.
//
// Проверять только при подключении недостаточно. Сессия живёт часами: человек
// подключился утром, днём продавец отключил его за неоплату — и до вечера
// ничего не изменится, потому что переподключаться клиенту незачем. Ровно так
// утекает выручка. Нода обязана перепроверять состояние периодически.
func (s *Session) Valid() error {
	if s == nil {
		return nil
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()

	acc, ok := s.registry.byAccount[s.id]
	if !ok {
		// Пользователя удалили или отозвали все его ключи.
		return ErrUnknown
	}
	if !acc.user.Enabled {
		return ErrDisabled
	}
	if !acc.user.ExpiresAt.IsZero() && s.registry.now().After(acc.user.ExpiresAt) {
		return ErrExpired
	}
	if acc.user.TrafficLimit > 0 && acc.usage.Total() >= acc.user.TrafficLimit {
		return ErrQuotaExceeded
	}
	return nil
}

// Close закрывает сессию. Безопасно вызывать несколько раз.
func (s *Session) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true

	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if acc, ok := s.registry.byAccount[s.id]; ok && acc.conns > 0 {
		acc.conns--
	}
}

// Stat — сводка по одному аккаунту.
type Stat struct {
	Account string `json:"account"`
	Label   string `json:"label,omitempty"`
	Usage   Usage  `json:"usage"`
	Conns   int    `json:"conns"`
	IPs     int    `json:"ips"`
}

// Stats отдаёт срез состояния — для журнала, выгрузки и, позже, панели.
func (r *Registry) Stats() []Stat {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	out := make([]Stat, 0, len(r.byAccount))
	for _, acc := range r.byAccount {
		acc.pruneIPs(now)
		out = append(out, Stat{
			Account: acc.id,
			Label:   acc.user.Label,
			Usage:   acc.usage,
			Conns:   acc.conns,
			IPs:     len(acc.ips),
		})
	}
	return out
}

// Len возвращает число зарегистрированных аккаунтов.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byAccount)
}

// RestoreUsage возвращает счётчики на место после перезапуска ноды.
func (r *Registry) RestoreUsage(saved map[string]Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, acc := range r.byAccount {
		if u, ok := saved[acc.id]; ok {
			acc.usage = u
		}
	}
}

func (a *account) pruneIPs(now time.Time) {
	deadline := now.Add(-ipWindow)
	for ip, seen := range a.ips {
		if seen.Before(deadline) {
			delete(a.ips, ip)
		}
	}
}

// hostOf достаёт адрес без порта: подключения с одного устройства приходят
// с разных портов, и считать их за разные устройства нельзя.
func hostOf(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
