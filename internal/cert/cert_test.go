package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestNormalizeDomains(t *testing.T) {
	got, err := NormalizeDomains([]string{" *.Example.com ", "api.example.com", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "*.example.com") || !strings.Contains(joined, "example.com") || !strings.Contains(joined, "api.example.com") {
		t.Fatalf("unexpected domains: %v", got)
	}
	if primaryDomain(got) != "example.com" {
		t.Fatalf("primary domain = %q, want example.com", primaryDomain(got))
	}
	if _, err := NormalizeDomains([]string{"bad domain"}); err == nil {
		t.Fatal("expected error for invalid domain")
	}
}

func TestDiscoverAndMatch(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.CertsDir(), "example.com")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestCert(t, filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem"), []string{"example.com", "*.example.com"})

	infos := Discover(store)
	if len(infos) == 0 {
		t.Fatal("Discover 未找到证书")
	}
	var found *Info
	for i := range infos {
		if infos[i].CertPath == filepath.Join(dir, "fullchain.pem") {
			found = &infos[i]
		}
	}
	if found == nil {
		t.Fatalf("未找到刚写入的证书: %+v", infos)
	}
	if found.DaysLeft < 1 || len(found.Domains) != 2 {
		t.Fatalf("证书信息解析异常: %+v", found)
	}

	matched := MatchManaged(infos, []config.CertEntry{{ID: "c1", TargetDir: dir}})
	ok := false
	for _, i := range matched {
		if i.CertPath == found.CertPath && i.ManagedID == "c1" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("托管证书未正确关联")
	}
}

func writeTestCert(t *testing.T, certPath, keyPath string, domains []string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domains[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		DNSNames:     domains,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDer, _ := x509.MarshalECPrivateKey(key)
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatal(err)
	}
}
