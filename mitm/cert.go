package mitm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	rootYears   = 5
	hostYears   = 1
	rsaBitsRoot = 4096
	rsaBitsHost = 2048
)

var (
	rootOnce sync.Once
	rootKey  *rsa.PrivateKey
	rootCert *x509.Certificate
)

type tlsCertPair struct {
	CertPEM []byte
	KeyPEM  []byte
}

var certLRU *lru.Cache[string, tlsCertPair]

func init() {
	// LRU 1000 hosts ≈ 40-60 MB
	certLRU, _ = lru.New[string, tlsCertPair](1000)
}

/* ----------------------------------------------------
 *  Public entry used by mitm.go
 * --------------------------------------------------*/
func getHostCert(host string) (tlsCertPair, error) {
	rootOnce.Do(initRootCA)

	if cp, ok := certLRU.Get(host); ok {
		return cp, nil
	}

	key, _ := rsa.GenerateKey(rand.Reader, rsaBitsHost)
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-30 * time.Minute),
		NotAfter:     time.Now().AddDate(hostYears, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, &key.PublicKey, rootKey)
	if err != nil {
		return tlsCertPair{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	pair := tlsCertPair{CertPEM: certPEM, KeyPEM: keyPEM}
	certLRU.Add(host, pair)
	return pair, nil
}

/* ----------------------------------------------------
 *  Root CA (create once, persist $HOME/go-whistle-lite)
 * --------------------------------------------------*/
func initRootCA() {
	dir := filepath.Join(os.Getenv("HOME"), "go-whistle-lite")
	_ = os.MkdirAll(dir, 0700)
	certPath := filepath.Join(dir, "rootCA.pem")
	keyPath := filepath.Join(dir, "rootCA.key")

	// load if exists
	cBytes, cErr := os.ReadFile(certPath)
	kBytes, kErr := os.ReadFile(keyPath)
	if cErr == nil && kErr == nil {
		block, _ := pem.Decode(cBytes)
		rootCert, _ = x509.ParseCertificate(block.Bytes)
		block, _ = pem.Decode(kBytes)
		rootKey, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
		log.Printf("[mitm] loaded root CA %s", certPath)
		return
	}

	// else generate new root
	var err error
	rootKey, err = rsa.GenerateKey(rand.Reader, rsaBitsRoot)
	if err != nil {
		log.Fatalf("generate root key: %v", err)
	}

	rootSerial, _ := rand.Int(rand.Reader, big.NewInt(1<<61))
	rootCert = &x509.Certificate{
		SerialNumber: rootSerial,
		Subject: pkix.Name{
			Organization:       []string{"go-whistle-lite"},
			OrganizationalUnit: []string{"Proxy MITM"},
			CommonName:         "go-whistle-lite Root CA",
		},
		NotBefore:  time.Now().Add(-1 * time.Hour),
		NotAfter:   time.Now().AddDate(rootYears, 0, 0),
		KeyUsage:   x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:       true,
		MaxPathLen: 0,
	}

	der, err := x509.CreateCertificate(rand.Reader, rootCert, rootCert, &rootKey.PublicKey, rootKey)
	if err != nil {
		log.Fatalf("create root cert: %v", err)
	}

	cBytes = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kBytes = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rootKey)})

	_ = os.WriteFile(certPath, cBytes, 0600)
	_ = os.WriteFile(keyPath, kBytes, 0600)
	log.Printf("[mitm] generated new root CA %s", certPath)

	fmt.Println("\n⚠️  Import the following RootCA into your system/ browser trust store:\n", certPath)
}
