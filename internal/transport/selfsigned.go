package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// SelfSignedCertificate выпускает самоподписанный сертификат на указанное имя.
//
// Только для отладки и тестов. В бою нужен настоящий сертификат: браузер,
// зашедший на домен ноды, должен увидеть валидный замок — иначе прикрытие
// разваливается на первом же посетителе, а сканеру самоподписанный сертификат
// на 443 порту сам по себе кажется подозрительным.
func SelfSignedCertificate(host string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("генерация ключа: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("серийный номер: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("выпуск сертификата: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("сериализация ключа: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// CertificatePool собирает пул доверия из уже разобранного сертификата.
// Нужен тестам и отладке, чтобы клиент мог доверять самоподписанному серверу,
// не отключая проверку целиком.
func CertificatePool(cert tls.Certificate) (*x509.CertPool, error) {
	if len(cert.Certificate) == 0 {
		return nil, fmt.Errorf("в сертификате нет ни одной записи")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("разбор сертификата: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return pool, nil
}
