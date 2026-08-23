// Команда veil-client — клиентская сторона туннеля.
//
// Поднимает локальный SOCKS5 и на каждое входящее соединение открывает
// туннель до сервера, маскируя его под обычный HTTPS с отпечатком Chrome.
//
// Отдельный туннель на каждое соединение — расточительно: лишний хендшейк на
// каждую картинку на странице. Мультиплексирование — следующий шаг M1.
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/veilproject/veil/internal/relay"
	"github.com/veilproject/veil/internal/socks5"
	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/tunnel"
	"github.com/veilproject/veil/internal/vp1"
)

const dialTimeout = 15 * time.Second

// tunnelDialer знает, как добраться до сервера и представиться.
type tunnelDialer struct {
	serverAddr string
	static     vp1.KeyPair
	serverPub  []byte
	tlsCfg     *transport.ClientConfig // nil — режим -plain, без маскировки
}

func main() {
	listenAddr := flag.String("listen", "127.0.0.1:1080", "адрес локального SOCKS5-инбаунда")
	serverAddr := flag.String("server", "", "адрес сервера host:port")
	serverPub := flag.String("pubkey", "", "публичный ключ сервера в base64")
	clientKey := flag.String("key", "", "приватный ключ клиента в base64 (по умолчанию — временный на время запуска)")

	sni := flag.String("sni", "", "имя домена в SNI (по умолчанию — хост из -server)")
	caFile := flag.String("ca", "", "файл PEM с доверенным сертификатом (для отладки с самоподписанным)")
	insecure := flag.Bool("insecure", false, "не проверять сертификат сервера (только для отладки)")
	plain := flag.Bool("plain", false, "работать без TLS-маскировки (режим M0, для отладки)")

	flag.Parse()

	opts := clientOptions{
		listenAddr: *listenAddr,
		serverAddr: *serverAddr,
		serverPub:  *serverPub,
		clientKey:  *clientKey,
		sni:        *sni,
		caFile:     *caFile,
		insecure:   *insecure,
		plain:      *plain,
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

type clientOptions struct {
	listenAddr string
	serverAddr string
	serverPub  string
	clientKey  string
	sni        string
	caFile     string
	insecure   bool
	plain      bool
}

func run(opts clientOptions) error {
	if opts.serverAddr == "" {
		return errors.New("не задан адрес сервера: укажи -server host:port")
	}

	pubStr := opts.serverPub
	if pubStr == "" {
		pubStr = os.Getenv("VEIL_SERVER_PUBKEY")
	}
	if pubStr == "" {
		return errors.New("не задан публичный ключ сервера: укажи -pubkey или VEIL_SERVER_PUBKEY")
	}
	serverPub, err := vp1.DecodeKey(strings.TrimSpace(pubStr))
	if err != nil {
		return fmt.Errorf("публичный ключ сервера: %w", err)
	}

	static, ephemeral, err := loadClientKey(opts.clientKey)
	if err != nil {
		return err
	}

	dialer := &tunnelDialer{serverAddr: opts.serverAddr, static: static, serverPub: serverPub}
	if !opts.plain {
		tlsCfg, err := buildTLSConfig(opts)
		if err != nil {
			return err
		}
		dialer.tlsCfg = tlsCfg
	}

	pool := tunnel.NewPool(dialer.dial, 0, 0)
	defer pool.Close()

	ln, err := net.Listen("tcp", opts.listenAddr)
	if err != nil {
		return fmt.Errorf("прослушивание %s: %w", opts.listenAddr, err)
	}
	defer ln.Close()

	log.Printf("veil-client: SOCKS5 на %s, сервер %s", ln.Addr(), opts.serverAddr)
	log.Printf("публичный ключ клиента: %s", vp1.EncodeKey(static.Public))
	if ephemeral {
		log.Printf("(ключ временный — при перезапуске сменится; для постоянного укажи -key)")
	}
	if dialer.tlsCfg == nil {
		log.Printf("ВНИМАНИЕ: режим -plain, маскировки нет — трафик опознаётся DPI")
	} else {
		log.Printf("маскировка: TLS, SNI %s, отпечаток Chrome", dialer.tlsCfg.ServerName)
		if dialer.tlsCfg.InsecureSkipVerify {
			log.Printf("ВНИМАНИЕ: проверка сертификата отключена — соединение можно перехватить")
		}
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept: %v", err)
			continue
		}
		go serve(conn, pool, opts.serverAddr)
	}
}

func buildTLSConfig(opts clientOptions) (*transport.ClientConfig, error) {
	name := opts.sni
	if name == "" {
		host, _, err := net.SplitHostPort(opts.serverAddr)
		if err != nil {
			return nil, fmt.Errorf("разбор адреса сервера: %w", err)
		}
		name = host
	}
	// SNI с голым IP — сильный признак: настоящие браузеры так почти не ходят.
	if ip := net.ParseIP(name); ip != nil && !opts.insecure {
		log.Printf("ВНИМАНИЕ: в SNI указан IP-адрес, а не домен — для DPI это выглядит необычно, задай -sni")
	}

	cfg := &transport.ClientConfig{
		ServerName:         name,
		InsecureSkipVerify: opts.insecure,
	}

	if opts.caFile != "" {
		pem, err := os.ReadFile(opts.caFile)
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", opts.caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("в %s не нашлось ни одного сертификата", opts.caFile)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

func serve(local net.Conn, pool *tunnel.Pool, serverAddr string) {
	defer local.Close()

	addr, err := socks5.Handshake(local)
	if err != nil {
		log.Printf("socks5: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	// Поток внутри уже существующей сессии, а не новое соединение до ноды.
	stream, err := pool.Open(ctx)
	if err != nil {
		log.Printf("туннель до %s: %v", serverAddr, err)
		_ = socks5.ReplyFailure(local, err)
		return
	}
	defer stream.Close()

	if err := vp1.WriteRequest(stream, addr); err != nil {
		log.Printf("запрос на %s: %v", addr, err)
		_ = socks5.ReplyFailure(local, err)
		return
	}

	status, err := vp1.ReadStatus(stream)
	if err != nil {
		log.Printf("ответ сервера по %s: %v", addr, err)
		_ = socks5.ReplyFailure(local, err)
		return
	}
	if status != vp1.StatusOK {
		log.Printf("сервер отказал по %s: %s", addr, vp1.StatusText(status))
		_ = socks5.ReplyFailure(local, errors.New(vp1.StatusText(status)))
		return
	}

	if err := socks5.ReplySuccess(local); err != nil {
		log.Printf("ответ локальному клиенту: %v", err)
		return
	}

	log.Printf("-> %s", addr)
	if err := relay.Bidirectional(local, stream); err != nil {
		log.Printf("-> %s: обрыв: %v", addr, err)
	}
}

func (d *tunnelDialer) dial(ctx context.Context) (net.Conn, error) {
	var (
		raw net.Conn
		err error
	)
	if d.tlsCfg != nil {
		raw, err = transport.Dial(ctx, d.serverAddr, *d.tlsCfg)
	} else {
		var dialer net.Dialer
		raw, err = dialer.DialContext(ctx, "tcp", d.serverAddr)
	}
	if err != nil {
		return nil, err
	}

	tunnel, err := vp1.ClientHandshake(raw, d.static, d.serverPub)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tunnel, nil
}

// loadClientKey берёт ключ клиента из флага или переменной окружения, а если
// его нет — генерирует временный.
//
// Временный ключ удобен для отладки, но у него есть цена: сервер не сможет
// отличить тебя от другого клиента и посчитать твой трафик. В
// многопользовательском режиме ключ станет обязательным.
func loadClientKey(keyStr string) (vp1.KeyPair, bool, error) {
	if keyStr == "" {
		keyStr = os.Getenv("VEIL_CLIENT_KEY")
	}
	if keyStr == "" {
		pair, err := vp1.GenerateKeyPair()
		return pair, true, err
	}
	priv, err := vp1.DecodeKey(strings.TrimSpace(keyStr))
	if err != nil {
		return vp1.KeyPair{}, false, fmt.Errorf("приватный ключ клиента: %w", err)
	}
	pair, err := vp1.KeyPairFromPrivate(priv)
	return pair, false, err
}
