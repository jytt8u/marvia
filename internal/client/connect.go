package client

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/veilproject/veil/internal/vp1"
)

// ErrPanel помечает неудачу, случившуюся до нод: до панели не достучались.
//
// Вид неудачи важен интерфейсу. «Нет связи с панелью» и «ни одна нода не
// отвечает» — разные беды: в первом случае человеку чаще всего надо просто
// проверить интернет, во втором — писать продавцу.
var ErrPanel = errors.New("панель недоступна")

// ErrExpired и ErrQuota — подписка кончилась.
//
// Отдельно от остальных, потому что это самая частая беда и единственная, где
// человеку надо не чинить, а заплатить. Нода про это знает, но сказать не
// может: она отказывает молча, иначе по её ответам перебирали бы чужие ключи.
// Зато знает панель — и знает заранее, до первого гудка.
var (
	ErrExpired = errors.New("подписка кончилась")
	ErrQuota   = errors.New("кончился трафик по подписке")
)

// ConnectConfig — всё, что нужно, чтобы поднять дозвон до лучшей ноды.
type ConnectConfig struct {
	// Account — разобранная ссылка доступа.
	Account Account

	// Key — ключевая пара покупателя.
	Key vp1.KeyPair

	// Dial — настройки дозвона. В бою пустые.
	Dial Options

	// CachePath — файл кэша подписки. Пусто означает работу без кэша: тогда
	// последовательность ровно та же, что была до его появления.
	CachePath string

	// Log — необязательный журнал хода подключения.
	Log func(format string, args ...any)
}

// Connect выбирает лучшую ноду, стараясь не ходить в панель.
//
// Порядок такой:
//
//  1. свежий кэш — меряем ноды из него и, если хоть одна жива, на этом всё:
//     домен подписки в запросах имён не появляется;
//  2. кэша нет, он протух или все его ноды мертвы — идём в панель и
//     перезаписываем кэш;
//  3. панель молчит, но кэш есть — пробуем его даже протухшим: вчерашняя
//     нода лучше, чем никакой.
//
// Замеры возвращаются всегда, в том числе вместе с ошибкой: их ждёт панель,
// и именно про случай «не работает ничего» продавцу важнее всего узнать.
func Connect(ctx context.Context, cfg ConnectConfig) (*Dialer, []Measurement, error) {
	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	var cached CachedSubscription
	if cfg.CachePath != "" {
		var err error
		cached, err = LoadCache(cfg.CachePath, cfg.Account.SubscriptionURL)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// Кэш — ускорение, а не условие работы. Испорченный файл
			// перезапишется следующим удачным походом в панель.
			logf("кэш подписки не прочитан: %v", err)
		}
	}

	// Замеры последней попытки. Держим их отдельно, чтобы отдать отчёт даже
	// когда все попытки провалились.
	var measurements []Measurement

	if cached.Fresh() {
		logf("замеряю ноды из кэша, их %d", len(cached.Nodes()))
		dialer, m, err := SelectBest(ctx, cached.Nodes(), cfg.Key, cfg.Dial)
		if err == nil {
			// Срок и остаток берём из кэша: он вчерашний, но показать
			// «осталось 12 ГБ» вчерашней точности лучше, чем не показать
			// ничего и погнать человека спрашивать у продавца.
			return dialer.withSubscription(cached.Subscription), m, nil
		}
		measurements = m
		if ctx.Err() != nil {
			// Человек нажал «отключиться», не дождавшись. Тревожить панель
			// незачем: он уже ушёл.
			return nil, measurements, err
		}
		logf("ни одна нода из кэша не ответила, иду в панель")
	}

	logf("забираю список нод")
	sub, err := FetchSubscription(ctx, cfg.Account.SubscriptionURL, cfg.Account.PanelIPs)
	if err != nil {
		dialer, m, ok := lastHope(ctx, cached, cfg, logf)
		if ok {
			return dialer, m, nil
		}
		if m != nil {
			measurements = m
		}
		return nil, measurements, fmt.Errorf("%w: %w", ErrPanel, err)
	}

	if cfg.CachePath != "" {
		if err := SaveCache(cfg.CachePath, cfg.Account.SubscriptionURL, sub); err != nil {
			// Не сохранился кэш — не повод не подключаться. В худшем случае
			// в следующий раз сходим в панель, как раньше.
			logf("кэш подписки не сохранён: %v", err)
		}
	}

	// Кончившуюся подписку видно здесь, и звонить нодам уже незачем: они
	// откажут, а человек прочитает «серверы не отвечают» и пойдёт к продавцу
	// чинить то, что не сломано.
	if err := sub.Allows(); err != nil {
		return nil, measurements, err
	}

	logf("замеряю ноды, их %d", len(sub.Nodes))
	dialer, m, err := SelectBest(ctx, sub.Nodes, cfg.Key, cfg.Dial)
	return dialer.withSubscription(sub), m, err
}

// lastHope пробует протухший кэш, когда панель не ответила.
//
// Панель могли заблокировать саму — тогда протухший список нод единственное,
// что у человека осталось. Свежий кэш сюда не попадает: его уже пробовали
// выше и он не сработал.
func lastHope(ctx context.Context, cached CachedSubscription, cfg ConnectConfig, logf func(string, ...any)) (*Dialer, []Measurement, bool) {
	if ctx.Err() != nil || cached.Fresh() || len(cached.Nodes()) == 0 {
		return nil, nil, false
	}

	logf("панель недоступна, пробую протухший кэш")
	dialer, m, err := SelectBest(ctx, cached.Nodes(), cfg.Key, cfg.Dial)
	if err != nil {
		return nil, m, false
	}
	return dialer.withSubscription(cached.Subscription), m, true
}
