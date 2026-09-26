// marvia-bench сравнивает пути данных на TCP loopback, не скорость интернета.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"runtime"

	"time"

	"github.com/jytt8u/marvia/internal/inbound"
	"github.com/jytt8u/marvia/internal/vp1"
)

const chunkSize = 64 << 10

var modes = []string{"TCP", "TLS", "VP1+TLS", "VLESS+TLS", "Trojan+TLS"}
var uuid = []byte("marvia-bench-key")
var digest = sha256.Sum224([]byte("ephemeral-local-benchmark"))
var target = vp1.Address{Type: vp1.AtypIPv4, Host: "127.0.0.1", Port: 443}

type sample struct {
	Round     int     `json:"round"`
	Mode      string  `json:"mode"`
	Bytes     int64   `json:"delivered_bytes"`
	Seconds   float64 `json:"transfer_seconds"`
	MBps      float64 `json:"MB_per_second"`
	RTTMeanUS float64 `json:"echo_mean_us"`
	Cipher    string  `json:"tls_cipher,omitempty"`
}

type setup struct {
	clientTLS, serverTLS *tls.Config
	clientKey, serverKey vp1.KeyPair
}

func main() {
	rounds := flag.Int("rounds", 5, "число чередующихся серий")
	mib := flag.Int64("mib", 512, "полезная нагрузка каждого прогона в МиБ")
	echoes := flag.Int("echoes", 1000, "число последовательных echo по 1400 байт")
	revision := flag.String("revision", "unknown", "ревизия измеряемого кода")
	flag.Parse()
	if *rounds < 1 || *mib < 1 || *echoes < 1 {
		fatal(errors.New("параметры должны быть положительными"))
	}
	s, err := newSetup()
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(map[string]any{"kind": "environment", "date_utc": time.Now().UTC().Format(time.RFC3339), "revision": *revision, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "rounds": *rounds, "mib": *mib, "chunk_bytes": chunkSize, "echoes": *echoes, "echo_bytes": 1400, "tls": "TLS 1.3, X25519, ECDSA P-256 certificate, session tickets disabled", "direction": "server to client", "warmup_mib": 8}); err != nil {
		fatal(err)
	}
	for round := 0; round < *rounds; round++ {
		// В каждой серии сдвигаем порядок: один протокол не получает постоянно
		// холодный процессор или последнее место после прогрева машины.
		for offset := range modes {
			mode := modes[(round+offset)%len(modes)]
			result, err := s.run(mode, *mib<<20, *echoes)
			if err != nil {
				fatal(fmt.Errorf("%s, серия %d: %w", mode, round+1, err))
			}
			result.Round = round + 1
			if err := enc.Encode(result); err != nil {
				fatal(err)
			}
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func newSetup() (*setup, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	base := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}, SessionTicketsDisabled: true}
	serverTLS := base.Clone()
	serverTLS.Certificates = []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}
	clientTLS := base.Clone()
	clientTLS.RootCAs = pool
	clientTLS.ServerName = "localhost"
	sk, err := vp1.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	ck, err := vp1.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return &setup{clientTLS, serverTLS, ck, sk}, nil
}

type connResult struct {
	conn net.Conn
	err  error
}

func (s *setup) pair(mode string) (net.Conn, net.Conn, string, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, nil, "", err
	}
	defer ln.Close()
	ready := make(chan connResult, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			ready <- connResult{nil, err}
			return
		}
		_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
		conn, err := s.server(mode, raw)
		if err != nil {
			raw.Close()
		}
		ready <- connResult{conn, err}
	}()
	raw, err := net.DialTimeout("tcp4", ln.Addr().String(), 10*time.Second)
	if err != nil {
		return nil, nil, "", err
	}
	_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
	client, cipher, err := s.client(mode, raw)
	if err != nil {
		raw.Close()
		result := <-ready
		if result.conn != nil {
			result.conn.Close()
		}
		return nil, nil, "", err
	}
	result := <-ready
	if result.err != nil {
		client.Close()
		return nil, nil, "", result.err
	}
	// Noise снимает собственный дедлайн после хендшейка. Обновляем общий
	// предел для всех режимов, чтобы неисправный тест не зависал бесконечно.
	deadline := time.Now().Add(60 * time.Second)
	_ = client.SetDeadline(deadline)
	_ = result.conn.SetDeadline(deadline)
	return client, result.conn, cipher, nil
}

func (s *setup) server(mode string, raw net.Conn) (net.Conn, error) {
	conn := raw
	if mode != "TCP" {
		t := tls.Server(raw, s.serverTLS)
		if err := t.Handshake(); err != nil {
			return nil, err
		}
		conn = t
	}
	switch mode {
	case "VP1+TLS":
		v, _, err := vp1.ServerHandshake(conn, s.serverKey, vp1.NewReplayGuard(vp1.ClockSkew), func(pub []byte) error {
			if !bytes.Equal(pub, s.clientKey.Public) {
				return vp1.ErrUnauthorized
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		addr, err := vp1.ReadRequest(v)
		if err != nil {
			return nil, err
		}
		if addr != target {
			return nil, errors.New("неверная цель VP1")
		}
		if err := vp1.WriteStatus(v, vp1.StatusOK); err != nil {
			return nil, err
		}
		conn = v
	case "VLESS+TLS":
		r, err := inbound.ReadVLESSRequest(conn)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(r.UUID, uuid) || r.Target != target {
			return nil, errors.New("неверный запрос VLESS")
		}
		conn = inbound.NewVLESSConn(conn)
	case "Trojan+TLS":
		r, err := inbound.ReadTrojanRequest(conn)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(r.Digest, digest[:]) || r.Target != target {
			return nil, errors.New("неверный запрос Trojan")
		}
	}
	// Барьер отделяет хендшейк от измерения передачи во всех режимах.
	if _, err := conn.Write([]byte{1}); err != nil {
		return nil, err
	}
	return conn, nil
}

func (s *setup) client(mode string, raw net.Conn) (net.Conn, string, error) {
	conn := raw
	cipher := ""
	if mode != "TCP" {
		t := tls.Client(raw, s.clientTLS)
		if err := t.Handshake(); err != nil {
			return nil, "", err
		}
		conn = t
		cipher = tls.CipherSuiteName(t.ConnectionState().CipherSuite)
	}
	switch mode {
	case "VP1+TLS":
		v, err := vp1.ClientHandshake(conn, s.clientKey, s.serverKey.Public)
		if err != nil {
			return nil, "", err
		}
		if err := vp1.WriteRequest(v, target); err != nil {
			return nil, "", err
		}
		status, err := vp1.ReadStatus(v)
		if err != nil {
			return nil, "", err
		}
		if status != vp1.StatusOK {
			return nil, "", errors.New("отказ VP1")
		}
		conn = v
	case "VLESS+TLS":
		header := append([]byte{0}, uuid...)
		header = append(header, 0, 1, 1, 187, 1, 127, 0, 0, 1)
		if _, err := conn.Write(header); err != nil {
			return nil, "", err
		}
		var response [2]byte
		if _, err := io.ReadFull(conn, response[:]); err != nil {
			return nil, "", err
		}
		if response != [2]byte{} {
			return nil, "", errors.New("неверный ответ VLESS")
		}
	case "Trojan+TLS":
		addr, err := target.Marshal()
		if err != nil {
			return nil, "", err
		}
		header := []byte(hex.EncodeToString(digest[:]) + "\r\n")
		header = append(header, 1)
		header = append(header, addr...)
		header = append(header, '\r', '\n')
		if _, err := conn.Write(header); err != nil {
			return nil, "", err
		}
	}
	var ready [1]byte
	if _, err := io.ReadFull(conn, ready[:]); err != nil {
		return nil, "", err
	}
	if ready[0] != 1 {
		return nil, "", errors.New("неверный барьер")
	}
	return conn, cipher, nil
}

func (s *setup) run(mode string, total int64, echoes int) (sample, error) {
	client, server, cipher, err := s.pair(mode)
	if err != nil {
		return sample{}, err
	}
	defer client.Close()
	defer server.Close()
	payload := make([]byte, chunkSize)
	for i := range payload {
		payload[i] = byte((i*131 + i/251) % 256)
	}
	sink := make([]byte, chunkSize)
	transfer := func(size int64) (time.Duration, error) {
		done := make(chan error, 1)
		start := time.Now()
		go func() {
			for n := int64(0); n < size; n += chunkSize {
				written, err := server.Write(payload)
				if err != nil {
					done <- err
					return
				}
				if written != len(payload) {
					done <- io.ErrShortWrite
					return
				}
			}
			done <- nil
		}()
		for n := int64(0); n < size; n += chunkSize {
			if _, err := io.ReadFull(client, sink); err != nil {
				return 0, err
			}
			if !bytes.Equal(payload, sink) {
				return 0, errors.New("данные отличаются")
			}
		}
		elapsed := time.Since(start)
		if err := <-done; err != nil {
			return 0, err
		}
		return elapsed, nil
	}
	if _, err := transfer(8 << 20); err != nil {
		return sample{}, err
	}
	elapsed, err := transfer(total)
	if err != nil {
		return sample{}, err
	}
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1400)
		for i := 0; i < echoes; i++ {
			if _, err := io.ReadFull(server, buf); err != nil {
				done <- err
				return
			}
			n, err := server.Write(buf)
			if err != nil {
				done <- err
				return
			}
			if n != len(buf) {
				done <- io.ErrShortWrite
				return
			}
		}
		done <- nil
	}()
	echoStart := time.Now()
	for i := 0; i < echoes; i++ {
		n, err := client.Write(payload[:1400])
		if err != nil {
			return sample{}, err
		}
		if n != 1400 {
			return sample{}, io.ErrShortWrite
		}
		if _, err := io.ReadFull(client, sink[:1400]); err != nil {
			return sample{}, err
		}
		if !bytes.Equal(payload[:1400], sink[:1400]) {
			return sample{}, errors.New("echo отличается")
		}
	}
	if err := <-done; err != nil {
		return sample{}, err
	}
	return sample{Mode: mode, Bytes: total, Seconds: elapsed.Seconds(), MBps: float64(total) / elapsed.Seconds() / 1e6, RTTMeanUS: float64(time.Since(echoStart).Nanoseconds()) / float64(echoes) / 1000, Cipher: cipher}, nil
}
