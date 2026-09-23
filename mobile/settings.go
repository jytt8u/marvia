package mobile

import (
	"encoding/json"
	"log"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
)

// tunnelSettings — то, что человек выбрал в настройках туннеля.
//
// Приходит строкой JSON, а не отдельными параметрами: иначе каждая новая
// настройка меняла бы подпись Start и MeasureNodes, а с ними все вызовы в
// приложении. Незнакомое поле просто не читается, пропущенное значит «как
// было».
type tunnelSettings struct {
	// Fragment — резать TLS-приветствие к нодам так, чтобы имя из SNI не
	// лежало целиком ни в одном TCP-сегменте. Против фильтров по имени.
	Fragment bool `json:"fragment"`

	// NoIPv6 — не пускать IPv6 через туннель вовсе. Маршрут остаётся в
	// туннеле, так что мимо IPv6 тоже не уходит: приложения видят «адресов
	// IPv6 нет» и идут по IPv4.
	NoIPv6 bool `json:"no_ipv6"`
}

// parseSettings разбирает настройки. Сломанная строка — не повод не
// подключаться: туннель поднимется с настройками по умолчанию, а в журнал
// ляжет, что именно не разобралось.
func parseSettings(raw string) tunnelSettings {
	var s tunnelSettings
	if raw == "" {
		return s
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		log.Printf("настройки туннеля не разобрались, беру умолчания: %v", err)
		return tunnelSettings{}
	}
	return s
}

// dial — настройки дозвона до нод VP1.
func (s tunnelSettings) dial() client.Options {
	return client.Options{Fragment: s.Fragment}
}

// foreign — настройки движков чужих протоколов. Свои у каждого движка, а не
// общие на процесс: замер другой подписки не должен переключать работающий
// туннель.
func (s tunnelSettings) foreign() foreign.Options {
	return foreign.Options{Fragment: s.Fragment}
}
