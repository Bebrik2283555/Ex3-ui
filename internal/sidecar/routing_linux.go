//go:build linux

package sidecar

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Routing convergence windows and retry budgets.
const (
	interfaceWaitTimeout = 3 * time.Second
	interfacePollPeriod  = 200 * time.Millisecond
	natDeleteAttempts    = 4
	managedMasqRounds    = 8
	managedMasqPause     = 150 * time.Millisecond
)

// EnsureRoutingForInbounds re-applies policy routing after Xray starts: the
// TUN devices only exist once the core is up.
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

// EnsureRouting applies the routing and firewall state of one instance.
func EnsureRouting(inst Instance) { ensureRouting(inst) }

func ensureRouting(inst Instance) {
	if !inst.RouteThroughXray {
		ClearRouting(inst)
		if inst.Protocol == model.CSQTT {
			applyCSQTTFirewall(inst, false)
		}
		return
	}
	if strings.TrimSpace(inst.TunName) == "" || inst.RouteTable <= 0 || len(inst.RouteIfaces) == 0 {
		return
	}
	if !waitForInterface(inst.RouteIfaces[0], interfaceWaitTimeout) {
		// The core has not created its client interface yet; nothing to steer.
		return
	}
	tun := inst.TunName
	if !interfaceExists(tun) {
		// The TUN belongs to Xray and appears later; the Xray-start hook or the
		// five-minute refresh completes the job.
		return
	}
	table := strconv.Itoa(inst.RouteTable)
	routingApply("ip", "route", "replace", "default", "dev", tun, "table", table)
	routingApply("sysctl", "-qw", "net.ipv4.conf."+tun+".rp_filter=2")
	routingApply("sysctl", "-qw", "net.ipv4.ip_forward=1")

	for _, iface := range inst.RouteIfaces {
		if !interfaceExists(iface) || routingRuleExists(iface, table) {
			continue
		}
		routingApply("ip", "rule", "add", "iif", iface, "lookup", table)
	}

	switch inst.Protocol {
	case model.QWDTT:
		// Drop the core's own source NAT so its packets enter the TUN.
		for _, subnet := range []string{"10.68.0.0/16", "10.66.0.0/16", "10.70.0.0/16"} {
			removeMasquerade(subnet)
		}
	case model.CSQTT:
		removeMasquerade("10.66.67.0/24")
		removeManagedMasquerade()
		applyCSQTTFirewall(inst, true)
	}
}

// ClearRouting removes only the policy rule and the table the panel owns.
// Firewall rules and sysctl values are left in place.
func ClearRouting(inst Instance) {
	if inst.RouteTable <= 0 {
		return
	}
	table := strconv.Itoa(inst.RouteTable)
	for _, iface := range inst.RouteIfaces {
		routingQuiet("ip", "rule", "del", "iif", iface, "lookup", table)
	}
	routingQuiet("ip", "route", "flush", "table", table)
}

// applyCSQTTFirewall installs the idempotent accept and MSS clamp rules. The
// direct set also appends the masquerade its own deploy script would install.
func applyCSQTTFirewall(inst Instance, routed bool) {
	const clientInterface = "csqtt1"
	routingApply("sysctl", "-qw", "net.ipv4.conf."+clientInterface+".rp_filter=2")

	interfaces := []string{clientInterface}
	if routed && strings.TrimSpace(inst.TunName) != "" {
		interfaces = append(interfaces, inst.TunName)
	}
	for _, iface := range interfaces {
		ensureForwardRule(iface, "-i")
		ensureForwardRule(iface, "-o")
		ensureMSSClampRule(iface, "-i")
		ensureMSSClampRule(iface, "-o")
	}
	if !routed {
		routingApply("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.66.67.0/24", "-j", "MASQUERADE")
	}
}

func ensureForwardRule(iface, direction string) {
	if routingOK("iptables", "-C", "FORWARD", direction, iface, "-j", "ACCEPT") {
		return
	}
	routingApply("iptables", "-I", "FORWARD", "1", direction, iface, "-j", "ACCEPT")
}

func ensureMSSClampRule(iface, direction string) {
	match := []string{"-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", direction, iface, "-j", "TCPMSS", "--clamp-mss-to-pmtu"}
	check := append([]string{"-t", "mangle", "-C", "FORWARD"}, match...)
	if routingOK("iptables", check...) {
		return
	}
	insert := append([]string{"-t", "mangle", "-I", "FORWARD", "1"}, match...)
	routingApply("iptables", insert...)
}

// removeMasquerade deletes one subnet's source NAT up to a few times; the
// core may have installed duplicates.
func removeMasquerade(subnet string) {
	for attempt := 0; attempt < natDeleteAttempts; attempt++ {
		if !routingOK("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", subnet, "-j", "MASQUERADE") {
			return
		}
	}
}

// removeManagedMasquerade deletes the rules the CSQTT binary installs for
// itself. It re-adds them right after its interface appears, hence the rounds.
func removeManagedMasquerade() {
	for round := 0; round < managedMasqRounds; round++ {
		if round > 0 {
			time.Sleep(managedMasqPause)
		}
		if removeManagedMasqueradeRound() > 0 {
			return
		}
	}
}

func removeManagedMasqueradeRound() int {
	output, ok := routingOutput("iptables", "-t", "nat", "-S", "POSTROUTING")
	if !ok {
		return 0
	}
	removed := 0
	for _, line := range strings.Split(output, "\n") {
		args, ok := managedNATDeleteArgs(line)
		if !ok {
			continue
		}
		if routingOK("iptables", args...) {
			removed++
		}
	}
	return removed
}

// managedNATDeleteArgs renders the delete form of a managed `-A POSTROUTING`
// rule line and reports whether the line is such a rule at all.
func managedNATDeleteArgs(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "-A POSTROUTING") ||
		!strings.Contains(line, "CSQTT_MANAGED") ||
		!strings.Contains(line, "MASQUERADE") {
		return nil, false
	}
	fields := strings.Fields(line)
	return append([]string{"-t", "nat", "-D"}, fields[1:]...), true
}

func routingRuleExists(iface, table string) bool {
	output, ok := routingOutput("ip", "rule", "show", "iif", iface)
	if !ok {
		return false
	}
	suffix := "lookup " + table
	for _, line := range strings.Split(output, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), suffix) {
			return true
		}
	}
	return false
}

func interfaceExists(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := net.InterfaceByName(name)
	return err == nil
}

func waitForInterface(name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if interfaceExists(name) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(interfacePollPeriod)
	}
}

// routingApply runs a mutating command and logs a warning when it fails.
func routingApply(name string, args ...string) bool {
	output, err := runRoutingCommand(name, args...)
	if err != nil {
		logger.Warningf("sidecar routing: %s %s: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(output))
		return false
	}
	return true
}

// routingOK runs an existence check where a non-zero exit is a normal answer.
func routingOK(name string, args ...string) bool {
	_, err := runRoutingCommand(name, args...)
	return err == nil
}

func routingOutput(name string, args ...string) (string, bool) {
	output, err := runRoutingCommand(name, args...)
	return output, err == nil
}

// routingQuiet deletes state whose absence is expected and not worth a log.
func routingQuiet(name string, args ...string) {
	_, _ = runRoutingCommand(name, args...)
}

func runRoutingCommand(name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
