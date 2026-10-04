//go:build qt && native

package main

import (
	"encoding/json"
	"testing"

	"gogis/drivers/proj"
)

func TestDesktopCRSCatalogPublishesSearchableEPSGEntries(t *testing.T) {
	var payload struct {
		Available bool                `json:"available"`
		Entries   []proj.CatalogEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(desktopCRSCatalogJSON()), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Available || len(payload.Entries) < 1000 {
		t.Fatalf("unexpected catalog availability/count: %t/%d", payload.Available, len(payload.Entries))
	}
	for _, entry := range payload.Entries {
		if entry.Code == "EPSG:5186" && entry.Name != "" {
			return
		}
	}
	t.Fatal("EPSG:5186 not published to the picker")
}
