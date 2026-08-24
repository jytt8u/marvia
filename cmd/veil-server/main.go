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
	"github.com/veilproject/veil/internal/nodesync"
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
	listenAddr      string
	keyStr          string
	keyFile         string
	usersFile       string
	panelURL        string
	panelToken      string
	usageFile       string
	certFile        string
	keyPEMFile      string
	selfSigned      string
	realityDest     string
	realitySNI      string
	realityKey      string
	realityShortIDs string
	wsPath          string
	plain           bool
	coverSite       string
	coverTitle      string
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

	flag.StringVar(&opts.realityDest, "reality-dest", "", "настоящий сайт для маскировки REALITY, например www.samsung.com:443")
	flag.StringVar(&opts.realitySNI, "reality-sni", "", "имена в SNI через запятую (по умолчанию — хост из -reality-dest)")
	flag.StringVar(&opts.realityKey, "reality-key", "", "приватный ключ REALITY в base64 (по умолчанию — из VEIL_REALITY_KEY)")
	flag.StringVar(&opts.realityShortIDs, "reality-short-id", "", "короткие идентификаторы клиентов через запятую, шестнадцатеричные")

	flag.StringVar(&opts.wsPath, "ws-path", "", "путь туннеля WebSocket, например /assets/app.js (режим для работы за CDN)")

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
	switch {
	case opts.wsPath != "":
		ln, err = listenWS(ln, opts, cover)
		if err != nil {
			return err
		}
	case opts.plain:
		// маскировки нет
	case opts.realityDest != "":
		ln, err = listenReality(ln, opts)
		if err != nil {
			return err
		}
	default:
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
	if cover == nil && opts.realityDest == "" && opts.wsPath == "" {
		log.Printf("ВНИМАНИЕ: сайт-прикрытие не задан — неопознанные соединения будут рваться, что заметно сканерам")
	}
	if cover == nil && opts.realityDest != "" {
		// Под REALITY прикрытие обеспечивает сам транспорт: чужой гость
		// уходит на настоящий сайт, не доходя до нашего кода. Флаг -cover
		// здесь нужен только для редкого случая, когда клиент прошёл
		// проверку REALITY, но говорит внутри что-то неопознанное.
		log.Printf("прикрытие обеспечивает REALITY: неопознанные гости уходят на %s", opts.realityDest)
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

// listenWS поднимает транспорт WebSocket — режим для работы за CDN.
//
// Смысл режима не в самом WebSocket, а в том, что настоящий адрес ноды
// перестаёт существовать для цензора: пользователь подключается к адресам
// CDN, за которыми живут миллионы обычных сайтов. Ковровая блокировка
// диапазонов хостингов такую ноду не задевает.
//
// Сертификат нужен не всегда. CDN сам занимается TLS перед пользователем, а
// до ноды может ходить как по HTTPS, так и по обычному HTTP — второе выбирают,
// когда нода стоит в закрытом периметре или туннеле до CDN.
func listenWS(inner net.Listener, opts serverOptions, cover *fallback.Handler) (net.Listener, error) {
	if opts.realityDest != "" {
		return nil, errors.New("-ws-path и -reality-dest вместе не работают: CDN расшифровывает TLS у себя, и REALITY через него невозможен")
	}

	cfg := transport.WSConfig{Path: opts.wsPath}
	if cover != nil {
		cfg.Cover = cover.Handler()
	}

	switch {
	case opts.plain:
		log.Printf("транспорт WebSocket на пути %s, без TLS — только за CDN или обратным прокси", opts.wsPath)
	default:
		cert, err := loadCertificate(opts)
		if err != nil {
			return nil, err
		}
		cfg.Certificate = &cert
		log.Printf("транспорт WebSocket на пути %s, поверх TLS", opts.wsPath)
	}

	listener, err := transport.ListenWS(inner, cfg)
	if err != nil {
		return nil, fmt.Errorf("транспорт WebSocket: %w", err)
	}
	if cover == nil {
		log.Printf("ВНИМАНИЕ: сайт-прикрытие не задан — на все прочие пути нода отвечает 404, что заметно при переборе")
	}
	return listener, nil
}

// listenReality поднимает маскировку REALITY.
//
// В этом режиме нода не предъявляет собственный сертификат: для всех, кто не
// доказал право входа, она становится прозрачным зеркалом чужого сайта. Свой
// домен и сертификат при этом не нужны вовсе.
func listenReality(inner net.Listener, opts serverOptions) (net.Listener, error) {
	if opts.certFile != "" || opts.selfSigned != "" {
		return nil, errors.New("-reality-dest и обычный TLS-сертификат вместе не работают: выбери что-то одно")
	}

	keyStr := opts.realityKey
	if keyStr == "" {
		keyStr = os.Getenv("VEIL_REALITY_KEY")
	}
	if keyStr == "" {
		return nil, errors.New("не задан ключ REALITY: укажи -reality-key или VEIL_REALITY_KEY (выпустить: veil-keygen -reality)")
	}
	key, err := vp1.DecodeKey(strings.TrimSpace(keyStr))
	if err != nil {
		return nil, fmt.Errorf("ключ REALITY: %w", err)
	}

	names := splitList(opts.realitySNI)
	if len(names) == 0 {
		// Умолчание безопасное: имя берём из самого адреса прикрытия, ровно
		// то, что клиент и напишет в SNI.
		host, _, splitErr := net.SplitHostPort(opts.realityDest)
		if splitErr != nil {
			return nil, fmt.Errorf("не разобрать -reality-dest %q: %w", opts.realityDest, splitErr)
		}
		names = []string{host}
	}

	listener, err := transport.ListenReality(inner, transport.RealityConfig{
		Dest:        opts.realityDest,
		ServerNames: names,
		PrivateKey:  key,
		ShortIDs:    splitList(opts.realityShortIDs),
	})
	if err != nil {
		return nil, fmt.Errorf("маскировка REALITY: %w", err)
	}

	pair, err := vp1.KeyPairFromPrivate(key)
	if err != nil {
		return nil, err
	}
	log.Printf("маскировка REALITY: прикрытие %s, SNI %s", opts.realityDest, strings.Join(names, ", "))
	log.Printf("публичный ключ REALITY (pbk для клиентов): %s", vp1.EncodeKey(pair.Public))

	return listener, nil
}

// splitList разбирает список, заданный через запятую.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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
