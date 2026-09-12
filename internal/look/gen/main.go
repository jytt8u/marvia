// Команда gen переписывает таблицу тем из look.js в Kotlin для Android.
//
// Запускается через go generate ./internal/look; путь к файлу можно задать
// аргументом. Тест в пакете look следит, что файл на диске совпадает с тем,
// что породил бы генератор сейчас.
package main

import (
	"fmt"
	"os"

	"github.com/jytt8u/marvia/internal/look"
)

func main() {
	out := look.KotlinPath
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.WriteFile(out, []byte(look.Kotlin()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
	fmt.Println("таблица тем записана:", out)
}
