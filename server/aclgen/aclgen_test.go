package aclgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidProfile(t *testing.T) {
	tempDir := t.TempDir()
	content := `# Test game
name: MyGame
process: game.exe
process: launcher.exe
domain: mygame.com
domain: api.mygame.com
ip: 1.2.3.4
ip: 5.6.7.0/24
udp: 5000-5100
`
	filePath := filepath.Join(tempDir, "mygame-hosts.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	profiles, err := LoadProfiles(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.Name != "MyGame" {
		t.Errorf("expected MyGame, got %s", p.Name)
	}
	if len(p.Processes) != 2 || p.Processes[0] != "game.exe" {
		t.Errorf("unexpected processes: %v", p.Processes)
	}
	if len(p.Domains) != 2 {
		t.Errorf("unexpected domains: %v", p.Domains)
	}
	if len(p.IPs) != 2 {
		t.Errorf("unexpected ips: %v", p.IPs)
	}
	if len(p.UDPRanges) != 1 || p.UDPRanges[0] != "5000-5100" {
		t.Errorf("unexpected udp ranges: %v", p.UDPRanges)
	}

	acl, err := GenerateACL(profiles)
	if err != nil {
		t.Fatalf("failed to generate ACL: %v", err)
	}

	if !strings.Contains(acl, "reject(all, */6881-6889)") {
		t.Errorf("missing base bittorrent blocks")
	}
	if !strings.Contains(acl, "direct(suffix:mygame.com)") {
		t.Errorf("missing direct suffix rule")
	}
	if !strings.Contains(acl, "direct(1.2.3.4, tcp/443)") {
		t.Errorf("missing direct IP rule")
	}
	if !strings.Contains(acl, "direct(5.6.7.0/24)") {
		t.Errorf("missing direct CIDR rule")
	}
	if !strings.Contains(acl, "direct(all, udp/5000-5100)") {
		t.Errorf("missing direct UDP rule")
	}
	if !strings.HasSuffix(strings.TrimSpace(acl), "reject(all)") {
		t.Errorf("ACL must strictly end with reject(all)")
	}
}

func TestRejectWildcard(t *testing.T) {
	tempDir := t.TempDir()
	content := `name: BadGame
domain: all
`
	filePath := filepath.Join(tempDir, "bad-hosts.txt")
	_ = os.WriteFile(filePath, []byte(content), 0644)

	_, err := LoadProfiles(tempDir)
	if err == nil {
		t.Errorf("expected error for 'all' wildcard, got nil")
	}
}

func TestRejectTooWideUDPRange(t *testing.T) {
	tempDir := t.TempDir()
	content := `name: BadGame
udp: 1000-10000
`
	filePath := filepath.Join(tempDir, "bad-hosts.txt")
	_ = os.WriteFile(filePath, []byte(content), 0644)

	_, err := LoadProfiles(tempDir)
	if err == nil {
		t.Errorf("expected error for UDP range > 1500 ports, got nil")
	}
}

func TestRejectPrivateIP(t *testing.T) {
	tempDir := t.TempDir()
	content := `name: BadGame
ip: 192.168.1.1
`
	filePath := filepath.Join(tempDir, "bad-hosts.txt")
	_ = os.WriteFile(filePath, []byte(content), 0644)

	_, err := LoadProfiles(tempDir)
	if err == nil {
		t.Errorf("expected error for private IP 192.168.1.1, got nil")
	}
}

func TestRejectGenericPorts(t *testing.T) {
	tempDir := t.TempDir()
	content := `name: BadGame
ip: 443
`
	filePath := filepath.Join(tempDir, "bad-hosts.txt")
	_ = os.WriteFile(filePath, []byte(content), 0644)

	_, err := LoadProfiles(tempDir)
	if err == nil {
		t.Errorf("expected error for generic port 443 without IP, got nil")
	}
}

func TestGamesReferenceFiles(t *testing.T) {
	profiles, err := LoadProfiles("../../games")
	if err != nil {
		t.Fatalf("failed to load reference games profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles (wardogs, socials), got %d", len(profiles))
	}
	acl, err := GenerateACL(profiles)
	if err != nil {
		t.Fatalf("failed to generate ACL from reference profiles: %v", err)
	}
	if !strings.Contains(acl, "reject(127.0.0.0/8)") {
		t.Errorf("missing reject(127.0.0.0/8)")
	}
	// Verify 127.0.0.0/8 is before 10.0.0.0/8
	idx127 := strings.Index(acl, "reject(127.0.0.0/8)")
	idx10 := strings.Index(acl, "reject(10.0.0.0/8)")
	if idx127 == -1 || idx10 == -1 || idx127 > idx10 {
		t.Errorf("reject(127.0.0.0/8) must be the first private reject rule before 10.0.0.0/8")
	}
	if !strings.Contains(acl, "reject(all, udp/123)") {
		t.Errorf("missing reject(all, udp/123)")
	}
	if strings.Contains(acl, "direct(all, udp/123)") {
		t.Errorf("direct(all, udp/123) must be removed")
	}
	if !strings.Contains(acl, "direct(suffix:wardogs.com)") {
		t.Errorf("missing wardogs in generated ACL")
	}
	if strings.Contains(acl, "direct(suffix:discord.com)") {
		t.Errorf("discord must be removed from gateway ACL")
	}
	if !strings.Contains(acl, "direct(suffix:telegram.org)") {
		t.Errorf("missing telegram in generated ACL")
	}
	if !strings.Contains(acl, "direct(85.236.96.0/19, tcp/443)") {
		t.Errorf("missing Vivox tcp/443 rule")
	}
	if !strings.Contains(acl, "direct(85.236.96.0/19, udp/54000-55000)") {
		t.Errorf("missing Vivox udp/54000-55000 rule")
	}
	if !strings.HasSuffix(strings.TrimSpace(acl), "reject(all)") {
		t.Errorf("generated ACL must end with reject(all)")
	}
}

func TestRejectTotalUDPPortsLimit(t *testing.T) {
	profiles := []Profile{
		{
			ID:        "game1",
			Name:      "Game 1",
			UDPRanges: []string{"1000-2400"}, // 1401 ports
		},
		{
			ID:        "game2",
			Name:      "Game 2",
			UDPRanges: []string{"3000-4400"}, // 1401 ports
		},
		{
			ID:        "game3",
			Name:      "Game 3",
			UDPRanges: []string{"5000-5500"}, // 501 ports (total 3303 > 3000)
		},
	}
	_, err := GenerateACL(profiles)
	if err == nil {
		t.Errorf("expected error when total UDP ports exceed 3000, got nil")
	}
}

func TestUDPPortRangeEdgeCases(t *testing.T) {
	// 1. Inverted range
	_, err := parseUDPRangePorts("6000-5000")
	if err == nil || !strings.Contains(err.Error(), "inverted") {
		t.Errorf("expected inverted range error, got %v", err)
	}

	// 2. Non-numeric range
	_, err = parseUDPRangePorts("5000-xyz")
	if err == nil {
		t.Errorf("expected error for non-numeric range, got nil")
	}

	// 3. Port 53 inclusion in dynamic range
	_, err = parseUDPRangePorts("50-60")
	if err == nil || !strings.Contains(err.Error(), "53") {
		t.Errorf("expected error rejecting port 53 in range, got %v", err)
	}

	// 4. Overlapping ranges should deduplicate
	p := []Profile{
		{
			ID:        "g1",
			UDPRanges: []string{"50000-50050", "50020-50070"}, // 51 + 51, overlap 31 -> 71 unique
		},
	}
	acl, err := GenerateACL(p)
	if err != nil {
		t.Fatalf("GenerateACL failed on overlapping ranges: %v", err)
	}
	if !strings.Contains(acl, "direct(all, udp/50000-50050)") || !strings.Contains(acl, "direct(all, udp/50020-50070)") {
		t.Errorf("expected both rules in generated output")
	}
}

func TestUDPExactBoundaryLimit(t *testing.T) {
	// Exactly 3000 ports: 1000-2499 (1500 ports) + 3000-4499 (1500 ports) = 3000 unique ports -> MUST PASS
	p3000 := []Profile{
		{
			ID:        "p1",
			UDPRanges: []string{"1000-2499", "3000-4499"},
		},
	}
	_, err := GenerateACL(p3000)
	if err != nil {
		t.Errorf("expected exactly 3000 unique ports to PASS, got: %v", err)
	}

	// Exactly 3001 ports: + single port 5000 -> MUST FAIL
	p3001 := []Profile{
		{
			ID:        "p1",
			UDPRanges: []string{"1000-2499", "3000-4499", "5000"},
		},
	}
	_, err = GenerateACL(p3001)
	if err == nil {
		t.Errorf("expected 3001 unique ports to FAIL, but passed")
	}
}

func TestACLRulesEqual(t *testing.T) {
	acl1 := "# Header\n# Generated at: 2026-09-19T10:00:00Z\nreject(127.0.0.0/8)\nreject(all)\n"
	acl2 := "# Header\n# Generated at: 2026-09-19T11:00:00Z\nreject(127.0.0.0/8)\nreject(all)\n"
	acl3 := "# Header\n# Generated at: 2026-09-19T10:00:00Z\nreject(10.0.0.0/8)\nreject(all)\n"

	if !ACLRulesEqual(acl1, acl2) {
		t.Errorf("expected acl1 and acl2 to be equal despite timestamp difference")
	}
	if ACLRulesEqual(acl1, acl3) {
		t.Errorf("expected acl1 and acl3 to NOT be equal")
	}
}
