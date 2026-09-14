package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	subscriptionTimeout = 20 * time.Second

	// pinnedDialTimeout — сколько ждём один адрес панели из подсказки, прежде
	// чем взяться за следующий.
	pinnedDialTimeout = 5 * time.Second

	// maxSubscription — верхняя граница ответа. Список на сотню нод занимает
	// десятки килобайт; мегабайт — уже повод не доверять источнику.
	maxSubscription = 1 << 20
)

// Node — точка входа в том виде, в каком её отдаёт панель.
type Node struct {
	// ID нужен, чтобы отчёты о доступности ссылались на ноду точно, а не по
	// имени, которое продавец может переименовать.
	ID int64 `json:"id"`

	Name string `json:"name"`

	// Country — страна словами, как её написал продавец. По ней клиент
	// подбирает флажок и группирует список: имя сервера покупателю ни о чём не
	// говорит, страна говорит. Пока продавец поле не заполнил, оно пустое, и
	// показывается одно имя.
	Country   string `json:"country,omitempty"`
	Address   string `json:"address"`
	SNI       string `json:"sni,omitempty"`
	PublicKey string `json:"public_key"`

	// SNIExtra — запасные имена прикрытия, кроме SNI. Работают только под
	// REALITY: там подлинность ноды подтверждает ключ, а не сертификат, и в
	// SNI годится любое из имён, которые нода принимает.
	//
	// Поле добавочное, а не замена SNI: подписку читают уже установленные
	// клиенты, и они про набор не знают. Им достаётся SNI, как и раньше.
	SNIExtra []string `json:"sni_extra,omitempty"`

	// WSPath не пуст, когда нода стоит за CDN: тогда Address — адрес CDN,
	// а не самой ноды.
	WSPath string `json:"ws_path,omitempty"`

	// RealityPublicKey не пуст, когда нода работает под маскировкой REALITY.
	RealityPublicKey string `json:"reality_public_key,omitempty"`
	RealityShortID   string `json:"reality_short_id,omitempty"`

	// QUIC означает, что нода принимает ещё и по UDP на том же порту.
	//
	// Это не замена TCP, а вторая дорога. На сети с потерями она заметно
	// ровнее: весь трафик человека идёт одним соединением, и по TCP один
	// потерянный пакет тормозит сразу всё. Плюс переход из вайфая в мобильный
	// интернет соединение переживает, а не умирает.
	//
	// Где UDP режут — а режут его целыми сетями и вырезают в белых списках —
	// клиент просто не дозвонится по нему и пойдёт обычным путём.
	QUIC bool `json:"quic,omitempty"`
}

// Transport — каким способом подключаться к ноде.
type Transport string

const (
	TransportTLS     Transport = "tls"
	TransportWS      Transport = "ws"
	TransportReality Transport = "reality"
	TransportQUIC    Transport = "quic"
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

// serverNames собирает имена прикрытия, из которых клиент выбирает на каждое
// соединение: основное плюс запасные.
//
// Пустые и повторы отбрасываются: продавец вводит имена руками, а повтор в
// наборе молча перекосил бы выбор в его сторону. Основное идёт первым, и если
// запасных нет, набор из него одного — тогда поведение ровно прежнее.
func (n Node) serverNames(primary string) []string {
	out := make([]string, 0, 1+len(n.SNIExtra))
	seen := make(map[string]bool, 1+len(n.SNIExtra))
	for _, name := range append([]string{primary}, n.SNIExtra...) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{primary}
	}
	return out
}

// Subscription — ответ панели для нашего клиента.
type Subscription struct {
	Nodes        []Node `json:"nodes"`
	TrafficLimit int64  `json:"traffic_limit"`
	Used         int64  `json:"used"`
	ExpiresAt    string `json:"expires_at,omitempty"`

	// Apps — что панель выложила для скачивания, по платформам: android,
	// windows. Версия может быть пустой — тогда сравнивать не с чем.
	Apps map[string]AppOffer `json:"apps,omitempty"`
}

// AppOffer — ссылка на приложение с домена панели и его версия.
type AppOffer struct {
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
}

// Update говорит, лежит ли на панели версия новее той, что запущена.
//
// Новее, а не «другая»: продавец мог выложить старую сборку, и звать
// человека на неё — значит звать назад. Версии сравниваются по числам;
// сборка dev не обновляется никогда — это разработчик, он знает, что
// запустил.
func (s Subscription) Update(platform, current string) (AppOffer, bool) {
	offer, ok := s.Apps[platform]
	if !ok || offer.Version == "" || offer.URL == "" || current == "" || current == "dev" {
		return AppOffer{}, false
	}
	if !newerVersion(offer.Version, current) {
		return AppOffer{}, false
	}
	return offer, true
}

// newerVersion — a новее b. Понимает vX.Y.Z и X.Y.Z; лишний хвост вроде
// -rc1 отбрасывается. Непонятная версия не считается новее ничего.
func newerVersion(a, b string) bool {
	pa, okA := versionParts(a)
	pb, okB := versionParts(b)
	if !okA || !okB {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	v, _, _ = strings.Cut(v, "+")
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
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
//
// pinned — адреса панели из ссылки доступа. Когда они заданы, имя домена у
// резолвера не спрашивается вовсе: соединение идёт прямо по адресу, а имя
// уходит в SNI и по нему проверяется сертификат. Пусто — обычный путь через
// системный резолвер.
func FetchSubscription(ctx context.Context, subURL string, pinned []netip.Addr) (Subscription, error) {
	ctx, cancel := context.WithTimeout(ctx, subscriptionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL+"?format=json", nil)
	if err != nil {
		return Subscription{}, err
	}

	resp, err := subscriptionClient(pinned).Do(req)
	if err != nil {
		return Subscription{}, fmt.Errorf("запрос подписки: %w", withoutSecret(err))
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

// subscriptionClient собирает клиента, который ходит по заданным адресам.
//
// Подменяется только адрес соединения. Имя из ссылки остаётся в запросе, и
// стандартный транспорт сам подставляет его в SNI и в проверку сертификата —
// поэтому подменить панель, зная лишь адрес, всё равно не выйдет.
func subscriptionClient(pinned []netip.Addr) *http.Client {
	if len(pinned) == 0 {
		return http.DefaultClient
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Прокси из настроек системы отключаем намеренно. Он разрешил бы имя сам
	// и увидел бы его — то есть подсказка перестала бы что-либо значить.
	// Продавец, вписавший адреса в ссылку, рассчитывает на прямое соединение.
	transport.Proxy = nil

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// Срок на каждую попытку отдельно. Без него первый же неотвечающий
		// адрес съедал бы весь срок запроса, и до живого мы бы не дошли — а у
		// панели за CDN мёртвый адрес в списке дело обычное.
		dialer := net.Dialer{Timeout: pinnedDialTimeout}

		for _, ip := range pinned {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}

		// Все подсказанные адреса молчат — спрашиваем имя как обычно.
		//
		// Подсказку вписывают один раз и живёт она годами, а адреса панели за
		// это время меняются: продавец переехал, CDN сменил диапазон. Упереться
		// в устаревшую подсказку и оставить покупателя без списка нод — хуже,
		// чем один запрос имени в редком случае. Ради этого запроса всё и
		// затевалось, но затевалось ради обычного дня, а не ради поломки.
		var plain net.Dialer
		conn, err := plain.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("адреса панели из ссылки не отвечают, и по имени тоже не вышло: %w", err)
		}
		return conn, nil
	}

	return &http.Client{Transport: transport}
}

// Until — до какого момента оплачена подписка.
//
// false означает «без ограничения по сроку»: так панель отвечает, когда
// продавец не поставил срок вовсе.
func (s Subscription) Until() (time.Time, bool) {
	if strings.TrimSpace(s.ExpiresAt) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Allows проверяет, есть ли смысл вообще звонить нодам.
//
// Без этой проверки кончившаяся подписка выглядит как «ни один сервер не
// отвечает»: ноды честно отказывают, а человек читает, что сломались мы. Он
// идёт к продавцу с «у вас всё лежит» вместо «продли», и платит за это
// продавец — своим временем на каждого такого.
func (s Subscription) Allows() error {
	if until, set := s.Until(); set && time.Now().After(until) {
		return fmt.Errorf("%w: %s", ErrExpired, until.Local().Format("02.01.2006"))
	}
	if s.TrafficLimit > 0 && s.Used >= s.TrafficLimit {
		return ErrQuota
	}
	return nil
}

// Title — как ноду называть человеку.
//
// Страна первой: покупателю «Нидерланды» говорит всё, а vm-4823917-ubuntu от
// хостера — ничего. Пустая страна ничего не портит, остаётся одно имя.
func (n Node) Title() string {
	switch {
	case n.Country == "":
		return n.Name
	case n.Name == "":
		return n.Country
	default:
		return n.Country + " · " + n.Name
	}
}
