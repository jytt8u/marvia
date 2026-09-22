// Программа только с тем, что попадает в приложение: пакет foreign и ничего
// из тестов. Запускает движок по ссылке и печатает «ok». Её собирает
// TestEngineStartsWithoutTestImports — см. там, зачем.
package main

import (
	"fmt"
	"os"

	"github.com/jytt8u/marvia/internal/foreign"
)

func main() {
	l, err := foreign.Parse(os.Args[1])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	e, err := foreign.Start(l)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = e.Close()
	fmt.Println("ok")
}
