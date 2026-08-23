// Команда veil-panel — управляющий слой платформы.
//
// Через неё продавец заводит подписчиков, продлевает и отключает их, а ноды
// забирают свои списки и сдают статистику. Пользовательский трафик через
// панель не идёт никогда: её домен светится только в момент обновления
// подписки, и это делает её сравнительно живучей.
package main

import (
	"context"
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

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "адрес HTTP-интерфейса")
	dbPath := flag.String("db", "veil-panel.db", "файл базы данных")
	adminToken := flag.String("admin-token", "", "админский токен (по умолчанию — из VEIL_ADMIN_TOKEN)")
	subBase := flag.String("sub-base", "", "внешний адрес панели для ссылок подписки, например https://sub.example.com")
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

	if err := run(*listen, *dbPath, *adminToken, *subBase); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(listen, dbPath, adminToken, subBase string) error {
	if adminToken == "" {
		adminToken = os.Getenv("VEIL_ADMIN_TOKEN")
	}
	if adminToken == "" {
		return errors.New("не задан админский токен: укажи -admin-token или VEIL_ADMIN_TOKEN (выпустить: veil-panel -new-token)")
	}
	if len(strings.TrimSpace(adminToken)) < 16 {
		return errors.New("админский токен слишком короткий: нужен хотя бы 16 символов, лучше выпустить через -new-token")
	}

	store, err := panel.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	api := panel.NewAPI(store, adminToken, subBase)
	server := &http.Server{
		Addr:              listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
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

	log.Printf("veil-panel слушает %s, база %s", listen, dbPath)
	if subBase == "" {
		log.Printf("ВНИМАНИЕ: не задан -sub-base, ссылки подписки будут с заглушкой вместо адреса")
	}
	// Панель обязана стоять за HTTPS: по её API ходят токены, а в ответах
	// уезжают приватные ключи подписчиков. Собственного TLS у неё нет
	// намеренно — сертификатами занимается обратный прокси перед ней.
	log.Printf("панель отдаёт голый HTTP: ставь её только за обратным прокси с TLS")

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP-сервер: %w", err)
	}
	return nil
}
