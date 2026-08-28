package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Раздача приложений покупателям.
//
// Приложение должен отдавать сервер продавца, а не магазин и не чужой
// файлохостинг. В августе 2026 провайдеры начали рубить загрузку из Google
// Play и App Store, объясняя это блокировкой международных CDN; GitHub, где
// лежат наши сборки, раздаётся ровно через такой же CDN. Ссылка «скачай
// приложение» перестаёт работать раньше всего остального — и продавец теряет
// покупателя до того, как тот заплатил.
//
// Домен панели у покупателя уже рабочий: он ходит на него за подпиской. Пусть
// с него же и качает.

// Приложения, которые панель раздаёт. Имя из адреса сопоставляется с файлом:
// брать имя файла прямо из адреса нельзя, иначе туда попросят «../../etc/shadow».
var appFiles = map[string]struct {
	file string
	mime string
}{
	"android": {"marvia-android.apk", "application/vnd.android.package-archive"},
	"windows": {"marvia-windows.exe", "application/octet-stream"},
}

// appDownload отдаёт приложение по токену подписки.
//
// Токен, а не открытый адрес: панель не должна на первом же запросе сканера
// признаваться, что она панель обхода блокировок. У покупателя токен есть — он
// пришёл в той же ссылке, что и доступ.
func (a *API) appDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := a.store.UserBySubToken(r.Context(), r.PathValue("token")); err != nil {
		// Не подсказываем, существует ли токен: перебор подписок — обычное
		// занятие тех, кто ищет чужие ноды.
		http.NotFound(w, r)
		return
	}

	app, known := appFiles[r.PathValue("name")]
	if !known {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(filepath.Join(a.distDir, app.file))
	if err != nil {
		fail(w, http.StatusServiceUnavailable,
			"приложение пока не выложено: положи "+app.file+" в "+a.distDir)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", app.mime)
	// Имя файла нужно телефону: без него браузер сохранит apk как «download»,
	// и человек не сможет его поставить.
	w.Header().Set("Content-Disposition", `attachment; filename="`+app.file+`"`)
	http.ServeContent(w, r, app.file, info.ModTime(), f)
}

// appLinks — готовые ссылки на приложения для одного покупателя.
//
// Возвращаем только то, что есть на диске: ссылка на отсутствующий файл хуже
// отсутствия ссылки, потому что покупатель по ней сходит и придёт с вопросом.
func (a *API) appLinks(subToken string) map[string]string {
	out := map[string]string{}
	for name, app := range appFiles {
		if _, err := os.Stat(filepath.Join(a.distDir, app.file)); err != nil {
			continue
		}
		out[name] = a.subURL(subToken) + "/app/" + name
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AppFile — что панель знает про выложенное приложение.
type AppFile struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// listApps говорит продавцу, какие приложения у него выложены.
//
// Нужно ровно затем, чтобы на вопрос «почему покупателю не пришла ссылка на
// приложение» был ответ, а не догадки. Сумма — чтобы сверить, что лежит именно
// то, что скачивал.
func (a *API) listApps(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(appFiles))
	for name := range appFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]AppFile, 0, len(names))
	for _, name := range names {
		app := appFiles[name]
		path := filepath.Join(a.distDir, app.file)

		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sum, err := fileSum(path)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, AppFile{Name: name, File: app.file, Size: info.Size(), SHA256: sum})
	}

	answer := map[string]any{"apps": out, "dist": a.distDir}
	if len(out) == 0 {
		answer["hint"] = "положи " + appNames() + " в " + a.distDir + " — покупателям они раздаются с домена панели"
	}
	ok(w, answer)
}

func appNames() string {
	names := make([]string, 0, len(appFiles))
	for _, app := range appFiles {
		names = append(names, app.file)
	}
	sort.Strings(names)
	return strings.Join(names, " и ")
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("сумма %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
