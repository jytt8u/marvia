package main

import (
	"sync"
	"time"
)

// history — сколько байт прошло через туннель за каждую из последних минут.
//
// Окно рисует по этому график «трафик за час». Считать его в самом окне
// нельзя: страница живёт, пока открыта вкладка, а человек сворачивает окно
// и разворачивает через полчаса — и увидел бы полчаса пустоты, хотя туннель
// работал. Поэтому помнит программа, а окно только рисует.
//
// Кольцо на час, а не растущий список: программа работает сутками, и держать
// в памяти всё, что когда-либо прошло, незачем.
type history struct {
	mu sync.Mutex

	slots [historyMinutes]int64
	// minute — номер минуты (unix / 60), которой принадлежит последний слот.
	// Ноль — снятий ещё не было.
	minute int64
	// last — счётчик на прошлом снятии: байты считаются накопительно, а
	// в слот кладётся разница.
	last int64
}

const historyMinutes = 60

// sample записывает текущее значение накопительного счётчика.
//
// Счётчик обнуляется при каждом подключении, и это не ошибка: раз он стал
// меньше прошлого, значит, туннель подняли заново, и всё, что на нём есть,
// прошло за эту минуту.
func (h *history) sample(now time.Time, total int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delta := total - h.last
	if delta < 0 {
		delta = total
	}
	h.last = total

	h.advance(now)
	h.slots[h.minute%historyMinutes] += delta
}

// advance доводит кольцо до текущей минуты, обнуляя пропущенные.
//
// Между снятиями машина могла спать: тогда минуты, которых программа не
// видела, — это минуты без трафика, а не минуты с тем, что было до сна.
func (h *history) advance(now time.Time) {
	cur := now.Unix() / 60
	switch {
	case h.minute == 0:
		h.minute = cur
	case cur-h.minute >= historyMinutes:
		// Пропуск длиннее кольца — от старого не остаётся ничего.
		h.slots = [historyMinutes]int64{}
		h.minute = cur
	default:
		for h.minute < cur {
			h.minute++
			h.slots[h.minute%historyMinutes] = 0
		}
	}
}

// minutes отдаёт байты по минутам за последний час, от старой к текущей.
func (h *history) minutes(now time.Time) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.advance(now)

	out := make([]int64, historyMinutes)
	for i := range out {
		m := h.minute - int64(historyMinutes-1-i)
		out[i] = h.slots[((m%historyMinutes)+historyMinutes)%historyMinutes]
	}
	return out
}
