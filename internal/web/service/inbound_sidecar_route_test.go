package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestSidecarRoutesThroughXray(t *testing.T) {
	tests := []struct {
		name string
		ib   *model.Inbound
		want bool
	}{
		{name: "nil", ib: nil, want: false},
		{
			name: "qwdtt enabled",
			ib:   &model.Inbound{Protocol: model.QWDTT, Settings: `{"sidecar":{"routeThroughXray":true}}`},
			want: true,
		},
		{
			name: "csqtt disabled",
			ib:   &model.Inbound{Protocol: model.CSQTT, Settings: `{"sidecar":{"routeThroughXray":false}}`},
			want: false,
		},
		{
			name: "invalid settings",
			ib:   &model.Inbound{Protocol: model.CSQTT, Settings: `{`},
			want: false,
		},
		{
			name: "xray inbound",
			ib:   &model.Inbound{Protocol: model.VLESS, Settings: `{"sidecar":{"routeThroughXray":true}}`},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sidecarRoutesThroughXray(tt.ib); got != tt.want {
				t.Fatalf("sidecarRoutesThroughXray() = %v, want %v", got, tt.want)
			}
		})
	}
}
