//go:build native

package proj

import (
	"strings"
	"testing"
)

func TestListEPSGCRSUsesInstalledDatabase(t *testing.T) {
	entries, err := ListEPSGCRS()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 1000 {
		t.Fatalf("EPSG catalog unexpectedly small: %d", len(entries))
	}
	wanted := map[string]bool{"EPSG:4326": false, "EPSG:5186": false, "EPSG:3857": false}
	for _, entry := range entries {
		if _, ok := wanted[entry.Code]; ok {
			if entry.Name == "" || !entry.HasBounds {
				t.Fatalf("incomplete CRS entry: %+v", entry)
			}
			wanted[entry.Code] = true
		}
	}
	for code, found := range wanted {
		if !found {
			t.Errorf("missing %s from installed EPSG catalog", code)
		}
	}
}

func TestListEPSGCRSHasRegionalProjectedCandidates(t *testing.T) {
	entries, err := ListEPSGCRS()
	if err != nil {
		t.Fatal(err)
	}
	regions := map[string][]string{
		"Korea": {"korea"}, "Japan": {"japan"}, "China": {"china"},
		"United States": {"united states", "usa"}, "United Kingdom": {"united kingdom", "great britain"},
		"Germany": {"germany"}, "France": {"france"}, "Australia": {"australia"},
		"Canada": {"canada"}, "Brazil": {"brazil"}, "India": {"india"},
		"New Zealand": {"new zealand"},
	}
	for country, aliases := range regions {
		found := false
		for _, entry := range entries {
			if entry.Kind != "projected" {
				continue
			}
			area := strings.ToLower(entry.Area)
			for _, alias := range aliases {
				if strings.Contains(area, alias) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("installed PROJ catalog has no projected CRS in %s", country)
		}
	}
}
