package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Датаграммы через живую ноду.
//
// Ради этого всё и делалось: видео в TikTok, YouTube и Instagram идёт по QUIC,
// то есть по UDP, а нода до сих пор умела только TCP. Проверять это разбором
// байтов бессмысленно — ломается такое на стыках, между клиентом, нодой и
// сокетом. Поэтому здесь настоящий сервер UDP, настоящее рукопожатие и
// настоящий обмен.

// echoUDP поднимает сервер, возвращающий каждую датаграмму обратно.
func echoUDP(t *testing.T) *net.UDPAddr {
	t.Helper()

	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("сервер udp: %v", err)
	}
	t.Cleanup(func() { _ = socket.Close() })

	go func() {
		buf := make([]byte, vp1.MaxDatagram)
		for {
			n, from, err := socket.ReadFrom(buf)
			if err != nil {
				return
			}
			// Отвечаем тем же, добавив пометку: так видно, что ответ
			// действительно вернулся с той стороны, а не отразился по пути.
			if _, err := socket.WriteTo(append([]byte("эхо:"), buf[:n]...), from); err != nil {
				return
			}
		}
	}()

	return socket.LocalAddr().(*net.UDPAddr)
}

func datagramsTo(t *testing.T, target *net.UDPAddr) net.Conn {
	t.Helper()

	node := startTestNode(t)

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })

	addr, err := vp1.AddressFromHostPort(target.IP.String(), uint16(target.Port))
	if err != nil {
		t.Fatalf("адрес цели: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := dialer.DialDatagrams(ctx, addr)
	if err != nil {
		t.Fatalf("поток датаграмм: %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })

	return stream
}

// TestDatagramsThroughNode — датаграмма доезжает до цели и возвращается.
func TestDatagramsThroughNode(t *testing.T) {
	stream := datagramsTo(t, echoUDP(t))

	if _, err := stream.Write([]byte("привет")); err != nil {
		t.Fatalf("отправка: %v", err)
	}

	buf := make([]byte, vp1.MaxDatagram)
	_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	if string(buf[:n]) != "эхо:привет" {
		t.Fatalf("вернулось %q", buf[:n])
	}
}

// TestDatagramsKeepOrderAndEdges — поток датаграмм не слипается в кашу.
//
// Самая правдоподобная поломка на этом пути — не потеря, а склейка: две
// датаграммы приезжают как одна, и приложение получает мусор. Для TCP такое
// незаметно, для QUIC это порванное соединение и видео, которое «не грузится».
func TestDatagramsKeepOrderAndEdges(t *testing.T) {
	stream := datagramsTo(t, echoUDP(t))

	sent := []string{"раз", "два-подлиннее", "три", "", "пять"}
	for _, s := range sent {
		if _, err := stream.Write([]byte(s)); err != nil {
			t.Fatalf("отправка %q: %v", s, err)
		}
	}

	buf := make([]byte, vp1.MaxDatagram)
	for _, want := range sent {
		_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
		n, err := stream.Read(buf)
		if err != nil {
			t.Fatalf("ответ на %q: %v", want, err)
		}
		if got := string(buf[:n]); got != "эхо:"+want {
			t.Fatalf("на %q вернулось %q", want, got)
		}
	}
}

// TestBigDatagramSurvives — датаграмма больше буфера io.Copy доезжает целой.
//
// Тридцать два килобайта — это размер, который io.Copy берёт по умолчанию.
// Если бы перекладыванием занимался он, датаграмма крупнее не просто
// обрезалась бы: её хвост поехал бы дальше по потоку и разъехались бы все
// следующие.
func TestBigDatagramSurvives(t *testing.T) {
	stream := datagramsTo(t, echoUDP(t))

	big := make([]byte, 40000)
	for i := range big {
		big[i] = byte('a' + i%26)
	}

	if _, err := stream.Write(big); err != nil {
		t.Fatalf("отправка: %v", err)
	}

	buf := make([]byte, vp1.MaxDatagram)
	_ = stream.SetReadDeadline(time.Now().Add(15 * time.Second))
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}

	want := len(big) + len("эхо:")
	if n != want {
		t.Fatalf("вернулось %d байт, ожидалось %d", n, want)
	}
	if string(buf[len("эхо:"):n]) != string(big) {
		t.Fatal("содержимое крупной датаграммы поехало")
	}
}

// TestDatagramsDoNotDisturbConnections — UDP и TCP уживаются в одной сессии.
//
// Оба вида ходят по одному мультиплексору, и перепутанный вид запроса означал
// бы, что соседнее соединение получит датаграмму с длиной вместо своих байтов.
func TestDatagramsDoNotDisturbConnections(t *testing.T) {
	const answer = "обычный ответ"

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("цель tcp: %v", err)
	}
	defer tcp.Close()

	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(answer))
			_ = conn.Close()
		}
	}()

	udpTarget := echoUDP(t)
	node := startTestNode(t)

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	udpAddr, err := vp1.AddressFromHostPort(udpTarget.IP.String(), uint16(udpTarget.Port))
	if err != nil {
		t.Fatalf("адрес udp: %v", err)
	}
	datagrams, err := dialer.DialDatagrams(ctx, udpAddr)
	if err != nil {
		t.Fatalf("поток датаграмм: %v", err)
	}
	defer datagrams.Close()

	// Соединение открываем, пока поток датаграмм жив: обе стороны сидят в
	// одной сессии мультиплексора.
	tcpHost, tcpPort := splitHostPort(t, tcp.Addr().String())
	tcpAddr, err := vp1.AddressFromHostPort(tcpHost, tcpPort)
	if err != nil {
		t.Fatalf("адрес tcp: %v", err)
	}
	stream, err := dialer.DialTarget(ctx, tcpAddr)
	if err != nil {
		t.Fatalf("поток до цели: %v", err)
	}
	defer stream.Close()

	if _, err := datagrams.Write([]byte("пока идёт tcp")); err != nil {
		t.Fatalf("отправка датаграммы: %v", err)
	}

	got := make([]byte, len(answer))
	_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := readFull(stream, got); err != nil {
		t.Fatalf("чтение по tcp: %v", err)
	}
	if string(got) != answer {
		t.Fatalf("по tcp приехало %q", got)
	}

	buf := make([]byte, vp1.MaxDatagram)
	_ = datagrams.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := datagrams.Read(buf)
	if err != nil {
		t.Fatalf("ответ на датаграмму: %v", err)
	}
	if string(buf[:n]) != "эхо:пока идёт tcp" {
		t.Fatalf("на датаграмму вернулось %q", buf[:n])
	}
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	read := 0
	for read < len(buf) {
		n, err := conn.Read(buf[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}
