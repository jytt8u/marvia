package foreign

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// IsForeign — строка не ключ Marvia, а чужая ссылка ноды или адрес чужой
// подписки. Ключ Marvia узнаётся по схеме, всё остальное http(s) — подписка.
func IsForeign(link string) bool {
	link = strings.TrimSpace(link)
	lower := strings.ToLower(link)
	if strings.HasPrefix(lower, "marvia://") || strings.HasPrefix(lower, "veil-account://") {
		return false
	}
	if IsLink(FirstLine(link)) {
		return true
	}
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// FirstLine — первая непустая строка: ссылки вставляют и столбиком.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// cacheFile — чужая подписка на диске: тело как пришло и заголовок с
// остатком. Нужна по той же причине, что кэш своей: список серверов виден и
// без туннеля, а панель продавца бывает недоступна именно тогда, когда
// туннель и нужен.
type cacheFile struct {
	FetchedAt int64  `json:"fetched_at"`
	Userinfo  string `json:"userinfo"`
	Body      string `json:"body"`
}

// CachePath — файл кэша чужой подписки в каталоге dir. Имя — от отпечатка
// адреса: токен подписки не должен лежать в имени файла.
func CachePath(dir, link string) string {
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(link)))
	return filepath.Join(dir, "foreign-"+hex.EncodeToString(sum[:4])+".json")
}

// Fresh — сколько чужая подписка считается свежей. Сутки, как у своей: ноды у
// продавцов меняются не чаще, а каждый поход — запрос имени его панели.
const Fresh = 24 * time.Hour

// fetchTimeout — дольше ждать подписку незачем: человек смотрит на экран.
const fetchTimeout = 20 * time.Second

// Load отдаёт чужую подписку: ссылки — как есть, адрес — из свежего кэша,
// из сети или, если сеть молчит, из старого кэша (stale).
func Load(link, cachePath string, refresh bool) (sub Subscription, fetched time.Time, stale bool, err error) {
	link = strings.TrimSpace(link)
	if IsLink(FirstLine(link)) {
		// Одна ссылка или несколько столбиком: подписка без панели.
		sub = ParseList([]byte(link))
		if len(sub.Links) == 0 {
			_, err := Parse(FirstLine(link))
			return sub, time.Time{}, false, err
		}
		return sub, time.Now(), false, nil
	}

	var cached cacheFile
	if cachePath != "" {
		if raw, rerr := os.ReadFile(cachePath); rerr == nil {
			_ = json.Unmarshal(raw, &cached)
		}
	}
	fromCache := func() Subscription {
		s := ParseList([]byte(cached.Body))
		s.ParseUserinfo(cached.Userinfo)
		return s
	}
	if !refresh && cached.Body != "" && time.Since(time.Unix(cached.FetchedAt, 0)) < Fresh {
		return fromCache(), time.Unix(cached.FetchedAt, 0), false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	body, userinfo, ferr := FetchRaw(ctx, link)
	if ferr != nil {
		if cached.Body != "" {
			return fromCache(), time.Unix(cached.FetchedAt, 0), true, nil
		}
		return Subscription{}, time.Time{}, false, ferr
	}
	sub = ParseList(body)
	sub.ParseUserinfo(userinfo)
	if err := sub.Usable(); err != nil {
		return sub, time.Time{}, false, err
	}
	if cachePath != "" {
		raw, _ := json.Marshal(cacheFile{FetchedAt: time.Now().Unix(), Userinfo: userinfo, Body: string(body)})
		// Во временный и переименованием: оборванная запись не должна
		// оставить кэш, который потом не прочитается.
		tmp := cachePath + ".tmp"
		if os.WriteFile(tmp, raw, 0o600) == nil {
			_ = os.Rename(tmp, cachePath)
		}
	}
	return sub, time.Now(), false, nil
}

// Pin закрепляет за нодой адрес: Xray будет дозваниваться по нему, а не по
// имени.
//
// Нужно там, где весь трафик машины заворачивается в туннель, — в окне на
// компьютере. Имя ноды, которое Xray разрешал бы при каждом соединении, ушло
// бы запросом в тот самый туннель, который он же и держит: петля. Поэтому имя
// разрешается один раз заранее, до того как туннель поднят, а адреса нод
// выводятся мимо него. Имя при этом остаётся там, где его проверяют: в SNI и
// в Host у WebSocket и прочих — за CDN без него не пустят.
func Pin(l Link, ip netip.Addr) Link {
	if _, err := netip.ParseAddr(l.Host); err == nil {
		return l
	}
	addr := ip.Unmap().String()
	out := cloneMap(l.Outbound)
	settings, _ := out["settings"].(map[string]any)
	for _, key := range []string{"vnext", "servers"} {
		if list, ok := settings[key].([]any); ok && len(list) > 0 {
			if first, ok := list[0].(map[string]any); ok {
				first["address"] = addr
			}
		}
	}
	if _, ok := settings["address"]; ok {
		settings["address"] = addr
	}
	if peers, ok := settings["peers"].([]any); ok && len(peers) > 0 {
		if p, ok := peers[0].(map[string]any); ok {
			p["endpoint"] = net.JoinHostPort(addr, strconv.Itoa(l.Port))
		}
	}
	if stream, ok := out["streamSettings"].(map[string]any); ok {
		for _, key := range []string{"wsSettings", "httpupgradeSettings", "xhttpSettings"} {
			if s, ok := stream[key].(map[string]any); ok {
				if h, _ := s["host"].(string); h == "" {
					s["host"] = l.Host
				}
			}
		}
		if g, ok := stream["grpcSettings"].(map[string]any); ok {
			if a, _ := g["authority"].(string); a == "" {
				g["authority"] = l.Host
			}
		}
		for _, key := range []string{"tlsSettings", "realitySettings"} {
			if s, ok := stream[key].(map[string]any); ok {
				if n, _ := s["serverName"].(string); n == "" {
					s["serverName"] = l.Host
				}
			}
		}
	}
	l.Outbound = out
	return l
}

// Resolve разрешает имена нод и закрепляет адреса. Отдаёт подписку с
// закреплёнными нодами и все их адреса — чтобы вывести мимо туннеля каждую,
// а не только текущую: иначе после переезда соединения новой ноды пошли бы
// в тот же туннель. Ноду, чьё имя не разрешилось, выбрасывает: подключиться
// к ней всё равно нельзя, а выбрать её надзор мог бы.
func Resolve(ctx context.Context, sub Subscription) (Subscription, []netip.Addr, error) {
	var (
		out   = sub
		addrs []netip.Addr
		seen  = map[netip.Addr]bool{}
	)
	out.Links = nil
	for _, l := range sub.Links {
		ips, err := lookup(ctx, l.Host)
		if err != nil || len(ips) == 0 {
			out.Skipped++
			continue
		}
		for _, ip := range ips {
			if !seen[ip] {
				seen[ip] = true
				addrs = append(addrs, ip)
			}
		}
		out.Links = append(out.Links, Pin(l, preferV4(ips)))
	}
	if len(out.Links) == 0 {
		return out, nil, errors.New("ни у одной ноды подписки не разрешилось имя")
	}
	return out, addrs, nil
}

func lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for i := range found {
		found[i] = found[i].Unmap()
	}
	return found, nil
}

// preferV4 — IPv4, если есть: IPv6 у домашних провайдеров бывает не везде, а
// нода с IPv4 доступна всегда.
func preferV4(ips []netip.Addr) netip.Addr {
	for _, ip := range ips {
		if ip.Is4() {
			return ip
		}
	}
	return ips[0]
}

func cloneMap(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
