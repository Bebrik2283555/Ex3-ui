//go:build linux

package sidecar

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Sidecar interface and subnet constants. They mirror what the external
// binaries create on the host.
const (
	qwdttIfaceWG   = "wdtt0"
	qwdttIfaceRaw  = "wdttraw0"
	qwdttSubnetWG  = "10.68.0.0/16"
	qwdttSubnetOld = "10.66.0.0/16"
	qwdttSubnetRaw = "10.70.0.0/16"
	csqttIface     = "csqtt1"
	csqttSubnet    = "10.66.67.0/24"
)

// EnsureRoutingForInbounds reapplies policy routing after Xray starts: the
// TUN devices only exist once the core is up, so the sidecar start path alone
// cannot finish the bridge.
func EnsureRoutingForInbounds(inbounds []*model.Inbound) {
	for _, ib := range inbounds {
		if ib == nil || !Supported(ib.Protocol) || !ib.Enable || ib.NodeID != nil {
			continue
		}
		inst, err := InstanceFromInbound(ib)
		if err != nil {
			continue
		}
		ensureRouting(inst)
	}
}

// ensureRouting converges the kernel state that steers a sidecar's client
// interface into its Xray TUN. Idempotent and tolerant of a not-yet-created
// link, so it is safe to call after every start or config change.
func ensureRouting(inst Instance) {
	if !inst.RouteThroughXray {
		// Turning the Xray bridge off must drop the stale iif rule, otherwise
		// packets still enter a table whose TUN no longer exists. csqtt also
		// needs the NAT rule its upstream deploy script normally installs.
		ClearRouting(inst)
		if inst.Protocol == model.CSQTT {
			applyFirewall(csqttDirectFirewall()...)
		}
		return
	}
	if strings.TrimSpace(inst.TunName) == "" || inst.RouteTable <= 0 {
		return
	}
	ifaces := inst.RouteIfaces
	if len(ifaces) == 0 {
		if inst.Protocol == model.CSQTT {
			ifaces = []string{csqttIface}
		} else {
			ifaces = []string{qwdttIfaceWG, qwdttIfaceRaw}
		}
	}
	waitLink(ifaces[0], 3*time.Second)
	if !linkExists(inst.TunName) {
		return
	}

	table := strconv.Itoa(inst.RouteTable)
	runCmd("ip", "route", "replace", "default", "dev", inst.TunName, "table", table)
	runCmd("sysctl", "-qw", "net.ipv4.conf."+inst.TunName+".rp_filter=2")
	runCmd("sysctl", "-qw", "net.ipv4.ip_forward=1")

	for _, iface := range ifaces {
		if !linkExists(iface) {
			continue
		}
		if !ruleExists(iface, inst.RouteTable) {
			runCmd("ip", "rule", "add", "iif", iface, "lookup", table)
		}
	}

	switch inst.Protocol {
	case model.QWDTT:
		stripMasquerade(qwdttSubnetWG, qwdttSubnetOld, qwdttSubnetRaw)
	case model.CSQTT:
		stripMasquerade(csqttSubnet)
		// csqtt 2.5+ installs its own CSQTT_MANAGED MASQUERADE as soon as its
		// TUN appears, which pre-empts the Xray TUN bridge. The binary starts
		// right after the link is created, so retry briefly.
		for i := 0; i < 8; i++ {
			if stripManagedCSQTTNAT() {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		applyFirewall(csqttFirewall(inst.TunName)...)
	}
}

// ClearRouting removes the policy rule and table owned by this inbound.
func ClearRouting(inst Instance) {
	if inst.RouteTable <= 0 {
		return
	}
	ifaces := inst.RouteIfaces
	if len(ifaces) == 0 {
		if inst.Protocol == model.CSQTT {
			ifaces = []string{csqttIface}
		} else {
			ifaces = []string{qwdttIfaceWG, qwdttIfaceRaw}
		}
	}
	table := strconv.Itoa(inst.RouteTable)
	for _, iface := range ifaces {
		_ = exec.CommandContext(context.Background(), "ip", "rule", "del", "iif", iface, "lookup", table).Run()
	}
	_ = exec.CommandContext(context.Background(), "ip", "route", "flush", "table", table).Run()
}

func waitLink(iface string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for !linkExists(iface) && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
}

func linkExists(iface string) bool {
	if iface == "" {
		return false
	}
	return exec.CommandContext(context.Background(), "ip", "link", "show", iface).Run() == nil
}

func ruleExists(iface string, table int) bool {
	out, err := exec.CommandContext(context.Background(), "ip", "rule", "show", "iif", iface).Output()
	if err != nil {
		return false
	}
	needle := "lookup " + strconv.Itoa(table)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), needle) {
			return true
		}
	}
	return false
}

func stripMasquerade(subnets ...string) {
	for _, subnet := range subnets {
		for i := 0; i < 4; i++ {
			if err := exec.CommandContext(context.Background(), "iptables", "-t", "nat", "-D", "POSTROUTING",
				"-s", subnet, "-j", "MASQUERADE").Run(); err != nil {
				break
			}
		}
	}
}

// stripManagedCSQTTNAT removes the NAT rules csqtt installs itself. In Xray
// mode they must go, otherwise packets are SNATed before reaching the TUN.
func stripManagedCSQTTNAT() bool {
	out, err := exec.CommandContext(context.Background(), "iptables", "-t", "nat", "-S", "POSTROUTING").Output()
	if err != nil {
		return false
	}
	removed := false
	for _, line := range strings.Split(string(out), "\n") {
		args, ok := managedNATDeleteArgs(line)
		if !ok {
			continue
		}
		runCmd("iptables", args...)
		removed = true
	}
	return removed
}

// managedNATDeleteArgs turns an iptables -S POSTROUTING line into the matching
// delete invocation, but only for csqtt's own NAT rule.
func managedNATDeleteArgs(line string) ([]string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "-A" || fields[1] != "POSTROUTING" {
		return nil, false
	}
	if !strings.Contains(line, "CSQTT_MANAGED") || !strings.Contains(line, "MASQUERADE") {
		return nil, false
	}
	for i, field := range fields {
		if strings.Trim(field, `"`) == "CSQTT_MANAGED" {
			fields[i] = "CSQTT_MANAGED"
		}
	}
	args := []string{"-t", "nat", "-D", "POSTROUTING"}
	return append(args, fields[2:]...), true
}

type firewallRule struct {
	table string
	chain string
	spec  []string
}

func ifaceRules(iface string) []firewallRule {
	return []firewallRule{
		{"", "FORWARD", []string{"-i", iface, "-j", "ACCEPT"}},
		{"", "FORWARD", []string{"-o", iface, "-j", "ACCEPT"}},
		{"mangle", "FORWARD", []string{"-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-i", iface, "-j", "TCPMSS", "--clamp-mss-to-pmtu"}},
		{"mangle", "FORWARD", []string{"-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-o", iface, "-j", "TCPMSS", "--clamp-mss-to-pmtu"}},
	}
}

func csqttFirewall(tun string) []firewallRule {
	rules := ifaceRules(csqttIface)
	if tun != "" {
		rules = append(rules, ifaceRules(tun)...)
	}
	return rules
}

// csqttDirectFirewall is the routeThroughXray=off path: csqtt's own deploy.sh
// installs this NAT rule, and the panel does not run deploy.sh.
func csqttDirectFirewall() []firewallRule {
	return append(ifaceRules(csqttIface), firewallRule{
		table: "nat", chain: "POSTROUTING",
		spec: []string{"-s", csqttSubnet, "-j", "MASQUERADE"},
	})
}

func applyFirewall(rules ...firewallRule) {
	runCmd("sysctl", "-qw", "net.ipv4.conf."+csqttIface+".rp_filter=2")
	for _, r := range rules {
		check := iptablesArgs(r.table, "-C", r.chain, r.spec...)
		if exec.CommandContext(context.Background(), "iptables", check...).Run() == nil {
			continue
		}
		// Prepend FORWARD rules so a later UFW reject cannot win.
		if r.chain == "FORWARD" {
			insert := iptablesArgs(r.table, "-I", r.chain, append([]string{"1"}, r.spec...)...)
			runCmd("iptables", insert...)
			continue
		}
		runCmd("iptables", iptablesArgs(r.table, "-A", r.chain, r.spec...)...)
	}
}

func iptablesArgs(table, op, chain string, spec ...string) []string {
	args := make([]string, 0, 6+len(spec))
	if table != "" && table != "filter" {
		args = append(args, "-t", table)
	}
	args = append(args, op, chain)
	return append(args, spec...)
}

func runCmd(name string, args ...string) {
	if out, err := exec.CommandContext(context.Background(), name, args...).CombinedOutput(); err != nil {
		logger.Warningf("sidecar routing: %s %s: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// EnsureRouting is the package entry point used by the manager.
func EnsureRouting(inst Instance) { ensureRouting(inst) }
