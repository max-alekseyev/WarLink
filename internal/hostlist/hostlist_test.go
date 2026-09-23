package hostlist

import (
	"strings"
	"testing"
)

func TestHostlistManager(t *testing.T) {
	mgr := NewManager(nil)
	hosts := mgr.GetAllHosts()
	if len(hosts) == 0 {
		t.Fatal("expected non-empty default hostlist")
	}

	// Verify all 10 key services are represented
	foundYT := false
	foundDiscord := false
	foundTwitchExt := false
	foundTelegram := false
	foundTwitter := false
	foundMeta := false
	foundDNS := false
	foundWA := false
	foundViber := false

	for _, h := range hosts {
		switch {
		case strings.Contains(h, "googlevideo"):
			foundYT = true
		case strings.Contains(h, "discord"):
			foundDiscord = true
		case strings.Contains(h, "7tv"):
			foundTwitchExt = true
		case strings.Contains(h, "telegram"):
			foundTelegram = true
		case strings.Contains(h, "twitter") || strings.Contains(h, "x.com"):
			foundTwitter = true
		case strings.Contains(h, "instagram") || strings.Contains(h, "facebook"):
			foundMeta = true
		case strings.Contains(h, "cloudflare-dns"):
			foundDNS = true
		case strings.Contains(h, "whatsapp"):
			foundWA = true
		case strings.Contains(h, "viber"):
			foundViber = true
		}
	}

	if !foundYT || !foundDiscord || !foundTwitchExt || !foundTelegram || !foundTwitter || !foundMeta || !foundDNS || !foundWA || !foundViber {
		t.Fatalf("missing one of the services: yt=%v dc=%v twitch=%v tg=%v tw=%v meta=%v dns=%v wa=%v viber=%v",
			foundYT, foundDiscord, foundTwitchExt, foundTelegram, foundTwitter, foundMeta, foundDNS, foundWA, foundViber)
	}

	if len(TelegramIPRanges) == 0 {
		t.Fatal("expected Telegram DC IP ranges to be populated")
	}
}

func TestGetGeneralHostsExclusions(t *testing.T) {
	mgr := NewManager(nil)
	genHosts := mgr.GetGeneralHosts()
	if len(genHosts) == 0 {
		t.Fatal("expected non-empty general hostlist")
	}

	for _, h := range genHosts {
		if strings.Contains(h, "youtube") || strings.Contains(h, "googlevideo") {
			t.Errorf("general hosts must not contain YouTube domains: found %s", h)
		}
		if strings.Contains(h, "instagram") || strings.Contains(h, "facebook") || strings.Contains(h, "threads") {
			t.Errorf("general hosts must not contain Meta domains: found %s", h)
		}
		if strings.Contains(h, "twitter") || h == "x.com" || strings.Contains(h, "twimg") {
			t.Errorf("general hosts must not contain Twitter domains: found %s", h)
		}
		if strings.Contains(h, "telegram") || strings.Contains(h, "t.me") {
			t.Errorf("general hosts must not contain Telegram domains: found %s", h)
		}
		if strings.Contains(h, "whatsapp") {
			t.Errorf("general hosts must not contain WhatsApp domains: found %s", h)
		}
	}
}
