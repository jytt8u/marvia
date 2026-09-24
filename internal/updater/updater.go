// Package updater — обновление панели и нод по кнопке в панели.
//
// Панель и нода работают от служебного пользователя и обновить себя не могут:
// бинарники подменяет root. Поэтому на каждой машине стоит маленькая служба
// обновления (agent.sh), которую systemd будит, когда в каталоге панели или
// ноды появляется файл-просьба. Процесс без прав кладёт просьбу (Request), а
// что и откуда ставить, решает служба: последний релиз с GitHub, сверенный с
// его SHA256SUMS. Взломанная панель может разве что попросить обновиться на
// официальный релиз — подсунуть свой бинарник ей некуда.
//
// Служба ставится флагом -install-updater у marvia-panel и marvia-node:
// установщики и upgrade.sh зовут его свежим бинарником из релиза, так что у
// сценариев, панели и ноды один источник — этот пакет.
package updater

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed agent.sh
var agent []byte

// Пути на машине. Переменные, а не константы, — ради проверок.
var (
	// StateDir — где служба оставляет состояние и журнал. Принадлежит root:
	// панель читает отсюда, но подменить статус не может.
	StateDir = "/var/lib/marvia-upgrade"

	// AgentPath — сценарий службы; есть файл — служба стоит.
	AgentPath = "/usr/local/lib/marvia/upgrade-agent.sh"
	unitDir   = "/etc/systemd/system"
)

// Каталоги, в которых ждут просьбу. Те же, что знает upgrade.sh.
var watched = []string{"/opt/marvia", "/opt/marvia-node"}

const serviceUnit = `[Unit]
Description=Marvia upgrade on request from the panel or node
# Сама служба ничего не решает: она ставит последний релиз с GitHub,
# сверенный с SHA256SUMS. Подробности — internal/updater.

[Service]
Type=oneshot
ExecStart=/bin/sh ` + "%s" + `
TimeoutStartSec=20min
`

const pathUnit = `[Unit]
Description=Marvia upgrade requests

[Path]
%s
Unit=marvia-upgrade.service

[Install]
WantedBy=multi-user.target
`

// Install ставит службу обновления: сценарий, два юнита и каталог состояния.
// Нужен root. Повторный вызов обновляет файлы — upgrade.sh зовёт его при
// каждом обновлении, чтобы служба приезжала вместе с релизом.
func Install() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("нужен GitHub CLI с gh attestation verify: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(AgentPath), 0o755); err != nil {
		return err
	}
	// Через временный файл и переименование: сценарий может как раз сейчас
	// исполняться (это он позвал upgrade.sh, а тот — нас), а shell читает
	// файл по ходу дела. Переписанный на месте файл он дочитал бы чужим.
	if err := writeAtomic(AgentPath, agent, 0o755); err != nil {
		return err
	}

	var watch []string
	for _, dir := range watched {
		watch = append(watch, "PathExists="+RequestPath(dir))
	}
	units := map[string]string{
		"marvia-upgrade.service": fmt.Sprintf(serviceUnit, AgentPath),
		"marvia-upgrade.path":    fmt.Sprintf(pathUnit, strings.Join(watch, "\n")),
	}
	for name, body := range units {
		if err := writeAtomic(filepath.Join(unitDir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(StateDir, 0o755); err != nil {
		return err
	}

	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "--now", "marvia-upgrade.path"},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// RequestPath — где лежит просьба об обновлении для службы из каталога home.
func RequestPath(home string) string {
	return filepath.Join(home, "upgrade", "request")
}

// Request просит службу обновления обновить машину. home — каталог панели
// или ноды, куда у процесса есть право писать.
//
// В просьбу ничего не пишется, кроме времени: служба её не читает, и любой
// «параметр» в ней был бы соблазном начать ему доверять.
func Request(home string) error {
	path := RequestPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// Status — что служба обновления сообщила о последнем запуске.
type Status struct {
	// Installed — служба стоит на этой машине. Без неё кнопке в панели
	// нечем работать, и панель прямо говорит, что сделать один раз руками.
	Installed bool `json:"installed"`

	// Pending — просьба лежит и ещё не подобрана.
	Pending bool `json:"pending"`

	// State — running, ok или failed; пусто, если служба ещё не запускалась.
	State  string    `json:"state,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Reason string    `json:"reason,omitempty"`

	// Log — хвост журнала последнего запуска, без цветов терминала.
	Log string `json:"log,omitempty"`
}

// ReadStatus читает состояние службы обновления для каталога home.
func ReadStatus(home string) Status {
	var s Status
	if _, err := os.Stat(AgentPath); err == nil {
		s.Installed = true
	}
	if _, err := os.Stat(RequestPath(home)); err == nil {
		s.Pending = true
	}
	raw, err := os.ReadFile(filepath.Join(StateDir, "status"))
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "state":
			s.State = value
		case "at":
			s.At, _ = time.Parse(time.RFC3339, value)
		case "reason":
			s.Reason = value
		}
	}
	if log, err := os.ReadFile(filepath.Join(StateDir, "log")); err == nil {
		s.Log = tail(stripColors(string(log)), 40)
	}
	return s
}

// ErrNotInstalled — службы обновления на машине нет.
var ErrNotInstalled = errors.New("служба обновления не установлена: один раз обнови сервер руками командой из руководства, дальше — кнопкой")

// stripColors убирает управляющие последовательности терминала: upgrade.sh
// красит галочки, а в панели это были бы обрывки вида [32m.
func stripColors(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func tail(s string, lines int) string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
