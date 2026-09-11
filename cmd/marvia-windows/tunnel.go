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

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
	"github.com/jytt8u/marvia/internal/wintun"
)

// State — что сейчас с туннелем.
type State string

const (
	StateIdle       State = "idle"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateFailed     State = "failed"

	// StateStalled — туннель поднят, но нода перестала отвечать.
	//
	// Отдельное состояние, а не «не подключилось»: туннель на месте, маршруты
	// стоят, и разница для человека принципиальная — «нажми ещё раз» против
	// «сейчас ничего не работает и вот почему».
	StateStalled State = "stalled"
)

// Status — всё, что показывает окно.
type Status struct {
	State   State  `json:"state"`
	Node    string `json:"node"`
	Address string `json:"address"`
	PingMS  int64  `json:"ping_ms"`
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
	Reason  string `json:"reason,omitempty"`

	// Подписка: до какого числа и сколько трафика осталось. Пустые значения
	// означают, что продавец не поставил ни срока, ни квоты, — врать про
	// «безлимит» в этом случае нельзя.
	Until      string `json:"until,omitempty"`
	LimitBytes int64  `json:"limit_bytes,omitempty"`
	LeftBytes  int64  `json:"left_bytes,omitempty"`
	HasAccount bool   `json:"has_account"`
	Elevated   bool   `json:"elevated"`

	// Proxy — предупреждение о системном прокси, если он есть.
	//
	// Пока он прописан, наше «весь трафик идёт через туннель» неправда:
	// браузеры и Steam слушаются прокси, а не маршрутов. Молчать об этом
	// нельзя — человек платит именно за то, чтобы трафик шёл через нас.
	Proxy      string `json:"proxy,omitempty"`
	ProxyOwner string `json:"proxy_owner,omitempty"`
	ProxyEnv   bool   `json:"proxy_env,omitempty"`
}

// Controller держит туннель и знает, как его включить и выключить.
//
// Всё состояние собрано в одном месте под одним замком. Разложить его по
// разным местам было бы соблазнительно, но кнопку в окне человек нажимает
// когда захочет, в том числе посреди подключения, — и тогда две половины
// состояния начали бы расходиться.
type Controller struct {
	mu     sync.Mutex
	state  State
	node   client.Node
	ping   time.Duration
	reason string

	// Подписка, с которой выбрана нода: её показывает окно.
	until      string
	limitBytes int64
	leftBytes  int64
	account    string

	dialer  *client.Supervisor
	adapter *wintun.Adapter
	bridge  *tunbridge.Bridge
	cancel  context.CancelFunc

	up, down atomic.Int64

	// proxy — что нашлось в системных настройках прокси на момент подключения.
	//
	// Проверяем при включении, а не на каждый опрос окна: перебор слушающих
	// сокетов ради надписи, которая меняется раз в неделю, — расточительство.
	proxy wintun.Proxy

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
		Node:       c.node.Title(),
		Address:    c.node.Address,
		PingMS:     c.ping.Milliseconds(),
		Up:         c.up.Load(),
		Down:       c.down.Load(),
		Reason:     c.reason,
		Until:      c.until,
		LimitBytes: c.limitBytes,
		LeftBytes:  c.leftBytes,
		HasAccount: c.account != "",
		Elevated:   elevated(),
		Proxy:      c.proxy.Describe(),
		ProxyOwner: c.proxy.Owner,
		ProxyEnv:   c.proxy.FromEnv,
	}
}

// DropProxy снимает системный прокси и перепроверяет, что получилось.
//
// Только по нажатию человека. Прокси мог быть поставлен осознанно — рабочим,
// родительским контролем, другим клиентом, которым он пользуется, — и снять
// его молча значило бы сломать то, чего мы не понимаем.
func (c *Controller) DropProxy() error {
	if err := wintun.DisableSystemProxy(); err != nil {
		return err
	}

	c.mu.Lock()
	c.proxy = wintun.SystemProxy()
	left := c.proxy
	c.mu.Unlock()

	c.log.add("%s", say("logProxyGone"))

	// Переменные окружения остаются жить в уже запущенных программах: они
	// прочитали их при старте, и наша правка реестра до них не дойдёт.
	if left.Found() && left.FromEnv {
		return errors.New(say("proxyInEnv"))
	}
	return nil
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
	// StateStalled сюда же: туннель поднят, просто нода молчит. Поднимать
	// второй поверх первого нельзя — адаптер и маршруты уже заняты.
	if c.state == StateConnecting || c.state == StateConnected || c.state == StateStalled {
		c.mu.Unlock()
		return nil
	}
	if c.account == "" {
		c.mu.Unlock()
		return errors.New(say("noAccount"))
	}
	if !elevated() {
		c.mu.Unlock()
		return errors.New(say("needAdmin"))
	}

	account := c.account
	c.proxy = wintun.SystemProxy()
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
		c.log.add("%s", sayf("logNoConnect", err))
		c.finish(StateFailed, err.Error())
	}
}

// raise делает всю работу подключения по шагам.
func (c *Controller) raise(ctx context.Context, link string) error {
	account, err := client.ParseAccountLink(link)
	if err != nil {
		return fmt.Errorf("%s: %w", say("accountLink"), err)
	}
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return fmt.Errorf("%s: %w", say("privateKey"), err)
	}

	// Список нод по возможности берём из кэша: каждый поход в панель — это
	// запрос имени её домена, а он с недавних пор уходит провайдеру открытым
	// текстом. Подробности в internal/client/cache.go.
	// Supervise, а не Connect: за нодой дальше следят и меняют её, если она
	// замолчала. Свой сторож здесь больше не нужен — он умел заметить разрыв,
	// но не умел его вылечить.
	dialer, measurements, err := client.Supervise(ctx, client.ConnectConfig{
		Account:   account,
		Key:       key,
		CachePath: cachePath(),
		Log:       c.log.add,
	}, client.Events{OnSwitch: c.moved, OnTrouble: c.stall, OnRecovered: c.recovered})

	go func() {
		_ = client.SendReports(context.Background(), account.SubscriptionURL, client.ReportsFrom(measurements))
	}()

	if err != nil {
		return err
	}

	node := dialer.Node()
	ping := latencyOf(measurements, node)
	c.log.add("%s", sayf("logNodePicked", node.Name, ping.Milliseconds()))

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

	c.log.add("%s", say("logAdapter"))
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
		OnError:  func(err error) { c.log.add("%s", sayf("logConn", err)) },
	})
	if err != nil {
		_ = adapter.Close()
		_ = dialer.Close()
		return fmt.Errorf("%s: %w", say("bridge"), err)
	}

	c.mu.Lock()
	c.dialer, c.adapter, c.bridge = dialer, adapter, bridge
	c.node, c.ping = node, ping
	c.until, c.limitBytes, c.leftBytes = subscriptionOf(dialer)
	c.state = StateConnected
	c.reason = ""
	c.mu.Unlock()

	c.log.add("%s", sayf("logTunnelUp", node.Name))

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
	c.until, c.limitBytes, c.leftBytes = "", 0, 0
	c.mu.Unlock()

	// Порядок обратный сборке: сначала перестаём разбирать пакеты, потом
	// снимаем маршруты и убираем адаптер, и только затем рвём туннель.
	if bridge != nil {
		_ = bridge.Close()
	}
	if adapter != nil {
		if err := adapter.Close(); err != nil {
			c.log.add("%s", sayf("logCleanup", err))
		}
	}
	if dialer != nil {
		_ = dialer.Close()
	}
	if bridge != nil || adapter != nil {
		c.log.add("%s", say("logTunnelDown"))
	}
}

// moved — ядро переехало на другую ноду.
//
// Имя переписываем обязательно: иначе окно продолжит показывать ноду, через
// которую трафик давно не идёт. Состояние возвращаем в «подключено» — если до
// этого висело «связь потеряна», то она уже нашлась.
func (c *Controller) moved(node client.Node) {
	c.mu.Lock()
	c.node = node
	if c.state == StateStalled {
		c.state = StateConnected
		c.reason = ""
	}
	bridge := c.bridge
	c.mu.Unlock()

	// Мост мог выключить датаграммы, решив, что нода их не умеет. Умирающая
	// нода обрывает поток там же, где старая отвечает отказом, так что вывод
	// мог быть про смерть, а не про старость. Новая нода за это не отвечает.
	bridge.NodeChanged()

	c.log.add("%s", sayf("logMoved", node.Title()))
}

// stall — нода замолчала, а переехать не на что.
//
// Туннель при этом намеренно не разбираем. Соблазн был — всё равно не
// работает, — но тогда снимутся маршруты, и трафик пойдёт мимо туннеля
// открыто ровно в тот момент, когда человек об этом не знает. Для средства
// обхода блокировок это хуже, чем отсутствие связи.
func (c *Controller) stall(code string) {
	// Из ядра приезжает код, а не фраза: оно не знает, на каком языке говорит
	// окно. Фразу подбираем здесь, из своего словаря.
	reason := say("nodeSilent")
	if code != client.TroubleNoNode {
		reason = code
	}

	c.mu.Lock()
	if c.state == StateConnected {
		c.state = StateStalled
		c.reason = reason
	}
	c.mu.Unlock()

	c.log.add("%s", reason)
}

// NodeView — одна нода так, как её видит покупатель в окне.
type NodeView struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	MS      int64  `json:"ms"`
	Alive   bool   `json:"alive"`

	// Current и Chosen — разные вещи, и обе нужны. Выбранная страна могла
	// замолчать, надзор уехал на живую, и показывать в этом случае одну
	// «выбранную» значило бы врать про то, куда идёт трафик.
	Current bool `json:"current"`
	Chosen  bool `json:"chosen"`
}

// Nodes отдаёт список нод без замера — тем, что известно с подключения.
func (c *Controller) Nodes() []NodeView {
	c.mu.Lock()
	dialer := c.dialer
	c.mu.Unlock()
	if dialer == nil {
		return nil
	}
	return nodeViews(dialer, dialer.Nodes(), nil)
}

// MeasureNodes меряет все ноды заново — по нажатию «Обновить».
func (c *Controller) MeasureNodes(ctx context.Context) []NodeView {
	c.mu.Lock()
	dialer := c.dialer
	c.mu.Unlock()
	if dialer == nil {
		return nil
	}
	return nodeViews(dialer, dialer.Nodes(), dialer.Measure(ctx))
}

// SelectNode переводит туннель на выбранную ноду. Ноль — обратно к автовыбору.
func (c *Controller) SelectNode(ctx context.Context, id int64) error {
	c.mu.Lock()
	dialer := c.dialer
	c.mu.Unlock()
	if dialer == nil {
		return errors.New(say("notConnected"))
	}

	if err := dialer.Select(ctx, id); err != nil {
		return err
	}

	c.mu.Lock()
	c.node = dialer.Node()
	c.mu.Unlock()

	c.log.add("%s", sayf("logChosen", dialer.Node().Title()))
	return nil
}

func nodeViews(dialer *client.Supervisor, nodes []client.Node, measured []client.Measurement) []NodeView {
	byID := make(map[int64]client.Measurement, len(measured))
	for _, m := range measured {
		byID[m.Node.ID] = m
	}

	current := dialer.Node().ID
	chosen := dialer.Selected()

	out := make([]NodeView, 0, len(nodes))
	for _, n := range nodes {
		v := NodeView{
			ID:      n.ID,
			Name:    n.Name,
			Country: n.Country,
			Current: n.ID == current,
			Chosen:  chosen != 0 && n.ID == chosen,
		}
		if m, ok := byID[n.ID]; ok {
			v.Alive = m.OK()
			if cost := m.Cost(); cost > 0 {
				v.MS = cost.Milliseconds()
			}
		} else if n.ID == current {
			// Текущую не мерили, но знаем точно: через неё идёт трафик.
			v.Alive = true
		}
		out = append(out, v)
	}
	return out
}

// recovered — нода снова отвечает после того, как её объявили молчащей.
//
// Без этого из «связь потеряна» не было выхода, кроме переезда. А самый
// обычный случай — не мёртвая нода, а пропавшая сеть: тогда переезжать некуда,
// и надпись оставалась навсегда, в том числе после возвращения сети. Хуже
// того, кнопка «Подключиться» в этом состоянии тоже не помогала: туннель
// считался поднятым.
func (c *Controller) recovered() {
	c.mu.Lock()
	if c.state == StateStalled {
		c.state = StateConnected
		c.reason = ""
	}
	c.mu.Unlock()

	c.log.add("%s", say("logNodeBack"))
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

func (d *countingDialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	conn, err := d.inner.DialDatagrams(ctx, target)
	if err != nil {
		return nil, err
	}
	// Обёртка та же: она считает байты, не заглядывая внутрь, а границы
	// датаграмм соблюдает нижележащее соединение.
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

// settingsDir — каталог настроек: %APPDATA%\Marvia.
func settingsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Marvia"), nil
}

// legacyAccountPath — где ключ лежал до переименования.
//
// Ключ доступа хранится у покупателя на диске, а не у нас в базе. Просто
// сменить имя каталога значило бы, что после обновления человек открывает окно
// и видит пустое поле там, где вчера был рабочий доступ, — и идёт к продавцу с
// «у меня всё пропало».
func legacyAccountPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Veil", "account"), nil
}

// accountPath — где лежит сохранённая ссылка доступа.
func accountPath() (string, error) {
	dir, err := settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "account"), nil
}

// cachePath — кэш подписки, рядом со ссылкой доступа.
//
// Пустая строка означает, что каталога настроек нет: тогда подключаемся без
// кэша, как раньше, — это медленнее и заметнее, но работает.
func cachePath() string {
	dir, err := settingsDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "subscription.json")
}

func readAccount() string {
	path, err := accountPath()
	if err != nil {
		return ""
	}
	if raw, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(raw))
	}

	// Нового ключа нет — смотрим, не остался ли он от прежнего имени. Найдя,
	// сразу перекладываем, чтобы этот путь понадобился ровно один раз.
	legacy, err := legacyAccountPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		return ""
	}
	link := strings.TrimSpace(string(raw))
	_ = writeAccount(link)
	return link
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

// subscriptionOf достаёт из дозвона то, что показывает окно: до какого числа
// оплачено и сколько трафика осталось.
//
// Человек заплатил и вправе это видеть, не спрашивая продавца. А продавец
// вправе не отвечать на такое вручную каждому.
func subscriptionOf(dialer *client.Supervisor) (until string, limit, left int64) {
	if dialer == nil {
		return "", 0, 0
	}
	sub := dialer.Subscription()
	if at, set := sub.Until(); set {
		until = at.Local().Format("02.01.2006")
	}
	return until, sub.TrafficLimit, sub.Remaining()
}
