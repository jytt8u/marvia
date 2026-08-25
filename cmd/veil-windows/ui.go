//go:build windows

package main

import (
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
)

//go:embed ui/app.html
var appHTML string

// journal — последние строки о происходящем, для вкладки «Журнал».
//
// Кольцо, а не растущий список: программа может работать сутками, и незачем
// копить в памяти всё, что она когда-либо сказала.
type journal struct {
	mu    sync.Mutex
	lines []string
}

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
}

// serveUI поднимает интерфейс и возвращает адрес, который надо открыть.
//
// Слушаем только на петле, но и этого мало: по адресу /api/account отдаётся
// ссылка доступа с личным ключом покупателя, а на компьютере может работать
// что угодно, в том числе чужое. Поэтому всё лежит под одноразовым ключом в
// адресе — угадать его чужой программе не проще, чем подобрать пароль.
func serveUI(ctl *Controller, log *journal) (string, *http.Server, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	key := base64.RawURLEncoding.EncodeToString(raw)

	u := &ui{ctl: ctl, log: log, key: key}

	mux := http.NewServeMux()
	prefix := "/" + key
	mux.HandleFunc("GET "+prefix+"/{$}", u.page)
	mux.HandleFunc("GET "+prefix+"/api/state", u.state)
	mux.HandleFunc("GET "+prefix+"/api/log", u.journal)
	mux.HandleFunc("GET "+prefix+"/api/account", u.getAccount)
	mux.HandleFunc("POST "+prefix+"/api/account", u.setAccount)
	mux.HandleFunc("POST "+prefix+"/api/connect", u.connect)
	mux.HandleFunc("POST "+prefix+"/api/disconnect", u.disconnect)

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
	_, _ = w.Write([]byte(appHTML))
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "не разобрал запрос"})
		return
	}
	if err := u.ctl.SetAccount(strings.TrimSpace(body.Link)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	u.log.add("ключ доступа сохранён")
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

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
