// Команда veil-bot — телеграм-бот, который продаёт доступ.
//
// Продавец заполняет один файл настроек и запускает бинарник. Своей базы у
// бота нет: покупатели, сроки и расход живут в панели, а бот ходит к ней по
// ключу с правом users. Поэтому потеря сервера с ботом не теряет ни одного
// покупателя — достаточно поднять его заново с тем же файлом настроек.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/veilproject/veil/internal/bot"
)

// version подставляется при сборке: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	configPath := flag.String("config", "veil-bot.json", "файл настроек")
	statePath := flag.String("state", "", "файл отметок об отправленных напоминаниях (по умолчанию — рядом с настройками)")
	example := flag.Bool("example", false, "напечатать образец файла настроек и выйти")
	showVersion := flag.Bool("version", false, "показать версию и выйти")

	flag.Parse()

	if *showVersion {
		fmt.Println("veil-bot", version)
		return
	}
	if *example {
		fmt.Print(bot.ExampleConfig)
		return
	}

	if err := run(*configPath, *statePath); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath, statePath string) error {
	cfg, err := bot.LoadConfig(configPath)
	if err != nil {
		return err
	}

	if statePath == "" {
		statePath = bot.StatePath(dirOf(configPath))
	}
	state, err := bot.OpenState(statePath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("veil-bot %s работает: панель %s, тарифов %d, оплата %s",
		version, cfg.Panel, len(cfg.Tariffs), cfg.Payment)

	return bot.New(cfg, state).Run(ctx)
}

// dirOf — каталог файла настроек, чтобы состояние легло рядом с ним.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
