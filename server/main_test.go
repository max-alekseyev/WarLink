package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"warlink/server/aclgen"
)

func TestHandleSingBoxConfigUnauthorized(t *testing.T) {
	state := &AppState{
		sessions:     make(map[string]*SessionInfo),
		deviceTokens: make(map[string]string),
	}

	// 1. Missing token
	req := httptest.NewRequest(http.MethodGet, "/api/v1/singbox/config", nil)
	w := httptest.NewRecorder()
	state.handleSingBoxConfig(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", w.Code)
	}

	// 2. Invalid/expired token
	req = httptest.NewRequest(http.MethodGet, "/api/v1/singbox/config?token=invalid_token", nil)
	w = httptest.NewRecorder()
	state.handleSingBoxConfig(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", w.Code)
	}
}

func TestHandleSingBoxConfigSuccess(t *testing.T) {
	state := &AppState{
		sessions:     make(map[string]*SessionInfo),
		deviceTokens: make(map[string]string),
		profiles: []aclgen.Profile{
			{
				ID:        "wardogs",
				Name:      "WARDOGS",
				Processes: []string{"WardogsClient-Win64-Shipping.exe"},
				Domains:   []string{"wardogs.com", "bulkhead.net"},
				IPs:       []string{"85.236.96.0/19"},
				UDPRanges: []string{"54000-55000", "4000-4500"},
			},
		},
		cfg: ServerConfig{
			ServerIP:     "138.124.103.99",
			ServerPorts:  "443,20000-30000",
			ObfsPassword: "test_obfs_pass",
		},
	}

	// Register valid session
	validToken := "wl_tok_unit_test_success"
	state.sessions[validToken] = &SessionInfo{
		DeviceID:  "dev-12345",
		Token:     validToken,
		ClientIP:  "192.0.2.1",
		Game:      "wardogs",
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/singbox/config?token="+validToken+"&game=wardogs&web=1", nil)
	req.RemoteAddr = "192.0.2.1:54321"
	w := httptest.NewRecorder()

	state.handleSingBoxConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success bool                   `json:"success"`
		Game    string                 `json:"game"`
		Config  map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !res.Success {
		t.Errorf("expected success: true")
	}
	if res.Game != "wardogs" {
		t.Errorf("expected game wardogs, got %s", res.Game)
	}
	if res.Config == nil {
		t.Fatalf("expected config object, got nil")
	}

	// Verify route rules contain direct match ports and SDR
	route, ok := res.Config["route"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing route in config")
	}
	rules, ok := route["rules"].([]interface{})
	if !ok || len(rules) == 0 {
		t.Fatalf("missing or empty rules in route config")
	}

	hasMatchTunnel := false
	hasSDRDirect := false
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]interface{})
		if !ok {
			continue
		}
		if rule["outbound"] == "hy2-stockholm" {
			if portRanges, ok := rule["port_range"].([]interface{}); ok {
				for _, pr := range portRanges {
					if pr == "4000:4500" {
						hasMatchTunnel = true
					}
				}
			}
		}
		if rule["outbound"] == "direct" {
			if portRanges, ok := rule["port_range"].([]interface{}); ok {
				for _, pr := range portRanges {
					if pr == "27000:27200" {
						hasSDRDirect = true
					}
				}
			}
		}
	}

	if !hasMatchTunnel {
		t.Errorf("expected hy2-stockholm tunnel rule for UDP 4000:4500")
	}
	if !hasSDRDirect {
		t.Errorf("expected direct rule for UDP 27000:27200")
	}
}

func TestHandleAnalyticsActivePlayers(t *testing.T) {
	state := &AppState{
		cfg: ServerConfig{
			DashboardKey: "test_secret_key",
		},
		sessions: make(map[string]*SessionInfo),
	}

	state.sessions["token_1"] = &SessionInfo{
		DeviceID:  "dev-user-pc-1",
		Token:     "token_1",
		ClientIP:  "198.51.100.22",
		Game:      "wardogs",
		CreatedAt: time.Now().Add(-15 * time.Minute),
		LastSeen:  time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics?key=test_secret_key", nil)
	w := httptest.NewRecorder()

	state.handleAnalytics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success       bool `json:"success"`
		Live          map[string]interface{} `json:"live"`
		ActivePlayers []struct {
			DeviceID     string `json:"device_id"`
			Game         string `json:"game"`
			ClientIP     string `json:"client_ip"`
			DurationDesc string `json:"duration_desc"`
		} `json:"active_players"`
	}

	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if !res.Success {
		t.Errorf("expected success true")
	}
	if len(res.ActivePlayers) != 1 {
		t.Fatalf("expected 1 active player, got %d", len(res.ActivePlayers))
	}
	p := res.ActivePlayers[0]
	if p.Game != "WARDOGS" {
		t.Errorf("expected game WARDOGS, got %s", p.Game)
	}
	if p.ClientIP != "198.51.100.22" {
		t.Errorf("expected IP 198.51.100.22, got %s", p.ClientIP)
	}
}

func TestSyncAezaDonations(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test_api_key" {
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}

		respData := map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":          101,
					"amount":      75.0, // 75 cents -> 98 RUB
					"bonusAmount": 0.0,
					"status":      "performed",
					"type":        "replenishment",
					"invoiceId":   1001,
					"createdAt":   "2026-09-20T18:01:00.000Z",
				},
				{
					"id":          102,
					"amount":      150.0, // 150 cents -> 196 RUB
					"bonusAmount": 0.0,
					"status":      "performed",
					"type":        "replenishment",
					"invoiceId":   1002,
					"createdAt":   "2026-09-26T01:46:00.000Z",
				},
				{
					"id":          103,
					"amount":      75.0,
					"bonusAmount": 0.0,
					"status":      "created", // Pending, not paid!
					"type":        "replenishment",
					"invoiceId":   1003,
					"createdAt":   "2026-09-26T20:00:00.000Z",
				},
				{
					"id":          104,
					"amount":      1450.0,
					"bonusAmount": 0.0,
					"status":      "performed",
					"type":        "buy", // VPS buy, not replenishment!
					"serviceId":   42,
					"createdAt":   "2026-09-26T20:00:00.000Z",
				},
				{
					"id":          105,
					"amount":      500.0,
					"bonusAmount": 0.0,
					"status":      "performed",
					"type":        "replenishment",
					"invoiceId":   999,
					"createdAt":   "2024-11-03T14:22:21.000Z", // Pre-launch old transaction, should be ignored!
				},
			},
			"total": 5,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respData)
	}))
	defer mockServer.Close()

	state := &AppState{
		cfg: ServerConfig{
			AezaAPIKey:      "test_api_key",
			DonateAmountRub: 98,
		},
	}

	state.syncAezaDonationsURL(mockServer.URL)

	if state.metricDonationsPaid != 2 {
		t.Fatalf("expected 2 paid donations, got %d", state.metricDonationsPaid)
	}

	// 98 + 196 = 294 RUB
	if state.metricDonationsRub != 294 {
		t.Fatalf("expected 294 RUB total donations, got %d", state.metricDonationsRub)
	}

	// Test Monotonicity: empty response does not reset counters
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []interface{}{},
			"total": 0,
		})
	}))
	defer emptyServer.Close()

	state.syncAezaDonationsURL(emptyServer.URL)

	if state.metricDonationsPaid != 2 {
		t.Fatalf("expected metricDonationsPaid to remain 2, got %d", state.metricDonationsPaid)
	}
	if state.metricDonationsRub != 294 {
		t.Fatalf("expected metricDonationsRub to remain 294, got %d", state.metricDonationsRub)
	}
}

func TestFetchAezaAccount(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test_key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":           568805,
			"balance":      50.0,  // 50 cents EUR -> 65 RUB
			"bonusBalance": 182.0, // 182 cents EUR -> 238 RUB
			"currency":     "EUR",
		})
	}))
	defer mockServer.Close()

	state := &AppState{
		cfg: ServerConfig{
			AezaAPIKey: "test_key",
		},
	}

	state.fetchAezaAccountURL(mockServer.URL)

	if state.metricAezaBalanceEurCents != 50 {
		t.Fatalf("expected 50 cents EUR, got %d", state.metricAezaBalanceEurCents)
	}
	if state.metricAezaBonusEurCents != 182 {
		t.Fatalf("expected 182 cents bonus EUR, got %d", state.metricAezaBonusEurCents)
	}
	if state.metricAezaBalanceRub != 65 {
		t.Fatalf("expected 65 RUB balance, got %d", state.metricAezaBalanceRub)
	}
	if state.metricAezaBonusRub != 238 {
		t.Fatalf("expected 238 RUB bonus, got %d", state.metricAezaBonusRub)
	}
}


