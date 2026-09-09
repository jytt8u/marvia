package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/vp1"
)

// Выбор ноды по скорости, а не только по задержке.
//
// Повод — живой замер: нода в Дубае отвечала за 150 мс и качала 8 Мбит/с, нода
// в Хельсинки — за 28 мс и 170 Мбит/с. По задержке разница пятикратная, по
// делу — четырнадцатикратная, и человек чувствует вторую. Клиент при этом
// уверенно сравнивал задержки и мог выбрать ту, что еле качает.

// TestSpeedSampleGoesThroughNode — нода отдаёт замер, клиент считает скорость.
func TestSpeedSampleGoesThroughNode(t *testing.T) {
	node := startTestNode(t)

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	took, err := dialer.MeasureFetch(ctx, vp1.DefaultSpeedSample)
	if err != nil {
		t.Fatalf("замер: %v", err)
	}
	if took <= 0 {
		t.Fatalf("замер дал %v", took)
	}
	t.Logf("по петле порция ушла за %s", took)
}

// TestSampleSizeIsCapped — нода не отдаёт больше потолка, сколько ни проси.
//
// Байты замера настоящие, и платит за них продавец. Проверка потолка на ноде,
// а не только на клиенте: чужой клиент попросить может сколько угодно, и без
// этой границы замер превращается в способ выкачать чужой трафик.
func TestSampleSizeIsCapped(t *testing.T) {
	node := startTestNode(t)

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Просим заведомо больше потолка.
	granted, err := dialer.Granted(ctx, vp1.MaxSpeedSample*100)
	if err != nil {
		t.Fatalf("замер: %v", err)
	}
	if granted > vp1.MaxSpeedSample {
		t.Fatalf("нода согласилась отдать %d байт при потолке %d", granted, vp1.MaxSpeedSample)
	}
	if got := node.sampled.Load(); got > int64(vp1.MaxSpeedSample) {
		t.Fatalf("нода приняла запрос на %d байт при потолке %d", got, vp1.MaxSpeedSample)
	}
}

// TestSlowNodeLosesToFastOne — быстрая нода выигрывает у отзывчивой, но медленной.
//
// Ровно тот случай, ради которого всё и делалось. Раньше побеждала та, что
// быстрее отвечает, — и человек получал восьмикратно более медленный канал.
func TestSlowNodeLosesToFastOne(t *testing.T) {
	// Одна и та же порция: Хельсинки отдал быстро, Дубай — медленно. При
	// этом задержки различаются в пять раз, а порции — в десять.
	fast := client.Measurement{
		Node:    client.Node{Name: "fi-1"},
		Latency: 28 * time.Millisecond,
		Fetch:   40 * time.Millisecond,
	}
	slow := client.Measurement{
		Node:    client.Node{Name: "ae-1"},
		Latency: 150 * time.Millisecond,
		Fetch:   400 * time.Millisecond,
	}

	if !(fast.Cost() < slow.Cost()) {
		t.Fatalf("медленная нода не проиграла: быстрая %s, медленная %s",
			fast.Cost().Round(time.Millisecond), slow.Cost().Round(time.Millisecond))
	}
}

// TestUnmeasuredNodeFallsBackToLatency — без замера сравниваем по задержке.
//
// Нода у продавца может быть старой версии и про замер не знать. Это не повод
// считать её негодной: она только что ответила на хендшейк.
func TestUnmeasuredNodeFallsBackToLatency(t *testing.T) {
	quick := client.Measurement{Latency: 30 * time.Millisecond}
	slow := client.Measurement{Latency: 200 * time.Millisecond}

	if quick.Cost() != 30*time.Millisecond {
		t.Fatalf("без замера цена посчиталась как %s", quick.Cost())
	}
	if !(quick.Cost() < slow.Cost()) {
		t.Fatal("без замера порядок нод перестал определяться задержкой")
	}
}

// TestFastNodeBeatsSlightlyQuickerOne — при равной скорости решает задержка.
//
// Иначе замер скорости заслонил бы задержку целиком, и клиент выбирал бы
// ноду на другом материке из-за случайных процентов в замере.
func TestFastNodeBeatsSlightlyQuickerOne(t *testing.T) {
	// Канал одинаковый, поэтому вся разница в порции — это круг до ноды.
	near := client.Measurement{Latency: 20 * time.Millisecond, Fetch: 30 * time.Millisecond}
	far := client.Measurement{Latency: 90 * time.Millisecond, Fetch: 100 * time.Millisecond}

	if !(near.Cost() < far.Cost()) {
		t.Fatal("при равной скорости ближняя нода не выиграла")
	}
}
