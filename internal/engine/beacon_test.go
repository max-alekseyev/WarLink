package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"warlink/internal/config"
	"warlink/internal/pingmeter"
)

func TestAutoBeacon_Lifecycle(t *testing.T) {
	eng := &Engine{
		cfg:        config.Load(),
		telemetry:  NewTelemetryMonitor(),
		pingMeter:  pingmeter.New(),
	}
	beacon := NewAutoBeacon(eng, nil)
	beacon.interval = 100 * time.Millisecond

	beacon.Start("wardogs", "WardogsClient-Win64-Shipping.exe")
	if !beacon.isRunning {
		t.Errorf("expected beacon to be running")
	}

	beacon.SetGame("wardogs", "WardogsClient.exe")
	if beacon.currentProcessName != "WardogsClient.exe" {
		t.Errorf("expected updated proc name")
	}

	time.Sleep(50 * time.Millisecond)
	beacon.Stop()

	if beacon.isRunning {
		t.Errorf("expected beacon to be stopped")
	}
}

func TestAutoBeacon_SendBeacon(t *testing.T) {
	receivedBeacon := false
	var receivedPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/telemetry/beacon" {
			receivedBeacon = true
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	eng := &Engine{
		cfg:        config.Load(),
		telemetry:  NewTelemetryMonitor(),
		pingMeter:  pingmeter.New(),
	}
	beacon := NewAutoBeacon(eng, nil)
	beacon.startTime = time.Now().Add(-5 * time.Minute)
	beacon.currentGameID = "wardogs"
	beacon.currentProcessName = "WardogsClient-Win64-Shipping.exe"

	// Mock server target
	t.Setenv("WARLINK_SERVER_API", server.URL)

	beacon.SendMatchSummary()

	if !receivedBeacon {
		t.Fatalf("expected server to receive beacon")
	}

	if receivedPayload["status"] != "match_summary" {
		t.Errorf("expected status 'match_summary', got %v", receivedPayload["status"])
	}
	if receivedPayload["is_final_report"] != true {
		t.Errorf("expected is_final_report true")
	}
	if receivedPayload["game_id"] != "wardogs" {
		t.Errorf("expected game_id 'wardogs', got %v", receivedPayload["game_id"])
	}
}
