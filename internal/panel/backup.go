package panel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Резервные копии базы панели.
//
// panel.db — это весь бизнес продавца: покупатели, сроки, оплаченные месяцы.
// Сервер умер, хостер снёс за жалобу, диск посыпался — и продавец не может
// даже сказать людям, до какого числа у них оплачено. Ноды при этом расходник
// и теряются штатно, а панель незаменима.
//
// Копию делает сама панель: продавец, которому надо помнить про cron, однажды
// про него не вспомнит.

const (
	// BackupEvery — как часто снимать копию по умолчанию.
	//
	// Сутки: покупателей заводят десятками в день, и потеря последних часов —
	// это несколько человек, которым продавец выдаст доступ заново. Потеря
	// месяца — это его бизнес.
	BackupEvery = 24 * time.Hour

	// BackupKeep — сколько копий держать по умолчанию.
	//
	// Недели хватает, чтобы заметить беду: испорченную базу обнаруживают не в
	// ту же минуту, а когда кто-то не смог подключиться.
	BackupKeep = 7

	backupPrefix = "panel-"
	backupSuffix = ".db"
)

// Backup складывает целостную копию базы в файл.
//
// VACUUM INTO, а не копирование файла: рядом с базой живёт журнал WAL, и
// скопированный на ходу panel.db без него — это база на момент последней
// контрольной точки, то есть без свежих покупателей. Читатели при этом не
// блокируются, панель продолжает работать.
func (s *Store) Backup(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("копия %s уже есть", filepath.Base(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("каталог копий: %w", err)
	}

	// Путь подставляется в запрос строкой: параметры в VACUUM INTO SQLite не
	// принимает. Апостроф удваиваем — иначе каталог с кавычкой в имени
	// превратился бы в обрывок запроса.
	quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return fmt.Errorf("копия базы: %w", err)
	}

	// Копия несёт всё, что есть в панели: токены подписок, секреты vless и
	// trojan, хеши ключей. Права как у самой базы, не шире.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("права на копию: %w", err)
	}
	return nil
}

// BackupNow снимает копию с именем по времени и подчищает старые.
func (s *Store) BackupNow(ctx context.Context, dir string, keep int) (string, error) {
	name := backupPrefix + time.Now().UTC().Format("2006-01-02-1504") + backupSuffix
	path := filepath.Join(dir, name)

	if err := s.Backup(ctx, path); err != nil {
		return "", err
	}
	if err := pruneBackups(dir, keep); err != nil {
		// Копия снята — это главное. О невычищенных старых говорим, но не
		// делаем вид, что копии нет.
		return path, err
	}
	return path, nil
}

// pruneBackups оставляет keep самых свежих копий.
//
// Иначе диск заканчивается через год, и продавец узнаёт об этом, когда панель
// перестаёт писать в базу — то есть когда перестаёт продавать.
func pruneBackups(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("чтение каталога копий: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupSuffix) {
			// Чужие файлы в каталоге не наше дело: удалять то, чего не
			// создавали, нельзя.
			continue
		}
		names = append(names, name)
	}
	if len(names) <= keep {
		return nil
	}

	// Имя содержит время в порядке, пригодном для сортировки строкой.
	sort.Strings(names)

	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("удаление старой копии: %w", err)
		}
	}
	return nil
}

// KeepBackups снимает копии, пока не отменят контекст.
//
// Первая — сразу при запуске: панель поднимают в том числе после переезда на
// новый сервер, и копия «как было до» в этот момент нужнее всего.
func (s *Store) KeepBackups(ctx context.Context, dir string, keep int, every time.Duration, notify func(string, error)) {
	if every <= 0 {
		return
	}

	for {
		path, err := s.BackupNow(ctx, dir, keep)
		if notify != nil {
			notify(path, err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// downloadBackup отдаёт свежую копию базы одним файлом.
//
// Копия на том же сервере спасает от испорченной базы, но не от потерянного
// сервера — а теряют их вместе с хостером, по жалобе и без предупреждения.
// Поэтому продавцу нужен способ забрать базу к себе одной командой, не заходя
// по ssh.
//
// Только админским токеном: в копии лежит всё, включая секреты покупателей.
// Ключ бота, который может её скачать, — это ключ, который может всё.
func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp("", "veil-backup")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(dir)

	name := backupPrefix + time.Now().UTC().Format("2006-01-02-1504") + backupSuffix
	path := filepath.Join(dir, name)
	if err := a.store.Backup(r.Context(), path); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	f, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}
