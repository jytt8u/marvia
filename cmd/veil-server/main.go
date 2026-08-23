// Команда veil-server — нода Veil.
//
// Принимает соединения, маскируясь под обычный сайт на 443 порту: тот, кто
// пришёл без правильного ключа, получает настоящую веб-страницу, а не разрыв
// соединения. Внутри TLS работает протокол VP1, внутри него — логические
// потоки пользователя.
//
// Нода намеренно ничего не знает о людях. У неё есть публичные ключи, лимиты
// к ним и счётчики трафика. Кто куда ходил — не пишется никуда: изъятие
// сервера не должно выдавать пользователей.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/veilproject/veil/internal/fallback"
	"github.com/veilproject/veil/internal/metered"
	"github.com/veilproject/veil/internal/mux"
	"github.com/veilproject/veil/internal/nodesync"
	"github.com/veilproject/veil/internal/relay"
	"github.com/veilproject/veil/internal/rewind"
	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/users"
	"github.com/veilproject/veil/internal/vp1"
)

const (
	// requestTimeout — сколько ждём от клиента адрес назначения.
	requestTimeout = 10 * time.Second

	// dialTimeout — сколько ждём соединения с целевым хостом.
	dialTimeout = 15 * time.Second

	// usersReloadInterval — как часто проверяем, не изменился ли список
	// пользователей. Панель или скрипт продавца просто перезаписывают файл.
	usersReloadInterval = 5 * time.Second

	// usageFlushInterval — как часто расход выгружается на диск и сверяется
	// с квотой. Чаще — лишние записи на диск, реже — больше трафика можно
	// проскочить сверх лимита при внезапной перезагрузке.
	usageFlushInterval = 30 * time.Second

	// meterInterval — как часто счётчики соединения переносятся пользователю.
	// От него зависит, насколько быстро сработает исчерпание квоты.
	meterInterval = 5 * time.Second

	// panelSyncInterval — как часто нода сверяется с панелью: забирает список
	// и сдаёт расход. Отзыв доступа доезжает до ноды за это время.
	panelSyncInterval = 15 * time.Second

	// panelFirstFetchTimeout — сколько ждём панель на старте.
	panelFirstFetchTimeout = 30 * time.Second
)

// deps — всё, что нужно обработчику одного соединения.
type deps struct {
	static   vp1.KeyPair
	guard    *vp1.ReplayGuard
	registry *users.Registry // nil — пускать любого, кто знает публичный ключ
	fallback *fallback.Handler
}

type serverOptions struct {
	listenAddr string
	keyStr     string
	keyFile    string
	usersFile  string
	panelURL   string
	panelToken string
	usageFile  string
	certFile   string
	keyPEMFile string
	selfSigned string
	plain      bool
	coverSite  string
	coverTitle string
}

func main() {
	var opts serverOptions

	flag.StringVar(&opts.listenAddr, "listen", ":8443", "адрес и порт для приёма соединений")
	flag.StringVar(&opts.keyStr, "key", "", "приватный ключ ноды в base64")
	flag.StringVar(&opts.keyFile, "key-file", "", "файл с приватным ключом ноды")
	flag.StringVar(&opts.usersFile, "users", "", "файл пользователей: JSON с лимитами либо просто список ключей (пусто — пускать всех)")
	flag.StringVar(&opts.usageFile, "usage", "", "файл для расхода трафика (по умолчанию — рядом с файлом пользователей)")
	flag.StringVar(&opts.panelURL, "panel", "", "адрес панели, например https://panel.example.com (вместо -users)")
	flag.StringVar(&opts.panelToken, "panel-token", "", "токен этой ноды (по умолчанию — из VEIL_NODE_TOKEN)")

	flag.StringVar(&opts.certFile, "tls-cert", "", "файл сертификата PEM")
	flag.StringVar(&opts.keyPEMFile, "tls-key", "", "файл приватного ключа сертификата PEM")
	flag.StringVar(&opts.selfSigned, "tls-self-signed", "", "выпустить самоподписанный сертификат на это имя (только для отладки)")
	flag.BoolVar(&opts.plain, "plain", false, "работать без TLS-маскировки (для отладки)")

	flag.StringVar(&opts.coverSite, "cover", "", "адрес настоящего сайта для неопознанных гостей, например https://example.org")
	flag.StringVar(&opts.coverTitle, "cover-title", "", "если сайт-прикрытие не задан, отдавать заглушку с таким заголовком")

	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(opts serverOptions) error {
	static, err := loadStaticKey(opts.keyStr, opts.keyFile)
	if err != nil {
		return err
	}

	cover, err := loadFallback(opts.coverSite, opts.coverTitle)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry, usagePath, err := setupUsers(ctx, opts)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", opts.listenAddr)
	if err != nil {
		return fmt.Errorf("прослушивание %s: %w", opts.listenAddr, err)
	}
	defer ln.Close()

	addr := ln.Addr()
	if !opts.plain {
		cert, err := loadCertificate(opts)
		if err != nil {
			return err
		}
		ln = transport.Listen(ln, transport.ServerConfig{Certificate: cert})
	}

	log.Printf("veil-server слушает %s", addr)
	log.Printf("публичный ключ ноды: %s", vp1.EncodeKey(static.Public))
	if opts.plain {
		log.Printf("ВНИМАНИЕ: режим -plain, маскировки нет — трафик опознаётся DPI")
	}
	if registry == nil {
		log.Printf("ВНИМАНИЕ: список пользователей не задан, пускаем любого, кто знает публичный ключ")
	} else {
		log.Printf("пользователей: %d, расход пишется в %s", registry.Len(), usagePath)
	}
	if cover == nil {
		log.Printf("ВНИМАНИЕ: сайт-прикрытие не задан — неопознанные соединения будут рваться, что заметно сканерам")
	}

	// Закрытие слушателя по сигналу разблокирует Accept.
	go func() {
		<-ctx.Done()
		log.Printf("завершаем работу")
		_ = ln.Close()
	}()

	d := deps{static: static, guard: vp1.NewReplayGuard(vp1.ClockSkew), registry: registry, fallback: cover}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept: %v", err)
			continue
		}
		go serve(conn, d)
	}
}

// setupUsers поднимает реестр пользователей и фоновые задачи вокруг него.
//
// Источник списка — либо панель, либо локальный файл. Панель приоритетнее:
// если она задана, файл не читается вовсе.
func setupUsers(ctx context.Context, opts serverOptions) (*users.Registry, string, error) {
	if opts.panelURL != "" {
		return setupPanelUsers(ctx, opts)
	}
	if opts.usersFile == "" {
		return nil, "", nil
	}

	list, err := users.LoadUsers(opts.usersFile)
	if err != nil {
		return nil, "", err
	}
	registry, err := users.NewRegistry(list)
	if err != nil {
		return nil, "", err
	}

	usagePath := opts.usageFile
	if usagePath == "" {
		usagePath = opts.usersFile + ".usage.json"
	}

	saved, err := users.LoadUsage(usagePath)
	if err != nil {
		// Битый файл расхода — не повод не запускаться: лучше потерять
		// статистику, чем оставить всех клиентов без связи.
		log.Printf("расход не восстановлен (%v), счётчики начнутся с нуля", err)
	} else {
		registry.RestoreUsage(saved)
	}

	go registry.WatchUsers(ctx, opts.usersFile, usersReloadInterval, func(err error, count int) {
		if err != nil {
			log.Printf("список пользователей не перечитан, работаем по старому: %v", err)
			return
		}
		log.Printf("список пользователей перечитан: %d записей", count)
	})

	go registry.PersistUsage(ctx, usagePath, usageFlushInterval, func(err error) {
		log.Printf("не удалось сохранить расход: %v", err)
	})

	return registry, usagePath, nil
}

// setupPanelUsers берёт список пользователей у панели.
//
// Локальный снимок расхода нужен и в этом режиме: между перезапуском ноды и
// первой успешной синхронизацией она должна знать, кто сколько израсходовал,
// иначе после каждой перезагрузки квоты начинались бы заново.
func setupPanelUsers(ctx context.Context, opts serverOptions) (*users.Registry, string, error) {
	token := opts.panelToken
	if token == "" {
		token = os.Getenv("VEIL_NODE_TOKEN")
	}
	if token == "" {
		return nil, "", errors.New("не задан токен ноды: укажи -panel-token или VEIL_NODE_TOKEN")
	}

	client := nodesync.New(opts.panelURL, token)

	// Первый список забираем синхронно: стартовать, не зная пользователей,
	// значит на несколько секунд открыть ноду для всех подряд.
	first, cancel := context.WithTimeout(ctx, panelFirstFetchTimeout)
	list, err := client.FetchUsers(first)
	cancel()
	if err != nil {
		return nil, "", fmt.Errorf("панель %s: %w", opts.panelURL, err)
	}

	registry, err := users.NewRegistry(list)
	if err != nil {
		return nil, "", err
	}

	usagePath := opts.usageFile
	if usagePath == "" {
		usagePath = "veil-node.usage.json"
	}
	if saved, err := users.LoadUsage(usagePath); err != nil {
		log.Printf("расход не восстановлен (%v), счётчики начнутся с нуля", err)
	} else {
		registry.RestoreUsage(saved)
	}

	go client.Run(ctx, registry, panelSyncInterval, nodesync.Events{
		OnUsers: func(count int) {
			if count == 0 {
				log.Printf("панель вернула пустой список пользователей")
			}
		},
		OnError: func(err error) {
			log.Printf("синхронизация с панелью не удалась, работаем по последнему списку: %v", err)
		},
	})

	go registry.PersistUsage(ctx, usagePath, usageFlushInterval, func(err error) {
		log.Printf("не удалось сохранить расход: %v", err)
	})

	return registry, usagePath, nil
}

func serve(conn net.Conn, d deps) {
	peer := conn.RemoteAddr()

	// Счётчик стоит снаружи протокола: считаем то, что реально прошло по
	// проводу, вместе с добивкой и служебными кадрами. Именно за эти байты
	// владелец ноды платит хостеру.
	meter := metered.New(conn)

	// Пока не разобран первый кадр, мы не знаем, кто пришёл. Запоминаем
	// прочитанное, чтобы можно было отдать гостя сайту-прикрытию.
	rc := rewind.New(meter)
	defer rc.Close()

	var session *users.Session
	authorize := vp1.AllowAll
	if d.registry != nil {
		authorize = func(pub []byte) error {
			s, err := d.registry.Admit(pub, peer)
			if err != nil {
				return err
			}
			session = s
			return nil
		}
	}

	tunnel, clientPub, err := vp1.ServerHandshake(rc, d.static, d.guard, authorize)
	if err != nil {
		serveCover(rc, peer, err, d.fallback)
		return
	}
	rc.Commit()
	defer tunnel.Close()

	defer func() {
		// Последняя выгрузка счётчиков: то, что накопилось после
		// предпоследнего тика, тоже должно попасть в статистику.
		up, down := meter.Drain()
		session.Add(up, down)
		session.Close()
	}()

	client := vp1.EncodeKey(clientPub)
	if label := session.Label(); label != "" {
		client = label
	}

	session2, err := mux.Server(tunnel)
	if err != nil {
		log.Printf("[%s] клиент %s: %v", peer, client, err)
		return
	}
	defer session2.Close()

	// Периодический перенос счётчиков и проверка квоты.
	done := make(chan struct{})
	defer close(done)
	go meterLoop(meter, session, tunnel, peer, client, done)

	log.Printf("[%s] клиент %s: сессия открыта", peer, client)
	for {
		stream, err := mux.Accept(session2)
		if err != nil {
			log.Printf("[%s] клиент %s: сессия закрыта", peer, client)
			return
		}
		go serveStream(stream, peer, client)
	}
}

// meterLoop переносит счётчики соединения пользователю и обрывает сессию,
// когда квота исчерпана.
func meterLoop(meter *metered.Conn, session *users.Session, tunnel *vp1.Conn, peer net.Addr, client string, done <-chan struct{}) {
	ticker := time.NewTicker(meterInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			up, down := meter.Drain()
			if up != 0 || down != 0 {
				session.Add(up, down)
			}
			// Проверяем не только квоту: подписку могли отключить или она
			// могла истечь прямо посреди сессии. Живое соединение обязано
			// это заметить, иначе человек пользуется сервисом до тех пор,
			// пока сам не переподключится.
			if err := session.Valid(); err != nil {
				log.Printf("[%s] клиент %s: доступ прекращён (%v), отключаем", peer, client, err)
				_ = tunnel.Close()
				return
			}
		}
	}
}

// serveStream обслуживает один логический поток — то есть одно соединение
// приложения пользователя.
func serveStream(stream net.Conn, peer net.Addr, client string) {
	defer stream.Close()

	_ = stream.SetReadDeadline(time.Now().Add(requestTimeout))
	addr, err := vp1.ReadRequest(stream)
	if err != nil {
		log.Printf("[%s] клиент %s: чтение запроса: %v", peer, client, err)
		return
	}
	_ = stream.SetReadDeadline(time.Time{})

	target, err := net.DialTimeout("tcp", addr.String(), dialTimeout)
	if err != nil {
		log.Printf("[%s] клиент %s: не подключились к %s: %v", peer, client, addr, err)
		_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
		return
	}
	defer target.Close()

	if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
		log.Printf("[%s] клиент %s: отправка статуса: %v", peer, client, err)
		return
	}

	log.Printf("[%s] клиент %s -> %s", peer, client, addr)
	if err := relay.Bidirectional(stream, target); err != nil {
		log.Printf("[%s] клиент %s -> %s: обрыв: %v", peer, client, addr, err)
	}
}

// serveCover обслуживает того, кто не прошёл аутентификацию.
//
// Сюда попадают сканеры, боты, случайные посетители домена, зонды цензора — и
// свои же клиенты с истёкшей подпиской или исчерпанной квотой. Реакция должна
// быть одинаковой во всех случаях: разница в поведении сама становится
// способом прощупать ноду.
func serveCover(rc *rewind.Conn, peer net.Addr, cause error, cover *fallback.Handler) {
	if cover == nil {
		log.Printf("[%s] хендшейк отклонён (%v), сайт-прикрытие не задан", peer, cause)
		return
	}
	if err := rc.Rewind(); err != nil {
		log.Printf("[%s] хендшейк отклонён (%v), отмотка не удалась: %v", peer, cause, err)
		return
	}
	_ = rc.SetDeadline(time.Time{})
	cover.Serve(rc)
}

func loadCertificate(opts serverOptions) (tls.Certificate, error) {
	switch {
	case opts.certFile != "" && opts.keyPEMFile != "":
		return transport.LoadCertificate(opts.certFile, opts.keyPEMFile)
	case opts.selfSigned != "":
		log.Printf("ВНИМАНИЕ: самоподписанный сертификат на %s — только для отладки", opts.selfSigned)
		return transport.SelfSignedCertificate(opts.selfSigned)
	default:
		return tls.Certificate{}, errors.New("нужен сертификат: укажи -tls-cert и -tls-key, либо -tls-self-signed для отладки, либо -plain чтобы работать без маскировки")
	}
}

func loadFallback(site, title string) (*fallback.Handler, error) {
	if site != "" {
		return fallback.NewReverseProxy(site)
	}
	if title != "" {
		return fallback.NewStatic(title, "This site is under construction."), nil
	}
	return nil, nil
}

// loadStaticKey достаёт приватный ключ из флага, файла или переменной
// окружения VEIL_SERVER_KEY.
//
// Порядок такой не случайно: ключ во флаге виден в выводе ps любому
// пользователю машины. Для боевой ноды — файл или переменная окружения.
func loadStaticKey(keyStr, keyFile string) (vp1.KeyPair, error) {
	if keyStr == "" {
		keyStr = os.Getenv("VEIL_SERVER_KEY")
	}
	if keyStr == "" && keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return vp1.KeyPair{}, fmt.Errorf("чтение %s: %w", keyFile, err)
		}
		keyStr = strings.TrimSpace(string(raw))
	}
	if keyStr == "" {
		return vp1.KeyPair{}, errors.New("не задан приватный ключ: укажи -key, -key-file или VEIL_SERVER_KEY (сгенерировать: veil-keygen)")
	}

	priv, err := vp1.DecodeKey(strings.TrimSpace(keyStr))
	if err != nil {
		return vp1.KeyPair{}, fmt.Errorf("приватный ключ: %w", err)
	}
	return vp1.KeyPairFromPrivate(priv)
}
