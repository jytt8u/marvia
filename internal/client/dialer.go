package client

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/tunnel"
	"github.com/veilproject/veil/internal/vp1"
)

// Dialer держит соединение до одной ноды и раздаёт потоки до целей.
type Dialer struct {
	node Node
	pool *tunnel.Pool
}

// Options — необязательные настройки дозвона.
//
// В бою оба поля пустые: доверяем системному хранилищу, как браузер. Нужны
// они для отладки — когда нода поднята с самоподписанным сертификатом.
type Options struct {
	// RootCAs — своё хранилище доверия вместо системного.
	RootCAs *x509.CertPool

	// InsecureSkipVerify отключает проверку сертификата. Только для отладки:
	// с ним любой, кто вклинится в соединение, становится нашей нодой.
	InsecureSkipVerify bool
}

// NewDialer готовит дозвон до ноды. Соединение поднимается лениво, при первом
// обращении: на телефоне включение VPN не должно упираться в сеть.
func NewDialer(node Node, key vp1.KeyPair, opts Options) (*Dialer, error) {
	if node.Address == "" {
		return nil, errors.New("у ноды нет адреса")
	}
	serverPub, err := vp1.DecodeKey(node.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("публичный ключ ноды %s: %w", node.Name, err)
	}

	name := node.SNI
	if name == "" {
		host, _, splitErr := net.SplitHostPort(node.Address)
		if splitErr != nil {
			return nil, fmt.Errorf("адрес ноды %q: %w", node.Address, splitErr)
		}
		name = host
	}

	dial, err := transportDialer(node, name, opts)
	if err != nil {
		return nil, err
	}

	pool := tunnel.NewPool(func(ctx context.Context) (net.Conn, error) {
		raw, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		conn, err := vp1.ClientHandshake(raw, key, serverPub)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}, 0, 0)

	return &Dialer{node: node, pool: pool}, nil
}

// transportDialer выбирает внешний слой под то, как настроена нода.
func transportDialer(node Node, serverName string, opts Options) (func(context.Context) (net.Conn, error), error) {
	tlsCfg := transport.ClientConfig{
		ServerName:         serverName,
		RootCAs:            opts.RootCAs,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}

	switch node.Transport() {
	case TransportWS:
		cfg := transport.WSDialConfig{Host: serverName, Path: node.WSPath, TLS: tlsCfg}
		return func(ctx context.Context) (net.Conn, error) {
			return transport.DialWS(ctx, node.Address, cfg)
		}, nil

	case TransportReality:
		pub, err := vp1.DecodeKey(node.RealityPublicKey)
		if err != nil {
			return nil, fmt.Errorf("публичный ключ REALITY ноды %s: %w", node.Name, err)
		}
		cfg := transport.RealityDialConfig{
			ServerName: serverName,
			PublicKey:  pub,
			ShortID:    node.RealityShortID,
		}
		return func(ctx context.Context) (net.Conn, error) {
			return transport.DialReality(ctx, node.Address, cfg)
		}, nil

	default:
		return func(ctx context.Context) (net.Conn, error) {
			return transport.Dial(ctx, node.Address, tlsCfg)
		}, nil
	}
}

// DialTarget открывает поток до цели через туннель.
func (d *Dialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	stream, err := d.pool.Open(ctx)
	if err != nil {
		return nil, err
	}

	if err := vp1.WriteRequest(stream, target); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("запрос на %s: %w", target, err)
	}

	status, err := vp1.ReadStatus(stream)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("ответ ноды по %s: %w", target, err)
	}
	if status != vp1.StatusOK {
		_ = stream.Close()
		return nil, fmt.Errorf("нода отказала по %s: %s", target, vp1.StatusText(status))
	}
	return stream, nil
}

// Node возвращает ноду, к которой подключён этот дозвон.
func (d *Dialer) Node() Node { return d.node }

// Close закрывает все соединения до ноды.
func (d *Dialer) Close() error { return d.pool.Close() }
