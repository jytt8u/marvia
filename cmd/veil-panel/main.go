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

	"github.com/veilproject/veil/internal/panel"
)

const shutdownGrace = 10 * time.Second

type options struct {
	listen     string
	dbPath     string
	adminToken string
	subBase    string
	certFile   string
	distDir    string
	keyFile    string
}

func main() {
	var opts options

	flag.StringVar(&opts.listen, "listen", "127.0.0.1:8080", "адрес HTTP-интерфейса")
	flag.StringVar(&opts.dbPath, "db", "veil-panel.db", "файл базы данных")
	flag.StringVar(&opts.adminToken, "admin-token", "", "админский токен (по умолчанию — из VEIL_ADMIN_TOKEN)")
	flag.StringVar(&opts.subBase, "sub-base", "", "внешний адрес панели для ссылок подписки, например https://sub.example.com")
	flag.StringVar(&opts.certFile, "tls-cert", "", "файл сертификата PEM (без него — голый HTTP за обратным прокси)")
	flag.StringVar(&opts.keyFile, "tls-key", "", "файл приватного ключа сертификата PEM")
	flag.StringVar(&opts.distDir, "dist", "dist", "каталог с бинарниками ноды: панель раздаёт их установщику")
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

	// Панель обязана стоять за HTTPS: по её API ходят токены, а в ответах
	// уезжают приватные ключи подписчиков. Клиент это и не обсуждает — адрес
	// подписки в ссылке доступа он всегда читает как https.
	//
	// Сертификат можно отдать панели напрямую, а можно оставить обратному
	// прокси. Своё TLS появилось не от недоверия к прокси, а потому что
	// продавец, поднявший панель на отдельной машине, не должен ради одной
	// подписки осваивать ещё и nginx.
	if opts.certFile == "" {
		log.Printf("панель отдаёт голый HTTP: ставь её только за обратным прокси с TLS")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP-сервер: %w", err)
		}
		return nil
	}

	log.Printf("панель отдаёт HTTPS, сертификат %s", opts.certFile)
	if err := server.ListenAndServeTLS(opts.certFile, opts.keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTPS-сервер: %w", err)
	}
	return nil
}
