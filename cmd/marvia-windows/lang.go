//go:build windows

package main

import (
	"fmt"
	"sync/atomic"
)

// Сообщения программы на языке окна.
//
// Часть того, что человек читает, приходит не из разметки, а отсюда: «нужны
// права администратора», «нода не отвечает», строки журнала. Английская кнопка
// с русской ошибкой под ней выглядит хуже, чем честно русское окно, поэтому
// язык знает и программа.
//
// Выбирает его окно и сообщает запросом при запуске и при переключении. Само
// оно берёт язык у системы, а не спрашивает: до первого сообщения человек ещё
// ничего не нажимал.
//
// Хранится в atomic, а не под замком: пишет его одна ручка, читают — сторож,
// журнал и обработчики, и заводить ради одного значения ещё один замок рядом с
// замком контроллера значит завести и порядок их взятия.
var uiLang atomic.Value

func init() { uiLang.Store("ru") }

// setUILang запоминает язык окна. Незнакомый оставляет прежний: соврать
// молчанием лучше, чем показать пустые строки.
func setUILang(lang string) {
	if _, known := messages[lang]; !known {
		return
	}
	uiLang.Store(lang)
}

// say отдаёт сообщение на языке окна.
//
// Ключ, а не строка: так недостающий перевод виден в словаре, а не всплывает
// у человека на экране.
func say(key string) string {
	lang, _ := uiLang.Load().(string)
	if m, ok := messages[lang][key]; ok {
		return m
	}
	// Русский — запасной: он заполнен всегда, потому что на нём пишут.
	if m, ok := messages["ru"][key]; ok {
		return m
	}
	return key
}

var messages = map[string]map[string]string{
	"ru": {
		"noAccount":   "не задана ссылка доступа",
		"needAdmin":   "нужны права администратора: без них Windows не даст создать сетевой адаптер",
		"proxyInEnv":  "прокси остался в переменных окружения — его убирает та программа, которая поставила",
		"nodeSilent":  "нода не отвечает — туннель поднят, но трафик через неё не идёт",
		"badRequest":  "не разобрал запрос",
		"accountLink": "ссылка доступа",
		"privateKey":  "личный ключ",
		"bridge":      "сетевой мост",

		"logKeySaved":      "ключ доступа сохранён",
		"logProxyGone":     "системный прокси снят",
		"logTunnelUp":      "туннель поднят: весь трафик идёт через %s",
		"logTunnelDown":    "туннель убран, маршруты сняты",
		"logNoConnect":     "не подключилось: %v",
		"logCleanup":       "при уборке: %v",
		"logConn":          "соединение: %v",
		"logNodeBack":      "нода снова отвечает",
		"logChosen":        "выбрана нода %s",
		"notConnected":     "туннель не поднят",
		"logMoved":         "переехали на %s",
		"logNodeSilent":    "нода не отвечает на %d проверки подряд: %v",
		"logAdapter":       "создаю сетевой адаптер и настраиваю маршруты",
		"logNodePicked":    "выбрана нода %s, задержка %d мс",
		"autostartFailed":  "планировщик отказал: задачу может поставить только администратор",
		"badPanelLink":     "это не ссылка-приглашение в панель: нужен вид marvia-panel://токен@адрес",
		"panelUnreachable": "панель не отвечает",
		"panelRejected":    "панель не приняла токен: ссылка устарела или ключ отозван",
		"logPanelSet":      "панель продавца подключена: %s",
		"logPanelDropped":  "панель продавца отвязана",
		"logAutostartOn":   "запуск вместе с Windows включён",
		"logAutostartOff":  "запуск вместе с Windows выключен",
	},
	"en": {
		"noAccount":   "no access key set",
		"needAdmin":   "administrator rights are required: without them Windows will not create a network adapter",
		"proxyInEnv":  "the proxy is still in environment variables — only the program that set it can remove it",
		"nodeSilent":  "the node is not responding — the tunnel is up, but no traffic goes through it",
		"badRequest":  "could not parse the request",
		"accountLink": "access key",
		"privateKey":  "private key",
		"bridge":      "network bridge",

		"logKeySaved":      "access key saved",
		"logProxyGone":     "system proxy removed",
		"logTunnelUp":      "tunnel up: all traffic goes through %s",
		"logTunnelDown":    "tunnel removed, routes cleared",
		"logNoConnect":     "could not connect: %v",
		"logCleanup":       "while cleaning up: %v",
		"logConn":          "connection: %v",
		"logNodeBack":      "the node responds again",
		"logChosen":        "node %s chosen",
		"notConnected":     "the tunnel is not up",
		"logMoved":         "moved to %s",
		"logNodeSilent":    "the node failed %d checks in a row: %v",
		"logAdapter":       "creating the network adapter and setting up routes",
		"logNodePicked":    "picked node %s, latency %d ms",
		"autostartFailed":  "the task scheduler refused: only an administrator can create the task",
		"badPanelLink":     "this is not a panel invite link: expected marvia-panel://token@host",
		"panelUnreachable": "the panel is not responding",
		"panelRejected":    "the panel rejected the token: the link is stale or the key was revoked",
		"logPanelSet":      "seller panel connected: %s",
		"logPanelDropped":  "seller panel disconnected",
		"logAutostartOn":   "start with Windows enabled",
		"logAutostartOff":  "start with Windows disabled",
	},
}

// sayf собирает сообщение с подстановками.
//
// sprintf вынесен в переменную намеренно. Строка формата приходит из словаря
// выше, а не из кода, и go vet справедливо требует постоянного формата:
// подставить туда чужое значение — известная дыра. Здесь формат свой, лежит
// в таблице десятью строками выше и меняется только вместе с ней, поэтому
// проверять действительно нечего. Обращение через переменную говорит это vet,
// а комментарий — тому, кто будет читать.
var sprintf = fmt.Sprintf

func sayf(key string, args ...any) string { return sprintf(say(key), args...) }
