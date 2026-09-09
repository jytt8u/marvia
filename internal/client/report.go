package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const reportTimeout = 10 * time.Second

// Report — отчёт об одной ноде в том виде, в каком его ждёт панель.
type Report struct {
	NodeID    int64 `json:"node_id"`
	OK        bool  `json:"ok"`
	LatencyMS int64 `json:"latency_ms"`
}

// ReportsFrom превращает замеры в отчёты.
//
// Ноды без идентификатора пропускаем: сослаться на них панель не сможет, а
// гадать по имени нельзя — продавец волен переименовать ноду когда угодно.
func ReportsFrom(measurements []Measurement) []Report {
	out := make([]Report, 0, len(measurements))

	for _, m := range measurements {
		if m.Node.ID == 0 {
			continue
		}
		report := Report{NodeID: m.Node.ID, OK: m.OK()}
		if m.OK() {
			report.LatencyMS = m.Latency.Milliseconds()
		}
		out = append(out, report)
	}
	return out
}

// SendReports отправляет отчёты панели.
//
// Отправка — дело необязательное: если панель недоступна, туннель всё равно
// должен работать. Поэтому вызывающий вправе просто записать ошибку в журнал
// и жить дальше.
func SendReports(ctx context.Context, subURL string, reports []Report) error {
	if len(reports) == 0 {
		return nil
	}

	payload, err := json.Marshal(map[string]any{"reports": reports})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subURL+"/report", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("отправка отчётов: %w", withoutSecret(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("панель ответила %s", resp.Status)
	}
	return nil
}
