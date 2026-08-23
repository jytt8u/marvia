// Package fallback отдаёт настоящий сайт тем, кто пришёл не по нашему протоколу.
//
// Это ключевая часть защиты от активного зондирования. Сканер китайского GFW
// работает так: находит сервер с TLS на 443, шлёт в него разный мусор и
// смотрит на реакцию. Обычный сайт отвечает страницей или ошибкой HTTP.
// Прокси, который на непонятный запрос молча рвёт соединение, ведёт себя
// иначе — и этого достаточно, чтобы IP уехал в блок-лист.
//
// Поэтому неопознанный гость должен получить не разрыв, а сайт. Настоящий.
package fallback

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 30 * time.Second
)

// Handler обслуживает соединения, не прошедшие аутентификацию.
type Handler struct {
	handler http.Handler
}

// NewReverseProxy проксирует запросы на настоящий сайт.
//
// Лучший вариант прикрытия: за нодой стоит живой сайт с осмысленным
// содержимым. Чем он обыденнее, тем лучше — сканер должен уйти, ничего не
// заподозрив.
func NewReverseProxy(target string) (*Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("адрес сайта-прикрытия %q: %w", target, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("адрес сайта-прикрытия должен начинаться с http:// или https://, получено %q", target)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			// Host должен быть хостом бэкенда, иначе многие сайты отдадут
			// либо чужую виртуалку, либо редирект, и прикрытие развалится.
			r.Out.Host = u.Host
			// Заголовки X-Forwarded-* не добавляем: они прямо сообщают
			// бэкенду и всем по дороге, что перед ним прокси.
			r.Out.Header.Del("X-Forwarded-For")
			r.Out.Header.Del("X-Forwarded-Host")
			r.Out.Header.Del("X-Forwarded-Proto")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			// Сайт-прикрытие недоступен. Отдаём обычную 502 — так же, как
			// повёл бы себя настоящий сервер с упавшим бэкендом.
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return &Handler{handler: proxy}, nil
}

// NewStatic отдаёт фиксированную страницу.
//
// Запасной вариант на случай, когда живого сайта под рукой нет. Он хуже
// обратного прокси: одна и та же страница на всех путях выглядит странно,
// если сканер запросит несколько адресов.
func NewStatic(title, body string) *Handler {
	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title></head>
<body><h1>%s</h1><p>%s</p></body>
</html>
`, title, title, body)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	return &Handler{handler: mux}
}

// Serve обслуживает уже принятое соединение как обычный HTTP-сервер.
func (h *Handler) Serve(conn net.Conn) {
	listener := newOneShot(conn.LocalAddr())

	server := &http.Server{
		Handler:           h.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		// Своих логов не пишем: чужие сканеры не должны раздувать наш журнал.
		ErrorLog: nil,
	}

	listener.offer(&notifyConn{Conn: conn, onClose: listener.Close})
	_ = server.Serve(listener)
}

// oneShot — слушатель ровно на одно уже существующее соединение.
// net/http умеет работать только со слушателем, а у нас на руках готовый
// сокет; это переходник между двумя интерфейсами.
type oneShot struct {
	ch   chan net.Conn
	addr net.Addr
	once sync.Once
}

func newOneShot(addr net.Addr) *oneShot {
	return &oneShot{ch: make(chan net.Conn, 1), addr: addr}
}

func (l *oneShot) offer(c net.Conn) { l.ch <- c }

func (l *oneShot) Accept() (net.Conn, error) {
	c, ok := <-l.ch
	if !ok {
		return nil, net.ErrClosed
	}
	return c, nil
}

func (l *oneShot) Close() error {
	l.once.Do(func() { close(l.ch) })
	return nil
}

func (l *oneShot) Addr() net.Addr { return l.addr }

// notifyConn закрывает слушатель вместе с соединением, чтобы http.Serve
// вышел из цикла Accept, а не висел вечно.
type notifyConn struct {
	net.Conn
	onClose func() error
	once    sync.Once
}

func (c *notifyConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { _ = c.onClose() })
	return err
}
