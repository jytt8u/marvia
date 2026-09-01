package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veilproject/veil/internal/client"
	"github.com/veilproject/veil/internal/mux"
	"github.com/veilproject/veil/internal/relay"
	"github.com/veilproject/veil/internal/transport"
	"github.com/veilproject/veil/internal/vp1"
)

func TestParseAccountLink(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()
	priv := vp1.EncodeKey(pair.Private)

	link := "marvia://" + priv + "@panel.example.com/sub/abc123#%D0%B7%D0%B0%D0%BA%D0%B0%D0%B7%201043"

	account, err := client.ParseAccountLink(link)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if vp1.EncodeKey(account.PrivateKey) != priv {
		t.Fatal("приватный ключ разобран неверно")
	}
	if account.SubscriptionURL != "https://panel.example.com/sub/abc123" {
		t.Fatalf("адрес подписки: %q", account.SubscriptionURL)
	}
	if account.Label != "заказ 1043" {
		t.Fatalf("метка: %q", account.Label)
	}
}

// TestParseAccountLinkRejectsGarbage: покупатель вставляет ссылку руками, и
// внятный отказ здесь дешевле, чем непонятная ошибка соединения потом.
func TestParseAccountLinkRejectsGarbage(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()
	priv := vp1.EncodeKey(pair.Private)

	cases := map[string]string{
		"пусто":             "",
		"чужая схема":       "vless://" + priv + "@panel.example.com/sub/abc",
		"без ключа":         "marvia://panel.example.com/sub/abc",
		"без пути подписки": "marvia://" + priv + "@panel.example.com",
		"ключ не разбирает": "marvia://не-ключ@panel.example.com/sub/abc",
		"ключ не той длины": "marvia://YWJj@panel.example.com/sub/abc",
		"вообще не ссылка":  "просто текст",
		"схема без адреса":  "marvia://",
	}

	for name, link := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := client.ParseAccountLink(link); err == nil {
				t.Fatalf("принята негодная ссылка: %q", link)
			}
		})
	}
}

func TestNodeTransportDetection(t *testing.T) {
	cases := []struct {
		node client.Node
		want client.Transport
	}{
		{client.Node{Address: "a:443"}, client.TransportTLS},
		{client.Node{Address: "a:443", WSPath: "/x"}, client.TransportWS},
		{client.Node{Address: "a:443", RealityPublicKey: "k"}, client.TransportReality},
		// За CDN REALITY невозможен, поэтому путь важнее ключа.
		{client.Node{Address: "a:443", WSPath: "/x", RealityPublicKey: "k"}, client.TransportWS},
	}

	for _, c := range cases {
		if got := c.node.Transport(); got != c.want {
			t.Fatalf("%+v: транспорт %q, ожидался %q", c.node, got, c.want)
		}
	}
}

// TestRealityNodeRejectsBadKey: кривой публичный ключ должен отвергаться
// сразу, при разборе подписки, а не превращаться в непонятный обрыв связи.
func TestRealityNodeRejectsBadKey(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()
	node := client.Node{
		Name: "msk", Address: "msk.example.com:443", SNI: "www.samsung.com",
		PublicKey: vp1.EncodeKey(pair.Public), RealityPublicKey: "не-ключ",
	}

	if _, err := client.NewDialer(node, pair, client.Options{}); err == nil {
		t.Fatal("кривой ключ REALITY принят молча")
	}
}

// TestRealityNodeBuildsDialer: с правильным ключом дозвон собирается.
func TestRealityNodeBuildsDialer(t *testing.T) {
	pair, _ := vp1.GenerateKeyPair()
	reality, _ := vp1.GenerateKeyPair()
	node := client.Node{
		Name: "msk", Address: "msk.example.com:443", SNI: "www.samsung.com",
		PublicKey:        vp1.EncodeKey(pair.Public),
		RealityPublicKey: vp1.EncodeKey(reality.Public), RealityShortID: "0123abcd",
	}

	if node.Transport() != client.TransportReality {
		t.Fatalf("транспорт определён как %q", node.Transport())
	}
	dialer, err := client.NewDialer(node, pair, client.Options{})
	if err != nil {
		t.Fatalf("дозвон до ноды под REALITY не собрался: %v", err)
	}
	_ = dialer.Close()
}

func TestFetchSubscription(t *testing.T) {
	body := map[string]any{
		"nodes": []map[string]any{
			{"name": "msk", "address": "msk.example.com:443", "sni": "msk.example.com", "public_key": "k", "ws_path": "/assets/app.js"},
		},
		"traffic_limit": 1000,
		"used":          400,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("клиент не попросил формат json: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	sub, err := client.FetchSubscription(context.Background(), srv.URL+"/sub/token", nil)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if len(sub.Nodes) != 1 || sub.Nodes[0].Transport() != client.TransportWS {
		t.Fatalf("ноды разобраны неверно: %+v", sub.Nodes)
	}
	if sub.Remaining() != 600 {
		t.Fatalf("остаток квоты %d, ожидалось 600", sub.Remaining())
	}
}

func TestEmptySubscriptionIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nodes":[]}`))
	}))
	defer srv.Close()

	if _, err := client.FetchSubscription(context.Background(), srv.URL+"/sub/token", nil); err == nil {
		t.Fatal("пустая подписка принята молча")
	}
}

// TestDialTargetThroughNode — весь клиентский путь целиком, без TUN: ссылка,
// нода, туннель, поток до цели.
func TestDialTargetThroughNode(t *testing.T) {
	const answer = "содержимое цели"

	// Цель, до которой клиент хочет добраться.
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("цель: %v", err)
	}
	defer target.Close()

	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte(answer))
			_ = conn.Close()
		}
	}()

	node := startTestNode(t)
	targetHost, targetPort := splitHostPort(t, target.Addr().String())

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	addr, err := vp1.AddressFromHostPort(targetHost, targetPort)
	if err != nil {
		t.Fatalf("адрес цели: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := dialer.DialTarget(ctx, addr)
	if err != nil {
		t.Fatalf("поток до цели: %v", err)
	}
	defer stream.Close()

	got := make([]byte, len(answer))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if string(got) != answer {
		t.Fatalf("получено %q", got)
	}
}

// testNode — нода под обычным TLS с самоподписанным сертификатом.
type testNode struct {
	info      client.Node
	clientKey vp1.KeyPair
	opts      client.Options
}

func startTestNode(t *testing.T) *testNode {
	t.Helper()

	serverKey, _ := vp1.GenerateKeyPair()
	clientKey, _ := vp1.GenerateKeyPair()

	cert, err := transport.SelfSignedCertificate("node.example")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	pool, err := transport.CertificatePool(cert)
	if err != nil {
		t.Fatalf("пул доверия: %v", err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	ln := transport.Listen(tcp, transport.ServerConfig{Certificate: cert})
	t.Cleanup(func() { _ = ln.Close() })

	guard := vp1.NewReplayGuard(vp1.ClockSkew)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveNodeConn(conn, serverKey, guard)
		}
	}()

	return &testNode{
		info: client.Node{
			Name:      "test",
			Address:   tcp.Addr().String(),
			SNI:       "node.example",
			PublicKey: vp1.EncodeKey(serverKey.Public),
		},
		clientKey: clientKey,
		// В бою доверие системное; здесь подсовываем свой сертификат,
		// чтобы проверка шла по-настоящему, а не отключалась.
		opts: client.Options{RootCAs: pool},
	}
}

func serveNodeConn(conn net.Conn, key vp1.KeyPair, guard *vp1.ReplayGuard) {
	tunnel, _, err := vp1.ServerHandshake(conn, key, guard, vp1.AllowAll)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer tunnel.Close()

	session, err := mux.Server(tunnel)
	if err != nil {
		return
	}
	defer session.Close()

	for {
		stream, err := mux.Accept(session)
		if err != nil {
			return
		}
		go func() {
			defer stream.Close()

			addr, kind, err := vp1.ReadRequestOf(stream)
			if err != nil {
				return
			}

			// Датаграммы отдаём тому же коду, что работает в бою: копия
			// здесь проверяла бы копию, а не ноду.
			if kind == vp1.KindUDP {
				socket, err := net.DialTimeout("udp", addr.String(), 10*time.Second)
				if err != nil {
					_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
					return
				}
				if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
					_ = socket.Close()
					return
				}
				relay.Datagrams(vp1.Datagrams(stream), socket, 10*time.Second)
				return
			}
			upstream, err := net.DialTimeout("tcp", addr.String(), 10*time.Second)
			if err != nil {
				_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
				return
			}
			defer upstream.Close()

			if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
				return
			}
			go func() { _, _ = io.Copy(upstream, stream) }()
			_, _ = io.Copy(stream, upstream)
		}()
	}
}

func splitHostPort(t *testing.T, addr string) (string, uint16) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("адрес %q: %v", addr, err)
	}
	var value uint16
	for _, c := range port {
		value = value*10 + uint16(c-'0')
	}
	if strings.TrimSpace(host) == "" {
		t.Fatalf("пустой хост в %q", addr)
	}
	return host, value
}

// TestSelectBestSkipsDeadNode — то, ради чего замеры и нужны.
//
// Мёртвая нода в списке — обычное дело: её заблокировали час назад, а панель
// за границей всё ещё считает её живой. Клиент обязан сам это увидеть.
func TestSelectBestSkipsDeadNode(t *testing.T) {
	node := startTestNode(t)

	// Порт, на котором заведомо никого нет.
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	deadAddr := closed.Addr().String()
	_ = closed.Close()

	nodes := []client.Node{
		{ID: 1, Name: "мёртвая", Address: deadAddr, SNI: "node.example", PublicKey: node.info.PublicKey},
		{ID: 2, Name: "живая", Address: node.info.Address, SNI: node.info.SNI, PublicKey: node.info.PublicKey},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialer, measurements, err := client.SelectBest(ctx, nodes, node.clientKey, node.opts)
	if err != nil {
		t.Fatalf("выбор ноды: %v", err)
	}
	defer dialer.Close()

	if dialer.Node().Name != "живая" {
		t.Fatalf("выбрана нода %q", dialer.Node().Name)
	}
	if len(measurements) != 2 {
		t.Fatalf("замеров %d, ожидалось 2", len(measurements))
	}

	// Порядок замеров обязан совпадать с порядком нод: иначе отчёт уедет
	// не про те ноды, и панель опустит в списке живую.
	if measurements[0].Node.Name != "мёртвая" || measurements[1].Node.Name != "живая" {
		t.Fatalf("порядок замеров сбился: %q, %q", measurements[0].Node.Name, measurements[1].Node.Name)
	}
	if measurements[0].OK() {
		t.Fatal("мёртвая нода помечена рабочей")
	}
	if !measurements[1].OK() {
		t.Fatalf("живая нода помечена мёртвой: %v", measurements[1].Err)
	}

	reports := client.ReportsFrom(measurements)
	if len(reports) != 2 {
		t.Fatalf("отчётов %d, ожидалось 2", len(reports))
	}
	if reports[0].OK || !reports[1].OK {
		t.Fatalf("отчёты не соответствуют замерам: %+v", reports)
	}
	if reports[1].LatencyMS < 0 {
		t.Fatal("отрицательная задержка")
	}
}

// TestSelectBestFailsWhenAllDead: если не работает ни одна нода, клиент должен
// сказать об этом прямо и всё равно отдать замеры — именно про такой случай
// продавцу важнее всего узнать.
func TestSelectBestFailsWhenAllDead(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	addr := closed.Addr().String()
	_ = closed.Close()

	pair, _ := vp1.GenerateKeyPair()
	nodes := []client.Node{{ID: 7, Name: "мёртвая", Address: addr, SNI: "node.example", PublicKey: vp1.EncodeKey(pair.Public)}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dialer, measurements, err := client.SelectBest(ctx, nodes, pair, client.Options{})
	if err == nil {
		_ = dialer.Close()
		t.Fatal("выбрана нода там, где не работает ни одна")
	}
	if len(measurements) != 1 || measurements[0].OK() {
		t.Fatalf("замеры не отданы или неверны: %+v", measurements)
	}
	if got := client.ReportsFrom(measurements); len(got) != 1 || got[0].NodeID != 7 {
		t.Fatalf("отчёт собран неверно: %+v", got)
	}
}
