package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheAgentNeverReadsTheRequest: просьбу кладёт процесс без прав, и всё,
// что служба с ней делает, — удаляет. Прочитай она из неё хоть слово, взломанная
// панель получила бы рычаг над root.
func TestTheAgentNeverReadsTheRequest(t *testing.T) {
	for i, line := range strings.Split(string(agent), "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "#") || !strings.Contains(code, "request") {
			continue
		}
		if !strings.HasPrefix(code, "rm -f ") {
			t.Fatalf("строка %d трогает просьбу не только удалением: %s", i+1, code)
		}
	}
}

// TestTheAgentInstallsOnlyAVerifiedScript: сценарий обновления исполняется
// только после сверки с SHA256SUMS релиза.
func TestTheAgentInstallsOnlyAVerifiedScript(t *testing.T) {
	s := string(agent)
	check := strings.Index(s, "sha256sum -c")
	run := strings.Index(s, `sh "$work/upgrade.sh"`)
	if check < 0 || run < 0 || check > run {
		t.Fatal("сценарий исполняется без сверки с SHA256SUMS или до неё")
	}
	if strings.Contains(s, "raw.githubusercontent") {
		t.Fatal("служба берёт сценарий из ветки, а не из релиза")
	}
}

// TestTheAgentIsValidShell — синтаксис проверяет сам sh, где он есть.
func TestTheAgentIsValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh нет")
	}
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, agent, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sh, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v: %s", err, out)
	}
}

// TestStatusReadsWhatTheAgentWrote: панель видит состояние, причину и хвост
// журнала без цветов терминала; просьба, ещё не подобранная службой, видна как
// ожидающая.
func TestStatusReadsWhatTheAgentWrote(t *testing.T) {
	state := t.TempDir()
	home := t.TempDir()
	was, wasAgent := StateDir, AgentPath
	StateDir, AgentPath = state, filepath.Join(state, "agent.sh")
	t.Cleanup(func() { StateDir, AgentPath = was, wasAgent })

	if s := ReadStatus(home); s.Installed || s.Pending || s.State != "" {
		t.Fatalf("пустая машина: %+v", s)
	}

	_ = os.WriteFile(AgentPath, agent, 0o755)
	if err := Request(home); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(state, "status"), []byte("state=failed\nat=2026-09-23T10:00:00Z\nreason=не скачался сценарий\n"), 0o644)
	_ = os.WriteFile(filepath.Join(state, "log"), []byte("  \x1b[32m✓\x1b[0m панель\n"), 0o644)

	s := ReadStatus(home)
	if !s.Installed || !s.Pending {
		t.Fatalf("служба или просьба не видны: %+v", s)
	}
	if s.State != "failed" || s.Reason != "не скачался сценарий" || !s.At.Equal(time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("статус прочитан не так: %+v", s)
	}
	if s.Log != "  ✓ панель" {
		t.Fatalf("журнал %q", s.Log)
	}
}
