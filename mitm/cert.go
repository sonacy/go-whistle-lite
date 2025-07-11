package mitm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	rootCert  *x509.Certificate
	rootKey   *rsa.PrivateKey
	certCache = map[string]tlsCertPair{}
	mu        sync.Mutex
)

type tlsCertPair struct {
	CertPEM []byte
	KeyPEM  []byte
}

func init() {
	if err := loadOrCreateCA(); err != nil {
		log.Fatalf("[mitm] CA init error: %v", err)
	}
}

func loadOrCreateCA() error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "go-whistle-lite")
	_ = os.MkdirAll(dir, 0700)

	certPath := filepath.Join(dir, "rootCA.pem")
	keyPath := filepath.Join(dir, "rootCA.key")

	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err2 := os.ReadFile(keyPath); err2 == nil {
			if cb, _ := pem.Decode(certPEM); cb != nil {
				if kb, _ := pem.Decode(keyPEM); kb != nil {
					if rc, err := x509.ParseCertificate(cb.Bytes); err == nil {
						if rk, err := x509.ParsePKCS1PrivateKey(kb.Bytes); err == nil {
							rootCert, rootKey = rc, rk
							log.Printf("[mitm] loaded root CA %s", certPath)
							return nil
						}
					}
				}
			}
		}
	}

	key, _ := rsa.GenerateKey(rand.Reader, 3072)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   "go-whistle-lite Root CA",
			Organization: []string{"go-whistle-lite"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	rootCert, rootKey = tmpl, key

	writePem(certPath, "CERTIFICATE", der)
	writePem(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
	log.Printf("[mitm] new root CA generated: %s (import & trust it)", certPath)
	return nil
}

func writePem(path, typ string, der []byte) {
	f, _ := os.Create(path)
	defer f.Close()
	_ = pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}

func getHostCert(host string) (tlsCertPair, error) {
	mu.Lock()
	defer mu.Unlock()

	if p, ok := certCache[host]; ok {
		return p, nil
	}

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, &key.PublicKey, rootKey)
	if err != nil {
		return tlsCertPair{}, err
	}

	pair := tlsCertPair{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
	certCache[host] = pair
	return pair, nil
}
