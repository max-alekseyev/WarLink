package features

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Flag represents an evaluated feature flag for the current client.
type Flag struct {
	Name    string                 `json:"name"`
	Enabled bool                   `json:"enabled"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// ClientFeaturesResponse represents the API response from /api/v1/client/features.
type ClientFeaturesResponse struct {
	Version  int             `json:"version"`
	Features []Flag          `json:"features"`
	Toggles  map[string]Flag `json:"toggles,omitempty"`
}

var (
	flagsMu   sync.RWMutex
	flagsMap  = make(map[string]Flag)
	cacheFile string
)

// Init initializes the feature flags system and loads cached flags from disk.
func Init(dataDir string) {
	flagsMu.Lock()
	defer flagsMu.Unlock()

	if dataDir != "" {
		cacheFile = filepath.Join(dataDir, "features_cache.json")
		if data, err := os.ReadFile(cacheFile); err == nil {
			var cached map[string]Flag
			if err := json.Unmarshal(data, &cached); err == nil && len(cached) > 0 {
				flagsMap = cached
			}
		}
	}

	// Seed built-in default flags if empty
	if len(flagsMap) == 0 {
		flagsMap["troubleshooter_deep_scan"] = Flag{
			Name:    "troubleshooter_deep_scan",
			Enabled: true,
		}
	}
}

// IsEnabled checks if a specific feature flag is currently active.
func IsEnabled(name string) bool {
	flagsMu.RLock()
	defer flagsMu.RUnlock()
	if flag, ok := flagsMap[name]; ok {
		return flag.Enabled
	}
	return false
}

// GetPayload retrieves the custom payload associated with a feature flag.
func GetPayload(name string) (map[string]interface{}, bool) {
	flagsMu.RLock()
	defer flagsMu.RUnlock()
	if flag, ok := flagsMap[name]; ok && flag.Enabled && flag.Payload != nil {
		return flag.Payload, true
	}
	return nil, false
}

// GetAll returns a copy of all current feature flags.
func GetAll() map[string]Flag {
	flagsMu.RLock()
	defer flagsMu.RUnlock()
	copyMap := make(map[string]Flag, len(flagsMap))
	for k, v := range flagsMap {
		copyMap[k] = v
	}
	return copyMap
}

// SetFlag sets or overrides a flag in memory.
func SetFlag(flag Flag) {
	flagsMu.Lock()
	defer flagsMu.Unlock()
	flagsMap[flag.Name] = flag
	saveToCacheLocked()
}

// Reset clears all in-memory flags (useful for tests).
func Reset() {
	flagsMu.Lock()
	defer flagsMu.Unlock()
	flagsMap = make(map[string]Flag)
	cacheFile = ""
}

// FetchRemoteFlags queries the server API for evaluated feature flags.
func FetchRemoteFlags(serverAPI, accountNum, deviceID, appVersion string) error {
	if serverAPI == "" {
		return fmt.Errorf("server API is empty")
	}

	endpoint := fmt.Sprintf("%s/api/v1/client/features?account_number=%s&device_id=%s&version=%s",
		strings.TrimRight(serverAPI, "/"),
		url.QueryEscape(accountNum),
		url.QueryEscape(deviceID),
		url.QueryEscape(appVersion),
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to fetch feature flags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var res ClientFeaturesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("failed to decode features response: %w", err)
	}

	flagsMu.Lock()
	defer flagsMu.Unlock()

	// Update flagsMap
	for _, f := range res.Features {
		flagsMap[f.Name] = f
	}
	if res.Toggles != nil {
		for k, v := range res.Toggles {
			flagsMap[k] = v
		}
	}

	saveToCacheLocked()
	return nil
}

func saveToCacheLocked() {
	if cacheFile == "" {
		return
	}
	data, err := json.MarshalIndent(flagsMap, "", "  ")
	if err == nil {
		_ = os.WriteFile(cacheFile, data, 0644)
	}
}

// EvaluateRollout computes whether an identifier falls within rollout percentage for a given flag.
func EvaluateRollout(flagName, identifier string, rolloutPct int) bool {
	if rolloutPct >= 100 {
		return true
	}
	if rolloutPct <= 0 {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(flagName + ":" + identifier))
	bucket := int(h.Sum32() % 100)
	return bucket < rolloutPct
}
