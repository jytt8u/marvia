// Команда marvia-keygen печатает новую пару статических ключей.
//
// Приватный ключ никуда не отправляется и нигде не сохраняется — он только
// выводится на экран. Что с ним делать дальше, решаешь ты.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/vp1"
)

// version подставляется при сборке: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	quiet := flag.Bool("quiet", false, "печатать только две строки: приватный и публичный ключ")
	forReality := flag.Bool("reality", false, "пояснения под ключ REALITY")
	showVersion := flag.Bool("version", false, "показать версию и выйти")
	forBackup := flag.Bool("backup", false, "пояснения под ключ для шифрования копий базы")

	// Расшифровка копии живёт здесь же: keygen едет в том же архиве, что
	// панель, и запускается где угодно — в том числе на своей машине, куда
	// копию и увозят. Отдельная утилита ради одного действия только
	// добавила бы файл, который надо не потерять.
	openBackup := flag.String("open-backup", "", "расшифровать копию базы: путь к файлу .sealed")
	backupPriv := flag.String("key", "", "приватный ключ для -open-backup (или MARVIA_BACKUP_KEY)")
	flag.Parse()

	if *showVersion {
		fmt.Println("marvia-keygen", version)
		return
	}

	if *openBackup != "" {
		if err := openSealedBackup(*openBackup, *backupPriv); err != nil {
			fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
			os.Exit(1)
		}
		return
	}

	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}

	priv := vp1.EncodeKey(pair.Private)
	pub := vp1.EncodeKey(pair.Public)

	if *quiet {
		fmt.Println(priv)
		fmt.Println(pub)
		return
	}

	fmt.Printf("Приватный ключ: %s\n", priv)
	fmt.Printf("Публичный ключ: %s\n", pub)
	fmt.Println()

	if *forReality {
		// Ключ тот же самый по сути — X25519, — но роли у сторон другие,
		// и путать их дорого: приватный ключ REALITY на ноде и приватный
		// ключ клиента живут в разных местах.
		fmt.Println("Приватный ключ — ноде:      -reality-key или MARVIA_REALITY_KEY")
		fmt.Println("Публичный ключ — клиентам:  параметр pbk в ссылке")
		fmt.Println()
		fmt.Println("Это обычная пара X25519, совместимая с тем, что выдаёт xray x25519.")
		return
	}

	if *forBackup {
		fmt.Println("Публичный ключ — панели:  -backup-key")
		fmt.Println("Приватный ключ — себе:    им открывается копия, и больше ничем")
		fmt.Println()
		fmt.Println("Держи приватный вне сервера: в менеджере паролей, на бумаге, где угодно,")
		fmt.Println("только не рядом с панелью. Панель умеет копию зашифровать и не умеет")
		fmt.Println("открыть — в этом весь смысл. Потеряешь приватный — копии станут мусором.")
		fmt.Println()
		fmt.Println("Открыть копию:  marvia-keygen -open-backup marvia-....sealed -key <приватный>")
		return
	}

	fmt.Println("Приватный ключ держи на своей машине. Публичный — это то, что")
	fmt.Println("нужно знать другой стороне, чтобы вообще начать разговор.")
}

// openSealedBackup расшифровывает копию базы приватным ключом.
//
// Пишет рядом, убрав .sealed: класть расшифрованную базу поверх
// зашифрованной нельзя — одна ошибка в ключе, и человек остаётся без обеих.
func openSealedBackup(path, key string) error {
	if key == "" {
		key = os.Getenv("MARVIA_BACKUP_KEY")
	}
	if key == "" {
		return errors.New("нужен приватный ключ: -key или MARVIA_BACKUP_KEY")
	}
	priv, err := vp1.DecodeKey(key)
	if err != nil {
		return fmt.Errorf("приватный ключ: %w", err)
	}

	sealed, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	plain, err := panel.OpenSealedBackup(sealed, priv)
	if err != nil {
		return err
	}

	out := strings.TrimSuffix(path, ".sealed")
	if out == path {
		out = path + ".db"
	}
	if _, err := os.Stat(out); err == nil {
		return fmt.Errorf("%s уже есть — убери его или переименуй", out)
	}
	if err := os.WriteFile(out, plain, 0o600); err != nil {
		return err
	}

	fmt.Printf("Копия расшифрована: %s\n", out)
	return nil
}
