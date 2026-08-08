package util

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGenerateSelfSigned(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := GenerateSelfSigned(certPath, keyPath, "https://example.com:2053", "203.0.113.10"); err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(cert.DNSNames, "example.com") {
		t.Errorf("DNSNames = %v, want example.com", cert.DNSNames)
	}
	if !containsIP(cert.IPAddresses, "203.0.113.10") {
		t.Errorf("IPAddresses = %v, want 203.0.113.10", cert.IPAddresses)
	}
	if !containsString(cert.DNSNames, "localhost") {
		t.Errorf("DNSNames = %v, want localhost", cert.DNSNames)
	}
	if cert.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Errorf("NotAfter = %v, want ~10 years validity", cert.NotAfter)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestTrimHost(t *testing.T) {
	cases := map[string]string{
		"https://example.com:2053": "example.com",
		"example.com":              "example.com",
		"203.0.113.10:56000":       "203.0.113.10",
		"[::1]:2053":               "::1",
		"  vpn.example.org  ":      "vpn.example.org",
		"":                         "",
	}
	for in, want := range cases {
		if got := trimHost(in); got != want {
			t.Errorf("trimHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func containsIP(list []net.IP, want string) bool {
	for _, ip := range list {
		if ip.String() == want {
			return true
		}
	}
	return false
}
