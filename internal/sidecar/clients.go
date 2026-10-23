package sidecar

import (
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// attachedClients reads the inbound's current members from the normalized
// client_inbounds table. The sidecar settings JSON is only a snapshot written
// when the inbound was saved, while the client form attaches and detaches
// clients through the normalized tables. Returning ok=false keeps the settings
// snapshot usable for imports and for tests that run without a database.
func attachedClients(inboundID int) ([]model.Client, bool) {
	if inboundID <= 0 {
		return nil, false
	}
	db := database.GetDB()
	if db == nil {
		return nil, false
	}
	var records []model.ClientRecord
	err := db.Table("clients").
		Select("clients.*").
		Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Where("client_inbounds.inbound_id = ?", inboundID).
		Order("clients.id ASC").
		Find(&records).Error
	if err != nil {
		return nil, false
	}
	out := make([]model.Client, 0, len(records))
	for i := range records {
		out = append(out, *records[i].ToClient())
	}
	return out, true
}
