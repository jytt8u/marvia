package panel

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Отчёты о доступности приходят от клиентов — то есть из недоверенного
// источника, и это определяет всю конструкцию.
//
// Панель стоит за границей и видит ноду живой ровно тогда, когда для телефона
// в Иркутске она уже мертва. Узнать правду можно только у самих устройств.
// Но раз источник недоверенный, отчёт не должен быть командой: иначе один
// злонамеренный подписчик выкинет из подписки все ноды продавца, и сервис
// умрёт без всякой блокировки.
//
// Отсюда три правила, которые здесь и реализованы.
//
// Первое: отчёт от каждого подписчика хранится ровно один на ноду, последний.
// Это и ограничивает рост таблицы, и делает вес одинаковым — накрутить
// количеством сообщений нельзя.
//
// Второе: ноды по отчётам не отключаются, а только переставляются в подписке.
// Худшее, чего добьётся вредитель, — подвинет живую ноду вниз списка.
//
// Третье: ноду считаем проблемной, только когда на неё жалуется несколько
// разных подписчиков. Один человек с выключенным вайфаем — не событие.

const (
	// healthWindow — за какой срок учитываем отчёты. Старые не выбрасываем
	// сразу: свежая жалоба перезапишет запись того же подписчика.
	healthWindow = 6 * time.Hour

	// minReporters — сколько разных подписчиков должны пожаловаться, прежде
	// чем нода считается проблемной.
	minReporters = 3
)

// Report — отчёт одного подписчика об одной ноде.
type Report struct {
	NodeID    int64 `json:"node_id"`
	OK        bool  `json:"ok"`
	LatencyMS int64 `json:"latency_ms"`
}

// Health — сводка по ноде.
type Health struct {
	NodeID int64 `json:"node_id"`

	// OK и Failed — сколько разных подписчиков сообщили об успехе и отказе.
	OK     int `json:"ok"`
	Failed int `json:"failed"`

	// MedianLatencyMS — медиана среди успешных замеров. Медиана, а не
	// среднее: один подписчик с очень плохой связью не должен утаскивать
	// оценку ноды вниз.
	MedianLatencyMS int64 `json:"median_latency_ms"`
}

// Degraded сообщает, считается ли нода проблемной.
func (h Health) Degraded() bool {
	return h.Failed >= minReporters && h.Failed > h.OK
}

// Total — сколько всего подписчиков отчитались.
func (h Health) Total() int { return h.OK + h.Failed }

// SaveReports принимает отчёты одного подписчика.
func (s *Store) SaveReports(ctx context.Context, userID int64, reports []Report) error {
	if len(reports) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := format(time.Now().UTC())
	for _, r := range reports {
		// Отчёт про несуществующую ноду тихо пропускаем: подписка могла
		// устареть, а падать из-за этого незачем.
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, r.NodeID).Scan(&exists); err != nil {
			continue
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_reports (node_id, user_id, ok, latency_ms, reported_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (node_id, user_id) DO UPDATE SET
				ok = excluded.ok, latency_ms = excluded.latency_ms, reported_at = excluded.reported_at`,
			r.NodeID, userID, boolInt(r.OK), r.LatencyMS, now); err != nil {
			return fmt.Errorf("запись отчёта: %w", err)
		}
	}
	return tx.Commit()
}

// NodeHealth собирает сводку по всем нодам.
func (s *Store) NodeHealth(ctx context.Context) (map[int64]Health, error) {
	since := format(time.Now().UTC().Add(-healthWindow))

	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, ok, latency_ms FROM node_reports
		WHERE reported_at >= ?
		ORDER BY node_id`, since)
	if err != nil {
		return nil, fmt.Errorf("чтение отчётов: %w", err)
	}
	defer rows.Close()

	latencies := map[int64][]int64{}
	out := map[int64]Health{}

	for rows.Next() {
		var (
			nodeID  int64
			okFlag  int
			latency int64
		)
		if err := rows.Scan(&nodeID, &okFlag, &latency); err != nil {
			return nil, err
		}

		h := out[nodeID]
		h.NodeID = nodeID
		if okFlag != 0 {
			h.OK++
			if latency > 0 {
				latencies[nodeID] = append(latencies[nodeID], latency)
			}
		} else {
			h.Failed++
		}
		out[nodeID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for nodeID, values := range latencies {
		h := out[nodeID]
		h.MedianLatencyMS = median(values)
		out[nodeID] = h
	}
	return out, nil
}

// median возвращает середину набора.
func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(a, b int) bool { return values[a] < values[b] })
	return values[len(values)/2]
}

// RankNodes расставляет ноды в том порядке, в каком их стоит предлагать.
//
// Сначала здоровые, потом по скорости, и только в самом конце те, на кого
// жалуются. Ни одна нода из списка не пропадает: отчёты — подсказка, а не
// команда, а источник у них недоверенный.
func RankNodes(nodes []Node, health map[int64]Health) []Node {
	ranked := make([]Node, len(nodes))
	copy(ranked, nodes)

	sort.SliceStable(ranked, func(a, b int) bool {
		ha, hb := health[ranked[a].ID], health[ranked[b].ID]

		if ha.Degraded() != hb.Degraded() {
			return hb.Degraded()
		}

		// Нода без единого отчёта не хуже и не лучше: ей просто ещё не
		// поставили оценку, и загонять её в конец списка было бы неверно —
		// именно так новая нода никогда бы не получила первого клиента.
		switch {
		case ha.MedianLatencyMS == 0 && hb.MedianLatencyMS == 0:
			return false
		case ha.MedianLatencyMS == 0:
			return false
		case hb.MedianLatencyMS == 0:
			return true
		default:
			return ha.MedianLatencyMS < hb.MedianLatencyMS
		}
	})
	return ranked
}
