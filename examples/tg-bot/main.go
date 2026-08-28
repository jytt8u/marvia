// Пример телеграм-бота, который продаёт доступ.
//
// Это не часть платформы и не то, что мы обещаем поддерживать. Бота продавец
// пишет сам: у каждого свои тарифы, свой приём денег, свой разговор с
// покупателем — и своя ответственность за всё это. Наше дело — панель и API,
// как и у остальных панелей.
//
// Здесь показано, как этим API пользоваться правильно, потому что три вещи в
// нём неочевидны и стоят продавцу денег:
//
//   - покупатель ищется по external_id, и повторная продажа тому же ключу
//     ничего не создаёт заново;
//   - срок считает панель, а не бот: extend_by прибавляет к остатку, поэтому
//     продливший заранее ничего не теряет, а два одновременных платежа не
//     затирают друг друга;
//   - номер платежа уходит в Idempotency-Key, и повтор уведомления от
//     платёжной системы не выдаёт второй срок за одни деньги.
//
// Копируй каталог целиком и переделывай под себя. Ключ панели нужен с правом
// users — админский токен боту не давать.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// version подставляется при сборке: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	configPath := flag.String("config", "veil-json", "файл настроек")
	statePath := flag.String("state", "", "файл отметок об отправленных напоминаниях (по умолчанию — рядом с настройками)")
	example := flag.Bool("example", false, "напечатать образец файла настроек и выйти")
	showVersion := flag.Bool("version", false, "показать версию и выйти")

	flag.Parse()

	if *showVersion {
		fmt.Println("marvia-bot", version)
		return
	}
	if *example {
		fmt.Print(ExampleConfig)
		return
	}

	if err := run(*configPath, *statePath); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath, statePath string) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}

	if statePath == "" {
		statePath = StatePath(dirOf(configPath))
	}
	state, err := OpenState(statePath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("marvia-bot %s работает: панель %s, тарифов %d, оплата %s",
		version, cfg.Panel, len(cfg.Tariffs), cfg.Payment)

	return New(cfg, state).Run(ctx)
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
