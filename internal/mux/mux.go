// Package mux мультиплексирует много логических потоков в одном соединении.
//
// Зачем это нужно. Без мультиплексирования каждое TCP-соединение браузера
// требовало отдельного TLS-хендшейка и отдельного хендшейка VP1. Страница с
// полусотней запросов открывалась медленно — и, что важнее, рисовала на
// проводе характернейшую картину: пачка одинаковых коротких соединений,
// вспыхивающих одновременно. Ни один браузер так себя не ведёт.
//
// С мультиплексированием картина другая: несколько долгоживущих
// HTTPS-соединений, внутри которых идёт непрерывный обмен. Именно так
// выглядит обычная работа с современным сайтом по HTTP/2.
package mux

import (
	"fmt"
	"io"
	"log"
	mrand "math/rand/v2"
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	// keepAliveBase и keepAliveJitter задают интервал пинга сессии.
	//
	// Интервал намеренно случайный у каждой сессии. Строго периодический
	// пакет раз в N секунд — сам по себе признак: реальный трафик так не
	// тикает, а вот туннели тикают, и по этому их находят. Совсем отключить
	// пинг нельзя, иначе NAT-трансляции протухают и сессия молча умирает.
	keepAliveBase   = 15 * time.Second
	keepAliveJitter = 30 // секунд

	// connectionWriteTimeout — сколько ждём, пока запись уйдёт в сеть.
	connectionWriteTimeout = 20 * time.Second

	// streamWindow — окно одного потока. Больше окно — выше потолок скорости
	// на длинных задержках: до Европы из Сибири RTT легко доходит до 100 мс,
	// и на окне по умолчанию скорость упирается в него, а не в канал.
	streamWindow = 1 << 20 // 1 МиБ

	// streamCloseTimeout — сколько ждём FIN от собеседника, прежде чем
	// прибрать поток принудительно. Умолчание yamux — пять минут; для ноды с
	// тысячами потоков это слишком долго держит память.
	streamCloseTimeout = 60 * time.Second
)

// Stream — логический поток внутри сессии.
//
// Обёртка нужна ради одного метода. В yamux `Close()` на живом потоке шлёт FIN
// и закрывает только исходящую половину — чтение продолжает работать. Это ровно
// то, что нам нужно, но имя вводит в заблуждение: код выше по стеку ищет
// `CloseWrite()` и, не найдя его, решил бы, что полузакрытие не поддерживается,
// и рвал бы соединение целиком. А это тот самый баг «страница загрузилась
// наполовину».
type Stream struct {
	*yamux.Stream
}

// CloseWrite закрывает исходящую половину потока, оставляя приём открытым.
func (s Stream) CloseWrite() error { return s.Stream.Close() }

// Open открывает новый поток в сессии.
func Open(session *yamux.Session) (net.Conn, error) {
	stream, err := session.OpenStream()
	if err != nil {
		return nil, err
	}
	return Stream{stream}, nil
}

// Accept принимает поток, открытый собеседником.
func Accept(session *yamux.Session) (net.Conn, error) {
	stream, err := session.AcceptStream()
	if err != nil {
		return nil, err
	}
	return Stream{stream}, nil
}

func config() *yamux.Config {
	cfg := yamux.DefaultConfig()

	// yamux ругается, если заданы сразу и Logger, и LogOutput.
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)

	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = keepAliveBase + time.Duration(mrand.IntN(keepAliveJitter))*time.Second
	cfg.ConnectionWriteTimeout = connectionWriteTimeout
	cfg.MaxStreamWindowSize = streamWindow
	cfg.StreamCloseTimeout = streamCloseTimeout

	return cfg
}

// Client открывает мультиплексированную сессию со стороны инициатора.
func Client(conn net.Conn) (*yamux.Session, error) {
	session, err := yamux.Client(conn, config())
	if err != nil {
		return nil, fmt.Errorf("открытие сессии: %w", err)
	}
	return session, nil
}

// Server принимает мультиплексированную сессию со стороны ответчика.
func Server(conn net.Conn) (*yamux.Session, error) {
	session, err := yamux.Server(conn, config())
	if err != nil {
		return nil, fmt.Errorf("приём сессии: %w", err)
	}
	return session, nil
}
