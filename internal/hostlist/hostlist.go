package hostlist

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"warlink/internal/deps"
)

// Default embedded domain lists for the 10 services
var DefaultDomains = map[string][]string{
	"youtube": {
		"youtube.com",
		"googlevideo.com",
		"ytimg.com",
		"ggpht.com",
		"youtu.be",
		"yt.be",
		"wide-youtube.l.google.com",
		"youtubei.googleapis.com",
		"video.google.com",
	},
	"discord": {
		"discord.com",
		"discord.gg",
		"discordapp.com",
		"discordapp.net",
		"discord.media",
		"gateway.discord.gg",
		"status.discord.com",
		"dis.gd",
	},
	"twitch_extensions": {
		"7tv.app",
		"7tv.io",
		"api.7tv.app",
		"cdn.7tv.app",
		"betterttv.net",
		"api.betterttv.net",
		"cdn.betterttv.net",
		"frankerfacez.com",
		"api.frankerfacez.com",
		"cdn.frankerfacez.com",
	},
	"twitter_x": {
		"x.com",
		"twitter.com",
		"t.co",
		"twimg.com",
		"pbs.twimg.com",
		"abs.twimg.com",
		"api.twitter.com",
		"api.x.com",
	},
	"meta": {
		"instagram.com",
		"cdninstagram.com",
		"facebook.com",
		"fbcdn.net",
		"threads.net",
		"meta.com",
		"messenger.com",
		"fbsbx.com",
	},
	"dns_cloudflare": {
		"cloudflare-dns.com",
		"one.one.one.one",
		"cloudflareclient.com",
		"api.cloudflareclient.com",
		"consumer-masque-proxy.cloudflareclient.com",
		"dns.google",
		"dns.quad9.net",
		"dns.adguard-dns.com",
		"doh.opendns.com",
	},
	"telegram": {
		"t.me",
		"telegram.org",
		"web.telegram.org",
		"telesco.pe",
		"tdesktop.com",
		"stel.com",
		"venus.web.telegram.org",
		"pluto.web.telegram.org",
		"flora.web.telegram.org",
		"aurora.web.telegram.org",
		"vesta.web.telegram.org",
	},
	"whatsapp": {
		"whatsapp.com",
		"whatsapp.net",
		"web.whatsapp.com",
		"wa.me",
		"whatsapp.org",
	},
	"viber": {
		"viber.com",
		"api.viber.com",
		"media.viber.com",
		"download.viber.com",
		"share.viber.com",
	},
}

// Telegram DC direct IP ranges (AS44907 / AS62041)
var TelegramIPRanges = []string{
	"149.154.160.0/20",
	"91.108.4.0/22",
	"91.108.8.0/22",
	"91.108.12.0/22",
	"91.108.16.0/22",
	"91.108.20.0/22",
	"91.108.56.0/22",
	"91.105.192.0/23",
}

type HostCache struct {
	LastUpdated int64               `json:"last_updated"`
	Categories  map[string][]string `json:"categories"`
}

type Manager struct {
	mu         sync.RWMutex
	cacheFile  string
	categories map[string][]string
	allHosts   []string
	logFn      func(string)
}

func NewManager(logFn func(string)) *Manager {
	cachePath := filepath.Join(deps.GetCoreDir(), "hosts_cache.json")
	m := &Manager{
		cacheFile:  cachePath,
		categories: make(map[string][]string),
		logFn:      logFn,
	}
	m.loadInitial()
	return m
}

func (m *Manager) log(msg string) {
	if m.logFn != nil {
		m.logFn(msg)
	}
}

func (m *Manager) loadInitial() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Copy embedded defaults
	for cat, list := range DefaultDomains {
		copied := make([]string, len(list))
		copy(copied, list)
		m.categories[cat] = copied
	}

	// 2. Try loading cached file from disk if present
	if data, err := os.ReadFile(m.cacheFile); err == nil {
		var cache HostCache
		if err := json.Unmarshal(data, &cache); err == nil && len(cache.Categories) > 0 {
			for cat, list := range cache.Categories {
				if len(list) > 0 {
					m.categories[cat] = list
				}
			}
		}
	}

	m.rebuildAllHostsLocked()
}

func (m *Manager) rebuildAllHostsLocked() {
	hostSet := make(map[string]struct{})
	for _, list := range m.categories {
		for _, h := range list {
			h = strings.ToLower(strings.TrimSpace(h))
			if h != "" && !strings.HasPrefix(h, "#") {
				hostSet[h] = struct{}{}
			}
		}
	}

	all := make([]string, 0, len(hostSet))
	for h := range hostSet {
		all = append(all, h)
	}
	sort.Strings(all)
	m.allHosts = all
}

// GetAllHosts returns deduplicated, sorted list of all active target domains
func (m *Manager) GetAllHosts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]string, len(m.allHosts))
	copy(res, m.allHosts)
	return res
}

// SyncUpstream checks GitHub upstream lists in background, merges updates, and writes hosts cache
func (m *Manager) SyncUpstream() {
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}

		// Pull list-general from Flowseal
		url := "https://raw.githubusercontent.com/Flowseal/zapret-discord-youtube/main/lists/list-general.txt"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "WarLink-Hostlist")

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return
		}
		defer resp.Body.Close()

		var upstreamDomains []string
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				upstreamDomains = append(upstreamDomains, strings.ToLower(line))
			}
		}

		if len(upstreamDomains) > 0 {
			m.mu.Lock()
			// Merge discord/general
			mergedSet := make(map[string]struct{})
			for _, d := range m.categories["discord"] {
				mergedSet[d] = struct{}{}
			}
			for _, d := range upstreamDomains {
				mergedSet[d] = struct{}{}
			}
			var merged []string
			for d := range mergedSet {
				merged = append(merged, d)
			}
			sort.Strings(merged)
			m.categories["discord"] = merged
			m.rebuildAllHostsLocked()
			m.mu.Unlock()

			m.saveCache()
			m.log(fmt.Sprintf("[HOSTLIST] Списки доменов актуализированы с GitHub (%d уникальных узлов)", len(m.allHosts)))
		}
	}()
}

func (m *Manager) saveCache() {
	m.mu.RLock()
	cache := HostCache{
		LastUpdated: time.Now().Unix(),
		Categories:  m.categories,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	m.mu.RUnlock()

	if err == nil {
		_ = os.WriteFile(m.cacheFile, data, 0644)
	}
}

// ExportFreeInternetList creates warlink_core/zapret/lists/list-free-internet.txt for winws
func (m *Manager) ExportFreeInternetList() (string, error) {
	zapretListsDir := filepath.Join(deps.GetZapretDir(), "lists")
	_ = os.MkdirAll(zapretListsDir, 0755)

	destFile := filepath.Join(zapretListsDir, "list-free-internet.txt")
	hosts := m.GetAllHosts()

	var sb strings.Builder
	sb.WriteString("# WarLink v1.1.0 - Free Internet Domain List\n")
	sb.WriteString("# Auto-generated and maintained for selective DPI desynchronization\n\n")
	for _, h := range hosts {
		sb.WriteString(h)
		sb.WriteString("\n")
	}

	err := os.WriteFile(destFile, []byte(sb.String()), 0644)
	if err != nil {
		return "", err
	}

	// Also sync into list-general-user.txt so standard zapret rules include these domains
	userListFile := filepath.Join(zapretListsDir, "list-general-user.txt")
	_ = os.WriteFile(userListFile, []byte(sb.String()), 0644)

	// Also ensure Telegram IP ranges are included in ipset-telegram.txt
	tgIpsetFile := filepath.Join(zapretListsDir, "ipset-telegram.txt")
	var tgSb strings.Builder
	tgSb.WriteString("# Telegram DC IP ranges\n")
	for _, ipr := range TelegramIPRanges {
		tgSb.WriteString(ipr)
		tgSb.WriteString("\n")
	}
	_ = os.WriteFile(tgIpsetFile, []byte(tgSb.String()), 0644)

	return destFile, nil
}
