package panel

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// Установка ноды одной командой.
//
// Продавец нажимает в панели «добавить ноду», получает строку, вставляет её на
// чистом сервере — и всё. Ключи рождаются на самой ноде, панель их приватных
// частей не видит; регистрируется нода сама, по одноразовому приглашению.
//
// Скрипт и бинарники раздаёт сама панель, а не публичный сайт. Причин две.
// Во-первых, тогда продавцу не нужно ничего нигде размещать. Во-вторых, всё
// лежит по адресу с приглашением внутри: посторонний, ткнувшийся в панель,
// не найдёт ни установщика, ни бинарника — а найденный установщик был бы
// однозначной вывеской «здесь панель обхода блокировок».

//go:embed install_node.sh.tmpl
var installNodeTemplate string

var installNodeTmpl = template.Must(template.New("install").Parse(installNodeTemplate))

// installParams — то, что подставляется в установщик при выдаче.
type installParams struct {
	Panel string
	Token string
}

// installScript отдаёт установщик, готовый к запуску: адрес панели и
// приглашение уже внутри.
func (a *API) installScript(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !a.inviteAlive(r, token) {
		// Отвечаем как обычный сайт на несуществующий путь: тому, кто просто
		// перебирает адреса, знать про установщик незачем.
		http.NotFound(w, r)
		return
	}

	base := a.subBase
	if base == "" {
		base = "https://" + r.Host
	}

	var out bytes.Buffer
	if err := installNodeTmpl.Execute(&out, installParams{Panel: base, Token: token}); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write(out.Bytes())
}

// installBinary отдаёт бинарник ноды по тому же приглашению.
func (a *API) installBinary(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	name := r.PathValue("name")

	if !a.inviteAlive(r, token) {
		http.NotFound(w, r)
		return
	}
	// Имя приходит из адреса, поэтому берём только то, что сами раздаём.
	// Иначе сюда пролезло бы «../../etc/shadow».
	if name != "marvia-node" && name != "marvia-keygen" {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(a.distDir, name)
	f, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusServiceUnavailable,
			"на панели нет файла "+name+": положи бинарники ноды в "+a.distDir)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// registerNode обменивает приглашение на запись ноды и её токен.
func (a *API) registerNode(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	if token == "" {
		fail(w, http.StatusUnauthorized, "нужно приглашение")
		return
	}

	var p struct {
		Name string `json:"name"`

		// Country — то, что продавец передал установщику в COUNTRY. Пусто —
		// нормально: страну можно проставить потом через PATCH.
		Country          string `json:"country"`
		Port             int    `json:"port"`
		SNI              string `json:"sni"`
		PublicKey        string `json:"public_key"`
		RealityPublicKey string `json:"reality_public_key"`
		RealityShortID   string `json:"reality_short_id"`
		WSPath           string `json:"ws_path"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.Port <= 0 || p.Port > 65535 {
		fail(w, http.StatusBadRequest, "нужен порт ноды")
		return
	}

	// Адрес ноды берём из самого соединения, а не из тела запроса. Нода не
	// знает своего внешнего адреса — за NAT она видит внутренний, и продавец
	// получил бы в ссылках 10.0.0.5. А панель видит настоящий.
	host := callerIP(r)
	if host == "" {
		fail(w, http.StatusBadRequest, "не удалось определить адрес ноды")
		return
	}

	node, nodeToken, err := a.store.RedeemInvite(r.Context(), token, CreateNodeParams{
		Name:             p.Name,
		Country:          p.Country,
		Address:          net.JoinHostPort(host, strconv.Itoa(p.Port)),
		SNI:              p.SNI,
		PublicKey:        p.PublicKey,
		RealityPublicKey: p.RealityPublicKey,
		RealityShortID:   p.RealityShortID,
		WSPath:           p.WSPath,
	})
	if errors.Is(err, ErrInviteSpent) {
		fail(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	ok(w, map[string]any{"node": node, "token": nodeToken})
}

// createNodeInvite выпускает одноразовое приглашение и готовую команду.
func (a *API) createNodeInvite(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Label string `json:"label"`
	}
	// Тело необязательное: приглашение без пометки — обычное дело.
	_ = decodeOptional(r, &p)

	invite, token, err := a.store.CreateInvite(r.Context(), p.Label)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	base := a.subBase
	if base == "" {
		base = "https://" + r.Host
	}

	ok(w, map[string]any{
		"invite": invite,
		// Приглашение показывается один раз: в базе только его хеш.
		"token":   token,
		"command": "curl -fsSL " + base + "/install/" + token + " | sh",
	})
}

// inviteAlive проверяет, что приглашение ещё можно использовать, не гася его.
func (a *API) inviteAlive(r *http.Request, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	alive, err := a.store.InviteAlive(r.Context(), token)
	return err == nil && alive
}

// callerIP достаёт адрес, с которого пришла нода.
//
// Когда панель стоит за обратным прокси, r.RemoteAddr — это сам прокси, то
// есть 127.0.0.1, и нода записалась бы с этим адресом. Поэтому если запрос
// пришёл из петли или из частной сети, верим заголовку X-Forwarded-For:
// подставить его туда мог только тот, кто уже стоит между панелью и нодой.
//
// Постороннему из интернета заголовок не верим совсем. Иначе достаточно было
// бы одного приглашения, чтобы записать ноду с чужим адресом — например с
// адресом сайта, который потом попал бы в конфиги всех покупателей.
func callerIP(r *http.Request) string {
	direct := hostOfAddr(r.RemoteAddr)
	if !trustedForwarder(direct) {
		return direct
	}

	head := r.Header.Get("X-Forwarded-For")
	if head == "" {
		return direct
	}
	first, _, _ := strings.Cut(head, ",")
	first = strings.TrimSpace(first)
	if net.ParseIP(first) == nil {
		return direct
	}
	return first
}

func hostOfAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return host
}

func trustedForwarder(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// decodeOptional разбирает тело запроса, если оно есть.
func decodeOptional(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
