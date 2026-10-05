package features

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestEvaluateRollout(t *testing.T) {
	flagName := "experimental_zapret_strategy"

	// Boundary checks
	if EvaluateRollout(flagName, "user-1", 0) {
		t.Fatal("0% rollout should always return false")
	}
	if !EvaluateRollout(flagName, "user-1", 100) {
		t.Fatal("100% rollout should always return true")
	}

	// Determinism check: same user with same flag must yield identical result across 100 runs
	res1 := EvaluateRollout(flagName, "user-12345", 50)
	for i := 0; i < 100; i++ {
		res2 := EvaluateRollout(flagName, "user-12345", 50)
		if res1 != res2 {
			t.Fatalf("EvaluateRollout is not deterministic! Run %d got %v, expected %v", i, res2, res1)
		}
	}

	// Percentage distribution check across 10,000 synthetic IDs for a 10% canary rollout
	enabledCount := 0
	total := 10000
	for i := 0; i < total; i++ {
		accID := fmt.Sprintf("acc-%04d-%04d", i, i*7%9999)
		if EvaluateRollout(flagName, accID, 10) {
			enabledCount++
		}
	}

	pct := float64(enabledCount) / float64(total) * 100.0
	// 10% expected, should be roughly between 8.5% and 11.5%
	if pct < 8.5 || pct > 11.5 {
		t.Fatalf("10%% canary rollout distribution skewed: got %.2f%% (%d of %d)", pct, enabledCount, total)
	}
}

func TestFlagsInitAndCache(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "warlink_features_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	Reset()
	Init(tempDir)

	if !IsEnabled("troubleshooter_deep_scan") {
		t.Fatal("expected default flag 'troubleshooter_deep_scan' to be true")
	}

	// Add dynamic flag
	SetFlag(Flag{
		Name:    "test_flag",
		Enabled: true,
		Payload: map[string]interface{}{"mode": "turbo"},
	})

	if !IsEnabled("test_flag") {
		t.Fatal("expected 'test_flag' to be enabled")
	}

	payload, ok := GetPayload("test_flag")
	if !ok || payload["mode"] != "turbo" {
		t.Fatalf("expected payload mode=turbo, got %v", payload)
	}

	// Re-init in same directory to test persistence from disk cache
	Reset()
	Init(tempDir)

	if !IsEnabled("test_flag") {
		t.Fatal("expected 'test_flag' to persist from cacheFile")
	}
	payload, ok = GetPayload("test_flag")
	if !ok || payload["mode"] != "turbo" {
		t.Fatalf("expected cached payload mode=turbo, got %v", payload)
	}

	all := GetAll()
	if len(all) < 2 {
		t.Fatalf("expected at least 2 flags, got %d", len(all))
	}
}

func TestFetchRemoteFlags(t *testing.T) {
	Reset()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/client/features" {
			http.NotFound(w, r)
			return
		}

		acc := r.URL.Query().Get("account_number")
		if acc != "acc-100" {
			t.Errorf("expected account_number acc-100, got %s", acc)
		}

		resp := ClientFeaturesResponse{
			Version: 1,
			Features: []Flag{
				{
					Name:    "remote_feature_a",
					Enabled: true,
					Payload: map[string]interface{}{"param": 42},
				},
				{
					Name:    "remote_feature_b",
					Enabled: false,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	err := FetchRemoteFlags(ts.URL, "acc-100", "dev-abc", "v2.2.1")
	if err != nil {
		t.Fatalf("FetchRemoteFlags failed: %v", err)
	}

	if !IsEnabled("remote_feature_a") {
		t.Fatal("expected remote_feature_a to be enabled")
	}
	if IsEnabled("remote_feature_b") {
		t.Fatal("expected remote_feature_b to be disabled")
	}

	p, ok := GetPayload("remote_feature_a")
	if !ok || p["param"].(float64) != 42 {
		t.Fatalf("expected payload param=42, got %v", p)
	}
}
