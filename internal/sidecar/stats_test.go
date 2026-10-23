package sidecar

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// Core counters are keyed by the per-protocol client password, not the legacy
// generic field alone; using it here keeps quota accounting for new clients.
func TestSidecarClientPassword(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		client   model.Client
		want     string
	}{
		{
			name:     "qwdtt explicit password",
			protocol: model.QWDTT,
			client:   model.Client{Password: "generic", QwdttPassword: "qwdtt"},
			want:     "qwdtt",
		},
		{
			name:     "qwdtt legacy generic password",
			protocol: model.QWDTT,
			client:   model.Client{Password: "generic"},
			want:     "generic",
		},
		{
			name:     "csqtt explicit password",
			protocol: model.CSQTT,
			client:   model.Client{Password: "generic", CsqttPassword: "csqtt"},
			want:     "csqtt",
		},
		{
			name:     "csqtt has no generic fallback",
			protocol: model.CSQTT,
			client:   model.Client{Password: "generic"},
			want:     "",
		},
		{
			name:     "olcrtc has no counter",
			protocol: model.OLCRTC,
			client:   model.Client{Password: "generic"},
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sidecarClientPassword(tt.protocol, tt.client); got != tt.want {
				t.Fatalf("sidecarClientPassword() = %q, want %q", got, tt.want)
			}
		})
	}
}
