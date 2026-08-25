//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/tunbridge"
	"github.com/veilproject/veil/internal/vp1"
	"github.com/veilproject/veil/internal/wintun"
)

// State — что сейчас с туннелем.
type State string

const (
	StateIdle       State = "idle"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateFailed     State = "failed"
)

// Status — всё, что показывает окно.
type Status struct {
	State      State  `json:"state"`
	Node       string `json:"node"`
	Address    string `json:"address"`
	PingMS     int64  `json:"ping_ms"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Reason     string `json:"reason,omitempty"`
	HasAccount bool   `json:"has_account"`
	Elevated   bool   `json:"elevated"`
}

// Controller держит туннель и знает, как его включить и выключить.
//
// Всё состояние собрано в одном месте под одним замком. Разложить его по
// разным местам было бы соблазнительно, но кнопку в окне человек нажимает
// когда захочет, в том числе посреди подключения, — и тогда две половины
// состояния начали бы расходиться.
type Controller struct {
	mu      sync.Mutex
	state   State
	node    client.Node
	ping    time.Duration
	reason  string
	account string

	dialer  *client.Dialer
	adapter *wintun.Adapter
	bridge  *tunbridge.Bridge
	cancel  context.CancelFunc

	up, down atomic.Int64

	dns string
	mtu uint32
	log *journal
}

// NewController готовит управление, подхватывая сохранённую ссылку доступа.
func NewController(dns string, mtu uint32, log *journal) *Controller {
	c := &Controller{state: StateIdle, dns: dns, mtu: mtu, log: log}
	c.account = readAccount()
	return c
}

// Status отдаёт снимок состояния для окна.
func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()

	return Status{
		State:      c.state,
		Node:       c.node.Name,
		Address:    c.node.Address,
		PingMS:     c.ping.Milliseconds(),
		Up:         c.up.Load(),
		Down:       c.down.Load(),
		Reason:     c.reason,
		HasAccount: c.account != "",
		Elevated:   elevated(),
	}
}

// Account отдаёт сохранённую ссылку доступа.
func (c *Controller) Account() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.account
}

// SetAccount проверяет ссылку и запоминает её.
//
// Проверяем тем же разбором, каким потом будем подключаться: сказать «ссылка
// не та» сразу при вставке гораздо лучше, чем после неудачной попытки.
func (c *Controller) SetAccount(link string) error {
	link = strings.TrimSpace(link)
	if _, err := client.ParseAccountLink(link); err != nil {
		return err
	}

	c.mu.Lock()
	c.account = link
	c.mu.Unlock()

	return writeAccount(link)
}

// Connect поднимает туннель.
func (c *Controller) Connect() error {
	c.mu.Lock()
	if c.state == StateConnecting || c.state == StateConnected {
		c.mu.Unlock()
		return nil
	}
	if c.account == "" {
		c.mu.Unlock()
		return errors.New("не задана ссылка доступа")
	}
	if !elevated() {
		c.mu.Unlock()
		return errors.New("нужны права администратора: без них Windows не даст создать сетевой адаптер")
	}

	account := c.account
	c.state = StateConnecting
	c.reason = ""
	c.up.Store(0)
	c.down.Store(0)

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.mu.Unlock()

	go c.connect(ctx, account)
	return nil
}

func (c *Controller) connect(ctx context.Context, link string) {
	if err := c.raise(ctx, link); err != nil {
		if ctx.Err() != nil {
			// Человек нажал «отключиться», не дождавшись. Это не ошибка.
			c.finish(StateIdle, "")
			return
		}
		c.log.add("не подключилось: %v", err)
		c.finish(StateFailed, err.Error())
	}
}

// raise делает всю работу подключения по шагам.
func (c *Controller) raise(ctx context.Context, link string) error {
	account, err := client.ParseAccountLink(link)
	if err != nil {
		return fmt.Errorf("ссылка доступа: %w", err)
	}
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return fmt.Errorf("личный ключ: %w", err)
	}

	c.log.add("забираю список нод")
	sub, err := client.FetchSubscription(ctx, account.SubscriptionURL)
	if err != nil {
		return fmt.Errorf("список нод: %w", err)
	}

	c.log.add("замеряю ноды, их %d", len(sub.Nodes))
	dialer, measurements, err := client.SelectBest(ctx, sub.Nodes, key, client.Options{})

	go func() {
		_ = client.SendReports(context.Background(), account.SubscriptionURL, client.ReportsFrom(measurements))
	}()

	if err != nil {
		return err
	}

	node := dialer.Node()
	ping := latencyOf(measurements, node)
	c.log.add("выбрана нода %s, задержка %d мс", node.Name, ping.Milliseconds())

	// Адреса ноды выясняем до того, как заберём себе трафик: после этого
	// запросы имён пойдут в туннель, которого ещё нет.
	bypass, err := nodeAddresses(node.Address)
	if err != nil {
		_ = dialer.Close()
		return err
	}

	address, err := netip.ParsePrefix(tunnelAddress)
	if err != nil {
		_ = dialer.Close()
		return err
	}
	dnsAddr, err := dnsAddress(c.dns)
	if err != nil {
		_ = dialer.Close()
		return err
	}

	c.log.add("создаю сетевой адаптер и настраиваю маршруты")
	adapter, err := wintun.Open(wintun.Config{
		Name:    adapterName,
		MTU:     c.mtu,
		Address: address,
		DNS:     dnsAddr,
		Bypass:  bypass,
	})
	if err != nil {
		_ = dialer.Close()
		return err
	}

	bridge, err := tunbridge.Start(tunbridge.Config{
		Endpoint: adapter.Endpoint(),
		MTU:      c.mtu,
		Dialer:   &countingDialer{inner: dialer, up: &c.up, down: &c.down},
		DNS:      c.dns,
		OnError:  func(err error) { c.log.add("соединение: %v", err) },
	})
	if err != nil {
		_ = adapter.Close()
		_ = dialer.Close()
		return fmt.Errorf("сетевой мост: %w", err)
	}

	c.mu.Lock()
	c.dialer, c.adapter, c.bridge = dialer, adapter, bridge
	c.node, c.ping = node, ping
	c.state = StateConnected
	c.reason = ""
	c.mu.Unlock()

	c.log.add("туннель поднят: весь трафик идёт через %s", node.Name)
	return nil
}

// Disconnect убирает туннель и всё, что под него настраивалось.
func (c *Controller) Disconnect() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	bridge, adapter, dialer := c.bridge, c.adapter, c.dialer
	c.bridge, c.adapter, c.dialer = nil, nil, nil
	c.state = StateIdle
	c.reason = ""
	c.node = client.Node{}
	c.mu.Unlock()

	// Порядок обратный сборке: сначала перестаём разбирать пакеты, потом
	// снимаем маршруты и убираем адаптер, и только затем рвём туннель.
	if bridge != nil {
		_ = bridge.Close()
	}
	if adapter != nil {
		if err := adapter.Close(); err != nil {
			c.log.add("при уборке: %v", err)
		}
	}
	if dialer != nil {
		_ = dialer.Close()
	}
	if bridge != nil || adapter != nil {
		c.log.add("туннель убран, маршруты сняты")
	}
}

func (c *Controller) finish(state State, reason string) {
	c.mu.Lock()
	c.state = state
	c.reason = reason
	c.cancel = nil
	c.mu.Unlock()
}

// countingDialer считает байты, прошедшие через туннель.
//
// Считаем здесь, а не в мосту: мост общий для телефона и компьютера, а
// показывать скорость нужно только окну.
type countingDialer struct {
	inner    tunbridge.Dialer
	up, down *atomic.Int64
}

func (d *countingDialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	conn, err := d.inner.DialTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, up: d.up, down: d.down}, nil
}

type countingConn struct {
	net.Conn
	up, down *atomic.Int64
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.down.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.up.Add(int64(n))
	return n, err
}

// latencyOf достаёт задержку выбранной ноды из замеров.
func latencyOf(measurements []client.Measurement, node client.Node) time.Duration {
	for _, m := range measurements {
		if m.Node.ID == node.ID && m.OK() {
			return m.Latency
		}
	}
	return 0
}

// accountPath — где лежит сохранённая ссылка доступа.
func accountPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Veil", "account"), nil
}

func readAccount() string {
	path, err := accountPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// writeAccount сохраняет ссылку доступа только для владельца.
//
// В ней личный ключ покупателя. Права 600 на Windows работают иначе, чем на
// Unix, но каталог настроек пользователя и так закрыт от других учётных
// записей — а вот от случайного чужого взгляда в общей папке это защищает.
func writeAccount(link string) error {
	path, err := accountPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(link), 0o600)
}
