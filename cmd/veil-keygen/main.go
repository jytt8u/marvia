// Команда veil-keygen печатает новую пару статических ключей.
//
// Приватный ключ никуда не отправляется и нигде не сохраняется — он только
// выводится на экран. Что с ним делать дальше, решаешь ты.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/veilproject/veil/internal/vp1"
)

func main() {
	quiet := flag.Bool("quiet", false, "печатать только две строки: приватный и публичный ключ")
	forReality := flag.Bool("reality", false, "пояснения под ключ REALITY")
	flag.Parse()

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
		fmt.Println("Приватный ключ — ноде:      -reality-key или VEIL_REALITY_KEY")
		fmt.Println("Публичный ключ — клиентам:  параметр pbk в ссылке")
		fmt.Println()
		fmt.Println("Это обычная пара X25519, совместимая с тем, что выдаёт xray x25519.")
		return
	}

	fmt.Println("Приватный ключ держи на своей машине. Публичный — это то, что")
	fmt.Println("нужно знать другой стороне, чтобы вообще начать разговор.")
}
