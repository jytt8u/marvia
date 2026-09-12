//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Режим продавца.
//
// Одна и та же программа служит двоим: покупателю — туннелем, продавцу —
// панелью. Панель при этом не переписывается заново на компьютере: окно
// открывает ту же веб-панель внутри себя по ссылке-приглашению. Второй
// набор экранов поверх того же API отставал бы от панели через месяц, а
// продавцу нужны все её разделы, а не половина.
//
// Ссылка-приглашение: marvia-panel://<токен>@<адрес панели>/. Токен — тот, с
// которым продавец вошёл в панель: администраторский или ключ бота с его
// правами. Ссылку показывает сама панель на странице приложений.

// PanelScheme — схема ссылки-приглашения в панель.
const PanelScheme = "marvia-panel"

// Роли: кто пользуется этой копией программы. Пусто — ещё не выбрано, и
// окно спрашивает «Кто вы?» первым экраном.
const (
	roleUser   = "user"
	roleSeller = "seller"
)

// partner — что программа знает о панели продавца.
type partner struct {
	Link string `json:"-"`
	Host string `json:"host"`
	// Кем войдём: из /api/v1/whoami панели, проверено при подключении.
	Admin  bool     `json:"admin"`
	Key    string   `json:"key,omitempty"`
	Scopes []string `json:"scopes"`
}

// parsePanelLink разбирает ссылку-приглашение. Токен и адрес обязательны.
func parsePanelLink(link string) (token, host string, err error) {
	link = strings.TrimSpace(link)
	u, err := url.Parse(link)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", say("badPanelLink"), err)
	}
	if u.Scheme != PanelScheme || u.User == nil || u.User.Username() == "" || u.Host == "" {
		return "", "", errors.New(say("badPanelLink"))
	}
	return u.User.Username(), u.Host, nil
}

// panelURL — адрес панели по хосту. Только https: по этому адресу ходит
// токен, и отдавать его открытым текстом нельзя.
func panelURL(host string) string { return "https://" + host + "/" }

// checkPanel спрашивает у панели, чей это токен. Заодно это проверка, что
// панель вообще там есть и ссылка не битая.
func checkPanel(ctx context.Context, token, host string) (partner, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, panelURL(host)+"api/v1/whoami", nil)
	if err != nil {
		return partner{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return partner{}, fmt.Errorf("%s: %w", say("panelUnreachable"), err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return partner{}, errors.New(say("panelRejected"))
	default:
		return partner{}, fmt.Errorf("%s: HTTP %d", say("panelUnreachable"), resp.StatusCode)
	}

	var who struct {
		Admin  bool     `json:"admin"`
		Key    string   `json:"key"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&who); err != nil {
		return partner{}, fmt.Errorf("%s: %w", say("panelUnreachable"), err)
	}
	if who.Scopes == nil {
		who.Scopes = []string{}
	}
	return partner{Host: host, Admin: who.Admin, Key: who.Key, Scopes: who.Scopes}, nil
}

// panelPath — где лежит ссылка-приглашение: рядом с ключом доступа, теми же
// правами. Это вход в панель, и лежит он у продавца на его машине.
func panelPath() (string, error) {
	dir, err := settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panel"), nil
}

func rolePath() (string, error) {
	dir, err := settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "role"), nil
}

func readPanelLink() string {
	path, err := panelPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func writePanelLink(link string) error {
	path, err := panelPath()
	if err != nil {
		return err
	}
	if link == "" {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(link), 0o600)
}

func readRole() string {
	path, err := rolePath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	role := strings.TrimSpace(string(raw))
	if role != roleUser && role != roleSeller {
		return ""
	}
	return role
}

func writeRole(role string) error {
	if role != roleUser && role != roleSeller {
		return errors.New(say("badRequest"))
	}
	path, err := rolePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(role), 0o600)
}

// panelFromArgs достаёт ссылку-приглашение из аргументов запуска: её
// нажали в панели, и Windows позвала нас с ней.
func panelFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), PanelScheme+"://") {
			return arg
		}
	}
	return ""
}
