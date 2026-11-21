package progression

import (
	"encoding/json"
	"testing"
)

func TestDatabaseMetadataAndUnlocks(t *testing.T) {
	db := GetDatabase()
	if db == nil {
		t.Fatalf("expected non-nil database")
	}

	ver := db.GetVersion()
	if ver != "3.0.0" {
		t.Errorf("expected version 3.0.0, got %s", ver)
	}

	total := db.GetTotalUnlocks()
	if total != 411 {
		t.Errorf("expected 411 total unlocks, got %d", total)
	}

	all := db.GetAllUnlocks()
	if len(all) != 411 {
		t.Errorf("expected len(GetAllUnlocks()) == 411, got %d", len(all))
	}

	// Verify API response serialization format
	payload := map[string]interface{}{
		"version":       db.GetVersion(),
		"total_unlocks": db.GetTotalUnlocks(),
		"unlocks":       db.GetAllUnlocks(),
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	var parsed struct {
		Version      string        `json:"version"`
		TotalUnlocks int           `json:"total_unlocks"`
		Unlocks      []UnlockItem  `json:"unlocks"`
	}
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if parsed.Version != "3.0.0" {
		t.Errorf("serialized version mismatch: %s", parsed.Version)
	}
	if parsed.TotalUnlocks != 411 {
		t.Errorf("serialized total_unlocks mismatch: %d", parsed.TotalUnlocks)
	}
	if len(parsed.Unlocks) != 411 {
		t.Errorf("serialized unlocks count mismatch: %d", len(parsed.Unlocks))
	}
}

func TestDatabaseUnlocksByRoleNotEmpty(t *testing.T) {
	db := GetDatabase()
	roles := []string{"career", "assault", "medic", "recon", "support", "driver", "pilot"}
	for _, r := range roles {
		items := db.GetUnlocksForRole(r)
		if len(items) == 0 {
			t.Errorf("expected non-empty unlocks for role %s", r)
		}
	}
}
