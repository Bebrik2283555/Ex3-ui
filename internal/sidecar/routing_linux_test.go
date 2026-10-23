//go:build linux

package sidecar

import (
	"reflect"
	"testing"
)

func TestManagedNATDeleteArgs(t *testing.T) {
	line := "-A POSTROUTING -s 10.66.67.0/24 ! -o csqtt1 -m comment --comment CSQTT_MANAGED -j MASQUERADE"
	got, ok := managedNATDeleteArgs(line)
	if !ok {
		t.Fatal("expected the csqtt NAT rule to be deletable")
	}
	want := []string{"-t", "nat", "-D", "POSTROUTING", "-s", "10.66.67.0/24", "!", "-o", "csqtt1", "-m", "comment", "--comment", "CSQTT_MANAGED", "-j", "MASQUERADE"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}

	if _, ok := managedNATDeleteArgs("-A POSTROUTING -s 10.66.67.0/24 -j MASQUERADE"); ok {
		t.Fatal("must not delete a rule without the CSQTT_MANAGED comment")
	}
	if _, ok := managedNATDeleteArgs("-A PREROUTING -j ACCEPT"); ok {
		t.Fatal("must ignore other chains")
	}
}
