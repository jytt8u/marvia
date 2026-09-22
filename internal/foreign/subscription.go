package foreign

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Subscription — чужая подписка: ноды и, если продавец их сообщил, остаток и
// срок.
type Subscription struct {
	Links []Link
	// Skipped — сколько строк не разобралось: чужие форматы, плагины. Экрану
	// это нужно, чтобы не делать вид, что продавец дал меньше нод, чем дал.
	Skipped int

	Upload, Download, Total int64
	Expire                  time.Time
}

// Remaining — сколько трафика осталось; -1 — без ограничения.
func (s Subscription) Remaining() int64 {
	if s.Total <= 0 {
		return -1
	}
	return max(0, s.Total-s.Upload-s.Download)
}

// ParseList разбирает тело подписки: ссылки по строке, как есть или в
// base64. Так отдают подписки 3x-ui, Marzban, Remnawave и сама панель Marvia
// в режиме «для чужих клиентов».
func ParseList(body []byte) Subscription {
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "://") {
		if dec, err := decodeBase64(strings.Join(strings.Fields(text), "")); err == nil {
			text = string(dec)
		}
	}
	var sub Subscription
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l, err := Parse(line)
		if err != nil {
			sub.Skipped++
			continue
		}
		sub.Links = append(sub.Links, l)
	}
	return sub
}

// ParseUserinfo разбирает subscription-userinfo: «upload=1; download=2;
// total=3; expire=1735689600». Заголовок не стандарт, но пишут его все панели.
func (s *Subscription) ParseUserinfo(h string) {
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(k) {
		case "upload":
			s.Upload = n
		case "download":
			s.Download = n
		case "total":
			s.Total = n
		case "expire":
			if n > 0 {
				s.Expire = time.Unix(n, 0)
			}
		}
	}
}

// maxBody — больше подписка не бывает: тысяча нод — это сотни килобайт.
const maxBody = 4 << 20

// FetchRaw забирает тело чужой подписки и заголовок с остатком — как есть,
// чтобы их можно было положить в кэш и разобрать потом тем же ParseList.
//
// Представляемся v2rayNG: панели выбирают формат ответа по User-Agent, и на
// незнакомый многие отдают страницу для браузера, а не список. Ошибка — без
// адреса: в нём токен подписки, а ошибки уходят в журнал.
func FetchRaw(ctx context.Context, url string) (body []byte, userinfo string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", errors.New("адрес подписки не разбирается")
	}
	req.Header.Set("User-Agent", "v2rayNG/1.10.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", errors.New("подписка недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("подписка ответила %s", resp.Status)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", errors.New("подписка оборвалась на полуслове")
	}
	return body, resp.Header.Get("Subscription-Userinfo"), nil
}

// Usable — подписка годится для подключения; иначе — почему нет.
func (s Subscription) Usable() error {
	if len(s.Links) > 0 {
		return nil
	}
	if s.Skipped > 0 {
		return fmt.Errorf("в подписке %d нод, и ни одну клиент не понимает", s.Skipped)
	}
	return errors.New("в подписке нет ни одной ноды")
}
