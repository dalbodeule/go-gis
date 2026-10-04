//go:build qt && native

package main

import (
	"encoding/json"
	"fmt"

	"gogis/drivers/proj"
)

func desktopCRSCatalogJSON() string {
	entries, err := proj.ListEPSGCRS()
	if err != nil {
		payload, _ := json.Marshal(map[string]any{"available": false, "error": err.Error(), "entries": []any{}})
		return string(payload)
	}
	payload, err := json.Marshal(map[string]any{"available": true, "entries": entries})
	if err != nil {
		return fmt.Sprintf("{\"available\":false,\"error\":%q,\"entries\":[]}", err.Error())
	}
	return string(payload)
}
