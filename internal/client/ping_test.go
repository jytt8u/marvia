package client_test

import (
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// Число рядом со страной человек читает как пинг и сравнивает с другими
// клиентами. Показывали сумму — круг, TLS, VP1 и пробную порцию — и выглядели
// втрое медленнее, чем есть на самом деле: 631 мс там, где у чужого клиента
// на том же телефоне 249.
//
// При этом выбирать ноду по одному кругу нельзя: заблокированная нода охотно
// принимает TCP и роняет всё дальше. Поэтому круг — для показа, полное время —
// для выбора, и эти два числа не должны снова слиться в одно.

func TestChoiceIsMadeByFullTimeNotByPing(t *testing.T) {
	// Ближняя нода с быстрым кругом, но медленной отдачей: её заблокировали,
	// и она тянет.
	near := client.Measurement{Connect: 20 * time.Millisecond, Fetch: 3 * time.Second}
	// Дальняя: круг вдвое длиннее, а работает.
	far := client.Measurement{Connect: 40 * time.Millisecond, Fetch: 300 * time.Millisecond}

	if near.Cost() <= far.Cost() {
		t.Fatalf("выбор пошёл бы на подтормаживающую ноду: %v против %v", near.Cost(), far.Cost())
	}
	if near.Connect >= far.Connect {
		t.Fatal("подготовка теста неверна: круг ближней должен быть короче")
	}
}

func TestPingIsTheNetworkRoundTripOnly(t *testing.T) {
	m := client.Measurement{
		Connect: 34 * time.Millisecond,
		Latency: 120 * time.Millisecond,
		Fetch:   631 * time.Millisecond,
	}

	if m.Connect >= m.Latency {
		t.Fatal("круг обязан быть короче полного прогрева: в прогреве ещё TLS и VP1")
	}
	// Ровно та подмена, из-за которой всё и затевалось.
	if m.Cost() == m.Connect {
		t.Fatal("стоимость и круг совпали — значит, показ снова считает их одним числом")
	}
}

// Где круг не измерен — за CDN и по QUIC, — показывать всё равно есть что.
func TestWithoutARoundTripThereIsStillANumber(t *testing.T) {
	m := client.Measurement{Latency: 200 * time.Millisecond}

	if m.Connect != 0 {
		t.Fatal("круг взялся ниоткуда")
	}
	if m.Cost() == 0 {
		t.Fatal("без круга не осталось ни одного числа для показа")
	}
}
