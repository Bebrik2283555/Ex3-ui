package optimize

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func setupOptimizeTestEnv(t *testing.T) (dbDir, etcDir string) {
	t.Helper()
	root := t.TempDir()
	dbDir = filepath.Join(root, "db")
	etcDir = filepath.Join(root, "etc")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(etcDir, "sysctl.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XUI_DB_FOLDER", dbDir)

	origResolv := optResolvPath
	origBBR := optSysctlBBRPath
	origTCP := optSysctlTCPPath
	origSwap := optSwapPath
	origLegacyMarker := optLegacyDNSMarker
	origLegacyResolv := optLegacyResolvBackup
	origProcSwaps := optProcSwapsPath
	origHostScript := optRunHostScript
	origRun := optRun
	origRunOutput := optRunOutput
	origRequireLinux := optRequireLinux

	optResolvPath = filepath.Join(etcDir, "resolv.conf")
	optSysctlBBRPath = filepath.Join(etcDir, "sysctl.d", "99-xui-bbr.conf")
	optSysctlTCPPath = filepath.Join(etcDir, "sysctl.d", "99-xui-tcp.conf")
	optSwapPath = filepath.Join(root, "swapfile")
	optLegacyDNSMarker = filepath.Join(etcDir, "x-ui-dns-managed")
	optLegacyResolvBackup = filepath.Join(root, "legacy-resolv.conf")
	optProcSwapsPath = filepath.Join(root, "proc-swaps")
	optRequireLinux = func() error { return nil }

	t.Cleanup(func() {
		optResolvPath = origResolv
		optSysctlBBRPath = origBBR
		optSysctlTCPPath = origTCP
		optSwapPath = origSwap
		optLegacyDNSMarker = origLegacyMarker
		optLegacyResolvBackup = origLegacyResolv
		optProcSwapsPath = origProcSwaps
		optRunHostScript = origHostScript
		optRun = origRun
		optRunOutput = origRunOutput
		optRequireLinux = origRequireLinux
	})
	return dbDir, etcDir
}

func TestSaveBackupsDoNotClobberOriginalState(t *testing.T) {
	dbDir, _ := setupOptimizeTestEnv(t)

	if err := os.WriteFile(optResolvPath, []byte("nameserver 8.8.4.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveDNSBackup(); err != nil {
		t.Fatalf("saveDNSBackup: %v", err)
	}
	// Simulate applyDNS overwriting resolv.conf and re-running applyDNS.
	if err := os.WriteFile(optResolvPath, []byte("nameserver 1.1.1.1\nnameserver 1.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveDNSBackup(); err != nil {
		t.Fatalf("second saveDNSBackup: %v", err)
	}

	optRunOutput = func(name string, args ...string) (string, error) {
		if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
			switch args[1] {
			case "net.core.default_qdisc":
				return "fq_codel", nil
			case "net.ipv4.tcp_congestion_control":
				return "cubic", nil
			case "net.ipv4.tcp_rmem":
				return "4096\t131072\t6291456", nil
			}
		}
		return "0", nil
	}
	saveSysctlBackup(optSysctlBBRPath, bbrSysctlDefaults)
	// Simulate 99-xui-bbr.conf already applied with bbr active.
	if err := os.WriteFile(optSysctlBBRPath, []byte(bbrSysctlConf), 0o644); err != nil {
		t.Fatal(err)
	}
	optRunOutput = func(name string, args ...string) (string, error) {
		if name == "sysctl" && len(args) == 2 && args[0] == "-n" && args[1] == "net.ipv4.tcp_congestion_control" {
			return "bbr", nil
		}
		return "fq", nil
	}
	saveSysctlBackup(optSysctlBBRPath, bbrSysctlDefaults)

	raw, err := os.ReadFile(filepath.Join(dbDir, "optimize-backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b optimizeBackup
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if !b.DNSBackupSaved || b.ResolvContent != "nameserver 8.8.4.4\n" {
		t.Fatalf("DNS backup = %+v, want original nameserver 8.8.4.4", b)
	}
	if b.Sysctl["net.ipv4.tcp_congestion_control"] != "cubic" {
		t.Fatalf("BBR backup = %q, want cubic", b.Sysctl["net.ipv4.tcp_congestion_control"])
	}
}

func TestRevertRestoresDNSAndLiveSysctlWithoutBackup(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)

	// Simulate a server that already ran pre-fix Apply + broken Revert once:
	// no backup file, no drop-ins, no DNS marker, yet live sysctl & resolv.conf remain tuned.
	if err := os.WriteFile(optResolvPath, []byte("nameserver 1.1.1.1\nnameserver 1.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bbrVal := "bbr"
	rmemVal := "4096 87380 16777216"
	optRunOutput = func(name string, args ...string) (string, error) {
		if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
			switch args[1] {
			case "net.ipv4.tcp_congestion_control":
				return bbrVal, nil
			case "net.ipv4.tcp_rmem":
				return rmemVal, nil
			}
		}
		return "", nil
	}

	var capturedAction, capturedScript string
	optRunHostScript = func(action, script string) error {
		capturedAction = action
		capturedScript = script
		bbrVal = "cubic"
		rmemVal = "4096 131072 6291456"
		return nil
	}

	if err := Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if capturedAction != "revert" {
		t.Fatalf("action = %q, want revert", capturedAction)
	}
	for _, want := range []string{
		"sysctl -qw 'net.ipv4.tcp_congestion_control=cubic'",
		"sysctl -qw 'net.ipv4.tcp_rmem=4096 131072 6291456'",
		"sysctl --system",
		"chattr -i",
		optSysctlBBRPath,
		optSysctlTCPPath,
	} {
		if !strings.Contains(capturedScript, want) {
			t.Fatalf("revert script missing %q:\n%s", want, capturedScript)
		}
	}

	st := GetStatus()
	if st.BBREnabled || st.TCPBufferTuned || st.DNSSet || st.SwapEnabled || st.SwapActive {
		t.Fatalf("status after revert = %+v, want all optimizations disabled", st)
	}
}

func TestRevertUsesBackedUpValuesWhenPresent(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)

	b := optimizeBackup{
		DNSBackupSaved: true,
		ResolvContent:  "nameserver 9.9.9.9\n",
		Sysctl: map[string]string{
			"net.core.default_qdisc":          "fq",
			"net.ipv4.tcp_congestion_control": "bbr",
			"net.ipv4.tcp_rmem":               "4096 87380 8388608",
		},
	}
	if err := writeBackup(b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsMarkerPath(), []byte("1.1.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var capturedScript string
	var restoredPayload string
	optRunOutput = func(string, ...string) (string, error) { return "", nil }
	optRunHostScript = func(_ string, script string) error {
		capturedScript = script
		// Extract the temp file path passed to cat '...' and read its contents before Revert cleans it up.
		for _, line := range strings.Split(script, "\n") {
			if strings.Contains(line, "cat '") && strings.Contains(line, "> '"+optResolvPath+"'") {
				start := strings.Index(line, "cat '") + len("cat '")
				end := strings.Index(line[start:], "'")
				if end > 0 {
					data, err := os.ReadFile(line[start : start+end])
					if err == nil {
						restoredPayload = string(data)
					}
				}
			}
		}
		return nil
	}

	if err := Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if restoredPayload != "nameserver 9.9.9.9\n" {
		t.Fatalf("restored resolv payload = %q, want original nameserver 9.9.9.9", restoredPayload)
	}
	if !strings.Contains(capturedScript, "sysctl -qw 'net.ipv4.tcp_rmem=4096 87380 8388608'") {
		t.Fatalf("revert script did not restore backed up tcp_rmem:\n%s", capturedScript)
	}
	if !strings.Contains(capturedScript, "sysctl -qw 'net.ipv4.tcp_congestion_control=bbr'") {
		t.Fatalf("revert script did not restore backed up congestion control:\n%s", capturedScript)
	}
	if _, err := os.Stat(backupPath()); !os.IsNotExist(err) {
		t.Fatalf("backup file still exists after revert: %v", err)
	}
	if _, err := os.Stat(dnsMarkerPath()); !os.IsNotExist(err) {
		t.Fatalf("DNS marker still exists after revert: %v", err)
	}
}

func TestGetStatusDNSRequiresMarker(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)
	optRunOutput = func(string, ...string) (string, error) { return "", nil }

	if err := os.WriteFile(optResolvPath, []byte("nameserver 1.1.1.1\nnameserver 1.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := GetStatus(); st.DNSSet {
		t.Fatalf("DNSSet = true without marker (would stay stuck after revert): %+v", st)
	}

	if err := os.WriteFile(dnsMarkerPath(), []byte("1.1.1.1\n1.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := GetStatus()
	if !st.DNSSet || len(st.DNSResolvers) != 2 {
		t.Fatalf("DNSSet with marker = %+v, want true with 2 resolvers", st)
	}
}

func TestGetStatusSwapAndUnsupportedPlatform(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)

	if err := os.WriteFile(optSwapPath, []byte("swap"), 0o600); err != nil {
		t.Fatal(err)
	}
	optRunOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "sysctl":
			if len(args) == 2 && args[1] == "net.ipv4.tcp_congestion_control" {
				return "bbr", nil
			}
			return "4096 87380 16777216", nil
		case "wc":
			return "536870912 " + optSwapPath, nil
		case "cat":
			return optSwapPath + " file 524288 0 -2", nil
		}
		return "", nil
	}
	st := GetStatus()
	if !st.PlatformSupported || !st.BBREnabled || !st.TCPBufferTuned || !st.SwapEnabled || !st.SwapActive || st.SwapSizeMiB != 512 {
		t.Fatalf("GetStatus() = %+v, want all enabled with 512 MiB swap", st)
	}

	optRequireLinux = func() error { return ErrUnsupportedPlatform }
	st = GetStatus()
	if st.PlatformSupported || st.Error != ErrUnsupportedPlatform.Error() {
		t.Fatalf("unsupported GetStatus() = %+v, want ErrUnsupportedPlatform", st)
	}
	if _, err := Apply(Options{BBR: true}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Apply unsupported err = %v, want ErrUnsupportedPlatform", err)
	}
	if err := Revert(); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Revert unsupported err = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestNormalizeDNSResolvers(t *testing.T) {
	got, err := normalizeDNSResolvers(nil)
	if err != nil || !slices.Equal(got, DefaultDNSResolvers) {
		t.Fatalf("empty = %v, %v; want %v", got, err, DefaultDNSResolvers)
	}
	got, err = normalizeDNSResolvers([]string{" ", ""})
	if err != nil || !slices.Equal(got, DefaultDNSResolvers) {
		t.Fatalf("whitespace = %v, %v; want %v", got, err, DefaultDNSResolvers)
	}
	got, err = normalizeDNSResolvers([]string{" 9.9.9.9 ", "1.1.1.1", "9.9.9.9", "8.8.8.8", "8.8.4.4"})
	want := []string{"9.9.9.9", "1.1.1.1", "8.8.8.8"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("dedup/cap = %v, %v; want %v", got, err, want)
	}
	if _, err := normalizeDNSResolvers([]string{"bad-ip"}); err == nil {
		t.Fatal("expected error for invalid resolver")
	}
}

func TestApplyFullFlowAndErrors(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)

	if err := os.WriteFile(optResolvPath, []byte("nameserver 8.8.8.8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	optRunOutput = func(name string, args ...string) (string, error) {
		if name == "sysctl" && len(args) == 2 && args[1] == "net.ipv4.tcp_congestion_control" {
			return "cubic", nil
		}
		return "100", nil
	}
	var sysctlCalls []string
	optRun = func(name string, args ...string) error {
		sysctlCalls = append(sysctlCalls, name+" "+strings.Join(args, " "))
		return nil
	}
	var swapScript, dnsPayload string
	optRunHostScript = func(action, script string) error {
		switch action {
		case "dns":
			for _, line := range strings.Split(script, "\n") {
				if strings.HasPrefix(line, "cat '") {
					end := strings.Index(line[5:], "'")
					data, err := os.ReadFile(line[5 : 5+end])
					if err == nil {
						dnsPayload = string(data)
					}
				}
			}
		case "swap":
			swapScript = script
			return os.WriteFile(optProcSwapsPath, []byte(optSwapPath+" file\n"), 0o600)
		}
		return nil
	}

	steps, err := Apply(Options{DNS: true, DNSResolvers: []string{"1.1.1.1", "9.9.9.9"}, BBR: true, TCP: true, Swap: true})
	if err != nil {
		t.Fatalf("Apply full: %v", err)
	}
	if !slices.Equal(steps, []string{"dns", "bbr", "tcp", "swap"}) {
		t.Fatalf("steps = %v, want dns, bbr, tcp, swap", steps)
	}
	if dnsPayload != "nameserver 1.1.1.1\nnameserver 9.9.9.9\n" {
		t.Fatalf("dnsPayload = %q", dnsPayload)
	}
	if !strings.Contains(swapScript, "fallocate -l 1024M") {
		t.Fatalf("swap script missing default 1024M:\n%s", swapScript)
	}
	if len(sysctlCalls) != 2 {
		t.Fatalf("sysctl calls = %v, want 2", sysctlCalls)
	}
	marker, err := os.ReadFile(dnsMarkerPath())
	if err != nil || string(marker) != "1.1.1.1\n9.9.9.9\n" {
		t.Fatalf("marker = %q, %v", string(marker), err)
	}

	// Re-applying with swap already active must be a no-op for swap.
	swapScript = ""
	if steps, err = Apply(Options{Swap: true, SwapSize: 256}); err != nil || !slices.Equal(steps, []string{"swap"}) || swapScript != "" {
		t.Fatalf("idempotent swap = (%v, %v, %q)", steps, err, swapScript)
	}

	if _, err := Apply(Options{}); err == nil {
		t.Fatal("expected error for empty options")
	}

	// Error propagation across steps.
	if gotSteps, err := Apply(Options{DNS: true, DNSResolvers: []string{"invalid"}}); err == nil || len(gotSteps) != 0 {
		t.Fatalf("bad DNS = (%v, %v)", gotSteps, err)
	}
	optRun = func(_ string, args ...string) error {
		if len(args) == 2 && args[1] == optSysctlBBRPath {
			return errors.New("bbr fail")
		}
		return errors.New("tcp fail")
	}
	if gotSteps, err := Apply(Options{DNS: true, BBR: true}); err == nil || !slices.Equal(gotSteps, []string{"dns"}) {
		t.Fatalf("BBR fail = (%v, %v)", gotSteps, err)
	}
	optRun = func(_ string, args ...string) error {
		if len(args) == 2 && args[1] == optSysctlTCPPath {
			return errors.New("tcp fail")
		}
		return nil
	}
	if gotSteps, err := Apply(Options{BBR: true, TCP: true}); err == nil || !slices.Equal(gotSteps, []string{"bbr"}) {
		t.Fatalf("TCP fail = (%v, %v)", gotSteps, err)
	}
	_ = os.Remove(optProcSwapsPath)
	optRunHostScript = func(string, string) error { return nil }
	if gotSteps, err := Apply(Options{Swap: true, SwapSize: 128}); err == nil || len(gotSteps) != 0 || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("inactive swap fail = (%v, %v)", gotSteps, err)
	}
}

func TestRevertSymlinkBackupAndRunErrors(t *testing.T) {
	_, _ = setupOptimizeTestEnv(t)

	b := optimizeBackup{
		DNSBackupSaved: true,
		ResolvSymlink:  "/run/systemd/resolve/stub-resolv.conf",
		ResolvContent:  "nameserver 127.0.0.53\n",
		Sysctl: map[string]string{
			"net.ipv4.tcp_congestion_control": "bad;value\n",
		},
	}
	script := buildRevertScript(b, "/tmp/resolv-tmp")
	if !strings.Contains(script, "ln -sfn '/run/systemd/resolve/stub-resolv.conf'") {
		t.Fatalf("expected symlink restore in script:\n%s", script)
	}
	if strings.Contains(script, "bad;value") {
		t.Fatalf("unsafe sysctl value leaked into script:\n%s", script)
	}

	if err := run("xui-nonexistent-binary-test"); err == nil {
		t.Fatal("expected error for missing command")
	}
	if _, err := runOutput("xui-nonexistent-binary-test"); err == nil {
		t.Fatal("expected error from runOutput for missing command")
	}
	if runtime.GOOS == "linux" {
		if err := requireLinux(); err != nil {
			t.Fatalf("requireLinux on linux: %v", err)
		}
	} else {
		if err := requireLinux(); !errors.Is(err, ErrUnsupportedPlatform) {
			t.Fatalf("requireLinux on %s = %v, want ErrUnsupportedPlatform", runtime.GOOS, err)
		}
	}
}
