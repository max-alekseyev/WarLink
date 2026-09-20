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

