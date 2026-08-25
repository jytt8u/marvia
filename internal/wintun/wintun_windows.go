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

	// Bypass — адреса, которые заворачивать в туннель нельзя. Сюда идут адреса
	// самой ноды: соединение до неё обязано идти напрямую.
	Bypass []netip.Addr
}

// Adapter — поднятый и настроенный интерфейс.
type Adapter struct {
	dev  device.Device
	luid winipcfg.LUID

	// bypass запоминает поставленные обходные маршруты, чтобы снять ровно их,
	// а не сбрасывать таблицу маршрутов целиком. Сбросить чужие маршруты на
	// выходе — быстрый способ оставить человека без интернета вообще.
	bypass []route

	gateway netip.Addr
	gwLUID  winipcfg.LUID

	// nrpt помнит, ставили ли мы политику разрешения имён. Снять её при
	// выходе обязательно: она общесистемная и переживёт нашу смерть, а
	// резолвер, на который она указывает, живёт только внутри туннеля.
	nrpt bool
}

type route struct {
	dest netip.Prefix
	next netip.Addr
}

// Open создаёт адаптер и настраивает его целиком.
func Open(cfg Config) (_ *Adapter, err error) {
	if cfg.Name == "" {
		return nil, errors.New("не задано имя адаптера")
	}
	if !cfg.Address.IsValid() {
		return nil, errors.New("не задан адрес внутри туннеля")
	}

	// Шлюз узнаём до того, как что-то менять. После добавления своих
	// маршрутов найти прежний путь наружу будет уже сложнее.
	gwLUID, gateway, err := defaultGateway()
	if err != nil {
		return nil, err
	}

	dev, err := tun.Open(cfg.Name, cfg.MTU)
	if err != nil {
		return nil, fmt.Errorf("создание адаптера %q: %w (проверь, что рядом с программой лежит wintun.dll и она запущена от администратора)", cfg.Name, err)
	}

	a := &Adapter{dev: dev, gateway: gateway, gwLUID: gwLUID}
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()

	a.luid, err = luidByName(cfg.Name)
	if err != nil {
		return nil, err
	}

	if err := a.luid.SetIPAddresses([]netip.Prefix{cfg.Address}); err != nil {
		return nil, fmt.Errorf("назначение адреса %s: %w", cfg.Address, err)
	}

	// Обходные маршруты ставим раньше своих: если что-то пойдёт не так, мы
	// ещё не успели забрать себе весь трафик и человек останется с интернетом.
	if err := a.addBypass(cfg.Bypass); err != nil {
		return nil, err
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
		if err := setNRPT(cfg.DNS); err != nil {
			return nil, err
		}
		a.nrpt = true
	}

	return a, nil
}

// claimTraffic забирает себе весь трафик двумя половинами вместо одного
// маршрута по умолчанию.
//
// 0.0.0.0/1 и 128.0.0.0/1 вместе покрывают весь адресный простор и при этом
// точнее, чем 0.0.0.0/0. Прежний маршрут по умолчанию остаётся нетронутым и
// продолжает работать для того, что мы явно вывели в обход, — а при выходе нам
// не надо его восстанавливать, потому что мы его и не ломали.
func (a *Adapter) claimTraffic() error {
	halves := []netip.Prefix{
		netip.PrefixFrom(netip.AddrFrom4([4]byte{0, 0, 0, 0}), 1),
		netip.PrefixFrom(netip.AddrFrom4([4]byte{128, 0, 0, 0}), 1),
	}
	for _, half := range halves {
		if err := a.luid.AddRoute(half, netip.IPv4Unspecified(), 0); err != nil {
			return fmt.Errorf("маршрут %s: %w", half, err)
		}
	}
	return nil
}

// addBypass выводит адреса ноды мимо туннеля, через прежний шлюз.
func (a *Adapter) addBypass(addrs []netip.Addr) error {
	if len(addrs) > 0 && !a.gateway.IsValid() {
		return errors.New("не нашёлся шлюз по умолчанию: без него соединение до ноды уйдёт в собственный туннель")
	}

	for _, addr := range addrs {
		if !addr.Is4() {
			// Обход по IPv6 не делаем: весь туннель сейчас работает по IPv4,
			// и обходить нечего.
			continue
		}
		dest := netip.PrefixFrom(addr, 32)
		if err := a.gwLUID.AddRoute(dest, a.gateway, 0); err != nil {
			// Маршрут уже есть — не беда, обход всё равно работает.
			if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
				continue
			}
			return fmt.Errorf("обходной маршрут до %s: %w", addr, err)
		}
		a.bypass = append(a.bypass, route{dest: dest, next: a.gateway})
	}
	return nil
}

// Endpoint отдаёт интерфейс в том виде, в каком его ждёт мост.
func (a *Adapter) Endpoint() stack.LinkEndpoint { return a.dev }

// Close снимает всё, что мы поставили, и убирает адаптер.
//
// Порядок обратный: сначала обходные маршруты, потом сам адаптер. Маршруты
// самого адаптера снимать не надо — они исчезают вместе с ним.
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

	for _, r := range a.bypass {
		if err := a.gwLUID.DeleteRoute(r.dest, r.next); err != nil && first == nil {
			first = fmt.Errorf("снятие обходного маршрута %s: %w", r.dest, err)
		}
	}
	a.bypass = nil

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

// defaultGateway находит нынешний путь наружу.
//
// Windows выбирает маршрут по сумме двух чисел: веса самого маршрута и веса
// интерфейса. Считать только первое — распространённая ошибка: на машине с
// вайфаем и проводом она уводит обход в тот интерфейс, которым система на
// самом деле не пользуется.
func defaultGateway() (winipcfg.LUID, netip.Addr, error) {
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return 0, netip.Addr{}, fmt.Errorf("таблица маршрутов: %w", err)
	}

	ifaces, err := winipcfg.GetIPInterfaceTable(windows.AF_INET)
	if err != nil {
		return 0, netip.Addr{}, fmt.Errorf("список интерфейсов: %w", err)
	}
	weight := make(map[winipcfg.LUID]uint32, len(ifaces))
	for _, iface := range ifaces {
		weight[iface.InterfaceLUID] = iface.Metric
	}

	var (
		bestLUID  winipcfg.LUID
		bestAddr  netip.Addr
		bestScore uint32
		found     bool
	)
	for i := range routes {
		r := &routes[i]
		if r.DestinationPrefix.Prefix().Bits() != 0 {
			continue
		}
		next := r.NextHop.Addr()
		if !next.IsValid() || next.IsUnspecified() {
			continue
		}
		score := r.Metric + weight[r.InterfaceLUID]
		if !found || score < bestScore {
			bestLUID, bestAddr, bestScore, found = r.InterfaceLUID, next, score, true
		}
	}

	if !found {
		return 0, netip.Addr{}, errors.New("не нашёлся маршрут по умолчанию: похоже, интернета нет вовсе")
	}
	return bestLUID, bestAddr, nil
}
