// Команда veil-panel — управляющий слой платформы.
//
// Через неё продавец заводит подписчиков, продлевает и отключает их, а ноды
// забирают свои списки и сдают статистику. Пользовательский трафик через
// панель не идёт никогда: её домен светится только в момент обновления
// подписки, и это делает её сравнительно живучей.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/veilproject/veil/internal/panel"
)

const shutdownGrace = 10 * time.Second

type options struct {
	listen     string
	dbPath     string
	adminToken string
	subBase    string
	distDir    string

	certFile string
	keyFile  string

	acmeDomain string
	acmeEmail  string
	acmeCache  string
	acmeHTTP   string
}

func main() {
	var opts options

	flag.StringVar(&opts.listen, "listen", "127.0.0.1:8080", "адрес HTTP-интерфейса")
	flag.StringVar(&opts.dbPath, "db", "veil-panel.db", "файл базы данных")
	flag.StringVar(&opts.adminToken, "admin-token", "", "админский токен (по умолчанию — из VEIL_ADMIN_TOKEN)")
	flag.StringVar(&opts.subBase, "sub-base", "", "внешний адрес панели для ссылок подписки, например https://sub.example.com")
	flag.StringVar(&opts.distDir, "dist", "dist", "каталог с бинарниками ноды: панель раздаёт их установщику")

	flag.StringVar(&opts.certFile, "tls-cert", "", "файл сертификата PEM (без него — голый HTTP за обратным прокси)")
	flag.StringVar(&opts.keyFile, "tls-key", "", "файл приватного ключа сертификата PEM")

	flag.StringVar(&opts.acmeDomain, "acme-domain", "", "получать сертификат самой на этот домен")
	flag.StringVar(&opts.acmeEmail, "acme-email", "", "почта для писем Let's Encrypt об истечении")
	flag.StringVar(&opts.acmeCache, "acme-cache", "acme", "каталог для полученных сертификатов")
	flag.StringVar(&opts.acmeHTTP, "acme-http", ":80", "адрес для ответов на проверку ACME (пусто — не поднимать)")

	newToken := flag.Bool("new-token", false, "выпустить админский токен и выйти")

	flag.Parse()

	if *newToken {
		token, err := panel.NewToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(token)
		return
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	if opts.adminToken == "" {
		opts.adminToken = os.Getenv("VEIL_ADMIN_TOKEN")
	}
	if opts.adminToken == "" {
		return errors.New("не задан админский токен: укажи -admin-token или VEIL_ADMIN_TOKEN (выпустить: veil-panel -new-token)")
	}
	if len(strings.TrimSpace(opts.adminToken)) < 16 {
		return errors.New("админский токен слишком короткий: нужен хотя бы 16 символов, лучше выпустить через -new-token")
	}
	if (opts.certFile == "") != (opts.keyFile == "") {
		return errors.New("-tls-cert и -tls-key задаются только вместе")
	}
	if opts.acmeDomain != "" && opts.certFile != "" {
		return errors.New("-acme-domain и -tls-cert вместе не работают: сертификат либо получает панель, либо даёшь его ты")
	}

	store, err := panel.Open(opts.dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	api := panel.NewAPI(store, opts.adminToken, opts.subBase, opts.distDir)
	server := &http.Server{
		Addr:              opts.listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Printf("завершаем работу")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("veil-panel слушает %s, база %s", opts.listen, opts.dbPath)
	if opts.subBase == "" {
		log.Printf("ВНИМАНИЕ: не задан -sub-base, ссылки подписки будут с заглушкой вместо адреса")
	}

	switch {
	case opts.acmeDomain != "":
		return serveACME(server, opts)

	case opts.certFile != "":
		log.Printf("панель отдаёт HTTPS, сертификат %s", opts.certFile)
		if err := server.ListenAndServeTLS(opts.certFile, opts.keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTPS-сервер: %w", err)
		}
		return nil

	default:
		// Панель обязана стоять за HTTPS: по её API ходят токены, а в ответах
		// уезжают приватные ключи подписчиков. Клиент это и не обсуждает —
		// адрес подписки в ссылке доступа он всегда читает как https.
		log.Printf("панель отдаёт голый HTTP: ставь её только за обратным прокси с TLS")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP-сервер: %w", err)
		}
		return nil
	}
}

// serveACME поднимает панель с сертификатом, который она получает сама.
//
// Своё ACME появилось не от недоверия к certbot, а потому что продавец,
// поднявший панель на отдельной машине, не должен ради одной подписки
// осваивать ещё и nginx с расписанием обновления. Сертификат тут расходник:
// берётся при первом обращении и продлевается сам, без чужих служб.
func serveACME(server *http.Server, opts options) error {
	manager := &autocert.Manager{
		// Хранилище обязано переживать перезапуск. Без него каждый рестарт
		// заказывал бы новый сертификат, а у Let's Encrypt на домен всего пять
		// штук в неделю: после пятого перезапуска панель осталась бы без
		// сертификата на неделю, а вместе с ней все покупатели — без
		// обновления списка нод.
		Cache:      autocert.DirCache(opts.acmeCache),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(opts.acmeDomain),
		Email:      opts.acmeEmail,
	}

	server.TLSConfig = manager.TLSConfig()
	server.TLSConfig.MinVersion = tls.VersionTLS12

	if opts.acmeHTTP != "" {
		// Let's Encrypt проверяет владение доменом одним из двух способов: по
		// http на 80-м порту или по TLS на 443-м. Второй обычно занят нодой, а
		// нода на незнакомого гостя отвечает сайтом прикрытия — проверка уйдёт
		// в Samsung и не вернётся. Поэтому держим 80-й.
		go func() {
			challenge := &http.Server{
				Addr:              opts.acmeHTTP,
				Handler:           manager.HTTPHandler(nil),
				ReadHeaderTimeout: 10 * time.Second,
			}
			if err := challenge.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("ВНИМАНИЕ: не занять %s для проверки ACME: %v", opts.acmeHTTP, err)
				log.Printf("пока порт занят, сертификат получить не выйдет")
			}
		}()
	}

	log.Printf("панель берёт сертификат сама: домен %s, хранилище %s", opts.acmeDomain, opts.acmeCache)
	log.Printf("первое обращение по https займёт несколько секунд — в этот момент заказывается сертификат")

	if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTPS-сервер: %w", err)
	}
	return nil
}
