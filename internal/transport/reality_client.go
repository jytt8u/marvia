package transport

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// Клиентская сторона REALITY.
//
// Написана вручную, потому что готовой библиотеки клиента не существует:
// github.com/xtls/reality — это только сервер, а клиент живёт внутри
// Xray-core, тащить который в мобильное приложение целиком неразумно.
//
// Ручная реализация протокола — то, чего мы избегали весь проект, и здесь
// оговорка важна: криптографию мы по-прежнему не изобретаем. Все примитивы
// стандартные (X25519, HKDF-SHA256, AES-GCM, HMAC-SHA512), собственных схем
// нет, а правильность проверяется единственным осмысленным способом — живым
// разговором с эталонным сервером. Если наш клиент договорился с
// xtls/reality, он верен по построению; тест это и делает.
//
// Как устроен вход. Клиент прячет метку в поле session_id своего ClientHello:
// снаружи это просто 32 случайных байта, каких там и полагается быть. Метка
// зашифрована на общем секрете, который получается из эфемерного ключа
// клиента и публичного ключа ноды, поэтому подделать её нельзя, а сторонний
// наблюдатель не отличит её от шума.
//
// Как клиент убеждается, что дошёл до ноды, а не до сайта прикрытия. Нода
// предъявляет сертификат, последние 64 байта которого — HMAC-SHA512 на том же
// общем секрете от её ed25519-ключа. Настоящий сайт такого не предъявит, а
// подделать не сможет: секрет знают только двое.

const (
	// realitySessionIDLen — длина поля session_id: 16 байт метки плюс тег.
	realitySessionIDLen = 32

	// realityAuthLen — сколько байт метки шифруется.
	realityAuthLen = 16

	// sessionIDOffset — где session_id лежит в сыром ClientHello.
	//
	// 4 байта заголовка сообщения, 2 версии, 32 случайных, 1 длина
	// session_id. Смещение зафиксировано форматом TLS и не меняется.
	sessionIDOffset = 39

	// realitySignatureLen — длина подписи в хвосте сертификата.
	realitySignatureLen = 64
)

// ErrNotRealityServer — на том конце не наша нода.
//
// Это не сбой связи: скорее всего мы дошли до настоящего сайта прикрытия,
// потому что нода нас не узнала. Причины обычно две — разошлись ключи или
// часы. Сообщение должно вести именно туда, иначе человек будет искать
// проблему в сети.
var ErrNotRealityServer = errors.New("на том конце не нода Veil, а настоящий сайт прикрытия: проверь публичный ключ, короткий идентификатор и часы")

// RealityDialConfig описывает клиентскую сторону REALITY.
type RealityDialConfig struct {
	// ServerName — имя сайта прикрытия. Оно уходит в SNI и видно цензору,
	// поэтому и должно быть чужим популярным доменом.
	ServerName string

	// PublicKey — публичный ключ ноды, 32 байта. В ссылках это pbk.
	PublicKey []byte

	// ShortID — короткий идентификатор в шестнадцатеричном виде, до 16
	// символов. В ссылках это sid. Пусто допустимо.
	ShortID string

	// Fingerprint — чей ClientHello изображаем. По умолчанию свежий Chrome.
	Fingerprint utls.ClientHelloID
}

func (c RealityDialConfig) fingerprint() utls.ClientHelloID {
	if c.Fingerprint.Client == "" {
		return utls.HelloChrome_Auto
	}
	return c.Fingerprint
}

// DialReality открывает соединение до ноды, работающей под REALITY.
func DialReality(ctx context.Context, addr string, cfg RealityDialConfig) (net.Conn, error) {
	if cfg.ServerName == "" {
		return nil, errors.New("не задано имя сайта прикрытия (SNI)")
	}
	if len(cfg.PublicKey) != realityKeyLen {
		return nil, fmt.Errorf("длина публичного ключа ноды %d байт, ожидается %d", len(cfg.PublicKey), realityKeyLen)
	}

	shortID, err := parseShortID(cfg.ShortID)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := realityHandshake(ctx, raw, cfg, shortID)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

// realityHandshake проводит хендшейк поверх уже открытого соединения.
func realityHandshake(ctx context.Context, raw net.Conn, cfg RealityDialConfig, shortID [shortIDLen]byte) (net.Conn, error) {
	uconn := utls.UClient(raw, &utls.Config{
		ServerName: cfg.ServerName,
		// Проверку цепочки отключаем осознанно: сертификат ноды подписан не
		// удостоверяющим центром, а общим секретом. Настоящую проверку
		// подлинности делаем сами ниже, и она строже обычной — подделать
		// её нельзя, не зная приватного ключа ноды.
		InsecureSkipVerify: true,
		// Возобновление сессий выключено: билет меняет вид повторного
		// хендшейка, а метку вставлять некуда.
		SessionTicketsDisabled: true,
	}, cfg.fingerprint())

	// Собираем ClientHello один раз: BuildHandshakeState уже кладёт готовые
	// байты в Raw. Пересобирать нельзя — состояние uTLS после этого
	// рассогласуется, и хендшейк падает с внутренней ошибкой.
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, fmt.Errorf("сборка ClientHello: %w", err)
	}

	hello := uconn.HandshakeState.Hello
	if len(hello.Random) < 32 {
		return nil, errors.New("в ClientHello нет случайных байтов")
	}
	if len(hello.Raw) < sessionIDOffset+realitySessionIDLen {
		return nil, errors.New("ClientHello короче, чем положено по формату")
	}

	private, err := ephemeralX25519(uconn)
	if err != nil {
		return nil, err
	}

	authKey, err := realityAuthKey(private, cfg.PublicKey, hello.Random[:20])
	if err != nil {
		return nil, err
	}

	if err := sealRealityAuth(hello, authKey, shortID); err != nil {
		return nil, err
	}

	if deadline, okDeadline := ctx.Deadline(); okDeadline {
		_ = raw.SetDeadline(deadline)
	} else {
		_ = raw.SetDeadline(time.Now().Add(RealityHandshakeTimeout))
	}

	if err := uconn.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("TLS-хендшейк с %s: %w", cfg.ServerName, err)
	}
	_ = raw.SetDeadline(time.Time{})

	if err := verifyRealityServer(uconn, authKey); err != nil {
		_ = uconn.Close()
		return nil, err
	}
	return uconn, nil
}

// ephemeralX25519 достаёт эфемерный ключ, которым клиент представился.
//
// Порядок предпочтения здесь не произвольный: он в точности повторяет выбор
// ноды. Свежие отпечатки Chrome шлют сразу два обмена ключами — гибридный
// постквантовый и обычный X25519, — а нода сначала ищет обычный и только при
// его отсутствии берёт половину гибрида. Возьми мы другой ключ, общий секрет
// разошёлся бы, и нода молча отправила бы нас на сайт прикрытия: со стороны
// это выглядит как «интернет не работает», а не как ошибка настройки.
func ephemeralX25519(uconn *utls.UConn) (*ecdh.PrivateKey, error) {
	keys := uconn.HandshakeState.State13.KeyShareKeys
	if keys == nil {
		return nil, errors.New("uTLS не отдал эфемерные ключи")
	}

	if keys.Ecdhe != nil && keys.Ecdhe.Curve() == ecdh.X25519() {
		return keys.Ecdhe, nil
	}
	if keys.MlkemEcdhe != nil {
		return keys.MlkemEcdhe, nil
	}
	return nil, errors.New("в ClientHello нет ключа X25519, а REALITY работает только с ним")
}

// realityAuthKey выводит общий секрет.
func realityAuthKey(private *ecdh.PrivateKey, serverPub, salt []byte) ([]byte, error) {
	shared, err := curve25519.X25519(private.Bytes(), serverPub)
	if err != nil {
		return nil, fmt.Errorf("общий секрет: %w", err)
	}

	authKey := make([]byte, realityKeyLen)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt, []byte("REALITY")), authKey); err != nil {
		return nil, fmt.Errorf("вывод ключа: %w", err)
	}
	return authKey, nil
}

// sealRealityAuth зашифровывает метку прямо в собранном ClientHello.
func sealRealityAuth(hello *utls.PubClientHelloMsg, authKey []byte, shortID [shortIDLen]byte) error {
	block, err := aes.NewCipher(authKey)
	if err != nil {
		return fmt.Errorf("шифр: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("режим шифрования: %w", err)
	}

	// Сначала обнуляем поле прямо в собранном сообщении: связанными данными
	// служит весь ClientHello с нулями на месте метки, и сервер при проверке
	// делает ровно то же самое.
	hello.SessionId = make([]byte, realitySessionIDLen)
	copy(hello.Raw[sessionIDOffset:], hello.SessionId)

	copy(hello.SessionId[:4], realityClientVersion)
	binary.BigEndian.PutUint32(hello.SessionId[4:8], uint32(time.Now().Unix()))
	copy(hello.SessionId[8:16], shortID[:])

	// Метка привязана к конкретному ClientHello: к его отпечатку, набору
	// расширений и случайным байтам. Переставить её в чужое приветствие
	// нельзя.
	sealed := aead.Seal(hello.SessionId[:0], hello.Random[20:32], hello.SessionId[:realityAuthLen], hello.Raw)
	if len(sealed) != realitySessionIDLen {
		return fmt.Errorf("метка получилась %d байт вместо %d", len(sealed), realitySessionIDLen)
	}
	copy(hello.Raw[sessionIDOffset:], sealed)
	return nil
}

// realityClientVersion — версия клиента в метке.
//
// Ноды с настройками MinClientVer и MaxClientVer сравнивают её со своими
// границами. Наши такого не требуют, но чужие могут.
var realityClientVersion = []byte{26, 3, 27, 0}

// verifyRealityServer проверяет, что мы дошли до ноды, а не до сайта прикрытия.
//
// Нода подписывает свой ed25519-ключ общим секретом и кладёт подпись в хвост
// сертификата. Настоящий сайт такого не предъявит: он о секрете не знает.
func verifyRealityServer(uconn *utls.UConn, authKey []byte) error {
	certs := uconn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return fmt.Errorf("%w: сертификат не предъявлен", ErrNotRealityServer)
	}

	cert := certs[0]
	pub, okKey := cert.PublicKey.(ed25519.PublicKey)
	if !okKey {
		return fmt.Errorf("%w: ключ сертификата не ed25519", ErrNotRealityServer)
	}
	if len(cert.Raw) < realitySignatureLen {
		return fmt.Errorf("%w: сертификат короче подписи", ErrNotRealityServer)
	}

	mac := hmac.New(sha512.New, authKey)
	mac.Write(pub)
	expected := mac.Sum(nil)

	got := cert.Raw[len(cert.Raw)-realitySignatureLen:]
	if !hmac.Equal(expected, got) {
		return ErrNotRealityServer
	}
	return nil
}

// parseShortID разбирает короткий идентификатор клиента.
func parseShortID(raw string) ([shortIDLen]byte, error) {
	var id [shortIDLen]byte
	if raw == "" {
		return id, nil
	}
	if len(raw) > shortIDLen*2 {
		return id, fmt.Errorf("короткий идентификатор %q длиннее %d символов", raw, shortIDLen*2)
	}
	if len(raw)%2 != 0 {
		return id, fmt.Errorf("короткий идентификатор %q должен состоять из чётного числа символов", raw)
	}

	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return id, fmt.Errorf("короткий идентификатор %q не шестнадцатеричный: %w", raw, err)
	}
	copy(id[:], decoded)
	return id, nil
}
