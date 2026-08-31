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

	"github.com/veilproject/veil/internal/routes"
	"github.com/veilproject/veil/internal/users"
)

// API — HTTP-интерфейс панели.
//
// Четыре круга доступа:
//   - админский токен — полный доступ и выпуск ключей. Им продавец входит в
//     панель, и больше он не должен попадать никуда;
//   - ключ доступа — права из числа users, nodes, read. Это то, что получает
//     бот продавца: отзывается отдельно и не даёт трогать ноды;
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

	// version — версия сборки. Отдаётся только с авторизацией: в /healthz,
	// который открыт всему интернету, точная версия говорит сканеру, какие
	// дыры пробовать, и заодно опознаёт панель как нашу.
	version string
}

// NewAPI собирает обработчики панели.
func NewAPI(store *Store, adminToken, subBase, distDir string) *API {
	return &API{store: store, adminToken: adminToken, subBase: strings.TrimRight(subBase, "/"), distDir: distDir}
}

// Handler возвращает готовый маршрутизатор.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Управление: пользователи. Это всё, что нужно боту продавца, и ровно
	// столько прав ему и достаётся.
	mux.HandleFunc("GET /api/v1/users", a.scoped(ScopeRead, a.listUsers))
	mux.HandleFunc("POST /api/v1/users", a.scoped(ScopeUsers, a.createUser))
	mux.HandleFunc("GET /api/v1/users/{id}", a.scoped(ScopeRead, a.getUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}", a.scoped(ScopeUsers, a.updateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", a.scoped(ScopeUsers, a.deleteUser))
	mux.HandleFunc("GET /api/v1/users/{id}/links", a.scoped(ScopeUsers, a.userLinks))
	mux.HandleFunc("POST /api/v1/users/{id}/credentials", a.scoped(ScopeUsers, a.addCredential))
	mux.HandleFunc("POST /api/v1/users/{id}/sub-token", a.scoped(ScopeUsers, a.rotateSubToken))
	mux.HandleFunc("DELETE /api/v1/credentials/{id}", a.scoped(ScopeUsers, a.deleteCredential))

	// Управление: ноды. Боту сюда не надо.
	mux.HandleFunc("GET /api/v1/nodes", a.scoped(ScopeRead, a.listNodes))
	mux.HandleFunc("POST /api/v1/nodes", a.scoped(ScopeNodes, a.createNode))
	mux.HandleFunc("PATCH /api/v1/nodes/{id}", a.scoped(ScopeNodes, a.updateNode))
	mux.HandleFunc("DELETE /api/v1/nodes/{id}", a.scoped(ScopeNodes, a.deleteNode))
	mux.HandleFunc("POST /api/v1/nodes/invite", a.scoped(ScopeNodes, a.createNodeInvite))

	// Ключи доступа. Только по админскому токену: ключ, умеющий выпускать
	// ключи, ничем не отличается от админского — и разделение теряет смысл.
	mux.HandleFunc("GET /api/v1/keys", a.admin(a.listKeys))
	mux.HandleFunc("POST /api/v1/keys", a.admin(a.createKey))
	mux.HandleFunc("DELETE /api/v1/keys/{id}", a.admin(a.revokeKey))

	// Копия базы. Тоже только админским: в ней лежит вся панель целиком.
	mux.HandleFunc("GET /api/v1/backup", a.admin(a.downloadBackup))

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

	// Приложения покупателям раздаёт сама панель — с домена продавца.
	// Подробности и причина в apps.go.
	mux.HandleFunc("GET /sub/{token}/app/{name}", a.appDownload)

	// Российские подсети для байпаса. По токену подписки: список не секрет, но
	// и раздавать его всему интернету с домена продавца незачем.
	mux.HandleFunc("GET /sub/{token}/bypass", a.bypassRoutes)
	mux.HandleFunc("GET /api/v1/apps", a.scoped(ScopeRead, a.listApps))
	mux.HandleFunc("GET /api/v1/stats", a.scoped(ScopeRead, a.stats))

	// Веб-интерфейс. Только по точному корню: всё остальное — 404, чтобы
	// панель не отвечала страницей на случайные пути сканеров.
	mux.HandleFunc("GET /{$}", a.ServeApp)

	// Версия — с авторизацией. Продавцу она нужна, когда он пишет в поддержку;
	// постороннему сканеру знать её незачем.
	mux.HandleFunc("GET /api/v1/version", a.scoped(ScopeRead, func(w http.ResponseWriter, _ *http.Request) {
		version := a.version
		if version == "" {
			version = "неизвестна"
		}
		ok(w, map[string]any{"version": version})
	}))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return mux
}

// scoped пускает по админскому токену или по ключу с нужным правом.
//
// Админский токен проверяется первым и за постоянное время: он лежит в памяти,
// и поход в базу за ним не нужен. Ключ ищется по хешу, и база заодно отмечает,
// что им воспользовались.
func (a *API) scoped(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			fail(w, http.StatusUnauthorized, "нужен токен: админский или ключ доступа")
			return
		}
		if TokensEqual(token, a.adminToken) {
			next(w, r)
			return
		}

		key, err := a.store.AuthenticateAPIKey(r.Context(), token)
		if err != nil {
			fail(w, http.StatusUnauthorized, "неизвестный токен")
			return
		}
		if !key.Allows(scope) {
			// Говорим, чего именно не хватает. «Доступ запрещён» без объяснения
			// заставляет выпустить ключ со всеми правами — и разделение, ради
			// которого всё затевалось, пропадает.
			fail(w, http.StatusForbidden, "ключу «"+key.Name+"» не хватает права "+scope)
			return
		}
		next(w, r)
	}
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

	// Ключ идемпотентности защищает и первую продажу, не только продление.
	//
	// Платёжные системы и телеграм повторяют уведомление, пока бот не ответил,
	// а бот может упасть ровно между выдачей доступа и ответом. Повтор той
	// оплаты, которая завела покупателя, без этого выдавал бы второй срок за
	// одни деньги: ErrAlreadyExists ловит только повтор по external_id, а
	// продавец, продающий без него, не защищён ничем.
	//
	// Область — вместе с покупателем: тот же ключ, пришедший на другого,
	// означает ошибку в боте, и лучше ответить 409, чем молча выдать первому.
	scope := "create:" + strings.TrimSpace(p.ExternalID)
	if handled := a.replayed(w, r, scope); handled {
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

	// Запоминаем ответ без секретов, хотя отдаём с ними.
	//
	// Приватную часть vp1 панель не хранит нигде — в этом весь смысл: утечка
	// её базы не даёт доступа ни к одному покупателю нашего протокола. Сложить
	// секрет в таблицу повторов ради удобства бота значило бы разменять это
	// свойство на сутки хранения. Поэтому повтор получает того же покупателя,
	// created=false и ссылки без ключа: доступ выдан один раз, и если бот его
	// потерял, выдаётся новый набор, а не старый.
	a.remember(r, scope, map[string]any{
		"user":    user,
		"created": false,
		"issued":  []Issued{},
		"links":   a.links(r.Context(), user, nil),
	})

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

	// Ссылки на приложения — с домена панели. Магазины приложений и чужие
	// файлохостинги отваливаются первыми, и покупатель застревает на шаге
	// «скачай», уже заплатив.
	if apps := a.appLinks(user.SubToken); apps != nil {
		out["apps"] = apps
	}

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

	// Ключ идемпотентности защищает продление: повтор уведомления от платёжной
	// системы иначе добавит срок дважды. Подробности в idempotency.go.
	scope := fmt.Sprintf("extend:%d", id)
	if handled := a.replayed(w, r, scope); handled {
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

	a.remember(r, scope, map[string]any{"user": user})
	ok(w, map[string]any{"user": user})
}

// replayed отдаёт сохранённый ответ, если запрос с этим ключом уже проходил.
//
// true означает, что ответ уже отправлен и обработчику делать нечего: либо это
// повтор, либо ключ занят другой операцией, либо он негоден.
func (a *API) replayed(w http.ResponseWriter, r *http.Request, scope string) bool {
	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" {
		return false
	}
	if len(key) > maxIdempotencyKey {
		fail(w, http.StatusBadRequest, "ключ идемпотентности длиннее допустимого")
		return true
	}

	stored, found, err := a.store.RememberedResponse(r.Context(), key, scope)
	if errors.Is(err, ErrIdempotencyScope) {
		fail(w, http.StatusConflict, err.Error())
		return true
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if !found {
		return false
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(stored))
	return true
}

// remember запоминает ответ под ключом идемпотентности, если он был задан.
func (a *API) remember(r *http.Request, scope string, payload any) {
	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Неудачу записи глотаем намеренно: операция уже прошла, и отвечать
	// продавцу ошибкой из-за незапомненного ответа хуже, чем рискнуть
	// повтором. Худший случай здесь — ровно то поведение, что было раньше.
	_ = a.store.RememberResponse(r.Context(), key, scope, string(body))
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

	answer := map[string]any{
		"subscription": a.subURL(user.SubToken),
		"stock":        StockLinks(nodes, user.Credentials, user.Label),
	}
	if apps := a.appLinks(user.SubToken); apps != nil {
		answer["apps"] = apps
	}
	ok(w, answer)
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

// updateNode переименовывает ноду, проставляет ей страну и выключает её.
//
// Выключение — это то, что делают в первую очередь при подозрении на
// блокировку: нода уходит из подписок, но её статистика остаётся, и решение
// отменяется обратным запросом.
func (a *API) updateNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	var p UpdateNodeParams
	if !decode(w, r, &p) {
		return
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		fail(w, http.StatusBadRequest, "имя ноды не может быть пустым")
		return
	}

	node, err := a.store.UpdateNode(r.Context(), id, p)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	ok(w, map[string]any{"node": node})
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
		ID   int64  `json:"id"`
		Name string `json:"name"`

		// Country — то, что продавец написал руками. Флаг клиент подбирает
		// сам: держать картинки на стороне панели незачем.
		Country   string `json:"country,omitempty"`
		Address   string `json:"address"`
		SNI       string `json:"sni,omitempty"`
		PublicKey string `json:"public_key"`
		Link      string `json:"link"`

		WSPath           string `json:"ws_path,omitempty"`
		QUIC             bool   `json:"quic,omitempty"`
		RealityPublicKey string `json:"reality_public_key,omitempty"`
		RealityShortID   string `json:"reality_short_id,omitempty"`
	}

	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		views = append(views, nodeView{
			ID: n.ID, Name: n.Name, Country: n.Country, Address: n.Address, SNI: n.SNI,
			PublicKey: n.PublicKey, Link: VeilNodeLink(n),
			WSPath:           n.WSPath,
			QUIC:             n.QUIC,
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

// WithVersion сообщает панели её версию сборки.
func (a *API) WithVersion(v string) *API {
	a.version = v
	return a
}

// listKeys перечисляет живые ключи доступа.
func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.store.ListAPIKeys(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"keys": keys})
}

// createKey выпускает ключ. Секрет уходит в ответ один раз и больше нигде не
// появляется — в базе от него только хеш.
func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !decode(w, r, &p) {
		return
	}

	key, secret, err := a.store.CreateAPIKey(r.Context(), p.Name, p.Scopes)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	ok(w, map[string]any{
		"key":    key,
		"secret": secret,
		"note":   "секрет показывается один раз: в базе лежит только его хеш",
	})
}

// revokeKey отзывает ключ.
func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	if err := a.store.RevokeAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			fail(w, http.StatusNotFound, "ключа с таким номером нет или он уже отозван")
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateSubToken меняет ссылку подписки, не трогая доступ.
//
// Ссылку подписки покупатели раздают знакомым: она попадает в переписки, на
// форумы и в чужие приложения, а по ней отдаются список нод и секреты vless с
// trojan. Продавцу нужен ответ мягче, чем отзыв всего доступа, — иначе за одну
// утёкшую ссылку он теряет покупателя.
//
// Старая ссылка умирает сразу — вместе с ней перестаёт работать и ссылка
// доступа покупателя: в ней зашит путь /sub/<токен>, по которому приложение
// забирает список нод. Поэтому смена адреса подписки всегда идёт в паре с
// выдачей нового набора доступа: POST /users/{id}/credentials.
//
// И это не отменяет отзыва утёкших наборов. Тот, кто успел прочитать старую
// подписку, унёс из неё секреты vless и trojan, и они действуют, пока их не
// отозвали: смена адреса закрывает будущие чтения, а не прошлые.
func (a *API) rotateSubToken(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}

	user, err := a.store.RotateSubToken(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}

	ok(w, map[string]any{
		"user": user,
		// Секретов здесь нет: меняется только адрес подписки, наборы доступа
		// остаются прежними, и заново их панель не выдаёт.
		"links": a.links(r.Context(), user, nil),
	})
}

// stats отдаёт историю расхода: по суткам и по нодам.
//
// Числа считает панель, а не браузер: у продавца может быть тысяча покупателей
// и год истории, и тащить это в страницу целиком ради трёх графиков — способ
// подвесить его ноутбук.
func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			fail(w, http.StatusBadRequest, "days: нужно положительное число дней")
			return
		}
		// Год с запасом. Больше — не отказ, а тихое обрезание: график за пять
		// лет всё равно нечитаем, а запрос на такую выборку легко сделать
		// случайно.
		days = min(n, 400)
	}

	byDay, err := a.store.UsageByDay(r.Context(), days)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	byNode, err := a.store.UsageByNode(r.Context(), days)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	var total int64
	for _, d := range byDay {
		total += d.Up + d.Down
	}

	ok(w, map[string]any{
		"days":    days,
		"by_day":  byDay,
		"by_node": byNode,
		"total":   total,
	})
}

// bypassRoutes отдаёт российские подсети, которые клиент ведёт мимо туннеля.
//
// Зачем это нужно. Нода стоит за границей, и для госуслуг, банков и всего
// государственного человек оказывается иностранцем — они просто не отвечают.
// Клиент исключает эти подсети из туннеля, и такие сайты открываются с
// настоящего адреса.
//
// Список отдаёт панель, а не приложение носит его в себе: он меняется раз в
// месяц, а обновление приложения у покупателя упирается в магазин, который в
// нужный момент как раз и не работает.
func (a *API) bypassRoutes(w http.ResponseWriter, r *http.Request) {
	if _, err := a.store.UserBySubToken(r.Context(), r.PathValue("token")); err != nil {
		// Как и подписка: не подсказываем, существует ли токен.
		http.NotFound(w, r)
		return
	}

	prefixes, err := routes.RussianPrefixes()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	ok(w, map[string]any{"prefixes": prefixes})
}
