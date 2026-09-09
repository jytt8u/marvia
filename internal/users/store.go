package users

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// DecodePublicKey разбирает публичный ключ клиента из base64.
func DecodePublicKey(s string) ([]byte, error) {
	return vp1.DecodeKey(strings.TrimSpace(s))
}

// fileFormat — содержимое файла пользователей.
type fileFormat struct {
	Users []User `json:"users"`
}

// LoadUsers читает список пользователей.
//
// Поддерживаются два формата. Полный — JSON с лимитами. Простой — по ключу на
// строку, без ограничений: так удобно начинать, когда пользователей трое и
// квоты не нужны, и не нужно учить JSON ради списка друзей.
func LoadUsers(path string) ([]User, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", path, err)
	}

	if isJSON(raw) {
		var parsed fileFormat
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("разбор %s: %w", path, err)
		}
		if len(parsed.Users) == 0 {
			return nil, fmt.Errorf("%s не содержит ни одного пользователя", path)
		}
		for i := range parsed.Users {
			parsed.Users[i] = parsed.Users[i].Normalized()
			if _, err := Identity(parsed.Users[i].Kind, parsed.Users[i].Secret); err != nil {
				return nil, fmt.Errorf("%s, пользователь %d: %w", path, i+1, err)
			}
		}
		return parsed.Users, nil
	}

	return parsePlainList(path, raw)
}

func isJSON(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n', 0xEF, 0xBB, 0xBF: // пропускаем пробелы и BOM
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

func parsePlainList(path string, raw []byte) ([]User, error) {
	var list []User
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	line := 0

	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		label := ""
		if idx := strings.IndexAny(text, " \t#"); idx >= 0 {
			label = strings.TrimSpace(strings.TrimLeft(text[idx:], " \t#"))
			text = strings.TrimSpace(text[:idx])
		}
		if _, err := DecodePublicKey(text); err != nil {
			return nil, fmt.Errorf("%s, строка %d: %w", path, line, err)
		}
		list = append(list, User{Kind: KindVP1, Secret: text, PublicKey: text, Label: label, Enabled: true})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("чтение %s: %w", path, err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%s не содержит ни одного ключа", path)
	}
	return list, nil
}

// usageFile — снимок расхода на диске.
type usageFile struct {
	UpdatedAt time.Time        `json:"updated_at"`
	Usage     map[string]Usage `json:"usage"`
}

// LoadUsage читает сохранённый расход, разложенный по аккаунтам.
// Отсутствие файла — не ошибка: нода могла запускаться впервые.
func LoadUsage(path string) (map[string]Usage, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]Usage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", path, err)
	}

	var parsed usageFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}
	if parsed.Usage == nil {
		parsed.Usage = map[string]Usage{}
	}
	return parsed.Usage, nil
}

// SaveUsage записывает расход на диск.
//
// Через временный файл с переименованием: если нода упадёт или её убьют
// посреди записи, на диске останется предыдущий целый снимок, а не обрубок.
// Потерять пару минут статистики не страшно, потерять весь учёт — страшно.
func (r *Registry) SaveUsage(path string) error {
	snapshot := usageFile{UpdatedAt: time.Now().UTC(), Usage: map[string]Usage{}}
	for _, s := range r.Stats() {
		snapshot.Usage[s.Account] = s.Usage
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация расхода: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".usage-*.tmp")
	if err != nil {
		return fmt.Errorf("временный файл в %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("запись расхода: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("закрытие временного файла: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("замена %s: %w", path, err)
	}
	return nil
}

// WatchUsers перечитывает файл пользователей, когда он меняется.
//
// Опрос времени изменения, а не подписка на события файловой системы: одна
// зависимость меньше, поведение одинаковое на Linux и Windows, а задержка в
// несколько секунд для списка пользователей несущественна.
func (r *Registry) WatchUsers(ctx context.Context, path string, interval time.Duration, onEvent func(error, int)) {
	last := modTime(path)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := modTime(path)
			if current.Equal(last) {
				continue
			}
			last = current

			list, err := LoadUsers(path)
			if err != nil {
				// Файл могли поймать посреди записи или сохранить с ошибкой.
				// Продолжаем работать по старому списку: отключить всех
				// пользователей из-за опечатки в конфиге — худший исход.
				onEvent(err, 0)
				continue
			}
			if err := r.Replace(list); err != nil {
				onEvent(err, 0)
				continue
			}
			onEvent(nil, len(list))
		}
	}
}

// PersistUsage периодически сохраняет расход и делает это ещё раз при выходе.
func (r *Registry) PersistUsage(ctx context.Context, path string, interval time.Duration, onError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	save := func() {
		if err := r.SaveUsage(path); err != nil && onError != nil {
			onError(err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			save()
			return
		case <-ticker.C:
			save()
		}
	}
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
