package transport

import (
	"context"
	"fmt"
	"net"

	uquic "github.com/refraction-networking/uquic"
	utls "github.com/refraction-networking/utls"
)

// Подделка отпечатка QUIC.
//
// Соединение по QUIC опознают не по содержимому — оно зашифровано, — а по
// тому, каким стеком с ним разговаривают. В первом же пакете видно многое:
// какие расширения TLS и в каком порядке, какие параметры транспорта и с
// какими значениями, какой длины идентификатор соединения, чем добит пакет до
// нужного размера. Библиотека quic-go складывает это по-своему, и такой
// рисунок не встречается больше нигде: увидев его, цензору не нужно ничего
// расшифровывать, чтобы понять, что перед ним не браузер.
//
// Здесь клиент здоровается так же, как Chrome. Сервер при этом остаётся на
// quic-go: его ответы изучены куда меньше, а держать две реализации на ноде
// значит удвоить место, где можно ошибиться.
//
// Chrome 115, а не свежее: это самый новый отпечаток, который умеет
// библиотека. Он старый, и это честный недостаток — за три года доля таких
// браузеров упала. Но разница между «как позапрошлый Chrome» и «как никто в
// мире» несопоставима.

// chromeSpec — отпечаток, которым представляется клиент.
var chromeSpec = uquic.QUICChrome_115

// dialQUICMimicking поднимает соединение с чужим отпечатком.
func dialQUICMimicking(ctx context.Context, addr string, tlsCfg *utls.Config) (uquic.Connection, error) {
	remote, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("адрес ноды %q: %w", addr, err)
	}

	spec, err := uquic.QUICID2Spec(chromeSpec)
	if err != nil {
		return nil, fmt.Errorf("отпечаток %s %s: %w", chromeSpec.Client, chromeSpec.Version, err)
	}

	// Порт слушаем любой свободный, как это делает браузер.
	udp, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, fmt.Errorf("сокет udp: %w", err)
	}

	tr := &uquic.UTransport{
		Transport: &uquic.Transport{Conn: udp},
		QUICSpec:  &spec,
	}

	conn, err := tr.Dial(ctx, remote, tlsCfg, &uquic.Config{
		MaxIdleTimeout:  quicIdleTimeout,
		KeepAlivePeriod: quicIdleTimeout / 3,
	})
	if err != nil {
		_ = udp.Close()
		return nil, fmt.Errorf("quic до %s: %w", addr, err)
	}

	return conn, nil
}
