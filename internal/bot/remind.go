package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Напоминание об окончании срока.
//
// Продлевают не потому, что довольны, а потому, что вовремя вспомнили:
// молчащий бот теряет ровно тех покупателей, которые никуда не собирались.

const (
	// remindBefore — за сколько до конца писать.
	remindBefore = 3 * 24 * time.Hour

	// remindEvery — как часто обходить список. Раз в шесть часов: точнее не
	// нужно, а реже — и напоминание уедет на день.
	remindEvery = 6 * time.Hour
)

// remind обходит подписчиков и пишет тем, у кого срок на исходе.
func (b *Bot) remind(ctx context.Context) {
	// Первый обход сразу после запуска: бот могли поднять как раз потому,
	// что он лежал, и чьи-то напоминания уже опоздали.
	for {
		b.remindOnce(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(remindEvery):
		}
	}
}

func (b *Bot) remindOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	users, err := b.panel.List(ctx)
	if err != nil {
		log.Printf("обход подписчиков: %v", err)
		return
	}

	now := time.Now()
	alive := make(map[string]bool, len(users))

	for _, u := range users {
		id, ok := telegramID(u.ExternalID)
		if !ok {
			// Покупатель заведён не ботом — руками или чужим ботом. Писать
			// ему некуда, и это нормально.
			continue
		}
		alive[u.ExternalID] = true

		if !u.Enabled || u.ExpiresAt == nil {
			continue
		}
		left := u.ExpiresAt.Sub(now)
		if left <= 0 || left > remindBefore {
			continue
		}

		// Отметка — сам срок: после продления он меняется, и следующее
		// напоминание придёт вовремя, а до продления второй раз не придёт.
		mark := u.ExpiresAt.UTC().Format(time.RFC3339)
		if b.state.remembered(u.ExternalID, mark) {
			continue
		}

		text := fmt.Sprintf("Доступ заканчивается <b>%s</b> — осталось %s.\n\nПродлить можно прямо сейчас: остаток дней не сгорает, он прибавится к новому сроку.",
			until(u.ExpiresAt), days(left))
		b.say(ctx, id, text, []Button{{Text: "Продлить", Data: "buy"}})
		b.state.remember(u.ExternalID, mark)
	}

	b.state.forget(alive)
}

// telegramID достаёт номер из внешнего ключа вида tg:584930221.
func telegramID(external string) (int64, bool) {
	raw, found := strings.CutPrefix(external, "tg:")
	if !found {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// days — сколько осталось, словами.
func days(left time.Duration) string {
	whole := int(left.Hours() / 24)
	switch {
	case whole >= 5:
		return strconv.Itoa(whole) + " дней"
	case whole >= 2:
		return strconv.Itoa(whole) + " дня"
	case whole == 1:
		return "один день"
	case left >= time.Hour:
		return strconv.Itoa(int(left.Hours())) + " ч"
	default:
		return "меньше часа"
	}
}
