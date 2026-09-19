package singbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateConfig(t *testing.T) {
	targets := []string{"WardogsClient-Win64-Shipping.exe"}
	data, err := GenerateConfig(targets, true, "test-session-token")
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal generated json: %v", err)
	}

	if len(parsed.Inbounds) == 0 || parsed.Inbounds[0].Type != "tun" {
		t.Errorf("Expected inbound tun, got: %v", parsed.Inbounds)
	}

	if parsed.Inbounds[0].InterfaceName != "WarLink-Tun" {
		t.Errorf("Expected interface WarLink-Tun, got: %s", parsed.Inbounds[0].InterfaceName)
	}

	if !parsed.Route.FindProcess {
		t.Errorf("Expected find_process to be true")
	}

	hasGameRule := false
	hasIPCIDRRule := false
	hasDomainRule := false
	hasDirectRule := false
	for _, r := range parsed.Route.Rules {
		if r.Outbound == "hy2-stockholm" {
			for _, p := range r.ProcessName {
				if p == "WardogsClient-Win64-Shipping.exe" {
					hasGameRule = true
				}
			}
			for _, ip := range r.IPCIDR {
				if ip == "157.240.0.0/16" {
					hasIPCIDRRule = true
				}
			}
			for _, d := range r.DomainSuffix {
				if d == "web.telegram.org" {
					hasDomainRule = true
				}
			}
		}
		if r.Outbound == "direct" {
			hasDirectRule = true
		}
	}

	if !hasGameRule {
		t.Errorf("Expected route rule for WardogsClient-Win64-Shipping.exe")
	}
	if !hasIPCIDRRule {
		t.Errorf("Expected route rule for WhatsApp IP CIDR 157.240.0.0/16")
	}
	if !hasDomainRule {
		t.Errorf("Expected route rule for web.telegram.org")
	}
	if !hasDirectRule {
		t.Errorf("Expected default direct route rule")
	}

	hasHysteria2 := false
	for _, o := range parsed.Outbounds {
		if o.Type == "hysteria2" && o.Tag == "hy2-stockholm" && o.Server == DefaultServerIP {
			hasHysteria2 = true
		}
	}
	if !hasHysteria2 {
		t.Errorf("Expected hy2-stockholm Hysteria 2 outbound")
	}

	if parsed.Experimental == nil || parsed.Experimental.CacheFile == nil || !parsed.Experimental.CacheFile.Enabled {
		t.Errorf("Expected experimental cache_file enabled")
	}

	// Test Game Only Mode (includeWebServices = false)
	gameOnlyData, err := GenerateConfig(targets, false, "test-session-token")
	if err != nil {
		t.Fatalf("GenerateConfig (GameOnly) failed: %v", err)
	}
	var gameParsed Config
	if err := json.Unmarshal(gameOnlyData, &gameParsed); err != nil {
		t.Fatalf("Failed to unmarshal game only json: %v", err)
	}
	for _, r := range gameParsed.Route.Rules {
		for _, d := range r.DomainSuffix {
			if d == "web.telegram.org" {
				t.Errorf("Did not expect web.telegram.org in GameOnly mode")
			}
		}
	}
}

func TestValidateConfigWithSingBoxBinary(t *testing.T) {
	exePath := filepath.Join("..", "..", "warlink_core", "singbox", "sing-box.exe")
	if _, err := os.Stat(exePath); err != nil {
		t.Skip("sing-box.exe not present in warlink_core, skipping binary validation")
	}

	targets := []string{"WardogsClient-Win64-Shipping.exe"}
	for _, includeWeb := range []bool{true, false} {
		data, err := GenerateConfig(targets, includeWeb, "test-token")
		if err != nil {
			t.Fatalf("GenerateConfig (includeWeb=%v) failed: %v", includeWeb, err)
		}

		tmpFile := filepath.Join(t.TempDir(), "test_config.json")
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			t.Fatalf("Failed to write temp config: %v", err)
		}

		cmd := exec.Command(exePath, "check", "-c", tmpFile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("sing-box check (includeWeb=%v) failed: %v\nOutput:\n%s", includeWeb, err, string(out))
		}
	}

	// Test with profiles that include comma-annotated IPs (e.g. Vivox subnets)
	complexProfiles := []Profile{
		{
			ID:        "wardogs",
			Name:      "WARDOGS",
			Processes: []string{"WardogsClient-Win64-Shipping.exe"},
			Domains:   []string{"bulkhead.net", "vivox.com"},
			IPs: []string{
				"85.236.96.0/19, tcp/443",
				"85.236.96.0/19, udp/54000-55000",
				"108.181.0.0/16, tcp/443",
				"195.154.239.0/24",
				"45.43.142.1",
			},
		},
	}
	profData, err := GenerateConfigFromProfiles(complexProfiles, []string{"WardogsClient-Win64-Shipping.exe"}, true, "test-token")
	if err != nil {
		t.Fatalf("GenerateConfigFromProfiles failed: %v", err)
	}
	tmpFileProf := filepath.Join(t.TempDir(), "test_prof_config.json")
	if err := os.WriteFile(tmpFileProf, profData, 0644); err != nil {
		t.Fatalf("Failed to write temp prof config: %v", err)
	}
	cmdProf := exec.Command(exePath, "check", "-c", tmpFileProf)
	outProf, errProf := cmdProf.CombinedOutput()
	if errProf != nil {
		t.Fatalf("sing-box check with complex profiles failed: %v\nOutput:\n%s", errProf, string(outProf))
	}
}

func TestLiveConfigFromStockholmWithSingBoxBinary(t *testing.T) {
	exePath := filepath.Join("..", "..", "warlink_core", "singbox", "sing-box.exe")
	if _, err := os.Stat(exePath); err != nil {
		t.Skip("sing-box.exe not found")
	}
	if GetServerAPI() == "" {
		t.Skip("Skipping live profile check: server API not configured")
	}
	profs, err := FetchProfiles()
	if err != nil {
		t.Skipf("Stockholm gateway not reachable: %v", err)
	}
	for _, includeWeb := range []bool{true, false} {
		for _, targets := range [][]string{{"WardogsClient-Win64-Shipping.exe"}, {}} {
			cfgBytes, err := GenerateConfigFromProfiles(profs, targets, includeWeb, "wl_tok_live_test")
			if err != nil {
				t.Fatalf("GenerateConfigFromProfiles failed: %v", err)
			}
			tmpFile := filepath.Join(t.TempDir(), "live_test_config.json")
			_ = os.WriteFile(tmpFile, cfgBytes, 0644)
			cmd := exec.Command(exePath, "check", "-c", tmpFile)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("sing-box check failed for live Stockholm config: %v\nOutput:\n%s", err, string(out))
			}
		}
	}
}

func TestGenerateConfigHysteria2Outbound(t *testing.T) {
	data, err := GenerateConfig([]string{"WardogsClient-Win64-Shipping.exe"}, false, "token123")
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal generated json: %v", err)
	}
	if parsed.Outbounds[0].Type != "hysteria2" || parsed.Outbounds[0].Tag != "hy2-stockholm" {
		t.Errorf("Expected hysteria2 outbound with tag hy2-stockholm, got %+v", parsed.Outbounds[0])
	}
}

func TestLiveStockholmGateway(t *testing.T) {
	if GetServerAPI() == "" {
		t.Skip("Skipping live gateway test: server API not configured")
	}
	st, err := GetServerGatewayStatus()
	if err != nil {
		t.Fatalf("Failed to get gateway status: %v", err)
	}
	t.Logf("Stockholm Gateway: %+v", st)
	if st.Status != "online" || st.MaxSessions != 100 {
		t.Errorf("Unexpected gateway status: %+v", st)
	}

	tok, err := AcquireSession()
	if err != nil {
		t.Fatalf("Failed to acquire session: %v", err)
	}
	defer ReleaseSession()
	t.Logf("Acquired session token: %s", tok)
	if !strings.HasPrefix(tok, "wl_tok_") {
		t.Errorf("Unexpected token format: %s", tok)
	}
}

func TestLiveSingBoxStartStop(t *testing.T) {
	if GetServerAPI() == "" {
		t.Skip("Skipping live singbox test: server API not configured")
	}
	exePath := filepath.Join("..", "..", "warlink_core", "singbox", "sing-box.exe")
	if _, err := os.Stat(exePath); err != nil {
		t.Skip("sing-box.exe not found")
	}

	coreDir := filepath.Join("..", "..", "warlink_core")
	mgr := NewManager(coreDir)

	err := mgr.Start([]string{"WardogsClient-Win64-Shipping.exe"}, true, func(msg string) {
		t.Log(msg)
	})
	if err != nil {
		t.Fatalf("mgr.Start failed: %v", err)
	}

	time.Sleep(1 * time.Second)
	if !mgr.IsRunning() {
		t.Errorf("Expected mgr to be running")
	}

	_ = mgr.Stop()
	time.Sleep(300 * time.Millisecond)
	if mgr.IsRunning() {
		t.Errorf("Expected mgr to be stopped")
	}
}
