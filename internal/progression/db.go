package progression

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed progression_db.json
var embeddedDBJSON []byte

// UnlockItem represents an item unlocked at a specific role level.
type UnlockItem struct {
	UnlockID    string                 `json:"unlock_id"`
	Slug        string                 `json:"slug"`
	Name        string                 `json:"name"`
	NameRU      string                 `json:"name_ru"`
	Role        string                 `json:"role"`
	Level       int                    `json:"level"`
	TotalXP     int                    `json:"total_xp"`
	Tab         string                 `json:"tab"`
	Subcategory string                 `json:"subcategory"`
	CategoryRU  string                 `json:"category_ru"`
	Price       int                    `json:"price"`       // In-game $
	UnlockCost  int                    `json:"unlock_cost"` // Unlock points
	Icon        string                 `json:"icon"`
	Image       string                 `json:"image"`
	Guide       string                 `json:"guide"`
	Description string                 `json:"description"`
	Specs       map[string]interface{} `json:"specs"`
}

// NextUnlockInfo describes the next upcoming unlock item for a role.
type NextUnlockInfo struct {
	Role            string      `json:"role"`
	CurrentLevel    int         `json:"current_level"`
	NextItem        *UnlockItem `json:"next_item"`
	LevelsRemaining int         `json:"levels_remaining"`
	UnlockCost      int         `json:"unlock_cost"` // Unlock points needed
	PurchasePrice   int         `json:"purchase_price"` // In-game $
	IsMaxLevel      bool        `json:"is_max_level"`
}

// Database holds full progression and catalog items.
type Database struct {
	mu            sync.RWMutex
	roles         []string
	unlocksByRole map[string][]UnlockItem
	allUnlocks    []UnlockItem
	catalog       []map[string]interface{}
}

var (
	defaultDB *Database
	dbOnce    sync.Once
)

// GetDatabase returns the singleton database instance.
func GetDatabase() *Database {
	dbOnce.Do(func() {
		defaultDB = loadDatabase()
	})
	return defaultDB
}

func loadDatabase() *Database {
	db := &Database{
		roles:         []string{"career", "assault", "medic", "recon", "support", "driver", "pilot"},
		unlocksByRole: make(map[string][]UnlockItem),
		catalog:       make([]map[string]interface{}, 0),
	}

	rawBytes := embeddedDBJSON
	// Check if cached database exists on disk (e.g. downloaded from server)
	for _, cachePath := range []string{"warlink_core/cached_progression_db.json", "core/cached_progression_db.json", "cached_progression_db.json"} {
		if cached, err := os.ReadFile(cachePath); err == nil && len(cached) > 1000 {
			rawBytes = cached
			break
		}
	}

	var raw struct {
		Roles   []string                 `json:"roles"`
		Unlocks []UnlockItem             `json:"unlocks"`
		Catalog []map[string]interface{} `json:"catalog"`
	}

	if err := json.Unmarshal(rawBytes, &raw); err == nil {
		if len(raw.Roles) > 0 {
			db.roles = raw.Roles
		}
		db.allUnlocks = raw.Unlocks
		db.catalog = raw.Catalog

		for _, item := range raw.Unlocks {
			role := strings.ToLower(item.Role)
			db.unlocksByRole[role] = append(db.unlocksByRole[role], item)
		}

		// Sort unlocks by level ascending
		for role := range db.unlocksByRole {
			items := db.unlocksByRole[role]
			sort.Slice(items, func(i, j int) bool {
				return items[i].Level < items[j].Level
			})
			db.unlocksByRole[role] = items
		}
	}
	return db
}

// UpdateFromJSON updates the database in memory from fresh JSON bytes.
func (db *Database) UpdateFromJSON(data []byte) error {
	var raw struct {
		Roles   []string                 `json:"roles"`
		Unlocks []UnlockItem             `json:"unlocks"`
		Catalog []map[string]interface{} `json:"catalog"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Unlocks) == 0 {
		return fmt.Errorf("empty unlocks in progression data")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if len(raw.Roles) > 0 {
		db.roles = raw.Roles
	}
	db.allUnlocks = raw.Unlocks
	db.catalog = raw.Catalog
	db.unlocksByRole = make(map[string][]UnlockItem)

	for _, item := range raw.Unlocks {
		role := strings.ToLower(item.Role)
		db.unlocksByRole[role] = append(db.unlocksByRole[role], item)
	}

	for role := range db.unlocksByRole {
		items := db.unlocksByRole[role]
		sort.Slice(items, func(i, j int) bool {
			return items[i].Level < items[j].Level
		})
		db.unlocksByRole[role] = items
	}
	return nil
}

// SyncWithServer fetches the latest progression database from the remote server API and caches it.
func SyncWithServer(serverAPI string, cacheDir string) error {
	if serverAPI == "" {
		return fmt.Errorf("empty serverAPI")
	}
	u := strings.TrimRight(serverAPI, "/") + "/api/v1/progression/database"
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(body) < 1000 {
		return fmt.Errorf("database response too small: %d bytes", len(body))
	}

	db := GetDatabase()
	if err := db.UpdateFromJSON(body); err != nil {
		return err
	}

	if cacheDir != "" {
		_ = os.MkdirAll(cacheDir, 0755)
		cacheFile := filepath.Join(cacheDir, "cached_progression_db.json")
		_ = os.WriteFile(cacheFile, body, 0644)
	}
	return nil
}


// GetAllUnlocks returns all unlocks across all roles.
func (db *Database) GetAllUnlocks() []UnlockItem {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.allUnlocks
}

// GetNextUnlock calculates the upcoming reward for a specific role and current level.
func (db *Database) GetNextUnlock(role string, currentLevel int) *NextUnlockInfo {
	db.mu.RLock()
	defer db.mu.RUnlock()

	items := db.unlocksByRole[strings.ToLower(role)]
	for _, it := range items {
		if it.Level > currentLevel {
			itemCopy := it
			return &NextUnlockInfo{
				Role:            role,
				CurrentLevel:    currentLevel,
				NextItem:        &itemCopy,
				LevelsRemaining: it.Level - currentLevel,
				UnlockCost:      it.UnlockCost,
				PurchasePrice:   it.Price,
				IsMaxLevel:      false,
			}
		}
	}

	// If no further unlocks
	var lastItem *UnlockItem
	if len(items) > 0 {
		c := items[len(items)-1]
		lastItem = &c
	}
	return &NextUnlockInfo{
		Role:            role,
		CurrentLevel:    currentLevel,
		NextItem:        lastItem,
		LevelsRemaining: 0,
		UnlockCost:      0,
		PurchasePrice:   0,
		IsMaxLevel:      true,
	}
}

// GetNextUnlocksForAllRoles computes next unlocks for all 6 roles + career.
func (db *Database) GetNextUnlocksForAllRoles(levels map[string]int) map[string]*NextUnlockInfo {
	db.mu.RLock()
	rolesCopy := make([]string, len(db.roles))
	copy(rolesCopy, db.roles)
	db.mu.RUnlock()

	result := make(map[string]*NextUnlockInfo, len(rolesCopy))
	for _, role := range rolesCopy {
		lvl := levels[role]
		result[role] = db.GetNextUnlock(role, lvl)
	}
	return result
}
