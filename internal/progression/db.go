package progression

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
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

	var raw struct {
		Roles   []string                 `json:"roles"`
		Unlocks []UnlockItem             `json:"unlocks"`
		Catalog []map[string]interface{} `json:"catalog"`
	}

	if err := json.Unmarshal(embeddedDBJSON, &raw); err == nil {
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

// GetUnlocksForRole returns all unlock items for a given role sorted by level.
func (db *Database) GetUnlocksForRole(role string) []UnlockItem {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.unlocksByRole[strings.ToLower(role)]
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
	result := make(map[string]*NextUnlockInfo, len(db.roles))
	for _, role := range db.roles {
		lvl := levels[role]
		result[role] = db.GetNextUnlock(role, lvl)
	}
	return result
}
