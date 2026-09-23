package foreign

import (
	"context"
	"sync/atomic"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"

	"github.com/jytt8u/marvia/internal/transport"
)

// Дробление приветствия у чужих протоколов.
//
// Своё у Xray есть (finalmask fragment), но режет оно вслепую: кусками
// случайной длины от начала записи. Chrome перемешивает расширения, и имя
// оказывается где угодно в двух килобайтах, так что слепая нарезка либо
// промахивается мимо имени, либо шлёт сотню крошечных сегментов. Поэтому
// сырое TCP-соединение Xray берёт у нас и режется тем же разрезом по имени,
// что и у VP1 (transport.FragmentHello), — одинаково для ключа Marvia и для
// чужой ссылки.
//
// Способ подменить дозвон у Xray один — общий на процесс системный дозвон.
// Поэтому и выключатель общий: SetFragment действует на все движки сразу.
// Для приложения это не ограничение: настройка у человека одна на всё.

var fragmentOn atomic.Bool

// SetFragment включает или выключает дробление для всех движков процесса.
// Действует на соединения, открытые после вызова.
func SetFragment(on bool) { fragmentOn.Store(on) }

// fragmentDialer — системный дозвон Xray, который при включённом дроблении
// оборачивает TCP-соединения. Датаграммы и выключенное дробление проходят
// как были.
type fragmentDialer struct {
	internet.SystemDialer
}

func (d fragmentDialer) Dial(ctx context.Context, src xnet.Address, dest xnet.Destination, sockopt *internet.SocketConfig) (xnet.Conn, error) {
	c, err := d.SystemDialer.Dial(ctx, src, dest, sockopt)
	if err != nil || dest.Network != xnet.Network_TCP || !fragmentOn.Load() {
		return c, err
	}
	return transport.FragmentHello(c), nil
}

func init() {
	// Ставится до первого экземпляра Xray: подменять дозвон под работающим
	// движком Xray не разрешает («caller must ensure there is no race»).
	internet.UseAlternativeSystemDialer(fragmentDialer{&internet.DefaultSystemDialer{}})
}
