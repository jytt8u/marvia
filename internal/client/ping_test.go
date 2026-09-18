package client_test

import (
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// Выбор сервера и отклик — независимые показатели.
func TestChoiceIsMadeByFullTimeNotByPing(t *testing.T) {
	near := client.Measurement{RTT: 20 * time.Millisecond, Fetch: 3 * time.Second}
	far := client.Measurement{RTT: 40 * time.Millisecond, Fetch: 300 * time.Millisecond}
	if near.Cost() <= far.Cost() || near.PingMS() >= far.PingMS() {
		t.Fatal("перепутаны отклик и стоимость")
	}
}

func TestPingNeverFallsBackToSetupOrFetch(t *testing.T) {
	m := client.Measurement{Connect: 100 * time.Millisecond, Latency: 600 * time.Millisecond, Fetch: time.Second}
	if m.PingMS() != 0 {
		t.Fatal("показан не измеренный отклик")
	}
	m.RTT = 42 * time.Millisecond
	if m.PingMS() != 42 {
		t.Fatal("отклик заменён другим показателем")
	}
	m.RTT = time.Nanosecond
	if m.PingMS() != 1 {
		t.Fatal("быстрый отклик принят за отсутствие замера")
	}
}
