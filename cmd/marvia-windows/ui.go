//go:build windows

package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/look"
)

//go:embed ui/app.html
var appHTML string

// appPage — страница с вшитой темой. Собирается один раз: тема не меняется
// между отдачами, а метку в разметке ищет тест, не отдача.
var appPage = look.Inline(appHTML)

// version подставляется при сборке релиза через -ldflags "-X main.version=…".
// Окно показывает её в шапке: человек, у которого что-то не работает,
// первым делом спрашивает у продавца «а какая у меня версия».
var version = "dev"

// journal — последние строки о происходящем, для вкладки «Журнал».
//
// Кольцо, а не растущий список: программа может работать сутками, и незачем
// копить в памяти всё, что она когда-либо сказала.
type journal struct {
	mu    sync.Mutex
	lines []string
}

// Сколько даём на замер всех нод и на переключение между ними.
//
// Замер идёт настоящими подключениями ко всем нодам разом, поэтому он не
// мгновенный; переключение — это обычное подключение, только к заданной ноде.
const (
	measureTimeout = 30 * time.Second
	selectTimeout  = 60 * time.Second
)

const journalDepth = 200

func newJournal() *journal { return &journal{} }

func (j *journal) add(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)

	j.mu.Lock()
	j.lines = append(j.lines, line)
	if len(j.lines) > journalDepth {
		j.lines = j.lines[len(j.lines)-journalDepth:]
	}
	j.mu.Unlock()
}

func (j *journal) snapshot() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, len(j.lines))
	copy(out, j.lines)
	return out
}

// ui — маленький сервер, который показывает окно и принимает от него команды.
type ui struct {
	ctl *Controller
	log *journal
	key string

	// onWindow переключает вид окна по просьбе страницы: из виджета в
	// полное окно на вкладку, крестиком виджета — в трей. Указатель, потому
	// что окно появляется позже сервера.
	onWindow *func(mode, tab string)
}

// serveUI поднимает интерфейс и возвращает адрес, который надо открыть.
//
// Слушаем только на петле, но и этого мало: по адресу /api/account отдаётся
// ссылка доступа с личным ключом покупателя, а на компьютере может работать
// что угодно, в том числе чужое. Поэтому всё лежит под одноразовым ключом в
// адресе — угадать его чужой программе не проще, чем подобрать пароль.
func serveUI(ctl *Controller, log *journal, onWindow *func(mode, tab string)) (string, *http.Server, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)

	u := &ui{ctl: ctl, log: log, key: key, onWindow: onWindow}

	mux := http.NewServeMux()
	prefix := "/" + key
	mux.HandleFunc("GET "+prefix+"/{$}", u.page)
	mux.HandleFunc("GET "+prefix+"/api/state", u.state)
	mux.HandleFunc("GET "+prefix+"/api/log", u.journal)
	mux.HandleFunc("GET "+prefix+"/api/account", u.getAccount)
	mux.HandleFunc("POST "+prefix+"/api/account", u.setAccount)
	mux.HandleFunc("POST "+prefix+"/api/connect", u.connect)
	mux.HandleFunc("POST "+prefix+"/api/disconnect", u.disconnect)
	mux.HandleFunc("GET "+prefix+"/api/nodes", u.nodes)
	mux.HandleFunc("POST "+prefix+"/api/nodes/measure", u.measureNodes)
	mux.HandleFunc("POST "+prefix+"/api/nodes/select", u.selectNode)
	mux.HandleFunc("POST "+prefix+"/api/proxy/off", u.dropProxy)
	mux.HandleFunc("POST "+prefix+"/api/lang", u.setLang)
	mux.HandleFunc("POST "+prefix+"/api/window", u.window)
	mux.HandleFunc("GET "+prefix+"/api/autostart", u.getAutostart)
	mux.HandleFunc("POST "+prefix+"/api/autostart", u.setAutostart)

	// Шрифты и знак — общие с панелью, из того же пакета. Под тем же
	// одноразовым ключом: адреса под ним не угадать, и чужой программе на
	// этой машине нечего опрашивать.
	mux.HandleFunc("GET "+prefix+"/fonts/{name}", look.ServeFont)
	mux.HandleFunc("GET "+prefix+"/assets/{name}", look.ServeAsset)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("не занять порт для окна: %w", err)
	}

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()

	return fmt.Sprintf("http://%s%s/", ln.Addr().String(), prefix), server, nil
}

func (u *ui) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(appPage))
}

func (u *ui) state(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, u.ctl.Status())
}

func (u *ui) journal(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"lines": u.log.snapshot()})
}

func (u *ui) getAccount(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"link": u.ctl.Account()})
}

func (u *ui) setAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": say("badRequest")})
		return
	}
	if err := u.ctl.SetAccount(strings.TrimSpace(body.Link)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	u.log.add("%s", say("logKeySaved"))
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (u *ui) connect(w http.ResponseWriter, _ *http.Request) {
	if err := u.ctl.Connect(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (u *ui) disconnect(w http.ResponseWriter, _ *http.Request) {
	u.ctl.Disconnect()
	writeJSON(w, http.StatusOK, map[string]any{})
}

// nodes отдаёт список нод без замера: тем, что уже известно.
func (u *ui) nodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"nodes": u.ctl.Nodes()})
}

// measureNodes перемеряет все ноды. Секунды, а не мгновение: каждая нода
// опрашивается настоящим подключением, иначе число было бы выдумкой.
func (u *ui) measureNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), measureTimeout)
	defer cancel()

	writeJSON(w, http.StatusOK, map[string]any{"nodes": u.ctl.MeasureNodes(ctx)})
}

// selectNode переводит туннель на выбранную ноду. Ноль — обратно к автовыбору.
func (u *ui) selectNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": say("badRequest")})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), selectTimeout)
	defer cancel()

	if err := u.ctl.SelectNode(ctx, body.ID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": u.ctl.Nodes()})
}

// dropProxy снимает системный прокси — по нажатию человека, не сам.
func (u *ui) dropProxy(w http.ResponseWriter, _ *http.Request) {
	if err := u.ctl.DropProxy(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, u.ctl.Status())
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

// setLang запоминает язык, выбранный в окне.
//
// Выбирает его окно, а не программа: язык там берётся у браузера, то есть у
// системы, и человек может его переключить. Программе он нужен затем, что
// часть сообщений — ошибки и строки журнала — собирается здесь.
func (u *ui) setLang(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lang string `json:"lang"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": say("badRequest")})
		return
	}
	setUILang(body.Lang)
	writeJSON(w, http.StatusOK, map[string]any{})
}

// window — страница просит переключить вид окна: «full» с вкладкой или «hide».
func (u *ui) window(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
		Tab  string `json:"tab"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": say("badRequest")})
		return
	}
	// Вкладку проверяем по списку: она уходит в адрес страницы, и мусор в
	// ней — это мусор в адресной строке движка.
	switch body.Tab {
	case "", "home", "nodes", "key", "log", "theme", "about":
	default:
		body.Tab = ""
	}
	if u.onWindow != nil && *u.onWindow != nil {
		(*u.onWindow)(body.Mode, body.Tab)
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (u *ui) getAutostart(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"on": autostartOn()})
}

func (u *ui) setAutostart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": say("badRequest")})
		return
	}
	if err := setAutostart(body.On); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.On {
		u.log.add("%s", say("logAutostartOn"))
	} else {
		u.log.add("%s", say("logAutostartOff"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": autostartOn()})
}
