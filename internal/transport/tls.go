// Package transport отвечает за то, как туннель выглядит на проводе.
//
// Разделение слоёв здесь не формальность: VP1 даёт шифрование и подлинность,
// транспорт — правдоподобие. Цензор ломает правдоподобие раз в полгода, и
// менять при этом криптографию нельзя. Поэтому транспорт можно выкинуть и
// заменить целиком, не трогая ничего под ним.
package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

// handshakeTimeout — сколько ждём завершения TLS-хендшейка.
const handshakeTimeout = 15 * time.Second

// ALPN, который объявляет сервер.
//
// Chrome предлагает h2 и http/1.1. Мы выбираем http/1.1 намеренно: так
// запасной обработчик, отдающий настоящий сайт случайному гостю, может быть
// обычным HTTP/1.1-сервером. Пообещать h2 и заговорить не на h2 — заметная
// аномалия, а множество настоящих сайтов http/1.1 и отдаёт.
var serverALPN = []string{"http/1.1"}

// ClientConfig описывает, как клиент маскирует соединение.
type ClientConfig struct {
	// ServerName — имя в SNI. Его видит DPI, и именно по нему в России
	// работает большинство блокировок. Должно выглядеть обыденно.
	ServerName string

	// RootCAs — доверенные корневые сертификаты. nil означает системные.
	RootCAs *x509.CertPool

	// InsecureSkipVerify отключает проверку сертификата.
	// Только для отладки: с ним любой, кто вклинится в соединение,
	// становится нашим сервером.
	InsecureSkipVerify bool

	// Fingerprint — чей ClientHello изображаем. По умолчанию свежий Chrome.
	Fingerprint utls.ClientHelloID
}

func (c ClientConfig) fingerprint() utls.ClientHelloID {
	if c.Fingerprint.Client == "" {
		return utls.HelloChrome_Auto
	}
	return c.Fingerprint
}

// Dial устанавливает замаскированное соединение с сервером.
//
// Хендшейк выполняет uTLS, а не стандартный crypto/tls. Разница
// принципиальная: у Go свой узнаваемый набор и порядок расширений в
// ClientHello, то есть свой отпечаток JA3/JA4. Отпечаток, не встречающийся у
// браузеров, — это готовый признак для классификатора, и в Иране по нему уже
// блокировали. uTLS повторяет ClientHello настоящего Chrome байт в байт.
func Dial(ctx context.Context, addr string, cfg ClientConfig) (net.Conn, error) {
	if cfg.ServerName == "" {
		return nil, fmt.Errorf("не задано имя сервера (SNI)")
	}

	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	uconn := utls.UClient(raw, &utls.Config{
		ServerName:         cfg.ServerName,
		RootCAs:            cfg.RootCAs,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}, cfg.fingerprint())

	hsCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	if err := uconn.HandshakeContext(hsCtx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("TLS-хендшейк с %s (SNI %s): %w", addr, cfg.ServerName, err)
	}
	return uconn, nil
}

// ServerConfig описывает серверную сторону маскировки.
type ServerConfig struct {
	// Certificate — сертификат домена, которым прикрывается нода.
	Certificate tls.Certificate
}

// Listen оборачивает TCP-слушатель в TLS.
//
// Известное ограничение M1: ServerHello генерирует стандартная библиотека Go,
// и её отпечаток отличается от nginx. Клиента мы замаскировали, сервер — пока
// нет. Лечится это по-настоящему только на M1b, где ServerHello приходит от
// настоящего чужого сайта (подход REALITY); промежуточный вариант — поставить
// перед нодой nginx.
func Listen(inner net.Listener, cfg ServerConfig) net.Listener {
	return tls.NewListener(inner, &tls.Config{
		Certificates: []tls.Certificate{cfg.Certificate},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   serverALPN,
	})
}

// LoadCertificate читает пару сертификат/ключ из файлов PEM.
func LoadCertificate(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("загрузка сертификата: %w", err)
	}
	return cert, nil
}
