package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WebSocket нужен не ради самого себя, а ради CDN.
//
// В России блокируют не протокол, а адрес: под ковровую блокировку диапазонов
// хостингов попадает и безупречно замаскированная нода, потому что её никто не
// разглядывал. Единственный способ спрятать адрес — не показывать его вовсе:
// пользователь подключается к адресам CDN, за которыми живут миллионы обычных
// сайтов, и заблокировать такой диапазон целиком означает уронить пол-интернета.
//
// Ограничение, из которого всё следует: CDN расшифровывает TLS у себя. Поэтому
// REALITY через CDN невозможен физически — ему нужен прямой TCP до ноды. Через
// CDN идёт обычный TLS, а внутри него WebSocket.
//
// Отсюда же вытекает, что CDN видит содержимое соединения. Здесь окупается
// слоёная архитектура: внутри WebSocket работает VP1 со своим шифрованием, и
// CDN достаётся поток, который он прочитать не может.
//
// Итого у ноды два режима под две разные угрозы:
//
//	REALITY на чистом адресе — трафик неотличим от чужого сайта, адрес виден
//	WebSocket за CDN       — адреса для цензора не существует, камуфляж слабее

const (
	// wsHandshakeTimeout — сколько ждём завершения переговоров.
	wsHandshakeTimeout = 20 * time.Second

	// wsAcceptQueue — очередь принятых соединений между HTTP-сервером и
	// вызывающим Accept.
	wsAcceptQueue = 64
)

// WSConfig описывает серверную сторону.
type WSConfig struct {
	// Path — путь, на котором живёт туннель. Всё остальное уходит в Cover.
	//
	// Путь стоит делать неочевидным: перебор популярных вроде /ws или /vless
	// по всем адресам хостера — дешёвый способ найти ноду.
	Path string

	// Certificate — сертификат для TLS. Пусто означает голый HTTP: так ноду
	// ставят за CDN, который сам занимается TLS перед пользователем.
	Certificate *tls.Certificate

	// Cover — что отдавать на все остальные пути. Обычно обратный прокси на
	// настоящий сайт.
	Cover http.Handler
}

// ListenWS поднимает HTTP-сервер и отдаёт слушатель, из которого выходят
// соединения, поднятые до WebSocket.
func ListenWS(inner net.Listener, cfg WSConfig) (net.Listener, error) {
	path := cfg.Path
	if path == "" {
		return nil, errors.New("не задан путь для WebSocket")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	cover := cfg.Cover
	if cover == nil {
		cover = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "404 page not found", http.StatusNotFound)
		})
	}

	listener := &wsListener{
		addr:  inner.Addr(),
		conns: make(chan net.Conn, wsAcceptQueue),
		done:  make(chan struct{}),
	}

	listener.server = &http.Server{
		Handler:           listener.handler(path, cover),
		ReadHeaderTimeout: wsHandshakeTimeout,
	}

	serving := inner
	if cfg.Certificate != nil {
		serving = tls.NewListener(inner, &tls.Config{
			Certificates: []tls.Certificate{*cfg.Certificate},
			MinVersion:   tls.VersionTLS12,
			NextProtos:   serverALPN,
		})
	}

	go func() {
		_ = listener.server.Serve(serving)
		listener.Close()
	}()

	return listener, nil
}

type wsListener struct {
	addr   net.Addr
	conns  chan net.Conn
	server *http.Server

	once sync.Once
	done chan struct{}
}

func (l *wsListener) handler(path string, cover http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Путь туннеля не должен выделяться среди прочих. Библиотека на
		// запрос без переговоров отвечает 426 Upgrade Required, а обычные
		// сайты так не отвечают никогда: перебрав пути, ноду нашли бы по
		// одному этому коду. Поэтому всё, что не похоже на переговоры о
		// WebSocket, уходит на сайт-прикрытие вместе с остальными путями.
		if r.URL.Path != path || !wantsWebSocket(r) {
			cover.ServeHTTP(w, r)
			return
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Проверка Origin здесь бессмысленна: к нам приходит не браузер,
			// а туннельный клиент, и заголовок он поставит любой.
			InsecureSkipVerify: true,
		})
		if err != nil {
			// Кто-то постучался в наш путь без переговоров о WebSocket.
			// Ответ уже отправлен библиотекой.
			return
		}

		conn := newWSConn(ws)

		select {
		case l.conns <- conn:
		case <-l.done:
			_ = conn.Close()
			return
		}

		// Обработчик обязан жить, пока живо соединение. На HTTP/1.1 сокет
		// перехвачен и вернуться было бы можно, а на HTTP/2 возврат из
		// обработчика закрыл бы поток прямо под туннелем.
		<-conn.closed
	})
}

func (l *wsListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *wsListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.server.Close()
	})
	return nil
}

func (l *wsListener) Addr() net.Addr { return l.addr }

// wsConn — соединение поверх WebSocket.
type wsConn struct {
	net.Conn
	ws     *websocket.Conn
	closed chan struct{}
	once   sync.Once
}

// newWSConn заворачивает соединение WebSocket в net.Conn.
func newWSConn(ws *websocket.Conn) *wsConn {
	return &wsConn{
		Conn:   websocket.NetConn(context.Background(), ws, websocket.MessageBinary),
		ws:     ws,
		closed: make(chan struct{}),
	}
}

// Close рвёт соединение, не разводя церемоний.
//
// Вежливое закрытие WebSocket отправляет закрывающий кадр и ждёт ответного —
// до пяти секунд. Для браузера это правильно, для прокси разорительно: на
// каждое закрытое соединение висела бы горутина, а закрываются они постоянно.
// Обрыв без прощания здесь честнее и ведёт себя как обычный TCP.
func (c *wsConn) Close() error {
	err := c.ws.CloseNow()
	c.once.Do(func() { close(c.closed) })
	return err
}

// CloseWrite закрывает соединение целиком.
//
// У WebSocket нет полузакрытия: закрывающий кадр завершает разговор в обе
// стороны. Для нас это приемлемо — до этого места код доходит, когда цель уже
// договорила, — но метод объявлен явно, чтобы вызывающий не думал, будто
// половинное закрытие тут работает.
func (c *wsConn) CloseWrite() error { return c.Close() }

// WSDialConfig описывает клиентскую сторону.
type WSDialConfig struct {
	// Host — доменное имя в адресе и в SNI. За CDN это имя, которое ведёт
	// на CDN, а не адрес самой ноды.
	Host string

	// Path — путь туннеля, тот же, что на ноде.
	Path string

	// TLS — параметры маскировки. Отпечаток Chrome нужен и здесь: через CDN
	// идёт обычный TLS, и по нему нас можно опознать.
	TLS ClientConfig

	// Plain отключает TLS. Только для отладки: за CDN пользовательский TLS
	// заканчивается на CDN, но до него он обязателен.
	Plain bool
}

// DialWS открывает соединение до ноды через WebSocket.
//
// Адрес и имя разделены намеренно: подключаемся к addr (адрес CDN), а имя в
// SNI, в заголовке Host и в адресе запроса берём из Host. Именно это и прячет
// настоящий адрес ноды — в конфиге его нет.
func DialWS(ctx context.Context, addr string, cfg WSDialConfig) (net.Conn, error) {
	if cfg.Host == "" {
		return nil, errors.New("не задано имя хоста для WebSocket")
	}
	path := cfg.Path
	if path == "" {
		return nil, errors.New("не задан путь для WebSocket")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	scheme := "wss"
	if cfg.Plain {
		scheme = "ws"
	}
	endpoint := (&url.URL{Scheme: scheme, Host: cfg.Host, Path: path}).String()

	tlsCfg := cfg.TLS
	if tlsCfg.ServerName == "" {
		tlsCfg.ServerName = cfg.Host
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return Dial(ctx, addr, tlsCfg)
		},
		// Соединение туннельное и живёт долго; пул переиспользования здесь
		// только мешает.
		DisableKeepAlives: true,
	}

	hsCtx, cancel := context.WithTimeout(ctx, wsHandshakeTimeout)
	defer cancel()

	ws, resp, err := websocket.Dial(hsCtx, endpoint, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
		HTTPHeader: browserHeaders(cfg.Host),
	})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("переговоры WebSocket с %s: %w (ответ %s)", endpoint, err, resp.Status)
		}
		return nil, fmt.Errorf("переговоры WebSocket с %s: %w", endpoint, err)
	}

	return newWSConn(ws), nil
}

// browserHeaders добавляет к запросу то, что послал бы обычный браузер.
//
// Отпечатка TLS мало: запрос на повышение до WebSocket без привычных
// заголовков сам по себе выглядит машинным, а на пути к ноде стоит CDN,
// который эти заголовки видит и логирует.
func browserHeaders(host string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", browserUserAgent)
	h.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	h.Set("Cache-Control", "no-cache")
	h.Set("Pragma", "no-cache")
	h.Set("Origin", "https://"+host)
	return h
}

// browserUserAgent должен соответствовать версии Chrome, чей отпечаток
// подделывает uTLS. Расхождение между отпечатком и заголовком — отдельная
// примета: так не бывает у настоящих браузеров.
const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

// wantsWebSocket проверяет, действительно ли гость просит поднять WebSocket.
//
// Заголовок Connection может нести несколько значений через запятую, поэтому
// сравнивать его целиком нельзя.
func wantsWebSocket(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}
