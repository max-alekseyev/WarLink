package desync

import (
	"strings"
	"testing"
)

func TestBuiltinPresets(t *testing.T) {
	names := GetAvailablePresetNames()
	if len(names) == 0 {
		t.Fatalf("expected non-empty preset names")
	}

	p := GetPreset("general (ALT9)")
	if p == nil {
		t.Fatalf("preset 'general (ALT9)' not found")
	}

	args := p.BuildArgs("C:\\WarLink\\warlink_core")
	if len(args) == 0 {
		t.Fatalf("expected non-empty arguments for ALT9")
	}

	hasLists := false
	for _, a := range args {
		if strings.Contains(a, "C:\\WarLink\\warlink_core\\lists\\") {
			hasLists = true
			break
		}
	}
	if !hasLists {
		t.Errorf("expected expanded lists path in args, got: %v", args)
	}
}

func TestBuildFilteredArgs(t *testing.T) {
	p := GetPreset("general (ALT9)")
	if p == nil {
		t.Fatalf("preset 'general (ALT9)' not found")
	}

	// 1. When freeInternet == false: only game UDP ports and WARP QUIC desync, NO web hostlists for google
	gameArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", false)
	joinedGame := strings.Join(gameArgs, " ")
	if strings.Contains(joinedGame, "list-google.txt") {
		t.Errorf("expected list-google.txt to be omitted when freeInternet is false")
	}
	if !strings.Contains(joinedGame, "50000-50100") {
		t.Errorf("expected game UDP port 50000-50100 in game args")
	}
	if !strings.Contains(joinedGame, "fake_default_quic") {
		t.Errorf("expected quic fake for WARP in game args")
	}
	if !strings.Contains(joinedGame, "--out-range=-d3") {
		t.Errorf("expected --out-range=-d3 for Discord UDP in game args")
	}

	// 2. When freeInternet == true: all web hostlists and rules are included
	fullArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", true)
	joinedFull := strings.Join(fullArgs, " ")
	if !strings.Contains(joinedFull, "list-google.txt") {
		t.Errorf("expected list-google.txt to be included when freeInternet is true")
	}
	if !strings.Contains(joinedFull, "list-general.txt") {
		t.Errorf("expected list-general.txt to be included when freeInternet is true")
	}

	// 3. When freeInternet == false: TCP hook must be omitted, no artifact port 12, exclude-user files present
	if strings.Contains(joinedGame, "--wf-tcp-out") {
		t.Errorf("expected --wf-tcp-out to be omitted when freeInternet is false")
	}
	if strings.Contains(joinedGame, ",12") {
		t.Errorf("expected artifact port 12 to be removed")
	}
	if !strings.Contains(joinedGame, "--wf-udp-out=443,19294-19344,50000-50100") {
		t.Errorf("expected --wf-udp-out=443,19294-19344,50000-50100 in game args")
	}
	if !strings.Contains(joinedGame, "list-exclude-user.txt") {
		t.Errorf("expected list-exclude-user.txt to be present in game args")
	}
	if !strings.Contains(joinedGame, "ipset-exclude-user.txt") {
		t.Errorf("expected ipset-exclude-user.txt to be present in game args")
	}
}

func TestTenTspuStrategies(t *testing.T) {
	if len(BuiltinPresets) < 10 {
		t.Fatalf("expected at least 10 TSPU strategies, got %d", len(BuiltinPresets))
	}

	for _, p := range BuiltinPresets {
		t.Run(p.Name, func(t *testing.T) {
			args := p.BuildArgs("C:\\WarLink\\warlink_core")
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "50000-50100") {
				t.Errorf("preset %s missing UDP voice port range 50000-50100", p.Name)
			}
			if !strings.Contains(joined, "--out-range=-d3") {
				t.Errorf("preset %s missing --out-range=-d3 Discord UDP cutoff", p.Name)
			}
		})
	}
}

func TestCircularAdaptivePreset(t *testing.T) {
	p := GetPreset("Автокалибровка (Circular Adaptive)")
	if p == nil {
		t.Fatalf("preset 'Автокалибровка (Circular Adaptive)' not found")
	}

	args := p.BuildArgs("C:\\WarLink\\warlink_core")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "circular:fails=3") {
		t.Errorf("expected circular orchestrator in args, got: %s", joined)
	}
	if !strings.Contains(joined, "strategy=1") || !strings.Contains(joined, "strategy=2") || !strings.Contains(joined, "strategy=3:final") {
		t.Errorf("expected strategy steps in circular preset, got: %s", joined)
	}
	if !strings.Contains(joined, "ip_autottl=-2,3-20") {
		t.Errorf("expected ip_autottl in circular preset, got: %s", joined)
	}
	if !strings.Contains(joined, "--hostlist-auto=") {
		t.Errorf("expected hostlist-auto in circular preset, got: %s", joined)
	}
	if !strings.Contains(joined, "--wf-tcp-in=80,443") {
		t.Errorf("expected --wf-tcp-in=80,443 in header, got: %s", joined)
	}
}

func TestGeneralAlt3Preset(t *testing.T) {
	p := GetPreset("general (ALT3)")
	if p == nil {
		t.Fatalf("preset 'general (ALT3)' not found")
	}

	joinedArgs := strings.Join(p.Args, " ")
	if strings.Contains(joinedArgs, "fakedsplit") {
		t.Errorf("expected fakedsplit to be removed from general (ALT3)")
	}
	if !strings.Contains(joinedArgs, "--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=6") {
		t.Errorf("expected fake desync action in general (ALT3)")
	}
	if !strings.Contains(joinedArgs, "--lua-desync=multisplit:pos=midsld") {
		t.Errorf("expected multisplit action in general (ALT3)")
	}
}

func TestGoogleAndDiscordRuleSafety(t *testing.T) {
	allPresets := append([]Preset{}, BuiltinPresets...)
	allPresets = append(allPresets, legacyPresets...)

	// Verify across ALL presets that Google and discord.media are protected from RST-inducing parameters
	for _, p := range allPresets {
		t.Run(p.Name, func(t *testing.T) {
			fullArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", true)
			var googleSection []string
			var discordMediaSection []string
			var currentSection *[]string

			for _, a := range fullArgs {
				if a == "--new" {
					currentSection = nil
					continue
				}
				if strings.HasPrefix(a, "--hostlist=") && strings.Contains(a, "list-google.txt") {
					currentSection = &googleSection
				} else if strings.HasPrefix(a, "--hostlist-domains=") && strings.Contains(a, "discord.media") {
					currentSection = &discordMediaSection
				}
				if currentSection != nil {
					*currentSection = append(*currentSection, a)
				}
			}

			// Google rule must never use tcp_md5 or repeats=11
			googleJoined := strings.Join(googleSection, " ")
			if strings.Contains(googleJoined, "tcp_md5") {
				t.Errorf("preset %s: Google rule must NOT use tcp_md5 (causes RST)", p.Name)
			}
			if strings.Contains(googleJoined, "repeats=11") {
				t.Errorf("preset %s: Google rule must NOT use repeats=11", p.Name)
			}
			if strings.Contains(googleJoined, "sni=www.google.com") {
				t.Errorf("preset %s: Google rule must NOT use sni=www.google.com", p.Name)
			}

			// discord.media must never use tcp_md5
			dmJoined := strings.Join(discordMediaSection, " ")
			if strings.Contains(dmJoined, "tcp_md5") {
				t.Errorf("preset %s: discord.media must NOT use tcp_md5 (causes RST from Cloudflare)", p.Name)
			}

			// General rule must exclude list-google.txt
			joinedAll := strings.Join(fullArgs, " ")
			if !strings.Contains(joinedAll, "list-exclude=C:\\WarLink\\warlink_core\\lists\\list-google.txt") &&
				!strings.Contains(joinedAll, "list-google.txt") {
				t.Errorf("preset %s: missing google exclusion from general rule", p.Name)
			}
		})
	}
}

func TestDiscordVoiceWebRTCSafety(t *testing.T) {
	p := GetPreset("Автокалибровка (Circular Adaptive)")
	if p == nil {
		t.Fatalf("preset not found")
	}

	fullArgs := p.BuildFilteredArgs("C:\\WarLink\\warlink_core", true)
	joinedFull := strings.Join(fullArgs, " ")

	if !strings.Contains(joinedFull, "--ctrack-timeouts=60:300:60:3600") {
		t.Errorf("expected 1-hour conntrack timeout for long voice sessions")
	}
	if !strings.Contains(joinedFull, "ACTIVE_DISCORD_UDP.bin") {
		t.Errorf("expected fake_discord to use ACTIVE_DISCORD_UDP.bin")
	}
	if !strings.Contains(joinedFull, "--filter-l7=discord,stun") && !strings.Contains(joinedFull, "--filter-l7=stun,discord") {
		t.Errorf("expected L7 filter for discord,stun to protect SRTP voice audio")
	}
	if strings.Contains(joinedFull, "--payload=all") {
		t.Errorf("payload=all must NOT be used for voice ports as it corrupts SRTP audio")
	}
	if !strings.Contains(joinedFull, "repeats=6") {
		t.Errorf("expected repeats=6 on Discord UDP discovery for reliable DPI bypass")
	}
}



