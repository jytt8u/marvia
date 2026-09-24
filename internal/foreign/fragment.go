package foreign

import (
	"context"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
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
// А решение «резать или нет» — своё у каждого движка (Options.Fragment):
// Xray кладёт свой экземпляр в контекст дозвона, по нему и узнаём. Общий на
// процесс выключатель ломал бы работающий туннель: замер другой подписки с
// другими настройками переключал бы и его.

// fragmented — экземпляры Xray, которым велено резать приветствие.
var fragmented sync.Map // *core.Instance → true

// fragmentDialer — системный дозвон Xray, который при включённом дроблении
// оборачивает TCP-соединения. Датаграммы и выключенное дробление проходят
// как были.
type fragmentDialer struct {
	internet.SystemDialer
}

func (d fragmentDialer) Dial(ctx context.Context, src xnet.Address, dest xnet.Destination, sockopt *internet.SocketConfig) (xnet.Conn, error) {
	c, err := dialSystem(d.SystemDialer, ctx, src, dest, sockopt)
	if err != nil || dest.Network != xnet.Network_TCP || !fragmentFor(ctx) {
		return c, err
	}
	fragmentedDial()
	return transport.FragmentHello(c), nil
}

// fragmentedDial отмечает каждое разрезанное соединение. Пустая в работе,
// проверкам она говорит, что дробление дошло до настоящего дозвона Xray, а
// не только до подставного.
var fragmentedDial = func() {}

// fragmentFor — велено ли резать приветствие движку, который дозванивается.
func fragmentFor(ctx context.Context) bool {
	inst := core.FromContext(ctx)
	if inst == nil {
		return false
	}
	_, on := fragmented.Load(inst)
	return on
}

func init() {
	// Ставится до первого экземпляра Xray: подменять дозвон под работающим
	// движком Xray не разрешает («caller must ensure there is no race»).
	internet.UseAlternativeSystemDialer(fragmentDialer{&internet.DefaultSystemDialer{}})
}
