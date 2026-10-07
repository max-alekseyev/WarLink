package hostlist

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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

// SyncUpstream kept for backward compatibility; domain lists are statically managed.
func (m *Manager) SyncUpstream() {
	// No external domain lists required for Hysteria 2 tunnel.
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

// GetGeneralHosts returns all target domains excluding YouTube/Google (handled by list-google.txt)
// and direct tunnel domains (Telegram, Meta, Twitter, WhatsApp).
func (m *Manager) GetGeneralHosts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hostSet := make(map[string]struct{})
	for cat, list := range m.categories {
		if cat == "youtube" {
			continue // Handled strictly by list-google.txt
		}
		if cat == "meta" || cat == "twitter_x" || cat == "telegram" || cat == "whatsapp" {
			continue // Handled strictly through Stockholm Hysteria 2 gateway
		}
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
	return all
}

// ExportFreeInternetList creates warlink_core/lists/list-free-internet.txt
func (m *Manager) ExportFreeInternetList() (string, error) {
	listsDir := filepath.Join(deps.GetCoreDir(), "lists")
	_ = os.MkdirAll(listsDir, 0755)

	destFile := filepath.Join(listsDir, "list-free-internet.txt")
	hosts := m.GetGeneralHosts()

	var sb strings.Builder
	sb.WriteString("# WarLink - Free Internet Domain List\n\n")
	for _, h := range hosts {
		sb.WriteString(h)
		sb.WriteString("\n")
	}

	err := os.WriteFile(destFile, []byte(sb.String()), 0644)
	if err != nil {
		return "", err
	}

	userListFile := filepath.Join(listsDir, "list-general-user.txt")
	_ = os.WriteFile(userListFile, []byte(sb.String()), 0644)

	tgIpsetFile := filepath.Join(listsDir, "ipset-telegram.txt")
	var tgSb strings.Builder
	tgSb.WriteString("# Telegram DC IP ranges\n")
	for _, ipr := range TelegramIPRanges {
		tgSb.WriteString(ipr)
		tgSb.WriteString("\n")
	}
	_ = os.WriteFile(tgIpsetFile, []byte(tgSb.String()), 0644)

	_, _ = m.EnsureAutoList()

	return destFile, nil
}

// EnsureAutoList guarantees that list-auto.txt exists in warlink_core/lists.
func (m *Manager) EnsureAutoList() (string, error) {
	listsDir := filepath.Join(deps.GetCoreDir(), "lists")
	_ = os.MkdirAll(listsDir, 0755)

	autoFile := filepath.Join(listsDir, "list-auto.txt")
	if _, err := os.Stat(autoFile); os.IsNotExist(err) {
		initialContent := "# WarLink - Auto-discovered blocked domains\n\n"
		err = os.WriteFile(autoFile, []byte(initialContent), 0644)
		if err != nil {
			return "", err
		}
	}
	return autoFile, nil
}

// StartAutoListWatcher starts a background goroutine that polls list-auto.txt
// and merges newly discovered blocked domains into the cache.
func (m *Manager) StartAutoListWatcher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	autoFile, err := m.EnsureAutoList()
	if err != nil {
		m.log(fmt.Sprintf("[HOSTLIST] Ошибка инициализации автохостлиста: %v", err))
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastModTime time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fi, err := os.Stat(autoFile)
				if err != nil {
					continue
				}
				if !fi.ModTime().After(lastModTime) {
					continue
				}
				lastModTime = fi.ModTime()

				f, err := os.Open(autoFile)
				if err != nil {
					continue
				}

				var newDomains []string
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line != "" && !strings.HasPrefix(line, "#") {
						newDomains = append(newDomains, strings.ToLower(line))
					}
				}
				_ = f.Close()

				if len(newDomains) == 0 {
					continue
				}

				m.mu.Lock()
				existing := make(map[string]struct{})
				for _, d := range m.categories["auto_discovered"] {
					existing[d] = struct{}{}
				}
				addedCount := 0
				for _, d := range newDomains {
					if _, found := existing[d]; !found {
						existing[d] = struct{}{}
						m.categories["auto_discovered"] = append(m.categories["auto_discovered"], d)
						addedCount++
					}
				}
				if addedCount > 0 {
					sort.Strings(m.categories["auto_discovered"])
					m.rebuildAllHostsLocked()
					m.mu.Unlock()
					m.saveCache()
					m.log(fmt.Sprintf("[HOSTLIST] Автоматически обнаружено и добавлено %d новых заблокированных доменов (всего в автосписке: %d)",
						addedCount, len(m.categories["auto_discovered"])))
				} else {
					m.mu.Unlock()
				}
			}
		}
	}()
}

// GetAutoDiscoveredCount returns the number of runtime auto-discovered domains.
func (m *Manager) GetAutoDiscoveredCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.categories["auto_discovered"])
}

// ResetAutoList clears all runtime auto-discovered domains and resets list-auto.txt.
func (m *Manager) ResetAutoList() error {
	m.mu.Lock()
	m.categories["auto_discovered"] = nil
	m.rebuildAllHostsLocked()
	m.mu.Unlock()
	m.saveCache()

	listsDir := filepath.Join(deps.GetCoreDir(), "lists")
	autoFile := filepath.Join(listsDir, "list-auto.txt")
	initialContent := "# WarLink - Auto-discovered blocked domains\n\n"
	return os.WriteFile(autoFile, []byte(initialContent), 0644)
}


