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
	// Verify across ALL builtin presets that Google and discord.media are protected from RST-inducing parameters
	for _, p := range BuiltinPresets {
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



