package panel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/veilproject/veil/internal/users"
)

// API — HTTP-интерфейс панели.
//
// Три круга доступа:
//   - админский токен — полный доступ; его получает бот продавца;
//   - токен ноды — только свой список пользователей и отправка статистики;
//   - подписка по токену — без авторизации, токен и есть секрет.
type API struct {
	store      *Store
	adminToken string
	subBase    string // базовый адрес подписок, например https://sub.example.com
}

// NewAPI собирает обработчики панели.
func NewAPI(store *Store, adminToken, subBase string) *API {
	return &API{store: store, adminToken: adminToken, subBase: strings.TrimRight(subBase, "/")}
}

// Handler возвращает готовый маршрутизатор.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Управление: пользователи.
	mux.HandleFunc("GET /api/v1/users", a.admin(a.listUsers))
	mux.HandleFunc("POST /api/v1/users", a.admin(a.createUser))
	mux.HandleFunc("GET /api/v1/users/{id}", a.admin(a.getUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", a.admin(a.updateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", a.admin(a.deleteUser))
	mux.HandleFunc("POST /api/v1/users/{id}/credentials", a.admin(a.addCredential))
	mux.HandleFunc("DELETE /api/v1/credentials/{id}", a.admin(a.deleteCredential))

	// Управление: ноды.
	mux.HandleFunc("GET /api/v1/nodes", a.admin(a.listNodes))
	mux.HandleFunc("POST /api/v1/nodes", a.admin(a.createNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", a.admin(a.deleteNode))

	// Ноды забирают свой список и сдают статистику.
	mux.HandleFunc("GET /api/v1/node/users", a.node(a.nodeUsers))
	mux.HandleFunc("POST /api/v1/node/usage", a.node(a.nodeUsage))

	// Подписка. Токен в адресе и есть авторизация.
	mux.HandleFunc("GET /sub/{token}", a.subscription)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return mux
}

// admin проверяет админский токен.
func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !TokensEqual(bearer(r), a.adminToken) {
			fail(w, http.StatusUnauthorized, "нужен админский токен")
			return
		}
		next(w, r)
	}
}

// nodeContext передаёт опознанную ноду обработчику.
type nodeHandler func(http.ResponseWriter, *http.Request, Node)

// node проверяет токен ноды.
func (a *API) node(next nodeHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			fail(w, http.StatusUnauthorized, "нужен токен ноды")
			return
		}
		n, err := a.store.AuthenticateNode(r.Context(), token)
		if err != nil {
			fail(w, http.StatusUnauthorized, "неизвестный токен ноды")
			return
		}
		if !n.Enabled {
			fail(w, http.StatusForbidden, "нода отключена")
			return
		}
		next(w, r, n)
	}
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListUsers(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"users": list})
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var p CreateUserParams
	if !decode(w, r, &p) {
		return
	}

	user, private, err := a.store.CreateUser(r.Context(), p)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	ok(w, map[string]any{
		"user": user,
		// Приватный ключ отдаётся ровно здесь и больше нигде: панель его не
		// хранит. Бот обязан сразу передать его покупателю.
		"private_key":  private,
		"account_link": a.accountLink(user, private, user.Label),
	})
}

func (a *API) getUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	ok(w, map[string]any{"user": user})
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	var p UpdateUserParams
	if !decode(w, r, &p) {
		return
	}
	user, err := a.store.UpdateUser(r.Context(), id, p)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	ok(w, map[string]any{"user": user})
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	if err := a.store.DeleteUser(r.Context(), id); err != nil {
		respondStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addCredential(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if r.ContentLength > 0 && !decode(w, r, &body) {
		return
	}

	cred, private, err := a.store.AddCredential(r.Context(), id, body.Label)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}

	ok(w, map[string]any{
		"credential":   cred,
		"private_key":  private,
		"account_link": a.accountLink(user, private, cred.Label),
	})
}

func (a *API) deleteCredential(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	if err := a.store.DeleteCredential(r.Context(), id); err != nil {
		respondStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listNodes(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"nodes": list})
}

func (a *API) createNode(w http.ResponseWriter, r *http.Request) {
	var p CreateNodeParams
	if !decode(w, r, &p) {
		return
	}
	if p.Address == "" {
		fail(w, http.StatusBadRequest, "нужен адрес ноды в виде host:port")
		return
	}

	node, token, err := a.store.CreateNode(r.Context(), p)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ok(w, map[string]any{
		"node": node,
		// Токен ноды тоже отдаётся один раз: в базе только его хеш.
		"token": token,
	})
}

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	if err := a.store.DeleteNode(r.Context(), id); err != nil {
		respondStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) nodeUsers(w http.ResponseWriter, r *http.Request, n Node) {
	list, err := a.store.NodeUsers(r.Context(), n.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"users": list})
}

func (a *API) nodeUsage(w http.ResponseWriter, r *http.Request, n Node) {
	var body struct {
		Usage map[string]users.Usage `json:"usage"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := a.store.ReportUsage(r.Context(), n.ID, body.Usage); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// subscription отдаёт список нод по токену подписки.
//
// Формат — как у всех: строки со ссылками, свёрнутые в base64. Плюс заголовок
// subscription-userinfo, по которому клиенты рисуют остаток трафика и дату
// окончания. Мелочь, но именно её продавцу приходится объяснять покупателям
// голосом, если её нет.
func (a *API) subscription(w http.ResponseWriter, r *http.Request) {
	user, err := a.store.UserBySubToken(r.Context(), r.PathValue("token"))
	if err != nil {
		// Не подсказываем, существует ли токен: перебор подписок — обычное
		// занятие тех, кто ищет чужие ноды.
		http.NotFound(w, r)
		return
	}

	nodes, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		lines = append(lines, nodeLink(n))
	}
	body := strings.Join(lines, "\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Subscription-Userinfo", userInfoHeader(user))
	w.Header().Set("Profile-Update-Interval", "12")

	if r.URL.Query().Get("format") == "raw" {
		_, _ = w.Write([]byte(body))
		return
	}
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
}

// nodeLink собирает ссылку на ноду.
func nodeLink(n Node) string {
	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	q.Set("fp", "chrome")

	link := "veil://" + url.PathEscape(n.PublicKey) + "@" + n.Address
	if encoded := q.Encode(); encoded != "" {
		link += "?" + encoded
	}
	if n.Name != "" {
		link += "#" + url.PathEscape(n.Name)
	}
	return link
}

// accountLink — то, что бот отправляет покупателю.
//
// В ссылке личный ключ и адрес подписки: клиент импортирует её один раз, а
// список нод потом обновляет сам. Ноды меняются часто, ключ — почти никогда.
func (a *API) accountLink(user User, privateKey, label string) string {
	base := a.subBase
	if base == "" {
		base = "https://ПОДСТАВЬ-АДРЕС-ПАНЕЛИ"
	}
	link := "veil-account://" + privateKey + "@" + strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://") +
		"/sub/" + user.SubToken
	if label != "" {
		link += "#" + url.PathEscape(label)
	}
	return link
}

// userInfoHeader формирует заголовок с остатком квоты.
func userInfoHeader(u User) string {
	parts := []string{
		"upload=0",
		fmt.Sprintf("download=%d", u.Used),
		fmt.Sprintf("total=%d", u.TrafficLimit),
	}
	if u.ExpiresAt != nil {
		parts = append(parts, fmt.Sprintf("expire=%d", u.ExpiresAt.Unix()))
	}
	return strings.Join(parts, "; ")
}

func bearer(r *http.Request) string {
	head := r.Header.Get("Authorization")
	if after, found := strings.CutPrefix(head, "Bearer "); found {
		return strings.TrimSpace(after)
	}
	return ""
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "идентификатор должен быть числом")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "не разобрал тело запроса: "+err.Error())
		return false
	}
	return true
}

func ok(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("отправка ответа: %v", err)
	}
}

func fail(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func respondStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		fail(w, http.StatusNotFound, "не найдено")
		return
	}
	fail(w, http.StatusInternalServerError, err.Error())
}

// ParseExpiry разбирает срок подписки, заданный человеком: либо дата, либо
// «через сколько», как «30d» или «12h». Второе удобнее боту: он продаёт месяц,
// а не «до 23 сентября».
func ParseExpiry(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if d, err := time.ParseDuration(strings.Replace(s, "d", "0h", 1)); err == nil && strings.HasSuffix(s, "d") {
		days, convErr := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if convErr == nil {
			t := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
			return &t, nil
		}
		t := time.Now().UTC().Add(d)
		return &t, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		t := time.Now().UTC().Add(d)
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("не понял срок %q: нужна дата в формате RFC3339 либо длительность вида 30d, 12h", s)
	}
	utc := t.UTC()
	return &utc, nil
}
