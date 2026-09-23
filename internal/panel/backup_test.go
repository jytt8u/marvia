package panel_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Копия базы — единственное, что отделяет продавца от потери всего бизнеса.
//
// Ноды расходник, их теряют штатно. Панель незаменима: в ней покупатели, сроки
// и оплаченные месяцы. Поэтому проверяем не «файл появился», а «из этого файла
// можно поднять панель».

func backupPanel(t *testing.T) (*panel.Store, string) {
	t.Helper()

	dir := t.TempDir()
	store, err := panel.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return store, dir
}

// TestBackupRestores — из копии поднимается работающая панель.
func TestBackupRestores(t *testing.T) {
	store, dir := backupPanel(t)
	ctx := context.Background()

	user, _, err := store.CreateUser(ctx, panel.CreateUserParams{
		Label: "покупатель", ExternalID: "tg:42", Kinds: []string{"vp1", "vless"},
	})
	if err != nil {
		t.Fatalf("продажа: %v", err)
	}

	copyPath := filepath.Join(dir, "копия.db")
	if err := store.Backup(ctx, copyPath); err != nil {
		t.Fatalf("копия не снялась: %v", err)
	}

	// Открываем копию как обычную базу — ровно так продавец и восстанавливается.
	restored, err := panel.Open(copyPath)
	if err != nil {
		t.Fatalf("копия не открылась: %v", err)
	}
	defer restored.Close()

	back, err := restored.UserByExternalID(ctx, "tg:42")
	if err != nil {
		t.Fatalf("покупателя нет в копии: %v", err)
	}
	if back.ID != user.ID || back.Label != user.Label {
		t.Fatalf("в копии другой покупатель: %+v", back)
	}
	if back.SubToken != user.SubToken {
		t.Error("токен подписки в копии не тот: восстановленная панель разошлась бы с приложениями покупателей")
	}
	if len(back.Credentials) != 2 {
		t.Errorf("наборы доступа не сохранились: %d вместо 2", len(back.Credentials))
	}
}

// TestBackupTakesLatestWrites — в копию попадает то, что записано только что.
//
// База живёт в режиме WAL: свежие записи лежат в отдельном файле журнала, и
// копирование одного panel.db дало бы базу на момент последней контрольной
// точки — без покупателей, заведённых за последние минуты. Их продавец
// хватился бы последними.
func TestBackupTakesLatestWrites(t *testing.T) {
	store, dir := backupPanel(t)
	ctx := context.Background()

	if _, _, err := store.CreateUser(ctx, panel.CreateUserParams{Label: "первый", ExternalID: "tg:1"}); err != nil {
		t.Fatalf("продажа: %v", err)
	}

	copyPath := filepath.Join(dir, "свежая.db")
	if err := store.Backup(ctx, copyPath); err != nil {
		t.Fatalf("копия не снялась: %v", err)
	}

	restored, err := panel.Open(copyPath)
	if err != nil {
		t.Fatalf("копия не открылась: %v", err)
	}
	defer restored.Close()

	if _, err := restored.UserByExternalID(ctx, "tg:1"); err != nil {
		t.Fatalf("свежая запись не попала в копию: %v", err)
	}
}

// TestBackupKeepsOnlyRecent — старые копии подчищаются.
//
// Иначе диск заканчивается через год, и продавец узнаёт об этом, когда панель
// перестаёт писать в базу, то есть когда перестаёт продавать.
func TestBackupKeepsOnlyRecent(t *testing.T) {
	store, dir := backupPanel(t)
	backups := filepath.Join(dir, "backup")

	// Имена копий содержат время с точностью до минуты, поэтому подкладываем
	// старые руками: ждать минуту в тесте нечестно.
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	for _, name := range []string{"panel-2026-01-01-0000.db", "panel-2026-02-01-0000.db", "panel-2026-03-01-0000.db"} {
		if err := os.WriteFile(filepath.Join(backups, name), []byte("старая"), 0o600); err != nil {
			t.Fatalf("подкладка: %v", err)
		}
	}
	// Чужой файл в каталоге трогать нельзя: удалять то, чего не создавали, —
	// не наше дело.
	if err := os.WriteFile(filepath.Join(backups, "заметка.txt"), []byte("не трогать"), 0o600); err != nil {
		t.Fatalf("подкладка: %v", err)
	}

	if _, err := store.BackupNow(context.Background(), backups, 2); err != nil {
		t.Fatalf("копия не снялась: %v", err)
	}

	left, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("чтение каталога: %v", err)
	}

	var copies []string
	stranger := false
	for _, e := range left {
		switch {
		case e.Name() == "заметка.txt":
			stranger = true
		case strings.HasPrefix(e.Name(), "panel-"):
			copies = append(copies, e.Name())
		}
	}

	if len(copies) != 2 {
		t.Fatalf("осталось %d копий вместо двух: %v", len(copies), copies)
	}
	if !stranger {
		t.Error("удалён чужой файл из каталога копий")
	}
	for _, name := range copies {
		if name == "panel-2026-01-01-0000.db" {
			t.Error("подчистились не самые старые копии")
		}
	}
}

// TestBackupIsPrivate — копия не читается посторонним пользователем системы.
func TestBackupIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права файлов на Windows устроены иначе")
	}

	store, dir := backupPanel(t)
	path := filepath.Join(dir, "права.db")
	if err := store.Backup(context.Background(), path); err != nil {
		t.Fatalf("копия не снялась: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("копия не нашлась: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("копия базы доступна не только владельцу: %v", mode)
	}
}

// TestBackupNeedsAdminToken — копию не отдают ключу бота.
//
// В ней вся панель: токены подписок, секреты vless и trojan, хеши ключей.
// Ключ, которым можно её скачать, — это ключ, который может всё.
func TestBackupNeedsAdminToken(t *testing.T) {
	srv, admin := keyPanel(t)

	_, body := do(t, srv, "POST", "/api/v1/keys", admin, `{"name":"бот","scopes":["users","nodes","read"]}`)
	secret := between(t, body, `"secret":"`, `"`)

	if code, _ := do(t, srv, "GET", "/api/v1/backup", secret, ""); code != http.StatusForbidden {
		t.Errorf("ключ со всеми правами скачал копию базы: %d", code)
	}
	if code, _ := do(t, srv, "GET", "/api/v1/backup", "", ""); code != http.StatusUnauthorized {
		t.Errorf("копия базы отдалась без токена: %d", code)
	}

	code, got := do(t, srv, "GET", "/api/v1/backup", admin, "")
	if code != http.StatusOK {
		t.Fatalf("админским токеном копия не скачалась: %d", code)
	}
	if !strings.HasPrefix(got, "SQLite format 3") {
		t.Fatalf("скачалось не похожее на базу: %.40q", got)
	}
}

// TestSealedBackupOpensOnlyWithItsKey — зашифрованная копия открывается своим
// приватным ключом и не открывается чужим.
//
// Копия, которую нечем открыть, — это не копия, и выяснять это в день, когда
// она понадобилась, поздно. Поэтому расшифровка проверяется здесь, а не
// остаётся на совести утилиты.
func TestSealedBackupOpensOnlyWithItsKey(t *testing.T) {
	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи: %v", err)
	}

	dir := t.TempDir()
	store, err := panel.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer store.Close()
	store.WithBackupKey(pair.Public)

	// Заводим покупателя, чтобы в копии было чему найтись.
	if _, _, err := store.CreateUser(context.Background(), panel.CreateUserParams{Label: "Артём"}); err != nil {
		t.Fatalf("покупатель: %v", err)
	}

	path, err := store.BackupNow(context.Background(), filepath.Join(dir, "backup"), 3)
	if err != nil {
		t.Fatalf("копия: %v", err)
	}
	if !strings.HasSuffix(path, ".sealed") {
		t.Fatalf("копия не зашифрована: %s", path)
	}

	// Открытой копии рядом остаться не должно.
	if _, err := os.Stat(strings.TrimSuffix(path, ".sealed")); !os.IsNotExist(err) {
		t.Fatal("открытая копия осталась на диске рядом с зашифрованной")
	}

	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение копии: %v", err)
	}

	// Шифротекст не должен нести имя покупателя открытым текстом.
	if bytes.Contains(sealed, []byte("Артём")) {
		t.Fatal("имя покупателя видно в зашифрованной копии")
	}

	// Своим ключом открывается, и внутри настоящая база SQLite.
	plain, err := panel.OpenSealedBackup(sealed, pair.Private)
	if err != nil {
		t.Fatalf("расшифровка своим ключом: %v", err)
	}
	if !bytes.HasPrefix(plain, []byte("SQLite format 3")) {
		t.Fatalf("расшифровалось не в базу: % x", plain[:16])
	}
	if !bytes.Contains(plain, []byte("Артём")) {
		t.Fatal("в расшифрованной копии нет покупателя")
	}

	// Чужим ключом — нет.
	other, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("чужие ключи: %v", err)
	}
	if _, err := panel.OpenSealedBackup(sealed, other.Private); err == nil {
		t.Fatal("копия открылась чужим ключом")
	}

	// Файл не нашего вида распознаётся, а не падает.
	if _, err := panel.OpenSealedBackup([]byte("просто мусор"), pair.Private); err == nil {
		t.Fatal("мусор принят за копию")
	}
}

// Без ключа копии остаются открытыми — как раньше, без сюрприза.
func TestBackupStaysPlainWithoutKey(t *testing.T) {
	dir := t.TempDir()
	store, err := panel.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer store.Close()

	path, err := store.BackupNow(context.Background(), filepath.Join(dir, "backup"), 3)
	if err != nil {
		t.Fatalf("копия: %v", err)
	}
	if strings.HasSuffix(path, ".sealed") {
		t.Fatalf("копия зашифрована без ключа: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("SQLite format 3")) {
		t.Fatal("открытая копия — не база")
	}
}
