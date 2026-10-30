package sidecar

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// LinkFor returns the share links of the client whose email matches the given
// one case-insensitively, first hit in list order. An unparsable inbound or an
// unknown email yields nil.
func LinkFor(ib *model.Inbound, email string) []string {
	inst, err := InstanceFromInbound(ib)
	if err != nil {
		return nil
	}
	for _, client := range inst.Clients {
		if strings.EqualFold(client.Email, email) {
			return inst.Links(client)
		}
	}
	return nil
}
