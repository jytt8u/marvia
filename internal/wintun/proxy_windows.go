package wintun

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Системный прокси — дыра в обещании «весь трафик идёт через туннель».
//
// Windows разрешает прописать прокси на всю систему, и почти все программы его
// слушаются: браузеры, Steam, магазины, обновления. Если он прописан, их
// трафик уходит не в наш интерфейс, а в тот прокси — и дальше туда, куда
// решит он. Маршруты при этом наши, интерфейс наш, счётчики в окне тикают: со
// стороны всё выглядит работающим.
//
// Ставят такой прокси другие клиенты обхода блокировок и оставляют после себя,
// когда их закрывают не той кнопкой. А приходят к нам ровно те люди, у кого
// такой клиент уже стоял, — то есть случай не редкий, а типичный.
//
// Молчать про это нельзя. Человек платит за то, чтобы его трафик шёл через
// нас; если часть идёт мимо, он должен знать, а не выяснять это через полгода.

// Proxy — что нашлось в системных настройках.
type Proxy struct {
	// Server — адрес прокси, как его видит система.
	Server string

	// Owner — чья это программа, если удалось выяснить по слушающему порту.
	// Пусто, если прокси не на этой машине или процесс не нашёлся.
	Owner string

	// FromEnv — прокси задан переменными окружения, а не настройками системы.
	// Такой снимается иначе, и сказать об этом надо честно.
	FromEnv bool

	// AutoConfig — адрес файла автонастройки. Он тоже уводит трафик, но что
	// именно там написано, мы не знаем и делать вид, что знаем, не будем.
	AutoConfig string
}

// Found сообщает, нашлось ли что-нибудь.
func (p Proxy) Found() bool {
	return p.Server != "" || p.AutoConfig != ""
}

// Describe — одна фраза для окна, без технических подробностей.
func (p Proxy) Describe() string {
	if !p.Found() {
		return ""
	}
	switch {
	case p.AutoConfig != "" && p.Server == "":
		return "В системе включена автонастройка прокси — часть программ пойдёт мимо туннеля"
	case p.Owner != "":
		return fmt.Sprintf("В системе прописан прокси %s (%s) — браузеры и Steam пойдут мимо туннеля",
			p.Server, p.Owner)
	default:
		return fmt.Sprintf("В системе прописан прокси %s — браузеры и Steam пойдут мимо туннеля", p.Server)
	}
}

const proxyKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// SystemProxy ищет прокси там, где его смотрят сами программы.
//
// Порядок важен: переменные окружения перекрывают настройки системы для части
// программ, а настройки системы действуют на браузеры. Сообщать надо про
// первый найденный — иначе окно превратится в перечисление.
func SystemProxy() Proxy {
	if server := envProxy(); server != "" {
		return Proxy{Server: server, Owner: ownerOf(server), FromEnv: true}
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, proxyKey, registry.QUERY_VALUE)
	if err != nil {
		return Proxy{}
	}
	defer key.Close()

	var found Proxy

	if auto, _, err := key.GetStringValue("AutoConfigURL"); err == nil && strings.TrimSpace(auto) != "" {
		found.AutoConfig = strings.TrimSpace(auto)
	}

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return found
	}

	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return found
	}

	found.Server = strings.TrimSpace(server)
	found.Owner = ownerOf(found.Server)
	return found
}

// envProxy читает переменные окружения. Пустая строка — ничего не задано.
func envProxy() string {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// DisableSystemProxy снимает прокси из настроек системы.
//
// Только настройки системы: переменные окружения живут в чужих процессах и
// чужих ярлыках, и лезть туда мы не будем — оттуда их убирает тот, кто
// поставил.
//
// Вызывается исключительно по нажатию человека. Сама программа чужих настроек
// не трогает: прокси мог быть поставлен осознанно — рабочим, родительским
// контролем, чем угодно, — и снять его молча значило бы сломать то, чего мы
// не понимаем.
func DisableSystemProxy() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, proxyKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("настройки прокси: %w", err)
	}
	defer key.Close()

	if err := key.SetDWordValue("ProxyEnable", 0); err != nil {
		return fmt.Errorf("выключение прокси: %w", err)
	}

	// Автонастройка — отдельная запись, и выключение прокси её не касается.
	// Без этой строки кнопка «убрать» на такой системе не делала ничего, а
	// программа рапортовала об успехе: трафик как шёл мимо, так и шёл.
	if err := key.DeleteValue("AutoConfigURL"); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("выключение автонастройки прокси: %w", err)
	}
	return nil
}
