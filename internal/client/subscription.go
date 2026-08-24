package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	subscriptionTimeout = 20 * time.Second

	// maxSubscription — верхняя граница ответа. Список на сотню нод занимает
	// десятки килобайт; мегабайт — уже повод не доверять источнику.
	maxSubscription = 1 << 20
)

// Node — точка входа в том виде, в каком её отдаёт панель.
type Node struct {
	// ID нужен, чтобы отчёты о доступности ссылались на ноду точно, а не по
	// имени, которое продавец может переименовать.
	ID int64 `json:"id"`

	Name      string `json:"name"`
	Address   string `json:"address"`
	SNI       string `json:"sni,omitempty"`
	PublicKey string `json:"public_key"`

	// WSPath не пуст, когда нода стоит за CDN: тогда Address — адрес CDN,
	// а не самой ноды.
	WSPath string `json:"ws_path,omitempty"`

	// RealityPublicKey не пуст, когда нода работает под маскировкой REALITY.
	RealityPublicKey string `json:"reality_public_key,omitempty"`
	RealityShortID   string `json:"reality_short_id,omitempty"`
}

// Transport — каким способом подключаться к ноде.
type Transport string

const (
	TransportTLS     Transport = "tls"
	TransportWS      Transport = "ws"
	TransportReality Transport = "reality"
)

// Transport определяет способ подключения по заполненным полям.
func (n Node) Transport() Transport {
	switch {
	case n.WSPath != "":
		return TransportWS
	case n.RealityPublicKey != "":
		return TransportReality
	default:
		return TransportTLS
	}
}

// Subscription — ответ панели для нашего клиента.
type Subscription struct {
	Nodes        []Node `json:"nodes"`
	TrafficLimit int64  `json:"traffic_limit"`
	Used         int64  `json:"used"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}

// Remaining возвращает остаток квоты. Ноль лимита означает «без ограничения».
func (s Subscription) Remaining() int64 {
	if s.TrafficLimit <= 0 {
		return 0
	}
	if left := s.TrafficLimit - s.Used; left > 0 {
		return left
	}
	return 0
}

// FetchSubscription забирает список нод у панели.
func FetchSubscription(ctx context.Context, subURL string) (Subscription, error) {
	ctx, cancel := context.WithTimeout(ctx, subscriptionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL+"?format=json", nil)
	if err != nil {
		return Subscription{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Subscription{}, fmt.Errorf("запрос подписки: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Subscription{}, fmt.Errorf("панель ответила %s", resp.Status)
	}

	var sub Subscription
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSubscription)).Decode(&sub); err != nil {
		return Subscription{}, fmt.Errorf("разбор подписки: %w", err)
	}
	if len(sub.Nodes) == 0 {
		return Subscription{}, errors.New("в подписке нет ни одной ноды")
	}
	return sub, nil
}
