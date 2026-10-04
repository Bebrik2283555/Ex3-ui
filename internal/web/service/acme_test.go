package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDomainName(t *testing.T) {
	valid := []string{
		"x5media.ru",
		"sub.example.co.uk",
		"xn--80akhbykjv.xn--p1ai",
		"a.example",
	}
	for _, d := range valid {
		if err := ValidateDomainName(d); err != nil {
			t.Errorf("ValidateDomainName(%q) = %v, want nil", d, err)
		}
	}
	invalid := []string{
		"",
		"1.2.3.4",
		"2001:db8::1",
		"http://x5media.ru",
		"x5media.ru:443",
		"*.x5media.ru",
		"-x5media.ru",
		"x5media.ru-",
		"x5media..ru",
		"x5media.ru/",
		"x5 media.ru",
		"X5MEDIA.RU:" + strings.Repeat("a", 400),
	}
	for _, d := range invalid {
		if err := ValidateDomainName(d); err == nil {
			t.Errorf("ValidateDomainName(%q) = nil, want error", d)
		}
	}
}

func TestAnyIPMatches(t *testing.T) {
	cases := []struct {
		name     string
		resolved []string
		v4, v6   string
		want     bool
	}{
		{name: "matches v4", resolved: []string{"203.0.113.5", "203.0.113.6"}, v4: "203.0.113.6", want: true},
		{name: "matches v6", resolved: []string{"2001:db8::1"}, v6: "2001:db8::1", want: true},
		{name: "no match", resolved: []string{"203.0.113.7"}, v4: "203.0.113.6", want: false},
		{name: "empty resolved", resolved: nil, v4: "203.0.113.6", want: false},
		{name: "N/A v4 never matches", resolved: []string{"N/A"}, v4: "N/A", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := anyIPMatches(tc.resolved, tc.v4, tc.v6); got != tc.want {
				t.Errorf("anyIPMatches(%v, %q, %q) = %v, want %v", tc.resolved, tc.v4, tc.v6, got, tc.want)
			}
		})
	}
}

func TestAcmeDomainFromCertFile(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "/root/cert/example.com/fullchain.pem", want: "example.com"},
		{path: "/root/cert/example.com/privkey.pem", want: "example.com"},
		{path: "/etc/x-ui/selfsigned.pem", want: ""},
		{path: "/root/cert/1.2.3.4/fullchain.pem", want: ""},
		{path: "/root/cert/fullchain.pem", want: ""},
		{path: "", want: ""},
	}
	for _, tc := range cases {
		if got := acmeDomainFromCertFile(tc.path); got != tc.want {
			t.Errorf("acmeDomainFromCertFile(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestAcmeEnvOverridesStaleHome(t *testing.T) {
	orig := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, e := range orig {
			_ = os.Setenv(e[:strings.Index(e, "=")], e[strings.Index(e, "=")+1:])
		}
	})
	os.Clearenv()
	_ = os.Setenv("HOME", "")
	_ = os.Setenv("LE_WORKING_DIR", "/stale")
	_ = os.Setenv("PATH", "/usr/bin")
	env := acmeEnv("/root")
	var home, workDir, path string
	homeCount, staleHome, staleWork := 0, 0, 0
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "HOME="):
			home = strings.TrimPrefix(e, "HOME=")
			homeCount++
		case strings.HasPrefix(e, "LE_WORKING_DIR="):
			workDir = strings.TrimPrefix(e, "LE_WORKING_DIR=")
		case e == "HOME=":
			staleHome++
		case e == "LE_WORKING_DIR=/stale":
			staleWork++
		case strings.HasPrefix(e, "PATH="):
			path = e
		}
	}
	if home != "/root" || homeCount != 1 {
		t.Errorf("HOME = %q (count %d), want /root exactly once", home, homeCount)
	}
	if workDir != filepath.Join("/root", ".acme.sh") {
		t.Errorf("LE_WORKING_DIR = %q, want %q", workDir, filepath.Join("/root", ".acme.sh"))
	}
	if staleHome != 0 || staleWork != 0 {
		t.Errorf("stale entries survived: HOME=%d LE_WORKING_DIR=/stale=%d", staleHome, staleWork)
	}
	if path == "" {
		t.Error("PATH missing from env")
	}
}

func TestAcmeHomeIgnoresEmptyHome(t *testing.T) {
	orig := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, e := range orig {
			_ = os.Setenv(e[:strings.Index(e, "=")], e[strings.Index(e, "=")+1:])
		}
	})
	os.Clearenv()
	_ = os.Setenv("HOME", "")
	if h := acmeHome(); h == "" {
		t.Error("acmeHome returned empty string with HOME unset")
	}
	_ = os.Setenv("HOME", "/custom")
	if h := acmeHome(); h != "/custom" {
		t.Errorf("acmeHome = %q, want /custom", h)
	}
}

func TestCertPairExists(t *testing.T) {
	dir := t.TempDir()
	if certPairExists(dir) {
		t.Error("empty dir reported as having a cert pair")
	}
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if certPairExists(dir) {
		t.Error("dir with only fullchain.pem reported as pair")
	}
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !certPairExists(dir) {
		t.Error("dir with both files not reported as pair")
	}
	if certPairExists(filepath.Join(dir, "missing")) {
		t.Error("missing dir reported as pair")
	}
}
func TestAcmeCertPathsFromStreamSettings(t *testing.T) {
	json := `{"network":"xhttp","security":"tls","tlsSettings":{"serverName":"example.com","certificates":[
		{"useFile":true,"certificateFile":"/root/cert/example.com/fullchain.pem","keyFile":"/root/cert/example.com/privkey.pem"},
		{"useFile":true,"certificateFile":"/etc/x-ui/custom.pem","keyFile":"/etc/x-ui/custom.key"}
	]}}`
	got := acmeCertPathsFromStreamSettings(json)
	want := []string{"example.com"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("acmeCertPathsFromStreamSettings = %v, want %v", got, want)
	}
	if got := acmeCertPathsFromStreamSettings(`{"network":"tcp"}`); got != nil {
		t.Errorf("acmeCertPathsFromStreamSettings(no tls) = %v, want nil", got)
	}
	if got := acmeCertPathsFromStreamSettings("not json"); got != nil {
		t.Errorf("acmeCertPathsFromStreamSettings(bad json) = %v, want nil", got)
	}
}
