package panel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	distDir    string // где лежат бинарники для раздачи новым нодам

	// panelIPs вписываются в ссылку доступа, чтобы клиент не спрашивал имя
	// домена подписки у резолвера провайдера. Подробности — в links.go.
	panelIPs []string
}

// NewAPI собирает обработчики панели.
func NewAPI(store *Store, adminToken, subBase, distDir string) *API {
	return &API{store: store, adminToken: adminToken, subBase: strings.TrimRight(subBase, "/"), distDir: distDir}
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
	mux.HandleFunc("GET /api/v1/users/{id}/links", a.admin(a.userLinks))
	mux.HandleFunc("POST /api/v1/users/{id}/credentials", a.admin(a.addCredential))
	mux.HandleFunc("DELETE /api/v1/credentials/{id}", a.admin(a.deleteCredential))

	// Управление: ноды.
	mux.HandleFunc("GET /api/v1/nodes", a.admin(a.listNodes))
	mux.HandleFunc("POST /api/v1/nodes", a.admin(a.createNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", a.admin(a.deleteNode))
	mux.HandleFunc("POST /api/v1/nodes/invite", a.admin(a.createNodeInvite))

	// Установка ноды одной командой. Приглашение стоит в адресе, потому что
	// команду продавец вставляет целиком, не разбираясь в заголовках.
	mux.HandleFunc("GET /install/{token}", a.installScript)
	mux.HandleFunc("GET /install/{token}/{name}", a.installBinary)
	mux.HandleFunc("POST /api/v1/nodes/register", a.registerNode)

	// Ноды забирают свой список и сдают статистику.
	mux.HandleFunc("GET /api/v1/node/users", a.node(a.nodeUsers))
	mux.HandleFunc("POST /api/v1/node/usage", a.node(a.nodeUsage))

	// Подписка. Токен в адресе и есть авторизация.
	mux.HandleFunc("GET /sub/{token}", a.subscription)

	// Отчёты о доступности от клиентов. Тем же токеном: админского на
	// устройстве покупателя быть не должно ни при каких обстоятельствах.
	mux.HandleFunc("POST /sub/{token}/report", a.report)

	// Веб-интерфейс. Только по точному корню: всё остальное — 404, чтобы
	// панель не отвечала страницей на случайные пути сканеров.
	mux.HandleFunc("GET /{$}", a.ServeApp)

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
	// Поиск по ключу продавца: боту надо по telegram id понять, кто перед ним,
	// и не держать ради этого вторую базу соответствий.
	if external := strings.TrimSpace(r.URL.Query().Get("external_id")); external != "" {
		user, err := a.store.UserByExternalID(r.Context(), external)
		if err != nil {
			respondStoreErr(w, err)
			return
		}
		ok(w, map[string]any{"users": []User{user}})
		return
	}

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

	user, issued, err := a.store.CreateUser(r.Context(), p)

	// Такой покупатель уже заведён — значит уведомление об оплате пришло
	// повторно. Отвечаем успехом и говорим, что ничего не создали: бот на
	// повторе не должен ни падать, ни выдавать второй доступ за ту же оплату.
	if errors.Is(err, ErrAlreadyExists) {
		ok(w, map[string]any{
			"user":    user,
			"created": false,
			"issued":  []Issued{},
			"links":   a.links(r.Context(), user, nil),
		})
		return
	}
	if err != nil {
		if errors.Is(err, ErrUnknownKind) {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	ok(w, map[string]any{
		"user":    user,
		"created": true,
		// Секреты отдаются ровно здесь и больше нигде. Для vp1 панель не
		// хранит приватную часть вовсе; для vless и trojan хранит, но
		// повторно через API не отдаёт. Бот обязан сразу переслать ссылки
		// покупателю.
		"issued": issued,
		"links":  a.links(r.Context(), user, issued),
	})
}

// links собирает готовые к отправке ссылки.
//
// Бот не должен ничего склеивать сам: одна ошибка в параметрах ссылки — и
// покупатель приходит в поддержку с «не работает», а продавец не понимает,
// в чём дело.
func (a *API) links(ctx context.Context, user User, issued []Issued) map[string]any {
	out := map[string]any{"subscription": a.subURL(user.SubToken)}

	stock := make([]Credential, 0, len(issued))
	for _, i := range issued {
		switch i.Kind {
		case CredVP1:
			out["account"] = AccountLink(a.subBase, i.Secret, user.SubToken, user.Label, a.panelIPs)
		default:
			stock = append(stock, Credential{Kind: i.Kind, Secret: i.Secret})
		}
	}

	if len(stock) == 0 {
		return out
	}

	nodes, err := a.store.ListNodes(ctx)
	if err != nil {
		// Ссылки на ноды не собрались, но подписку отдать всё равно можно:
		// клиент возьмёт список оттуда.
		log.Printf("сборка ссылок: %v", err)
		return out
	}
	if links := StockLinks(nodes, stock, user.Label); len(links) > 0 {
		out["stock"] = links
	}
	return out
}

// subURL — адрес подписки для чужих клиентов.
func (a *API) subURL(token string) string {
	base := a.subBase
	if base == "" {
		base = "https://ПОДСТАВЬ-АДРЕС-ПАНЕЛИ"
	}
	return base + "/sub/" + token
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
	// ExtendBy отдельно от остальных полей: это не «поставить срок», а
	// «добавить к тому, что есть». Разница видна на покупателе, который
	// продлевает за неделю до конца: с абсолютным сроком он эту неделю теряет.
	var p struct {
		UpdateUserParams
		ExtendBy string `json:"extend_by,omitempty"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.ExtendBy != "" && p.ExpiresAt != nil {
		fail(w, http.StatusBadRequest, "extend_by и expires_at вместе не работают: либо продлить, либо назначить срок")
		return
	}

	if p.ExtendBy != "" {
		d, err := ParseDuration(p.ExtendBy)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := a.store.ExtendUser(r.Context(), id, d); err != nil {
			respondStoreErr(w, err)
			return
		}
	}

	user, err := a.store.UpdateUser(r.Context(), id, p.UpdateUserParams)
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

// userLinks отдаёт ссылки уже заведённого подписчика.
//
// Нужно для самого частого обращения в поддержку: «потерял конфиг, пришлите
// заново». Для vless и trojan ссылку можно собрать снова — секрет хранится в
// базе. Для vp1 нельзя: приватной части у панели нет, и это не недоработка,
// а осознанное свойство. Такому подписчику выпускают новый ключ.
func (a *API) userLinks(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}

	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	nodes, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	ok(w, map[string]any{
		"subscription": a.subURL(user.SubToken),
		"stock":        StockLinks(nodes, user.Credentials, user.Label),
	})
}

func (a *API) addCredential(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	var body struct {
		Kind  string `json:"kind"`
		Label string `json:"label"`
	}
	if r.ContentLength > 0 && !decode(w, r, &body) {
		return
	}

	cred, secret, err := a.store.AddCredential(r.Context(), id, body.Kind, body.Label)
	if err != nil {
		if errors.Is(err, ErrUnknownKind) {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		respondStoreErr(w, err)
		return
	}
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}

	issued := []Issued{{ID: cred.ID, Kind: cred.Kind, Secret: secret}}
	ok(w, map[string]any{
		"credential": cred,
		"issued":     issued,
		"links":      a.links(r.Context(), user, issued),
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

// report принимает от клиента отчёты о доступности нод.
//
// Ответ всегда 204, даже если часть отчётов не про наши ноды: подписка на
// устройстве могла устареть, и превращать это в ошибку незачем.
func (a *API) report(w http.ResponseWriter, r *http.Request) {
	user, err := a.store.UserBySubToken(r.Context(), r.PathValue("token"))
	if err != nil {
		// Как и в подписке, не подсказываем, существует ли токен.
		http.NotFound(w, r)
		return
	}

	var body struct {
		Reports []Report `json:"reports"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Reports) > maxReportsPerRequest {
		fail(w, http.StatusBadRequest, "слишком много отчётов в одном запросе")
		return
	}

	if err := a.store.SaveReports(r.Context(), user.ID, body.Reports); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxReportsPerRequest — верхняя граница на один запрос. Нод у продавца
// десятки, а не тысячи; всё сверх этого — попытка нагрузить панель.
const maxReportsPerRequest = 256

func (a *API) listNodes(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Продавцу нужна не просто «нода включена», а «сколько людей до неё не
	// доходит». Без этого он узнаёт о блокировке из обращений в поддержку.
	health, err := a.store.NodeHealth(r.Context())
	if err != nil {
		log.Printf("сводка доступности: %v", err)
		health = map[int64]Health{}
	}

	type view struct {
		Node
		Health   Health `json:"health"`
		Degraded bool   `json:"degraded"`
	}

	views := make([]view, 0, len(list))
	for _, n := range list {
		h := health[n.ID]
		views = append(views, view{Node: n, Health: h, Degraded: h.Degraded()})
	}

	ok(w, map[string]any{"nodes": views})
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

	// Ноды идут в том порядке, в каком их стоит пробовать: сверху те, на
	// которые не жалуются и которые отвечают быстрее. Ничего не выбрасываем —
	// отчёты приходят от недоверенных клиентов, и один вредитель не должен
	// лишать остальных рабочей ноды.
	if health, err := a.store.NodeHealth(r.Context()); err == nil {
		nodes = RankNodes(nodes, health)
	} else {
		log.Printf("сводка доступности: %v", err)
	}

	w.Header().Set("Subscription-Userinfo", userInfoHeader(user))
	w.Header().Set("Profile-Update-Interval", "12")

	if r.URL.Query().Get("format") == "json" {
		a.subscriptionJSON(w, user, nodes)
		return
	}

	// По умолчанию отдаём то, что понимают чужие приложения. Наши ссылки
	// сюда не попадают намеренно: на незнакомой схеме часть клиентов
	// спотыкается и не принимает подписку целиком.
	body := strings.Join(StockLinks(nodes, user.Credentials, user.Label), "\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Query().Get("format") == "raw" {
		_, _ = w.Write([]byte(body))
		return
	}
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
}

// subscriptionJSON — подписка для нашего клиента.
//
// Секретов здесь нет и быть не должно: свой ключ клиент получил один раз в
// ссылке аккаунта, а список нод он обновляет постоянно и по открытому каналу.
func (a *API) subscriptionJSON(w http.ResponseWriter, user User, nodes []Node) {
	// Транспорт передаётся явно: нашему клиенту нужно знать, как именно
	// подключаться, а разбирать это из ссылки — лишний источник расхождений
	// между тем, что собрала панель, и тем, что понял клиент.
	type nodeView struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Address   string `json:"address"`
		SNI       string `json:"sni,omitempty"`
		PublicKey string `json:"public_key"`
		Link      string `json:"link"`

		WSPath           string `json:"ws_path,omitempty"`
		RealityPublicKey string `json:"reality_public_key,omitempty"`
		RealityShortID   string `json:"reality_short_id,omitempty"`
	}

	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		views = append(views, nodeView{
			ID: n.ID, Name: n.Name, Address: n.Address, SNI: n.SNI,
			PublicKey: n.PublicKey, Link: VeilNodeLink(n),
			WSPath:           n.WSPath,
			RealityPublicKey: n.RealityPublicKey,
			RealityShortID:   n.RealityShortID,
		})
	}

	ok(w, map[string]any{
		"nodes":         views,
		"traffic_limit": user.TrafficLimit,
		"used":          user.Used,
		"expires_at":    user.ExpiresAt,
	})
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

// ParseDuration разбирает срок продления: «30d», «12h», «90m».
//
// time.ParseDuration дней не знает, а бот продаёт именно месяцы и дни, а не
// семьсот двадцать часов.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("не понял срок %q: нужно число дней, например 30d", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("не понял срок %q: нужно 30d, 12h или 90m", s)
	}
	return d, nil
}

// WithPanelIPs вписывает адреса панели в выдаваемые ссылки доступа.
//
// Отдельным вызовом, а не ещё одним доводом конструктора: адреса не нужны для
// работы панели и появляются позже остального — их выясняет команда запуска.
func (a *API) WithPanelIPs(ips []string) *API {
	a.panelIPs = ips
	return a
}
