package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestHandleDesyncConfig(t *testing.T) {
	state := &AppState{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/desync/config", nil)
	w := httptest.NewRecorder()
	state.handleDesyncConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res struct {
		Success bool          `json:"success"`
		Presets []interface{} `json:"presets"`
	}
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !res.Success || len(res.Presets) == 0 {
		t.Fatalf("expected non-empty presets list from desync config")
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
	hasWardogsTunnel := false
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]interface{})
		if !ok {
			continue
		}
		if rule["outbound"] == "hy2-gateway" {
			if portRanges, ok := rule["port_range"].([]interface{}); ok {
				for _, pr := range portRanges {
					if pr == "4000:4500" {
						hasMatchTunnel = true
					}
				}
			}
			if processes, ok := rule["process_name"].([]interface{}); ok {
				for _, proc := range processes {
					if proc == "WardogsClient-Win64-Shipping.exe" {
						hasWardogsTunnel = true
					}
				}
			}
		}
	}

	if !hasMatchTunnel {
		t.Errorf("expected hy2-gateway tunnel rule for UDP 4000:4500")
	}
	if !hasWardogsTunnel {
		t.Errorf("expected hy2-gateway tunnel rule for WardogsClient-Win64-Shipping.exe")
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
	if p.ClientIP != "198.51.100.***" {
		t.Errorf("expected masked IP 198.51.100.***, got %s", p.ClientIP)
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

func TestResizeImage(t *testing.T) {
	// Create dummy 200x100 RGBA image
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			src.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	scaled := resizeImage(src, 128, 128)
	if scaled.Bounds().Dx() != 128 || scaled.Bounds().Dy() != 128 {
		t.Fatalf("expected 128x128 bounds, got %dx%d", scaled.Bounds().Dx(), scaled.Bounds().Dy())
	}
	r, _, _, _ := scaled.At(64, 64).RGBA()
	if r == 0 {
		t.Fatalf("expected non-zero red component in scaled image")
	}
}

func TestHandleStatusSponsorSlots(t *testing.T) {
	state := &AppState{
		cfg: ServerConfig{
			MaxSessions:           61,
			DedicatedSponsorSlots: 10,
		},
		sessions: make(map[string]*SessionInfo),
	}

	// Add 1 free session and 1 sponsor session
	state.sessions["tok1"] = &SessionInfo{IsSponsor: false}
	state.sessions["tok2"] = &SessionInfo{IsSponsor: true}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	state.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var res struct {
		ActiveSessions        int `json:"active_sessions"`
		MaxSessions           int `json:"max_sessions"`
		ActiveFreeSessions    int `json:"active_free_sessions"`
		FreeSlotsLimit        int `json:"free_slots_limit"`
		ActiveSponsorSessions int `json:"active_sponsor_sessions"`
		DedicatedSponsorSlots int `json:"dedicated_sponsor_slots"`
		DedicatedAdminSlots   int `json:"dedicated_admin_slots"`
	}

	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if res.ActiveSessions != 2 {
		t.Errorf("expected active_sessions 2, got %d", res.ActiveSessions)
	}
	if res.MaxSessions != 61 {
		t.Errorf("expected max_sessions 61, got %d", res.MaxSessions)
	}
	if res.ActiveFreeSessions != 1 {
		t.Errorf("expected active_free_sessions 1, got %d", res.ActiveFreeSessions)
	}
	if res.FreeSlotsLimit != 50 {
		t.Errorf("expected free_slots_limit 50, got %d", res.FreeSlotsLimit)
	}
	if res.ActiveSponsorSessions != 1 {
		t.Errorf("expected active_sponsor_sessions 1, got %d", res.ActiveSponsorSessions)
	}
	if res.DedicatedSponsorSlots != 10 {
		t.Errorf("expected dedicated_sponsor_slots 10, got %d", res.DedicatedSponsorSlots)
	}
	if res.DedicatedAdminSlots != 1 {
		t.Errorf("expected dedicated_admin_slots 1, got %d", res.DedicatedAdminSlots)
	}
}

func TestAdminSlotReservation(t *testing.T) {
	if AdminAccountNumber != "5230-6527-2989-4096" {
		t.Fatalf("unexpected admin account number: %s", AdminAccountNumber)
	}
	if MaxActiveSessions != 61 {
		t.Fatalf("expected MaxActiveSessions 61, got %d", MaxActiveSessions)
	}
}

func TestTelemetryBeaconAndRouteMode(t *testing.T) {
	state := &AppState{
		sessions:     make(map[string]*SessionInfo),
		deviceTokens: make(map[string]string),
		rateLimiter:  NewIPRateLimiter(60, time.Minute),
		cfg: ServerConfig{
			ServerIP:    "138.124.103.99",
			MaxSessions: 100,
		},
	}

	// 1. Session request with route_mode
	ts := time.Now().Unix()
	sessReq := SessionRequest{
		DeviceID:      "dev-123",
		AccountNumber: "1111-2222-3333-4444",
		Timestamp:     ts,
		Nonce:         "testnonce",
		Game:          "wardogs",
		RouteMode:     "direct_stockholm",
	}
	bodyBytes, _ := json.Marshal(sessReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	state.handleSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from handleSession, got %d (%s)", w.Code, w.Body.String())
	}

	token := state.deviceTokens["dev-123"]
	sess := state.sessions[token]
	if sess == nil {
		t.Fatalf("expected session to be created")
	}
	if sess.RouteMode != "direct_frankfurt" {
		t.Errorf("expected RouteMode 'direct_frankfurt', got '%s'", sess.RouteMode)
	}

	// 2. Telemetry Beacon update
	beacon := TelemetryBeaconPayload{
		DeviceID:           "dev-123",
		AccountNumber:      "1111-2222-3333-4444",
		AppVersion:         "v2.2.3",
		RouteMode:          "transit",
		Status:             "beacon",
		PingMoscowMs:       24,
		PingStockholmMs:    48,
		InGamePing:         54,
		JitterMs:           3,
		PacketLossPct:      0,
		GameID:             "wardogs",
		ProcessName:        "WardogsClient-Win64-Shipping.exe",
		MatchServer:        "54.115.8.196:4192",
		IsFinalReport:      false,
		SessionDurationSec: 120,
	}
	beaconBytes, _ := json.Marshal(beacon)
	bReq := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry/beacon", bytes.NewReader(beaconBytes))
	bW := httptest.NewRecorder()
	state.handleTelemetryBeacon(bW, bReq)

	if bW.Code != http.StatusOK {
		t.Fatalf("expected 200 from handleTelemetryBeacon, got %d (%s)", bW.Code, bW.Body.String())
	}

	// Verify route mode was updated in session
	if sess.RouteMode != "transit" {
		t.Errorf("expected updated RouteMode 'transit', got '%s'", sess.RouteMode)
	}

	// 3. Prometheus metrics output
	mReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	mReq.RemoteAddr = "127.0.0.1:1234"
	mW := httptest.NewRecorder()
	state.handleMetrics(mW, mReq)

	if mW.Code != http.StatusOK {
		t.Fatalf("expected 200 from handleMetrics, got %d", mW.Code)
	}
	mBody := mW.Body.String()
	if !strings.Contains(mBody, "warlink_sessions_by_route_mode") {
		t.Errorf("expected metrics to contain warlink_sessions_by_route_mode, got:\n%s", mBody)
	}
	if !strings.Contains(mBody, `warlink_sessions_by_route_mode{route_mode="transit"} 1`) {
		t.Errorf("expected metrics to report 1 transit session, got:\n%s", mBody)
	}
}

func TestCommunityGoalsCluster(t *testing.T) {
	state := &AppState{
		cachedDisplayDays:  23,
		cachedRealDaysLeft: 30,
		cachedPrice:        1690,
		cachedVPSDetails: []VPSServerDetail{
			{
				ID:        1,
				Name:      "decisive-amber",
				IP:        "138.124.103.99",
				DaysLeft:  46,
				PriceRub:  385,
				PriceEur:  3.5,
				Status:    "active",
			},
			{
				ID:        2,
				Name:      "minor-orange",
				IP:        "45.12.63.85",
				DaysLeft:  30,
				PriceRub:  528,
				PriceEur:  4.75,
				Status:    "active",
			},
			{
				ID:        3,
				Name:      "mechanical-azure",
				IP:        "85.192.24.254",
				DaysLeft:  30,
				PriceRub:  528,
				PriceEur:  4.75,
				Status:    "active",
			},
		},
	}
	atomic.StoreUint64(&state.metricAezaBalanceRub, 1690)
	atomic.StoreUint64(&state.metricAezaBalanceEurCents, 1300)

	goals := state.getCommunityGoals()
	if !goals.Success {
		t.Fatalf("expected goals.Success to be true")
	}

	// 1. Cluster infrastructure checks (13 EUR / 1690 RUB)
	if goals.Infrastructure.TargetAmountRub != 1690 {
		t.Errorf("expected target 1690 RUB, got %d", goals.Infrastructure.TargetAmountRub)
	}
	if goals.Infrastructure.TargetAmountEur != 13.0 {
		t.Errorf("expected target 13.0 EUR, got %.1f", goals.Infrastructure.TargetAmountEur)
	}
	if goals.Infrastructure.DaysLeft != 23 {
		t.Errorf("expected displayed days 23 (-7 buffer), got %d", goals.Infrastructure.DaysLeft)
	}
	if goals.Infrastructure.RealDaysLeft != 30 {
		t.Errorf("expected real days 30, got %d", goals.Infrastructure.RealDaysLeft)
	}
	if !goals.Infrastructure.IsCovered {
		t.Errorf("expected infrastructure to be covered")
	}
	if len(goals.Infrastructure.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(goals.Infrastructure.Nodes))
	}

	// 2. Expansion Frankfurt is now covered / active in cluster
	if goals.Expansion.Role != "В СТРОЮ" {
		t.Errorf("expected Frankfurt role 'В СТРОЮ', got '%s'", goals.Expansion.Role)
	}
	if !goals.Expansion.IsCovered {
		t.Errorf("expected Frankfurt to be covered in cluster")
	}

	// 3. Special Project BF6 checks (independent, with Steam & Boosty links)
	if len(goals.SpecialProjects) == 0 {
		t.Fatalf("expected at least 1 special project")
	}
	bf6 := goals.SpecialProjects[0]
	if bf6.ID != "bf6" {
		t.Errorf("expected ID 'bf6', got '%s'", bf6.ID)
	}
	if bf6.TargetAmountRub != 1600 {
		t.Errorf("expected discount price 1600 RUB, got %d", bf6.TargetAmountRub)
	}
	if bf6.BasePriceRub != 3200 {
		t.Errorf("expected base price 3200 RUB, got %d", bf6.BasePriceRub)
	}
	if !strings.Contains(bf6.BoostyURL, "832459") {
		t.Errorf("expected Boosty target link, got '%s'", bf6.BoostyURL)
	}
	if !strings.Contains(bf6.SteamURL, "MaksimPaladin") {
		t.Errorf("expected Steam profile link, got '%s'", bf6.SteamURL)
	}
}

func TestGamesCatalog(t *testing.T) {
	state := &AppState{}
	catalog := state.getGamesCatalog()
	if len(catalog) < 4 {
		t.Fatalf("expected at least 4 games in default catalog, got %d", len(catalog))
	}

	foundWardogs := false
	foundBF6 := false
	for _, g := range catalog {
		if g.ID == "wardogs" {
			foundWardogs = true
			if len(g.Processes) == 0 || g.Processes[0] != "WardogsClient-Win64-Shipping.exe" {
				t.Errorf("unexpected wardogs processes: %v", g.Processes)
			}
		}
		if g.ID == "bf6" {
			foundBF6 = true
			if g.Status != "crowdfunding" {
				t.Errorf("expected bf6 status 'crowdfunding', got '%s'", g.Status)
			}
		}
	}
	if !foundWardogs {
		t.Errorf("wardogs not found in catalog")
	}
	if !foundBF6 {
		t.Errorf("bf6 not found in catalog")
	}
}

func TestSupportedGamesRejection(t *testing.T) {
	state := &AppState{
		enableVoting: true,
	}

	testCases := []struct {
		appID int
		title string
	}{
		{1867240, "WARDOGS"},
		{1808500, "ARC Raiders"},
		{2016590, "Dark and Darker"},
	}

	for _, tc := range testCases {
		isSup, name := state.isSupportedSteamGame(tc.appID, tc.title)
		if !isSup {
			t.Errorf("expected appID %d (%s) to be recognized as supported game", tc.appID, tc.title)
		}
		if name == "" {
			t.Errorf("expected non-empty title for appID %d", tc.appID)
		}

		body, _ := json.Marshal(map[string]interface{}{
			"device_id":    "test_dev",
			"steam_app_id": tc.appID,
			"title":        tc.title,
			"icon_url":     "https://example.com/icon.jpg",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/votes", bytes.NewReader(body))
		w := httptest.NewRecorder()
		state.handleVotes(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for supported game %d, got %d", tc.appID, w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		errMsg, _ := resp["error"].(string)
		if !strings.Contains(errMsg, "уже официально поддерживается в WarLink") {
			t.Errorf("expected error message to mention support, got %q", errMsg)
		}
	}
}

func TestFeatureFlagsEvaluation(t *testing.T) {
	// 1. evalRolloutFNV bounds
	if evalRolloutFNV("test_flag", "id1", 0) {
		t.Fatal("0% rollout should always be false")
	}
	if !evalRolloutFNV("test_flag", "id1", 100) {
		t.Fatal("100% rollout should always be true")
	}

	// 2. Determinism
	v1 := evalRolloutFNV("test_flag", "acc-1234-5678", 35)
	for i := 0; i < 20; i++ {
		v2 := evalRolloutFNV("test_flag", "acc-1234-5678", 35)
		if v1 != v2 {
			t.Fatalf("evalRolloutFNV non-deterministic: iter %d got %v, expected %v", i, v2, v1)
		}
	}

	// 3. handleAdminFeatureFlags auth check
	state := &AppState{
		cfg: ServerConfig{DashboardKey: "test_secret_password"},
	}

	reqUnauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/admin/features/flags", nil)
	wUnauthorized := httptest.NewRecorder()
	state.handleAdminFeatureFlags(wUnauthorized, reqUnauthorized)
	if wUnauthorized.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unauthorized admin access, got %d", wUnauthorized.Code)
	}

	// 4. handleClientFeatures with empty DB returns empty list and HTTP 200
	reqClient := httptest.NewRequest(http.MethodGet, "/api/v1/client/features?account_number=1234-5678-9012-3456", nil)
	wClient := httptest.NewRecorder()
	state.handleClientFeatures(wClient, reqClient)
	if wClient.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wClient.Code)
	}

	var resp struct {
		Version  int                      `json:"version"`
		Features []map[string]interface{} `json:"features"`
	}
	if err := json.Unmarshal(wClient.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode client features response: %v", err)
	}
	if resp.Version != 2 {
		t.Fatalf("expected version 2, got %d", resp.Version)
	}
}
