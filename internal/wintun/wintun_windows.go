//go:build windows

package wintun

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Config описывает интерфейс, который нужно поднять.
type Config struct {
	// Name — имя адаптера, каким его увидит человек в настройках сети.
	Name string

	// MTU интерфейса.
	MTU uint32

	// Address — адрес внутри туннеля. Наружу он не выходит и ни с чем не
	// спорит: его видит только сетевой стек самого компьютера.
	Address netip.Prefix

	// DNS — адрес для запросов имён. Куда бы он ни указывал, запрос перехватит
	// наш мост и унесёт в туннель по TCP.
	DNS netip.Addr
}

// Adapter — поднятый и настроенный интерфейс.
type Adapter struct {
	dev  device.Device
	luid winipcfg.LUID

	// nrpt помнит, ставили ли мы политику разрешения имён. Снять её при
	// выходе обязательно: она общесистемная и переживёт нашу смерть, а
	// резолвер, на который она указывает, живёт только внутри туннеля.
	nrpt bool
}

// Open создаёт адаптер и настраивает его целиком.
func Open(cfg Config) (_ *Adapter, err error) {
	if cfg.Name == "" {
		return nil, errors.New("не задано имя адаптера")
	}
	if !cfg.Address.IsValid() {
		return nil, errors.New("не задан адрес внутри туннеля")
	}

	dev, err := tun.Open(cfg.Name, cfg.MTU)
	if err != nil {
		return nil, fmt.Errorf("создание адаптера %q: %w (проверь, что рядом с программой лежит wintun.dll и она запущена от администратора)", cfg.Name, err)
	}

	a := &Adapter{dev: dev}
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()

	a.luid, err = luidByName(cfg.Name)
	if err != nil {
		return nil, err
	}

	// IPv6 тоже остаётся внутри VPN: иначе браузер обойдёт исправление
	// маршрутов IPv4, просто выбрав AAAA-запись того же сайта.
	if err := a.luid.SetIPAddresses([]netip.Prefix{cfg.Address, netip.MustParsePrefix("fd00:6d61:7276::1/128")}); err != nil {
		return nil, fmt.Errorf("назначение адреса %s: %w", cfg.Address, err)
	}

	if err := a.claimTraffic(); err != nil {
		return nil, err
	}

	if cfg.DNS.IsValid() {
		if err := a.luid.SetDNS(windows.AF_INET, []netip.Addr{cfg.DNS}, nil); err != nil {
			return nil, fmt.Errorf("адрес для запросов имён: %w", err)
		}

		// Адреса на адаптере мало: система всё равно рассылает запросы по
		// всем адаптерам сразу. Закрываем это политикой — подробности в
		// nrpt_windows.go.
		//
		// Отмечаем намерение до попытки, а не после успеха, и это не
		// педантизм. setNRPT сначала пишет правило в реестр и только потом
		// просит службу его перечитать; второй шаг отказывает — и раньше мы
		// возвращали ошибку, не выставив флаг. Правило оставалось в реестре
		// навсегда: человек сносил программу и оставался без имён, потому что
		// система продолжала спрашивать резолвер, которого больше нет.
		a.nrpt = true
		if err := setNRPT(cfg.DNS); err != nil {
			return nil, err
		}
	}

	return a, nil
}

// claimTraffic забирает IPv4 и IPv6 половинами адресного пространства.
//
// 0.0.0.0/1 и 128.0.0.0/1 вместе покрывают весь адресный простор и при этом
// точнее, чем 0.0.0.0/0. Прежний маршрут по умолчанию остаётся нетронутым и
// служит сокетам ноды, привязанным к внешнему интерфейсу через netpath.
// Общесистемных исключений для IP нод здесь больше нет.
func (a *Adapter) claimTraffic() error {
	halves := []netip.Prefix{
		netip.PrefixFrom(netip.AddrFrom4([4]byte{0, 0, 0, 0}), 1),
		netip.PrefixFrom(netip.AddrFrom4([4]byte{128, 0, 0, 0}), 1),
		netip.MustParsePrefix("::/1"),
		netip.MustParsePrefix("8000::/1"),
	}
	for _, half := range halves {
		next := netip.IPv4Unspecified()
		if half.Addr().Is6() {
			next = netip.IPv6Unspecified()
		}
		if err := a.luid.AddRoute(half, next, 0); err != nil {
			return fmt.Errorf("маршрут %s: %w", half, err)
		}
	}
	return nil
}

// Endpoint отдаёт интерфейс в том виде, в каком его ждёт мост.
func (a *Adapter) Endpoint() stack.LinkEndpoint { return a.dev }

// Close снимает всё, что мы поставили, и убирает адаптер.
//
// Маршруты адаптера исчезают вместе с ним; маршруты внешней сети не менялись.
func (a *Adapter) Close() error {
	var first error

	// Политику снимаем первой. Всё остальное можно чинить перезапуском, а
	// забытое правило оставит человека с резолвером, до которого больше нет
	// дороги, — и без имён он не откроет даже страницу поддержки.
	if a.nrpt {
		if err := clearNRPT(); err != nil {
			first = err
		}
		a.nrpt = false
	}

	if a.dev != nil {
		a.dev.Close()
		a.dev = nil
	}
	return first
}

// luidByName находит адаптер по имени, которым мы его назвали.
func luidByName(name string) (winipcfg.LUID, error) {
	adapters, err := winipcfg.GetAdaptersAddresses(windows.AF_UNSPEC, winipcfg.GAAFlagIncludeAllInterfaces)
	if err != nil {
		return 0, fmt.Errorf("список сетевых адаптеров: %w", err)
	}
	for _, adapter := range adapters {
		if adapter.FriendlyName() == name {
			return adapter.LUID, nil
		}
	}
	return 0, fmt.Errorf("адаптер %q создан, но не нашёлся в списке сетевых интерфейсов", name)
}
