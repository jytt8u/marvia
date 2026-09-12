package main

import (
	"testing"
	"time"
)

// График за час обязан говорить правду о минутах, а не о снятиях.

func at(minute int) time.Time { return time.Unix(int64(minute)*60+7, 0) }

func TestBytesLandInTheMinuteTheyPassed(t *testing.T) {
	var h history
	h.sample(at(100), 0)
	h.sample(at(100), 500)
	h.sample(at(101), 800)
	h.sample(at(101), 900)

	got := h.minutes(at(101))
	if n := len(got); n != historyMinutes {
		t.Fatalf("слотов %d, ожидалось %d", n, historyMinutes)
	}
	if got[historyMinutes-1] != 400 || got[historyMinutes-2] != 500 {
		t.Errorf("последние две минуты: %d и %d, ожидалось 500 и 400",
			got[historyMinutes-2], got[historyMinutes-1])
	}
}

// Счётчик обнуляется при новом подключении — это не минус, а новый отсчёт.
func TestReconnectDoesNotProduceNegativeTraffic(t *testing.T) {
	var h history
	h.sample(at(10), 5000)
	h.sample(at(10), 300) // туннель подняли заново, счётчик с нуля

	got := h.minutes(at(10))
	if last := got[historyMinutes-1]; last != 5300 {
		t.Errorf("за минуту насчитано %d, ожидалось 5000+300", last)
	}
}

// Минуты, в которые программа спала, пустые, а не унаследованные.
func TestSleptMinutesAreEmpty(t *testing.T) {
	var h history
	h.sample(at(10), 1000)
	h.sample(at(40), 1000) // полчаса без снятий

	got := h.minutes(at(40))
	for i := historyMinutes - 30; i < historyMinutes-1; i++ {
		if got[i] != 0 {
			t.Fatalf("минута %d не пустая: %d", i, got[i])
		}
	}
	if got[historyMinutes-31] != 1000 {
		t.Errorf("минута до сна потеряна: %d", got[historyMinutes-31])
	}
}

// Пропуск дольше часа стирает кольцо целиком.
func TestGapLongerThanAnHourClearsEverything(t *testing.T) {
	var h history
	h.sample(at(10), 1000)
	h.sample(at(10+historyMinutes+5), 1000)

	for i, v := range h.minutes(at(10 + historyMinutes + 5)) {
		if v != 0 {
			t.Fatalf("после пропуска в кольце остался старый трафик: слот %d = %d", i, v)
		}
	}
}

// Опрос сдвигает кольцо и без снятий: окно смотрит на график, а трафика нет.
func TestReadingAdvancesTheRing(t *testing.T) {
	var h history
	h.sample(at(10), 700)

	got := h.minutes(at(12))
	if got[historyMinutes-1] != 0 || got[historyMinutes-3] != 700 {
		t.Errorf("кольцо не сдвинулось: последняя %d, третья с конца %d", got[historyMinutes-1], got[historyMinutes-3])
	}
}
