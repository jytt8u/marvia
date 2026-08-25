package wintun

import (
	"fmt"
	"net/netip"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Прописать резолвер на своём адаптере недостаточно, и это главная утечка
// имён под Windows.
//
// Система разрешает имена «умно»: рассылает запрос по всем адаптерам сразу и
// берёт первый ответ. Значит запрос уходит и на резолвер провайдера тоже, в
// открытом виде и мимо туннеля. Пока работал DoH, это ещё как-то пряталось; с
// августа 2026, после его блокировки, провайдер видит список посещённых сайтов
// целиком — при поднятом VPN.
//
// Лечится таблицей политики разрешения имён. Правило с именем "." покрывает
// все домены и заставляет систему спрашивать только у нашего резолвера, поверх
// настроек любых адаптеров. Так же поступает клиент WireGuard.

const (
	// nrptRoot — где Windows хранит правила разрешения имён.
	nrptRoot = `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`

	// nrptRule — наше правило. Имя постоянное, чтобы его всегда можно было
	// найти и убрать — в том числе после падения, когда убрать за собой мы не
	// успели.
	nrptRule = nrptRoot + `\Marvia`

	// nrptAllDomains — правило на все имена сразу.
	nrptAllDomains = "."

	// nrptGenericServer — вид правила «спрашивать у этих серверов».
	nrptGenericServer = 8
)

// setNRPT заставляет систему разрешать все имена через заданный резолвер.
func setNRPT(dns netip.Addr) error {
	// Сначала убираем прежнее правило, если оно осталось от упавшего запуска.
	// Иначе к нему добавится второе, и какое из них подействует — неизвестно.
	_ = clearNRPT()

	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, nrptRule, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("правило разрешения имён: %w", err)
	}
	defer key.Close()

	if err := key.SetStringsValue("Name", []string{nrptAllDomains}); err != nil {
		return fmt.Errorf("правило разрешения имён, список доменов: %w", err)
	}
	if err := key.SetStringValue("GenericDNSServers", dns.String()); err != nil {
		return fmt.Errorf("правило разрешения имён, резолвер: %w", err)
	}
	if err := key.SetDWordValue("ConfigOptions", nrptGenericServer); err != nil {
		return fmt.Errorf("правило разрешения имён, вид правила: %w", err)
	}
	if err := key.SetDWordValue("Version", 1); err != nil {
		return fmt.Errorf("правило разрешения имён, версия: %w", err)
	}

	return reloadDNS()
}

// clearNRPT снимает наше правило.
//
// Вызывать безопасно всегда: отсутствие правила ошибкой не считается.
func clearNRPT() error {
	err := registry.DeleteKey(registry.LOCAL_MACHINE, nrptRule)
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("снятие правила разрешения имён: %w", err)
	}
	if err == registry.ErrNotExist {
		return nil
	}
	return reloadDNS()
}

// reloadDNS просит службу кэша имён перечитать правила.
//
// Без этого правило подействует только после перезагрузки, а нам оно нужно
// прямо сейчас — и снять его тоже нужно сразу, иначе человек останется с
// резолвером, до которого больше нет дороги.
func reloadDNS() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("служба кэша имён: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService("Dnscache")
	if err != nil {
		return fmt.Errorf("служба кэша имён: %w", err)
	}
	defer service.Close()

	if _, err := service.Control(svc.ParamChange); err != nil {
		return fmt.Errorf("служба кэша имён не перечитала правила: %w", err)
	}
	return nil
}
