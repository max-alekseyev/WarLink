package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"warlink/server/aclgen"
)

const (
	AdminAccountNumber   = "5230-6527-2989-4096"
	DefaultHMACSecret    = ""
	DefaultObfsPassword  = ""
	DefaultServerIP      = ""
	DefaultServerPorts   = "443,20000-30000"
	DefaultDueDate       = "2026-10-19T14:48:00Z"
	MaxActiveSessions    = 61 // 50 Free + 10 Sponsor + 1 Dedicated Admin
	MaxSessionsPerIP     = 2
	SessionTTL           = 24 * time.Hour
	SessionInactivityTTL = 5 * time.Minute
	PerUserRateDownBps   = 12500000 // 100 Mbps in bytes/sec
	PerUserRateUpBps     = 6250000  // 50 Mbps in bytes/sec
)

type ServerConfig struct {
	ServerIP              string `json:"server_ip"`
	ServerName            string `json:"server_name"`
	ServerLocation        string `json:"server_location"`
	ServerPorts           string `json:"server_ports"`
	AezaAPIKey            string `json:"aeza_api_key"`
	HMACSecret            string `json:"hmac_secret"`
	ObfsPassword          string `json:"obfs_password"`
	DashboardKey          string `json:"dashboard_key"`
	FallbackDueDate       string `json:"fallback_due_date"`
	DonateURL             string `json:"donate_url"`
	DonateAmountRub       int    `json:"donate_amount_rub"`
	MaxSessions           int    `json:"max_sessions"`
	DedicatedSponsorSlots int    `json:"dedicated_sponsor_slots"`
	ListenPublic          string `json:"listen_public"`
	ListenInternal        string `json:"listen_internal"`
	DatabaseURL           string `json:"database_url"`
	RedisAddr             string `json:"redis_addr,omitempty"`
	GeminiAPIKey          string `json:"gemini_api_key,omitempty"`
}

type SessionInfo struct {
	DeviceID      string    `json:"device_id"`
	AccountNumber string    `json:"account_number,omitempty"`
	IsSponsor     bool      `json:"is_sponsor"`
	Token         string    `json:"token"`
	ClientIP      string    `json:"client_ip"`
	Game          string    `json:"game"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeen      time.Time `json:"last_seen"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type IPRateLimiter struct {
	mu      sync.Mutex
	clients map[string][]time.Time
	limit   int
	window  time.Duration
}

func NewIPRateLimiter(limit int, window time.Duration) *IPRateLimiter {
	rl := &IPRateLimiter{
		clients: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *IPRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	times, exists := rl.clients[ip]
	if !exists {
		rl.clients[ip] = []time.Time{now}
		return true
	}

	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.clients[ip] = valid
		return false
	}

	rl.clients[ip] = append(valid, now)
	return true
}

func (rl *IPRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.window)
	for ip, times := range rl.clients {
		valid := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.clients, ip)
		} else {
			rl.clients[ip] = valid
		}
	}
}

type LoadSnapshot struct {
	ID             int64     `json:"id"`
	RecordedAt     time.Time `json:"recorded_at"`
	ActiveSessions int       `json:"active_sessions"`
	MaxSessions    int       `json:"max_sessions"`
	CPUPercent     float64   `json:"cpu_percent"`
	RAMUsedMB      int       `json:"ram_used_mb"`
	RAMTotalMB     int       `json:"ram_total_mb"`
	NetBytesRecv   int64     `json:"net_bytes_recv"`
	NetBytesSent   int64     `json:"net_bytes_sent"`
	NetRxRateKbps  int       `json:"net_rx_rate_kbps"`
	NetTxRateKbps  int       `json:"net_tx_rate_kbps"`
	GatewayPingMs  int       `json:"gateway_ping_ms"`
}

type UserTrafficStats struct {
	Tx uint64 `json:"tx"`
	Rx uint64 `json:"rx"`
}

type GeoInfo struct {
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	City        string  `json:"city"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

type AppState struct {
	mu           sync.RWMutex
	cfg          ServerConfig
	profiles     []aclgen.Profile
	sessions     map[string]*SessionInfo // token -> SessionInfo
	deviceTokens map[string]string       // device_id -> token
	cachedDue    time.Time
	cachedDueStr string
	rateLimiter  *IPRateLimiter
	db           *sql.DB
	rdb          *redis.Client

	startTime time.Time
	geoCache  map[string]GeoInfo
	geoMu     sync.RWMutex

	// Server load telemetry
	prevCPUTotal uint64
	prevCPUIdle  uint64
	prevNetRx    int64
	prevNetTx    int64
	prevNetTime  time.Time
	latestLoad   *LoadSnapshot
	loadMu       sync.RWMutex

	// Dynamic Feature Toggles
	enableDonate        bool
	enableVoting        bool
	enableCommunityGoal bool
	featureMu           sync.RWMutex

	// Telemetry and Analytics Counters
	metricRequestsTotal       uint64
	metricRejectionsRateLimit uint64
	metricRejectionsIPLimit   uint64
	metricRejectionsBadSig    uint64
	metricRejectionsCapacity  uint64
	metricTerminationsUserRelease uint64
	metricTerminationsInactivity  uint64
	metricTerminationsTTLExpired  uint64
	metricInvoicesCreated     uint64
	metricDonationsPaid       uint64
	metricDonationsRub        uint64
	metricAezaBalanceRub      uint64
	metricAezaBonusRub        uint64
	metricAezaBalanceEurCents uint64
	metricAezaBonusEurCents   uint64
	cachedPrice               int
	prevHyTraffic             map[string]UserTrafficStats
	nicknameCache             sync.Map
}

func main() {
	var genACL bool
	var listsDir string
	var cfgPath string

	flag.BoolVar(&genACL, "gen-acl", false, "Generate, test with Hysteria and apply strict ACL from lists")
	flag.StringVar(&listsDir, "lists", "/etc/warlink/lists", "Directory containing game and social hosts list files")
	flag.StringVar(&cfgPath, "config", "/opt/warlink-server/config.json", "Path to warlink-server config.json")
	flag.Parse()

	// Handle standalone or CLI ACL generation request
	if genACL {
		runCLIACLGen(listsDir)
		return
	}

	// Positional arg compatibility
	if flag.NArg() > 0 {
		cfgPath = flag.Arg(0)
	}

	state := &AppState{
		sessions:      make(map[string]*SessionInfo),
		deviceTokens:  make(map[string]string),
		rateLimiter:         NewIPRateLimiter(5, 1*time.Minute),
		enableDonate:        true,
		enableVoting:        true,
		enableCommunityGoal: true,
		startTime:           time.Now(),
		geoCache:      make(map[string]GeoInfo),
		prevHyTraffic: make(map[string]UserTrafficStats),
	}
	state.loadConfig(cfgPath)

	// Sync ACL and load profiles on startup
	if err := state.syncACL(listsDir); err != nil {
		log.Printf("[ACL] Warning: startup ACL sync: %v", err)
	}

	// Background session cleaner and active Hysteria reconciler
	go func() {
		state.cleanupExpiredSessions()
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			state.cleanupExpiredSessions()
		}
	}()

	// Background Aeza due date updater and donation reconciler
	go func() {
		state.updateDueDateFromAeza()
		state.syncAezaDonations()

		dueTicker := time.NewTicker(2 * time.Minute)
		donateTicker := time.NewTicker(2 * time.Minute)
		defer dueTicker.Stop()
		defer donateTicker.Stop()

		for {
			select {
			case <-dueTicker.C:
				state.updateDueDateFromAeza()
			case <-donateTicker.C:
				state.syncAezaDonations()
			}
		}
	}()

	// Connect to PostgreSQL
	dbURL := state.cfg.DatabaseURL
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL != "" {
		db, err := sql.Open("postgres", dbURL)
		if err == nil {
			if errPing := db.Ping(); errPing == nil {
				state.db = db
				log.Printf("[DB] Successfully connected to PostgreSQL")
				state.initDatabase()
				state.loadFeatureSettings()
				go state.startAnalyticsCollector()
			} else {
				log.Printf("[DB] Warning: PostgreSQL ping failed: %v", errPing)
			}
		} else {
			log.Printf("[DB] Warning: Failed to open PostgreSQL: %v", err)
		}
	} else {
		log.Printf("[DB] Notice: DATABASE_URL not configured")
	}

	// Connect to Redis (in-memory RAM session store)
	redisAddr := state.cfg.RedisAddr
	if redisAddr == "" {
		redisAddr = os.Getenv("REDIS_ADDR")
	}
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	state.initRedis(redisAddr)

	// Internal HTTP Auth server for Hysteria 2 (listening only on 127.0.0.1:8080)
	go func() {
		internalMux := http.NewServeMux()
		internalMux.HandleFunc("/internal/auth", state.handleInternalAuth)
		internalMux.HandleFunc("/metrics", state.handleMetrics)
		internalMux.HandleFunc("/api/v1/admin/settings", state.handleAdminSettings)
		log.Printf("[INTERNAL] Starting Hysteria 2 HTTP Auth and Metrics on %s", state.cfg.ListenInternal)
		if err := http.ListenAndServe(state.cfg.ListenInternal, internalMux); err != nil {
			log.Fatalf("[INTERNAL] Failed to start internal auth listener: %v", err)
		}
	}()

	// Public WarLink API (listening on :8443 or :80)
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/api/v1/status", state.handleStatus)
	publicMux.HandleFunc("/api/v1/session", state.handleSession)
	publicMux.HandleFunc("/api/v1/session/release", state.handleSessionRelease)
	publicMux.HandleFunc("/api/v1/profiles", state.handleProfiles)
	publicMux.HandleFunc("/api/v1/singbox/config", state.handleSingBoxConfig)
	publicMux.HandleFunc("/api/v1/donate", state.handleDonate)
	publicMux.HandleFunc("/api/v1/votes", state.handleVotes)
	publicMux.HandleFunc("/api/v1/analytics", state.handleAnalytics)
	publicMux.HandleFunc("/api/v1/releases", state.handleReleases)
	publicMux.HandleFunc("/api/v1/notifications", state.handleNotifications)
	publicMux.HandleFunc("/api/v1/notifications/read", state.handleNotificationRead)
	publicMux.HandleFunc("/api/v1/admin/notifications", state.handleAdminNotifications)
	publicMux.HandleFunc("/api/v1/sponsors", state.handleSponsors)
	publicMux.HandleFunc("/api/v1/profile", state.handleProfile)
	publicMux.HandleFunc("/api/v1/profile/avatar", state.handleProfileAvatar)

	avatarsDir := "/opt/warlink-server/avatars"
	if _, err := os.Stat("/opt/warlink-server"); os.IsNotExist(err) {
		avatarsDir = "./avatars"
	}
	_ = os.MkdirAll(avatarsDir, 0755)
	publicMux.Handle("/avatars/", http.StripPrefix("/avatars/", http.FileServer(http.Dir(avatarsDir))))

	publicMux.HandleFunc("/api/v1/admin/features", state.handleAdminFeatures)
	publicMux.HandleFunc("/api/v1/admin/settings", state.handleAdminSettings)
	publicMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "version": "v2.1.6"})
	})
	publicMux.HandleFunc("/metrics", state.handleMetrics)
	publicMux.HandleFunc("/dashboard", state.handleDashboard)
	publicMux.HandleFunc("/dashboard/", state.handleDashboard)
	publicMux.HandleFunc("/admin", state.handleDashboard)
	publicMux.HandleFunc("/admin/", state.handleDashboard)
	publicMux.HandleFunc("/control", state.handleDashboard)
	publicMux.HandleFunc("/control/", state.handleDashboard)

	log.Printf("[PUBLIC] Starting WarLink API on %s", state.cfg.ListenPublic)
	if err := http.ListenAndServe(state.cfg.ListenPublic, publicMux); err != nil {
		log.Fatalf("[PUBLIC] Failed to start public API listener: %v", err)
	}
}

func (s *AppState) loadConfig(path string) {
	s.cfg = ServerConfig{
		HMACSecret:      os.Getenv("WARLINK_HMAC_SECRET"),
		ObfsPassword:    os.Getenv("WARLINK_OBFS_PASSWORD"),
		DashboardKey:    os.Getenv("WARLINK_DASHBOARD_KEY"),
		FallbackDueDate: DefaultDueDate,
		ListenPublic:    "0.0.0.0:80",
		ListenInternal:  "127.0.0.1:8080",
	}

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.cfg)
	} else {
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		initData, _ := json.MarshalIndent(s.cfg, "", "  ")
		_ = os.WriteFile(path, initData, 0600)
	}

	due, err := time.Parse(time.RFC3339, s.cfg.FallbackDueDate)
	if err == nil {
		s.cachedDue = due
		s.cachedDueStr = s.cfg.FallbackDueDate
	} else {
		s.cachedDue = time.Now().Add(30 * 24 * time.Hour)
		s.cachedDueStr = s.cachedDue.Format(time.RFC3339)
	}

	if s.cfg.ServerName == "" {
		s.cfg.ServerName = "WarLink • Stockholm GPN Telemetry"
	}
	if s.cfg.ServerLocation == "" {
		s.cfg.ServerLocation = "Stockholm, Sweden"
	}
	if s.cfg.ServerPorts == "" {
		s.cfg.ServerPorts = DefaultServerPorts
	}
	if s.cfg.DonateAmountRub <= 0 {
		s.cfg.DonateAmountRub = 98
	}
	if s.cfg.MaxSessions <= 0 {
		s.cfg.MaxSessions = MaxActiveSessions
	}
	if s.cfg.DedicatedSponsorSlots <= 0 {
		s.cfg.DedicatedSponsorSlots = 10
	}
	if s.cfg.GeminiAPIKey == "" {
		s.cfg.GeminiAPIKey = os.Getenv("WARLINK_GEMINI_API_KEY")
	}
}

func (s *AppState) updateDueDateFromAeza() {
	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	s.mu.RUnlock()

	if apiKey == "" {
		return
	}

	req, err := http.NewRequest(http.MethodGet, "https://my.aeza.net/api/v2/services", nil)
	if err != nil {
		return
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			IP        string `json:"ip"`
			TypeSlug  string `json:"typeSlug"`
			ExpiresAt string `json:"expiresAt"`
			Status    string `json:"status"`
			Price     int    `json:"price"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
		serverIP := s.getPublicIP(nil)
		for _, item := range result.Items {
			if (item.IP == serverIP || (item.TypeSlug == "vps" && item.ExpiresAt != "")) && item.ExpiresAt != "" {
				if parsed, err := time.Parse(time.RFC3339, item.ExpiresAt); err == nil {
					s.mu.Lock()
					s.cachedDue = parsed
					s.cachedDueStr = item.ExpiresAt
					priceRub := item.Price
					if item.Price > 0 {
						if item.Price < 500 {
							priceRub = int(math.Round(float64(item.Price) * 1.3066667))
						}
						s.cachedPrice = priceRub
					}
					s.mu.Unlock()
					log.Printf("[AEZA] Updated due date from API: %s (status: %s, price: %d RUB / %d cents)", item.ExpiresAt, item.Status, s.cachedPrice, item.Price)
					break
				}
			}
		}
	}
	s.fetchAezaAccount()
	go s.syncAezaDonations()
}

func (s *AppState) fetchAezaAccount() {
	s.fetchAezaAccountURL("https://my.aeza.net/api/v2/accounts/me")
}

func (s *AppState) fetchAezaAccountURL(apiEndpoint string) {
	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	s.mu.RUnlock()

	if apiKey == "" {
		return
	}

	req, err := http.NewRequest(http.MethodGet, apiEndpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return
	}
	defer resp.Body.Close()

	var acc struct {
		Balance      float64 `json:"balance"`
		BonusBalance float64 `json:"bonusBalance"`
		Currency     string  `json:"currency"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acc); err == nil {
		balRub := int(math.Round(acc.Balance * 1.3066667))
		bonusRub := int(math.Round(acc.BonusBalance * 1.3066667))
		if strings.EqualFold(acc.Currency, "rub") {
			balRub = int(math.Round(acc.Balance))
			bonusRub = int(math.Round(acc.BonusBalance))
		}
		if balRub < 0 {
			balRub = 0
		}
		if bonusRub < 0 {
			bonusRub = 0
		}
		atomic.StoreUint64(&s.metricAezaBalanceRub, uint64(balRub))
		atomic.StoreUint64(&s.metricAezaBonusRub, uint64(bonusRub))
		atomic.StoreUint64(&s.metricAezaBalanceEurCents, uint64(math.Max(0, acc.Balance)))
		atomic.StoreUint64(&s.metricAezaBonusEurCents, uint64(math.Max(0, acc.BonusBalance)))
		log.Printf("[AEZA] Updated account balance: %d RUB (%.2f EUR) + bonus %d RUB (%.2f EUR)",
			balRub, acc.Balance/100.0, bonusRub, acc.BonusBalance/100.0)
	}
}

func (s *AppState) syncAezaDonations() {
	s.syncAezaDonationsURL("https://my.aeza.net/api/v2/billing/transactions")
}

func (s *AppState) syncAezaDonationsURL(apiEndpoint string) {
	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	donateRub := s.cfg.DonateAmountRub
	s.mu.RUnlock()

	if apiKey == "" {
		return
	}
	if donateRub <= 0 {
		donateRub = 98
	}

	client := &http.Client{Timeout: 15 * time.Second}
	offset := 0
	limit := 100
	var totalPaidCount uint64
	var totalPaidRub uint64

	sep := "?"
	if strings.Contains(apiEndpoint, "?") {
		sep = "&"
	}

	for {
		url := fmt.Sprintf("%s%slimit=%d&offset=%d", apiEndpoint, sep, limit, offset)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			log.Printf("[DONATE-SYNC] Error creating request: %v", err)
			return
		}
		req.Header.Set("X-API-KEY", apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[DONATE-SYNC] Request error: %v", err)
			return
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			log.Printf("[DONATE-SYNC] HTTP status %d from Aeza transactions API", resp.StatusCode)
			return
		}

		var result struct {
			Items []struct {
				ID          int     `json:"id"`
				Amount      float64 `json:"amount"`
				BonusAmount float64 `json:"bonusAmount"`
				Status      string  `json:"status"`
				Type        string  `json:"type"`
				InvoiceID   *int    `json:"invoiceId"`
				PerformedAt string  `json:"performedAt"`
				CreatedAt   string  `json:"createdAt"`
			} `json:"items"`
			Total int `json:"total"`
		}

		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if decodeErr != nil {
			log.Printf("[DONATE-SYNC] Failed to decode transactions: %v", decodeErr)
			return
		}

		if len(result.Items) == 0 {
			break
		}

		for _, item := range result.Items {
			// Count only confirmed WarLink community donations:
			// 1. Transaction must be performed replenishment
			// 2. Created since WarLink project launch (2026-09-20T00:00:00Z)
			// 3. Must be associated with an invoice (item.InvoiceID != nil)
			// 4. Exclude personal account/server setup purchase transactions
			if strings.EqualFold(item.Status, "performed") && strings.EqualFold(item.Type, "replenishment") {
				if item.CreatedAt < "2026-09-20T00:00:00Z" {
					continue
				}
				if item.InvoiceID == nil || *item.InvoiceID <= 0 {
					continue
				}
				// Exclude initial VPS server order on 2026-09-19/20
				if item.Amount == 199 && item.CreatedAt < "2026-09-20T12:00:00Z" {
					continue
				}

				totalPaidCount++

				rub := donateRub
				if item.Amount > 0 && item.Amount != 75 {
					if item.Amount < 500 {
						rub = int(math.Round(item.Amount * 1.3066667))
					} else {
						rub = int(math.Round(item.Amount))
					}
				}
				if rub <= 0 {
					rub = donateRub
				}
				totalPaidRub += uint64(rub)

				if s.db != nil && item.InvoiceID != nil {
					var accNum string
					var pStatus string
					var amtRub int
					err := s.db.QueryRow(`SELECT account_number, status, amount_rub FROM pending_donations WHERE invoice_id = $1`, *item.InvoiceID).Scan(&accNum, &pStatus, &amtRub)
					if err == nil && pStatus == "pending" {
						if amtRub <= 0 {
							amtRub = rub
						}
						days := (amtRub / 100) * 30
						if days < 30 {
							days = 30
						}
						_, _ = s.db.Exec(`UPDATE pending_donations SET status = 'paid' WHERE invoice_id = $1`, *item.InvoiceID)
						if accNum != "" {
							_, _ = s.db.Exec(`
								INSERT INTO accounts (account_number, tier, sponsor_until, total_donated_rub)
								VALUES ($1, 'sponsor', NOW() + ($2 * INTERVAL '1 day'), $3)
								ON CONFLICT (account_number) DO UPDATE
								SET tier = 'sponsor',
								    sponsor_until = GREATEST(COALESCE(accounts.sponsor_until, NOW()), NOW()) + ($2 * INTERVAL '1 day'),
								    total_donated_rub = accounts.total_donated_rub + $3,
								    updated_at = NOW()
							`, accNum, days, amtRub)

							_, _ = s.db.Exec(`
								INSERT INTO in_app_notifications (target_type, target_id, title, message, severity, created_at)
								VALUES ('account', $1, 'Статус Спонсора активирован', $2, 'info', NOW())
							`, accNum, fmt.Sprintf("Спасибо за поддержку WarLink! Статус Спонсора продлен на %d дней. Вам открыт приоритетный пул слотов.", days))
							log.Printf("[DONATE-SYNC] Credited %d sponsor days to account %s for invoice #%d", days, accNum, *item.InvoiceID)
						}
					}
				}
			}
		}

		offset += len(result.Items)
		if offset >= result.Total || len(result.Items) < limit || offset >= 1000 {
			break
		}
	}

	if totalPaidCount > 0 {
		atomic.StoreUint64(&s.metricDonationsPaid, totalPaidCount)
		atomic.StoreUint64(&s.metricDonationsRub, totalPaidRub)

		if s.db != nil {
			_, _ = s.db.Exec(`INSERT INTO telemetry_counters (name, value) VALUES ('donations_paid', $1) ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, totalPaidCount)
			_, _ = s.db.Exec(`INSERT INTO telemetry_counters (name, value) VALUES ('donations_rub', $1) ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, totalPaidRub)
		}
	}

	log.Printf("[DONATE-SYNC] Reconciled Aeza billing: %d paid donations, %d RUB total donations",
		totalPaidCount, totalPaidRub)
}

func (s *AppState) getPublicIP(r *http.Request) string {
	if s.cfg.ServerIP != "" {
		return s.cfg.ServerIP
	}
	if envIP := os.Getenv("WARLINK_SERVER_IP"); envIP != "" {
		return envIP
	}
	if DefaultServerIP != "" {
		return DefaultServerIP
	}
	if r != nil && r.Host != "" {
		h, _, err := net.SplitHostPort(r.Host)
		if err == nil && h != "" {
			return h
		}
		return r.Host
	}
	return ""
}

func (s *AppState) getClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	clientIP := r.Header.Get("X-Real-IP")
	if clientIP == "" {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			clientIP = strings.SplitN(fwd, ",", 2)[0]
			clientIP = strings.TrimSpace(clientIP)
		}
	}
	if clientIP == "" {
		clientIP, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	return clientIP
}

func (s *AppState) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	activeCount := len(s.sessions)
	activeFreeCount := 0
	activeSponsorCount := 0
	for _, sess := range s.sessions {
		if sess.IsSponsor {
			activeSponsorCount++
		} else {
			activeFreeCount++
		}
	}
	dueDate := s.cachedDue
	dueDateStr := s.cachedDueStr
	maxSessions := s.cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = MaxActiveSessions
	}
	dedicatedAdmin := 1
	dedicatedSponsor := s.cfg.DedicatedSponsorSlots
	if dedicatedSponsor <= 0 {
		dedicatedSponsor = 10
	}
	freeSlotsLimit := maxSessions - dedicatedSponsor - dedicatedAdmin
	if freeSlotsLimit < 0 {
		freeSlotsLimit = 50
	}
	s.mu.RUnlock()

	s.featureMu.RLock()
	enDonate := s.enableDonate
	enVoting := s.enableVoting
	enCommunityGoal := s.enableCommunityGoal
	s.featureMu.RUnlock()

	daysLeft := int(time.Until(dueDate).Hours() / 24)
	if daysLeft < 0 {
		daysLeft = 0
	}

	var octoberPoolRub int64
	if s.db != nil {
		_ = s.db.QueryRow(`
			SELECT COALESCE(SUM(amount_rub), 0)
			FROM pending_donations
			WHERE status = 'paid'
			  AND created_at >= '2026-10-01 00:00:00+03'
		`).Scan(&octoberPoolRub)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                  "online",
		"location":                s.cfg.ServerLocation,
		"ping_hint_ms":            27,
		"active_sessions":         activeCount,
		"max_sessions":            maxSessions,
		"active_free_sessions":    activeFreeCount,
		"free_slots_limit":        freeSlotsLimit,
		"active_sponsor_sessions": activeSponsorCount,
		"dedicated_sponsor_slots": dedicatedSponsor,
		"dedicated_admin_slots":   dedicatedAdmin,
		"server_ip":               s.getPublicIP(r),
		"server_ports":            s.cfg.ServerPorts,
		"due_date":                dueDateStr,
		"days_left":               daysLeft,
		"donate_amount_rub":       s.cfg.DonateAmountRub,
		"enable_donate":           enDonate,
		"enable_voting":           enVoting,
		"enable_community_goal":   enCommunityGoal,
		"october_pool_rub":        octoberPoolRub,
	})
}

type SessionRequest struct {
	DeviceID      string `json:"device_id"`
	AccountNumber string `json:"account_number,omitempty"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	Game          string `json:"game,omitempty"`
}

func (s *AppState) handleSession(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&s.metricRequestsTotal, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// When behind nginx reverse proxy, r.RemoteAddr is always 127.0.0.1.
	// Prefer X-Real-IP (set by nginx: proxy_set_header X-Real-IP $remote_addr).
	clientIP := r.Header.Get("X-Real-IP")
	if clientIP == "" {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			clientIP = strings.SplitN(fwd, ",", 2)[0]
			clientIP = strings.TrimSpace(clientIP)
		}
	}
	if clientIP == "" {
		clientIP, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if s.rateLimiter != nil && !s.rateLimiter.Allow(clientIP) {
		atomic.AddUint64(&s.metricRejectionsRateLimit, 1)
		s.recordCounterAsync("rejections_rate_limit")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "rate_limit",
			"message": "Слишком много запросов. Пожалуйста, подождите минуту.",
		})
		return
	}

	var req SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	targetGame := strings.TrimSpace(req.Game)
	if targetGame == "" {
		targetGame = "wardogs"
	}

	// 1. Validate Timestamp (within +- 60 seconds)
	now := time.Now().Unix()
	if req.Timestamp < now-60 || req.Timestamp > now+60 {
		atomic.AddUint64(&s.metricRejectionsBadSig, 1)
		s.recordCounterAsync("rejections_bad_sig")
		http.Error(w, "expired timestamp", http.StatusUnauthorized)
		return
	}

	// 2. Validate HMAC Signature: strictly mandatory if server has secret configured
	s.mu.RLock()
	secret := s.cfg.HMACSecret
	s.mu.RUnlock()

	if secret != "" {
		clientSig := r.Header.Get("X-Signature")
		if clientSig == "" {
			atomic.AddUint64(&s.metricRejectionsBadSig, 1)
			s.recordCounterAsync("rejections_bad_sig")
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return
		}
		dataToSign := fmt.Sprintf("%s:%d:%s", req.DeviceID, req.Timestamp, req.Nonce)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(dataToSign))
		expectedSig := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(clientSig), []byte(expectedSig)) {
			atomic.AddUint64(&s.metricRejectionsBadSig, 1)
			s.recordCounterAsync("rejections_bad_sig")
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	s.recordDeviceActivityAsync(req.DeviceID, clientIP)

	// Resolve account, sponsor and admin status
	isSponsor := false
	isAdmin := false
	accountNumber := strings.TrimSpace(req.AccountNumber)
	if accountNumber == AdminAccountNumber {
		isAdmin = true
		isSponsor = true
	}
	if s.db != nil {
		if accountNumber != "" {
			_, _ = s.db.Exec(`INSERT INTO accounts (account_number) VALUES ($1) ON CONFLICT (account_number) DO NOTHING`, accountNumber)
			_, _ = s.db.Exec(`INSERT INTO account_devices (account_number, device_id) VALUES ($1, $2) ON CONFLICT (account_number, device_id) DO NOTHING`, accountNumber, req.DeviceID)
			var sponsorUntil *time.Time
			_ = s.db.QueryRow(`SELECT sponsor_until FROM accounts WHERE account_number = $1`, accountNumber).Scan(&sponsorUntil)
			if (sponsorUntil != nil && sponsorUntil.After(time.Now())) || isAdmin {
				isSponsor = true
			}
		} else if req.DeviceID != "" {
			var sponsorUntil *time.Time
			var linkedAcc string
			_ = s.db.QueryRow(`
				SELECT a.account_number, a.sponsor_until 
				FROM account_devices ad 
				JOIN accounts a ON ad.account_number = a.account_number 
				WHERE ad.device_id = $1 
				ORDER BY a.sponsor_until DESC NULLS LAST LIMIT 1
			`, req.DeviceID).Scan(&linkedAcc, &sponsorUntil)
			if linkedAcc != "" {
				accountNumber = linkedAcc
				if linkedAcc == AdminAccountNumber {
					isAdmin = true
					isSponsor = true
				} else if sponsorUntil != nil && sponsorUntil.After(time.Now()) {
					isSponsor = true
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pubIP := s.getPublicIP(r)
	serverField := ":443"
	if pubIP != "" {
		serverField = fmt.Sprintf("%s:443", pubIP)
	}

	maxSessions := s.cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = MaxActiveSessions
	}
	dedicatedAdmin := 1
	dedicatedSponsor := s.cfg.DedicatedSponsorSlots
	if dedicatedSponsor <= 0 {
		dedicatedSponsor = 10
	}
	freeSlotsLimit := maxSessions - dedicatedSponsor - dedicatedAdmin
	if freeSlotsLimit < 0 {
		freeSlotsLimit = 50
	}

	// Check if this device already has an active session
	existingToken, exists := s.deviceTokens[req.DeviceID]
	serverPorts := s.cfg.ServerPorts
	if serverPorts == "" {
		serverPorts = DefaultServerPorts
	}

	if exists {
		if sess, ok := s.sessions[existingToken]; ok {
			sess.LastSeen = time.Now()
			sess.ClientIP = clientIP
			sess.Game = targetGame
			sess.AccountNumber = accountNumber
			sess.IsSponsor = isSponsor
			sess.ExpiresAt = time.Now().Add(SessionTTL)
			s.saveSessionAsync(sess)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":          sess.Token,
				"server":         serverField,
				"server_ports":   serverPorts,
				"obfs":           s.cfg.ObfsPassword,
				"expires_in_sec": int(SessionTTL.Seconds()),
				"is_sponsor":     isSponsor,
				"is_admin":       isAdmin,
			})
			return
		}
	}

	// Check per-IP concurrency cap (max 2 active sessions per IP for family/households)
	ipSessions := 0
	for _, activeSess := range s.sessions {
		if activeSess.ClientIP == clientIP && activeSess.DeviceID != req.DeviceID {
			ipSessions++
		}
	}
	if !isAdmin && ipSessions >= MaxSessionsPerIP {
		atomic.AddUint64(&s.metricRejectionsIPLimit, 1)
		s.recordCounterAsync("rejections_ip_limit")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "ip_limit_exceeded",
			"message": "Достигнут лимит одновременных подключений для вашей сети (максимум 2 устройства на семью/роутер).",
		})
		return
	}

	// Check dedicated sponsor slot reservation
	activeFreeCount := 0
	for _, activeSess := range s.sessions {
		if !activeSess.IsSponsor {
			activeFreeCount++
		}
	}

	if !isSponsor && activeFreeCount >= freeSlotsLimit {
		atomic.AddUint64(&s.metricRejectionsCapacity, 1)
		s.recordCounterAsync("rejections_capacity")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":          "server_full",
			"message":        fmt.Sprintf("Общий пул слотов (%d/%d) заполнен. %d слотов зарезервированы для Спонсоров WarLink. Ожидайте освобождения места или поддержите сервер для гарантированного слота.", activeFreeCount, freeSlotsLimit, dedicatedSponsor),
			"is_sponsor":     false,
			"free_pool_full": true,
		})
		return
	}

	// Global concurrency cap (Strict zero-kick / no-preemption policy)
	// Normal players cannot take the dedicated admin slot (maxSessions - dedicatedAdmin = 60)
	if !isAdmin && len(s.sessions) >= (maxSessions - dedicatedAdmin) {
		atomic.AddUint64(&s.metricRejectionsCapacity, 1)
		s.recordCounterAsync("rejections_capacity")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":      "server_full",
			"message":    fmt.Sprintf("Все %d слотов шлюза заняты. Мы не отключаем активных игроков. Пожалуйста, подождите несколько минут.", maxSessions - dedicatedAdmin),
			"is_sponsor": isSponsor,
		})
		return
	}

	// Generate new secure session token
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	newToken := "wl_tok_" + hex.EncodeToString(tokenBytes)

	sess := &SessionInfo{
		DeviceID:      req.DeviceID,
		AccountNumber: accountNumber,
		IsSponsor:     isSponsor,
		Token:         newToken,
		ClientIP:      clientIP,
		Game:          targetGame,
		CreatedAt:     time.Now(),
		LastSeen:      time.Now(),
		ExpiresAt:     time.Now().Add(SessionTTL),
	}
	s.sessions[newToken] = sess
	s.deviceTokens[req.DeviceID] = newToken
	s.saveSessionAsync(sess)

	log.Printf("[SESSION] Allocated slot for device %s (acc: %s, sponsor: %t, game: %s) from IP %s (Active: %d/%d, Free: %d/%d)",
		req.DeviceID, accountNumber, isSponsor, targetGame, clientIP, len(s.sessions), maxSessions, activeFreeCount+1, freeSlotsLimit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":          newToken,
		"server":         serverField,
		"server_ports":   serverPorts,
		"obfs":           s.cfg.ObfsPassword,
		"expires_in_sec": int(SessionTTL.Seconds()),
		"is_sponsor":     isSponsor,
	})
}

// Internal Auth Handler for Hysteria 2 daemon
func (s *AppState) handleInternalAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Addr string `json:"addr"`
		Auth string `json:"auth"`
		Tx   int64  `json:"tx"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false})
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[req.Auth]
	if ok && time.Now().Before(sess.ExpiresAt) {
		// Allow automatic IP migration for valid session
		connectingIP, _, _ := net.SplitHostPort(req.Addr)
		if connectingIP == "" {
			connectingIP = req.Addr
		}
		if sess.ClientIP != connectingIP {
			sess.ClientIP = connectingIP
			sess.LastSeen = time.Now()
		} else {
			sess.LastSeen = time.Now()
		}

		deviceID := sess.DeviceID
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"id": deviceID,
			"rate": map[string]int64{
				"up":   PerUserRateUpBps,
				"down": PerUserRateDownBps,
			},
		})
		return
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false})
}

func (s *AppState) handleDonate(w http.ResponseWriter, r *http.Request) {
	s.featureMu.RLock()
	enDonate := s.enableDonate
	s.featureMu.RUnlock()
	if !enDonate {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Сбор пожертвований временно приостановлен",
		})
		return
	}

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if s.rateLimiter != nil && !s.rateLimiter.Allow(clientIP) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "rate_limit",
			"message": "Слишком много запросов на пополнение. Пожалуйста, подождите минуту.",
		})
		return
	}

	var reqBody struct {
		AccountNumber string `json:"account_number"`
		DeviceID      string `json:"device_id"`
		AmountRub     int    `json:"amount_rub"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
	}

	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	s.mu.RUnlock()

	if apiKey != "" {
		amount := s.cfg.DonateAmountRub
		if reqBody.AmountRub >= 100 {
			amount = reqBody.AmountRub
		} else if amount <= 0 {
			amount = 100
		}
		// Aeza API v2 uses minor currency units (cents). 1 EUR ≈ 130.66 RUB.
		aezaCents := int(float64(amount) / 1.3066)
		if aezaCents < 50 {
			aezaCents = 50
		}
		payload := map[string]interface{}{
			"method": "yookassa:sbp",
			"amount": aezaCents,
		}
		body, _ := json.Marshal(payload)
		aezaReq, err := http.NewRequest(http.MethodPost, "https://my.aeza.net/api/v2/billing/invoices", bytes.NewReader(body))
		if err == nil {
			aezaReq.Header.Set("X-API-KEY", apiKey)
			aezaReq.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(aezaReq)
			if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated) {
				defer resp.Body.Close()
				var invResp struct {
					ID      int    `json:"id"`
					Amount  int    `json:"amount"`
					Status  string `json:"status"`
					Payload struct {
						URL string `json:"url"`
					} `json:"payload"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&invResp); err == nil && invResp.Payload.URL != "" {
					actualRub := amount
					if actualRub <= 0 {
						actualRub = 100
					}

					atomic.AddUint64(&s.metricInvoicesCreated, 1)
					s.recordCounterAsync("invoices_created")

					if s.db != nil && invResp.ID > 0 {
						_, _ = s.db.Exec(`
							INSERT INTO pending_donations (invoice_id, account_number, device_id, amount_rub, status, created_at)
							VALUES ($1, $2, $3, $4, 'pending', NOW())
							ON CONFLICT (invoice_id) DO UPDATE SET amount_rub = EXCLUDED.amount_rub, account_number = EXCLUDED.account_number, device_id = EXCLUDED.device_id
						`, invResp.ID, reqBody.AccountNumber, reqBody.DeviceID, actualRub)
					}

					// Trigger background reconciliation after user has time to scan and pay via SBP
					go func() {
						time.Sleep(30 * time.Second)
						s.syncAezaDonations()
						time.Sleep(60 * time.Second)
						s.syncAezaDonations()
					}()

					log.Printf("[DONATE] Created Aeza SBP invoice #%d for %d RUB (charged %d cents) for acc %s: %s",
						invResp.ID, actualRub, aezaCents, reqBody.AccountNumber, invResp.Payload.URL)
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"success":     true,
						"pay_url":     invResp.Payload.URL,
						"payment_url": invResp.Payload.URL,
						"url":         invResp.Payload.URL,
					})
					return
				}
			} else if err != nil {
				log.Printf("[DONATE] Warning: Aeza API request failed: %v", err)
			}
		}
	}

	// Fallback payment/dashboard link from config if available
	w.Header().Set("Content-Type", "application/json")
	if s.cfg.DonateURL != "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"pay_url": s.cfg.DonateURL,
		})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   "Платёжный шлюз не настроен на сервере",
	})
}

func (s *AppState) handleSessionRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token    string `json:"token"`
		DeviceID string `json:"device_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	s.mu.Lock()
	defer s.mu.Unlock()

	released := false
	for tok, sess := range s.sessions {
		matchToken := req.Token != "" && tok == req.Token
		matchDevice := req.DeviceID != "" && (sess.DeviceID == req.DeviceID || strings.EqualFold(sess.DeviceID, req.DeviceID))
		if matchToken || matchDevice {
			delete(s.sessions, tok)
			delete(s.deviceTokens, sess.DeviceID)
			go s.deleteSessionFromRedis(tok, sess.DeviceID)
			atomic.AddUint64(&s.metricTerminationsUserRelease, 1)
			s.recordCounterAsync("terminations_user_release")
			log.Printf("[SESSION] Explicitly released slot for device %s (Active: %d/%d)", sess.DeviceID, len(s.sessions), MaxActiveSessions)
			released = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"released": released,
	})
}

type GameSuggestionItem struct {
	SteamAppID int       `json:"steam_app_id"`
	Title      string    `json:"title"`
	IconURL    string    `json:"icon_url"`
	VotesCount int       `json:"votes_count"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UserVoted  bool      `json:"user_voted"`
}

func (s *AppState) handleVotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "База данных временно недоступна",
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		deviceID := r.URL.Query().Get("device_id")
		userVotedMap := make(map[int]bool)
		userVotesUsed := 0
		if deviceID != "" {
			rows, err := s.db.Query(`
				SELECT dv.steam_app_id, COALESCE(gs.status, 'voting')
				FROM device_votes dv
				LEFT JOIN game_suggestions gs ON dv.steam_app_id = gs.steam_app_id
				WHERE dv.device_id = $1
			`, deviceID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var appID int
					var status string
					if err := rows.Scan(&appID, &status); err == nil {
						userVotedMap[appID] = true
						if status == "voting" {
							userVotesUsed++
						}
					}
				}
			}
		}

		rows, err := s.db.Query(`
			SELECT steam_app_id, title, icon_url, votes_count, status, created_at
			FROM game_suggestions
			ORDER BY 
				CASE WHEN status = 'queue_integration' THEN 1 ELSE 2 END,
				votes_count DESC, 
				created_at ASC
			LIMIT 100
		`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		games := make([]GameSuggestionItem, 0)
		for rows.Next() {
			var g GameSuggestionItem
			var icon sql.NullString
			if err := rows.Scan(&g.SteamAppID, &g.Title, &icon, &g.VotesCount, &g.Status, &g.CreatedAt); err == nil {
				g.IconURL = icon.String
				if g.IconURL == "" || strings.HasSuffix(g.IconURL, fmt.Sprintf("/%d/capsule_231x87.jpg", g.SteamAppID)) {
					if fixedIcon := resolveSteamStoreIcon(g.SteamAppID); fixedIcon != "" {
						g.IconURL = fixedIcon
						go func(id int, u string) {
							_, _ = s.db.Exec("UPDATE game_suggestions SET icon_url = $1, updated_at = NOW() WHERE steam_app_id = $2", u, id)
						}(g.SteamAppID, fixedIcon)
					}
				}
				g.UserVoted = userVotedMap[g.SteamAppID]
				games = append(games, g)
			}
		}

		userVotePower := 1
		if deviceID != "" {
			var tier string
			var spUntil *time.Time
			_ = s.db.QueryRow(`
				SELECT a.tier, a.sponsor_until
				FROM account_devices ad
				JOIN accounts a ON ad.account_number = a.account_number
				WHERE ad.device_id = $1
				ORDER BY CASE WHEN a.tier = 'sponsor' OR (a.sponsor_until IS NOT NULL AND a.sponsor_until > NOW()) THEN 1 ELSE 2 END LIMIT 1
			`, deviceID).Scan(&tier, &spUntil)
			if tier == "sponsor" || (spUntil != nil && spUntil.After(time.Now())) {
				userVotePower = 3
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"target_votes":    50,
			"max_user_votes":  3,
			"user_votes_used": userVotesUsed,
			"user_vote_power": userVotePower,
			"games":           games,
		})

	case http.MethodPost:
		s.featureMu.RLock()
		enVoting := s.enableVoting
		s.featureMu.RUnlock()
		if !enVoting {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Голосование за игры временно приостановлено",
			})
			return
		}

		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}
		if s.rateLimiter != nil && !s.rateLimiter.Allow(clientIP) {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Слишком много запросов. Пожалуйста, подождите минуту.",
			})
			return
		}

		var req struct {
			DeviceID   string `json:"device_id"`
			SteamAppID int    `json:"steam_app_id"`
			Title      string `json:"title"`
			IconURL    string `json:"icon_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" || req.SteamAppID <= 0 || strings.TrimSpace(req.Title) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Некорректные параметры игры",
			})
			return
		}

		iconURL := strings.TrimSpace(req.IconURL)
		if iconURL == "" || strings.HasSuffix(iconURL, fmt.Sprintf("/%d/capsule_231x87.jpg", req.SteamAppID)) {
			if resolved := resolveSteamStoreIcon(req.SteamAppID); resolved != "" {
				iconURL = resolved
			}
		}

		// Reject officially supported games (WARDOGS is already built into WarLink)
		if req.SteamAppID == 1867240 || strings.EqualFold(strings.TrimSpace(req.Title), "wardogs") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Игра WARDOGS уже официально поддерживается в WarLink!",
			})
			return
		}

		tx, err := s.db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Check if game has already won/graduated
		var currGameStatus string
		_ = tx.QueryRow("SELECT status FROM game_suggestions WHERE steam_app_id = $1", req.SteamAppID).Scan(&currGameStatus)
		if currGameStatus != "" && currGameStatus != "voting" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Эта игра уже победила в голосовании и находится в очереди на интеграцию!",
			})
			return
		}

		// Check user active vote count limit (max 3 on games currently in 'voting' status)
		var userVoteCount int
		_ = tx.QueryRow(`
			SELECT COUNT(*) 
			FROM device_votes dv
			JOIN game_suggestions gs ON dv.steam_app_id = gs.steam_app_id
			WHERE dv.device_id = $1 AND gs.status = 'voting'
		`, req.DeviceID).Scan(&userVoteCount)
		if userVoteCount >= 3 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Вы исчерпали лимит в 3 активных голоса. Чтобы проголосовать за другую игру, дождитесь победы текущей или отзовите один из отданных голосов.",
			})
			return
		}

		// Check per-IP vote limit (max 6 active votes per IP network for 2 family members)
		var ipVoteCount int
		_ = tx.QueryRow(`
			SELECT COUNT(*) 
			FROM device_votes dv
			JOIN game_suggestions gs ON dv.steam_app_id = gs.steam_app_id
			WHERE dv.client_ip = $1 AND gs.status = 'voting'
		`, clientIP).Scan(&ipVoteCount)
		if ipVoteCount >= 6 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Достигнут лимит активных голосов для вашей сети (максимум 6 голосов на семью/роутер).",
			})
			return
		}

		// Check if already voted for this game
		var alreadyVoted int
		_ = tx.QueryRow("SELECT COUNT(*) FROM device_votes WHERE device_id = $1 AND steam_app_id = $2", req.DeviceID, req.SteamAppID).Scan(&alreadyVoted)
		if alreadyVoted > 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Вы уже отдали голос за эту игру.",
			})
			return
		}

		voteWeight := 1
		var tier string
		var spUntil *time.Time
		_ = tx.QueryRow(`
			SELECT a.tier, a.sponsor_until
			FROM account_devices ad
			JOIN accounts a ON ad.account_number = a.account_number
			WHERE ad.device_id = $1
			ORDER BY CASE WHEN a.tier = 'sponsor' OR (a.sponsor_until IS NOT NULL AND a.sponsor_until > NOW()) THEN 1 ELSE 2 END LIMIT 1
		`, req.DeviceID).Scan(&tier, &spUntil)
		if tier == "sponsor" || (spUntil != nil && spUntil.After(time.Now())) {
			voteWeight = 3
		}

		// Upsert game_suggestions
		var newVotes int
		var currStatus string
		err = tx.QueryRow(`
			INSERT INTO game_suggestions (steam_app_id, title, icon_url, votes_count, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'voting', NOW(), NOW())
			ON CONFLICT (steam_app_id) DO UPDATE
			SET votes_count = game_suggestions.votes_count + $4,
			    title = EXCLUDED.title,
			    icon_url = CASE WHEN EXCLUDED.icon_url <> '' THEN EXCLUDED.icon_url ELSE game_suggestions.icon_url END,
			    updated_at = NOW()
			RETURNING votes_count, status
		`, req.SteamAppID, req.Title, iconURL, voteWeight).Scan(&newVotes, &currStatus)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// If reached 50, update status to queue_integration
		if newVotes >= 50 && currStatus != "queue_integration" {
			currStatus = "queue_integration"
			_, _ = tx.Exec("UPDATE game_suggestions SET status = 'queue_integration', updated_at = NOW() WHERE steam_app_id = $1", req.SteamAppID)
			log.Printf("[VOTES] Game %s (AppID %d) reached 50 votes! Status: queue_integration", req.Title, req.SteamAppID)
		}

		// Record vote
		_, err = tx.Exec("INSERT INTO device_votes (device_id, steam_app_id, client_ip, vote_weight, created_at) VALUES ($1, $2, $3, $4, NOW())", req.DeviceID, req.SteamAppID, clientIP, voteWeight)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[VOTES] Device %s voted for %s (AppID: %d, Weight: %d, Total: %d)", req.DeviceID, req.Title, req.SteamAppID, voteWeight, newVotes)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"votes_count": newVotes,
			"vote_weight": voteWeight,
			"status":      currStatus,
		})

	case http.MethodDelete:
		s.featureMu.RLock()
		enVoting := s.enableVoting
		s.featureMu.RUnlock()
		if !enVoting {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Голосование за игры временно приостановлено",
			})
			return
		}

		var req struct {
			DeviceID   string `json:"device_id"`
			SteamAppID int    `json:"steam_app_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DeviceID == "" || req.SteamAppID <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Некорректные параметры",
			})
			return
		}

		tx, err := s.db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		var voteWeight int = 1
		_ = tx.QueryRow("SELECT vote_weight FROM device_votes WHERE device_id = $1 AND steam_app_id = $2", req.DeviceID, req.SteamAppID).Scan(&voteWeight)
		if voteWeight <= 0 {
			voteWeight = 1
		}

		res, err := tx.Exec("DELETE FROM device_votes WHERE device_id = $1 AND steam_app_id = $2", req.DeviceID, req.SteamAppID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			var newVotes int
			err = tx.QueryRow(`
				UPDATE game_suggestions
				SET votes_count = GREATEST(0, votes_count - $2),
				    status = CASE WHEN votes_count - $2 < 50 AND status = 'queue_integration' THEN 'voting' ELSE status END,
				    updated_at = NOW()
				WHERE steam_app_id = $1
				RETURNING votes_count
			`, req.SteamAppID, voteWeight).Scan(&newVotes)
			if err == nil {
				log.Printf("[VOTES] Device %s unvoted for AppID %d (Weight: %d, New total: %d)", req.DeviceID, req.SteamAppID, voteWeight, newVotes)
			}
		}

		_ = tx.Commit()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"unvoted": affected > 0,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *AppState) cleanupExpiredSessions() {
	activeDevices := make(map[string]bool)
	hyClient := &http.Client{Timeout: 500 * time.Millisecond}

	s.mu.Lock()
	firstPoll := len(s.prevHyTraffic) == 0
	s.mu.Unlock()

	if firstPoll {
		// On startup, take two samples spaced 2 seconds apart to accurately detect active transmitting sessions
		if hyResp, err := hyClient.Get("http://127.0.0.1:9090/traffic"); err == nil {
			var initialData map[string]UserTrafficStats
			if err := json.NewDecoder(hyResp.Body).Decode(&initialData); err == nil {
				s.mu.Lock()
				s.prevHyTraffic = initialData
				s.mu.Unlock()
			}
			hyResp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}

	if hyResp, err := hyClient.Get("http://127.0.0.1:9090/traffic"); err == nil {
		var hyData map[string]UserTrafficStats
		if err := json.NewDecoder(hyResp.Body).Decode(&hyData); err == nil {
			s.mu.Lock()
			if s.prevHyTraffic == nil {
				s.prevHyTraffic = make(map[string]UserTrafficStats)
			}
			for devID, cur := range hyData {
				prev, exists := s.prevHyTraffic[devID]
				// A device is actively transmitting ONLY if traffic increased between samples
				if exists && (cur.Tx > prev.Tx || cur.Rx > prev.Rx) {
					activeDevices[devID] = true
				}
				s.prevHyTraffic[devID] = cur
			}
			s.mu.Unlock()
		}
		hyResp.Body.Close()
	}

	s.mu.Lock()
	now := time.Now()

	// Re-attach active Hysteria 2 connections into active sessions if missing
	for devID := range activeDevices {
		tok, exists := s.deviceTokens[devID]
		if !exists {
			var restoredSess *SessionInfo
			if s.rdb != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				if savedTok, err := s.rdb.Get(ctx, "wl:dev:"+devID).Result(); err == nil && savedTok != "" {
					if raw, err := s.rdb.Get(ctx, "wl:sess:"+savedTok).Result(); err == nil {
						var parsed SessionInfo
						if json.Unmarshal([]byte(raw), &parsed) == nil {
							restoredSess = &parsed
							tok = savedTok
						}
					}
				}
				cancel()
			}
			if restoredSess != nil {
				restoredSess.LastSeen = now
				restoredSess.ExpiresAt = now.Add(SessionTTL)
				s.deviceTokens[devID] = tok
				s.sessions[tok] = restoredSess
				s.saveSessionAsync(restoredSess)
			} else {
				tok = "wl_tok_hy2_" + devID
				s.deviceTokens[devID] = tok
				sess := &SessionInfo{
					DeviceID:  devID,
					Token:     tok,
					Game:      "wardogs",
					CreatedAt: now,
					LastSeen:  now,
					ExpiresAt: now.Add(SessionTTL),
				}
				if s.db != nil {
					var cip string
					_ = s.db.QueryRow(`SELECT client_ip FROM daily_active_devices WHERE device_id = $1 ORDER BY last_seen DESC LIMIT 1`, devID).Scan(&cip)
					if cip != "" {
						sess.ClientIP = cip
					}
				}
				s.sessions[tok] = sess
				s.saveSessionAsync(sess)
			}
		} else if sess, ok := s.sessions[tok]; ok {
			sess.LastSeen = now
		}
	}

	for token, sess := range s.sessions {
		if activeDevices[sess.DeviceID] {
			sess.LastSeen = now
			continue
		}
		// Inferred Hysteria sessions expire much faster when traffic ceases
		maxInactivity := SessionInactivityTTL
		if strings.HasPrefix(token, "wl_tok_hy2_") {
			maxInactivity = 60 * time.Second
		}
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, token)
			delete(s.deviceTokens, sess.DeviceID)
			go s.deleteSessionFromRedis(token, sess.DeviceID)
			atomic.AddUint64(&s.metricTerminationsTTLExpired, 1)
			s.recordCounterAsync("terminations_ttl_expired")
			log.Printf("[CLEANUP] Released expired slot for device %s (Active: %d/%d)", sess.DeviceID, len(s.sessions), MaxActiveSessions)
		} else if now.Sub(sess.LastSeen) > maxInactivity {
			delete(s.sessions, token)
			delete(s.deviceTokens, sess.DeviceID)
			go s.deleteSessionFromRedis(token, sess.DeviceID)
			atomic.AddUint64(&s.metricTerminationsInactivity, 1)
			s.recordCounterAsync("terminations_inactivity")
			log.Printf("[CLEANUP] Released inactive slot for device %s (Active: %d/%d)", sess.DeviceID, len(s.sessions), MaxActiveSessions)
		}
	}
	s.mu.Unlock()
}

func (s *AppState) handleProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	profiles := s.profiles
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"profiles": profiles,
		"count":    len(profiles),
	})
}

func (s *AppState) handleSingBoxConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "missing_token",
			"message": "Сессионный токен обязателен для получения конфигурации",
		})
		return
	}

	clientIP := s.getClientIP(r)

	s.mu.RLock()
	sess, exists := s.sessions[token]
	if !exists || time.Now().After(sess.ExpiresAt) {
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "invalid_or_expired_token",
			"message": "Недействительный или истекший сессионный токен",
		})
		return
	}

	// Verify connecting client IP matches session IP (bypass if connecting via local reverse proxy)
	isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost"
	if !isLocal && sess.ClientIP != "" && clientIP != "" && sess.ClientIP != clientIP {
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "ip_mismatch",
			"message": "IP-адрес клиента не соответствует сессии",
		})
		return
	}

	profiles := s.profiles
	obfsPassword := s.cfg.ObfsPassword
	serverPorts := s.cfg.ServerPorts
	if serverPorts == "" {
		serverPorts = DefaultServerPorts
	}
	s.mu.RUnlock()

	pubIP := s.getPublicIP(r)
	if pubIP == "" {
		s.mu.RLock()
		pubIP = s.cfg.ServerIP
		s.mu.RUnlock()
	}

	targetGame := r.URL.Query().Get("game")
	if targetGame == "" {
		targetGame = sess.Game
	}
	if targetGame == "" {
		targetGame = "wardogs"
	}

	webParam := r.URL.Query().Get("web")
	includeWebServices := webParam == "1" || strings.ToLower(webParam) == "true"

	var extraProcs []string
	if pStr := r.URL.Query().Get("procs"); pStr != "" {
		for _, part := range strings.Split(pStr, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				extraProcs = append(extraProcs, part)
			}
		}
	}
	for _, p := range r.URL.Query()["proc"] {
		p = strings.TrimSpace(p)
		if p != "" {
			extraProcs = append(extraProcs, p)
		}
	}

	baseGame := strings.TrimSpace(strings.Split(targetGame, "(")[0])
	baseGame = strings.TrimSpace(strings.Split(baseGame, "•")[0])
	baseGame = strings.TrimSpace(strings.Split(baseGame, "+")[0])
	if baseGame == "" {
		baseGame = targetGame
	}

	var activeProfiles []aclgen.Profile
	for _, p := range profiles {
		if strings.EqualFold(p.ID, targetGame) || strings.EqualFold(p.Name, targetGame) ||
			strings.EqualFold(p.ID, baseGame) || strings.EqualFold(p.Name, baseGame) ||
			p.ID == "socials" || p.ID == "wardogs" {
			activeProfiles = append(activeProfiles, p)
		}
	}
	if len(activeProfiles) == 0 {
		activeProfiles = profiles
	}

	cfgBytes, err := aclgen.GenerateSingBoxConfig(
		activeProfiles,
		extraProcs,
		includeWebServices,
		pubIP,
		serverPorts,
		obfsPassword,
		token,
	)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "config_generation_failed",
			"message": err.Error(),
		})
		return
	}

	var parsedCfg interface{}
	_ = json.Unmarshal(cfgBytes, &parsedCfg)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"game":    targetGame,
		"config":  parsedCfg,
	})
}

func (s *AppState) syncACL(listsDir string) error {
	if _, err := os.Stat(listsDir); os.IsNotExist(err) {
		listsDir = "games"
	}
	if _, err := os.Stat(listsDir); os.IsNotExist(err) {
		return fmt.Errorf("lists directory %s does not exist", listsDir)
	}

	profiles, err := aclgen.LoadProfiles(listsDir)
	if err != nil {
		return fmt.Errorf("failed to load profiles from %s: %w", listsDir, err)
	}

	aclContent, err := aclgen.GenerateACL(profiles)
	if err != nil {
		return fmt.Errorf("failed to generate ACL: %w", err)
	}

	targetACL := "/etc/hysteria/acl.txt"
	certPath := "/etc/hysteria/certs/server.crt"
	keyPath := "/etc/hysteria/certs/server.key"
	hysteriaBin, err := exec.LookPath("hysteria")
	if err != nil {
		hysteriaBin = "/usr/local/bin/hysteria"
	}

	// Apply and reload only if running on host with server certificate
	if _, err := os.Stat(certPath); err == nil {
		if err := aclgen.ApplyAndReload(aclContent, targetACL, hysteriaBin, certPath, keyPath); err != nil {
			return fmt.Errorf("failed to verify and reload Hysteria ACL: %w", err)
		}
		log.Printf("[ACL] Verified and applied strict ACL from %d profiles to %s", len(profiles), targetACL)
	} else {
		log.Printf("[ACL] Local environment (no %s), loaded %d profiles without Hysteria reload", certPath, len(profiles))
	}

	s.mu.Lock()
	s.profiles = profiles
	s.mu.Unlock()
	return nil
}

func runCLIACLGen(listsDir string) {
	fmt.Printf("[ACL-GEN] Loading profiles from %s...\n", listsDir)
	profiles, err := aclgen.LoadProfiles(listsDir)
	if err != nil {
		log.Fatalf("[ACL-GEN] FATAL: Error loading profiles: %v", err)
	}

	fmt.Printf("[ACL-GEN] Successfully validated %d profiles:\n", len(profiles))
	for _, p := range profiles {
		fmt.Printf(" - %s (%s): %d processes, %d domains, %d ips, %d udp ranges\n",
			p.Name, p.ID, len(p.Processes), len(p.Domains), len(p.IPs), len(p.UDPRanges))
	}

	aclContent, err := aclgen.GenerateACL(profiles)
	if err != nil {
		log.Fatalf("[ACL-GEN] FATAL: Error generating ACL content: %v", err)
	}

	targetACL := "/etc/hysteria/acl.txt"
	certPath := "/etc/hysteria/certs/server.crt"
	keyPath := "/etc/hysteria/certs/server.key"
	hysteriaBin, err := exec.LookPath("hysteria")
	if err != nil {
		hysteriaBin = "/usr/local/bin/hysteria"
	}

	fmt.Printf("[ACL-GEN] Verifying candidate ACL with test Hysteria process (%s)...\n", hysteriaBin)
	if err := aclgen.ApplyAndReload(aclContent, targetACL, hysteriaBin, certPath, keyPath); err != nil {
		log.Fatalf("[ACL-GEN] FATAL: Verification or reload failed: %v", err)
	}

	fmt.Printf("[ACL-GEN] SUCCESS: Candidate ACL verified, applied to %s, and hysteria-server restarted!\n", targetACL)
	os.Exit(0)
}

func resolveSteamStoreIcon(appID int) string {
	if appID <= 0 {
		return ""
	}
	client := &http.Client{Timeout: 3500 * time.Millisecond}

	// 1. Priority 1: Steam Community App Hub square icon
	hubURL := fmt.Sprintf("https://steamcommunity.com/app/%d", appID)
	req, err := http.NewRequest("GET", hubURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
		req.Header.Set("Cookie", "wants_mature_content=1; birthtime=568022401")
		if resp, err := client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			re := regexp.MustCompile(`<div[^>]*class="apphub_AppIcon"[^>]*>\s*<img[^>]*src="([^"]+)"`)
			if m := re.FindSubmatch(body); len(m) > 1 {
				icon := string(m[1])
				icon = strings.Replace(icon, "http://", "https://", 1)
				return icon
			}
		}
	}

	// 2. Fallback: Store search
	u := fmt.Sprintf("https://store.steampowered.com/api/storesearch/?term=%d&l=russian&cc=US", appID)
	resp, err := client.Get(u)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var searchRes struct {
		Items []struct {
			ID        int    `json:"id"`
			TinyImage string `json:"tiny_image"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchRes); err == nil {
		for _, it := range searchRes.Items {
			if it.ID == appID && it.TinyImage != "" {
				return it.TinyImage
			}
		}
		if len(searchRes.Items) > 0 && searchRes.Items[0].TinyImage != "" {
			return searchRes.Items[0].TinyImage
		}
	}
	return ""
}

func (s *AppState) initDatabase() {
	if s.db == nil {
		return
	}
	schema := `
	CREATE TABLE IF NOT EXISTS server_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	INSERT INTO server_settings (key, value) VALUES ('enable_donate', 'true') ON CONFLICT (key) DO NOTHING;
	INSERT INTO server_settings (key, value) VALUES ('enable_voting', 'true') ON CONFLICT (key) DO NOTHING;

	CREATE TABLE IF NOT EXISTS server_load_history (
		id BIGSERIAL PRIMARY KEY,
		recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		active_sessions INT NOT NULL,
		max_sessions INT NOT NULL DEFAULT 100,
		cpu_percent DOUBLE PRECISION NOT NULL,
		ram_used_mb INT NOT NULL,
		ram_total_mb INT NOT NULL,
		net_bytes_recv BIGINT NOT NULL,
		net_bytes_sent BIGINT NOT NULL,
		net_rx_rate_kbps INT NOT NULL DEFAULT 0,
		net_tx_rate_kbps INT NOT NULL DEFAULT 0,
		gateway_ping_ms INT NOT NULL DEFAULT 27
	);
	CREATE INDEX IF NOT EXISTS idx_server_load_recorded_at ON server_load_history(recorded_at DESC);

	CREATE TABLE IF NOT EXISTS game_suggestions (
		steam_app_id INT PRIMARY KEY,
		title TEXT NOT NULL,
		icon_url TEXT,
		votes_count INT NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'voting',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS device_votes (
		id BIGSERIAL PRIMARY KEY,
		device_id TEXT NOT NULL,
		steam_app_id INT NOT NULL,
		client_ip TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(device_id, steam_app_id)
	);
	ALTER TABLE device_votes ADD COLUMN IF NOT EXISTS client_ip TEXT;
	ALTER TABLE device_votes ADD COLUMN IF NOT EXISTS vote_weight INT NOT NULL DEFAULT 1;
	CREATE INDEX IF NOT EXISTS idx_device_votes_ip ON device_votes(client_ip);
	CREATE INDEX IF NOT EXISTS idx_device_votes_device ON device_votes(device_id);

	CREATE TABLE IF NOT EXISTS telemetry_counters (
		name TEXT PRIMARY KEY,
		value BIGINT NOT NULL DEFAULT 0
	);
	INSERT INTO telemetry_counters (name, value) VALUES ('invoices_created', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('donations_paid', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('donations_rub', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('rejections_rate_limit', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('rejections_ip_limit', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('rejections_bad_sig', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('rejections_capacity', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('terminations_user_release', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('terminations_inactivity', 0) ON CONFLICT (name) DO NOTHING;
	INSERT INTO telemetry_counters (name, value) VALUES ('terminations_ttl_expired', 0) ON CONFLICT (name) DO NOTHING;

	CREATE TABLE IF NOT EXISTS daily_active_devices (
		device_id TEXT NOT NULL,
		seen_date DATE NOT NULL DEFAULT CURRENT_DATE,
		client_ip TEXT,
		last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(device_id, seen_date)
	);
	CREATE INDEX IF NOT EXISTS idx_daily_active_date ON daily_active_devices(seen_date);

	CREATE TABLE IF NOT EXISTS ip_geo_cache (
		ip TEXT PRIMARY KEY,
		country TEXT NOT NULL DEFAULT 'Unknown',
		country_code TEXT NOT NULL DEFAULT 'XX',
		city TEXT NOT NULL DEFAULT 'Unknown',
		lat DOUBLE PRECISION NOT NULL DEFAULT 0,
		lon DOUBLE PRECISION NOT NULL DEFAULT 0,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS accounts (
		account_number TEXT PRIMARY KEY,
		nickname TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		tier TEXT NOT NULL DEFAULT 'free',
		sponsor_until TIMESTAMPTZ,
		total_donated_rub INT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_accounts_sponsor ON accounts(sponsor_until);
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS steam_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS motto TEXT NOT NULL DEFAULT '';
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS hide_donation_amount BOOLEAN NOT NULL DEFAULT FALSE;

	CREATE TABLE IF NOT EXISTS account_devices (
		account_number TEXT NOT NULL,
		device_id TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(account_number, device_id)
	);
	CREATE INDEX IF NOT EXISTS idx_account_devices_dev ON account_devices(device_id);

	CREATE TABLE IF NOT EXISTS in_app_notifications (
		id BIGSERIAL PRIMARY KEY,
		target_type TEXT NOT NULL DEFAULT 'broadcast',
		target_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		message TEXT NOT NULL,
		severity TEXT NOT NULL DEFAULT 'info',
		action_label TEXT NOT NULL DEFAULT '',
		action_url TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_notifications_target ON in_app_notifications(target_type, target_id);

	CREATE TABLE IF NOT EXISTS notification_reads (
		notification_id BIGINT NOT NULL,
		reader_id TEXT NOT NULL,
		read_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(notification_id, reader_id)
	);

	CREATE TABLE IF NOT EXISTS pending_donations (
		invoice_id INT PRIMARY KEY,
		account_number TEXT NOT NULL DEFAULT '',
		device_id TEXT NOT NULL DEFAULT '',
		amount_rub INT NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	DROP TABLE IF EXISTS active_sessions CASCADE;
	`
	if _, err := s.db.Exec(schema); err != nil {
		log.Printf("[DB] Error initializing schema: %v", err)
	} else {
		log.Printf("[DB] Database tables initialized successfully (accounts, notifications, active sessions in RAM)")
	}
}

func (s *AppState) resolveIPGeo(ip string) GeoInfo {
	if ip == "" || ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return GeoInfo{Country: "Local", CountryCode: "LO", City: "Localhost", Lat: 59.3293, Lon: 18.0686}
	}
	s.geoMu.RLock()
	if info, ok := s.geoCache[ip]; ok {
		s.geoMu.RUnlock()
		return info
	}
	s.geoMu.RUnlock()

	// Try DB
	if s.db != nil {
		var info GeoInfo
		err := s.db.QueryRow(`SELECT country, country_code, city, lat, lon FROM ip_geo_cache WHERE ip = $1`, ip).
			Scan(&info.Country, &info.CountryCode, &info.City, &info.Lat, &info.Lon)
		if err == nil {
			s.geoMu.Lock()
			s.geoCache[ip] = info
			s.geoMu.Unlock()
			return info
		}
	}

	// Default fallback
	info := GeoInfo{Country: "Unknown", CountryCode: "XX", City: "Unknown", Lat: 0, Lon: 0}

	// Fetch asynchronously so we never block callers
	go func(targetIP string) {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://ip-api.com/json/" + targetIP + "?fields=status,country,countryCode,city,lat,lon")
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var res struct {
			Status      string  `json:"status"`
			Country     string  `json:"country"`
			CountryCode string  `json:"countryCode"`
			City        string  `json:"city"`
			Lat         float64 `json:"lat"`
			Lon         float64 `json:"lon"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Status == "success" {
			fetched := GeoInfo{
				Country:     res.Country,
				CountryCode: res.CountryCode,
				City:        res.City,
				Lat:         res.Lat,
				Lon:         res.Lon,
			}
			s.geoMu.Lock()
			if s.geoCache == nil {
				s.geoCache = make(map[string]GeoInfo)
			}
			s.geoCache[targetIP] = fetched
			s.geoMu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO ip_geo_cache (ip, country, country_code, city, lat, lon, updated_at) VALUES ($1, $2, $3, $4, $5, $6, NOW()) ON CONFLICT (ip) DO UPDATE SET country = $2, country_code = $3, city = $4, lat = $5, lon = $6, updated_at = NOW()`,
					targetIP, fetched.Country, fetched.CountryCode, fetched.City, fetched.Lat, fetched.Lon)
			}
		}
	}(ip)

	return info
}

func (s *AppState) recordCounterAsync(name string) {
	if s.db == nil {
		return
	}
	go func() {
		_, _ = s.db.Exec(`INSERT INTO telemetry_counters (name, value) VALUES ($1, 1) ON CONFLICT (name) DO UPDATE SET value = telemetry_counters.value + 1`, name)
	}()
}

func (s *AppState) recordDeviceActivityAsync(deviceID, clientIP string) {
	if s.db == nil || deviceID == "" {
		return
	}
	go func() {
		_, _ = s.db.Exec(`INSERT INTO daily_active_devices (device_id, seen_date, client_ip, last_seen) VALUES ($1, CURRENT_DATE, $2, NOW()) ON CONFLICT (device_id, seen_date) DO UPDATE SET last_seen = NOW(), client_ip = $2`, deviceID, clientIP)
	}()
}

func (s *AppState) initRedis(addr string) {
	s.rdb = redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[REDIS] Notice: Redis not reachable at %s (%v), active sessions will be kept in process RAM only", addr, err)
		s.rdb = nil
	} else {
		log.Printf("[REDIS] Successfully connected to Redis in-memory session store at %s", addr)
		s.loadSessionsFromRedis()
	}
}

func (s *AppState) loadSessionsFromRedis() {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	iter := s.rdb.Scan(ctx, 0, "wl:sess:*", 500).Iterator()
	loaded := 0
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for iter.Next(ctx) {
		key := iter.Val()
		val, err := s.rdb.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var sess SessionInfo
		if err := json.Unmarshal([]byte(val), &sess); err == nil {
			if now.Before(sess.ExpiresAt) && !strings.HasPrefix(sess.Token, "wl_tok_hy2_") {
				s.sessions[sess.Token] = &sess
				s.deviceTokens[sess.DeviceID] = sess.Token
				loaded++
			}
		}
	}
	if loaded > 0 {
		log.Printf("[REDIS] Restored %d active sessions from Redis RAM across restart", loaded)
	}
}

func (s *AppState) saveSessionAsync(sess *SessionInfo) {
	if sess == nil || s.rdb == nil {
		return
	}
	tok := sess.Token
	devID := sess.DeviceID
	exp := sess.ExpiresAt
	ttl := time.Until(exp)
	if ttl <= 0 {
		return
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.rdb.Set(ctx, "wl:sess:"+tok, data, ttl).Err()
		_ = s.rdb.Set(ctx, "wl:dev:"+devID, tok, ttl).Err()
	}()
}

func (s *AppState) deleteSessionFromRedis(token, deviceID string) {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if token != "" && deviceID != "" {
		_ = s.rdb.Del(ctx, "wl:sess:"+token, "wl:dev:"+deviceID).Err()
	} else if token != "" {
		_ = s.rdb.Del(ctx, "wl:sess:"+token).Err()
	} else if deviceID != "" {
		_ = s.rdb.Del(ctx, "wl:dev:"+deviceID).Err()
	}
}

func (s *AppState) loadFeatureSettings() {
	s.featureMu.Lock()
	s.enableDonate = true
	s.enableVoting = true
	s.featureMu.Unlock()

	if s.db == nil {
		return
	}

	rows, err := s.db.Query("SELECT key, value FROM server_settings")
	if err != nil {
		log.Printf("[SETTINGS] Warning: failed to query server_settings: %v", err)
		return
	}
	defer rows.Close()

	s.featureMu.Lock()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			switch k {
			case "enable_donate":
				s.enableDonate = (v == "true" || v == "1")
			case "enable_voting":
				s.enableVoting = (v == "true" || v == "1")
			case "enable_community_goal":
				s.enableCommunityGoal = (v == "true" || v == "1")
			case "donate_amount_rub":
				if amt, err := strconv.Atoi(v); err == nil && amt > 0 {
					s.mu.Lock()
					s.cfg.DonateAmountRub = amt
					s.mu.Unlock()
				}
			case "max_sessions":
				if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
					s.mu.Lock()
					s.cfg.MaxSessions = ms
					s.mu.Unlock()
				}
			case "dedicated_sponsor_slots":
				if ds, err := strconv.Atoi(v); err == nil && ds >= 0 {
					s.mu.Lock()
					s.cfg.DedicatedSponsorSlots = ds
					s.mu.Unlock()
				}
			}
		}
	}
	s.featureMu.Unlock()

	// Load persistent telemetry counters
	cntRows, err := s.db.Query("SELECT name, value FROM telemetry_counters")
	if err == nil {
		defer cntRows.Close()
		for cntRows.Next() {
			var n string
			var v uint64
			if err := cntRows.Scan(&n, &v); err == nil {
				switch n {
				case "invoices_created":
					atomic.StoreUint64(&s.metricInvoicesCreated, v)
				case "donations_paid":
					atomic.StoreUint64(&s.metricDonationsPaid, v)
				case "donations_rub":
					atomic.StoreUint64(&s.metricDonationsRub, v)
				case "rejections_rate_limit":
					atomic.StoreUint64(&s.metricRejectionsRateLimit, v)
				case "rejections_ip_limit":
					atomic.StoreUint64(&s.metricRejectionsIPLimit, v)
				case "rejections_bad_sig":
					atomic.StoreUint64(&s.metricRejectionsBadSig, v)
				case "rejections_capacity":
					atomic.StoreUint64(&s.metricRejectionsCapacity, v)
				case "terminations_user_release":
					atomic.StoreUint64(&s.metricTerminationsUserRelease, v)
				case "terminations_inactivity":
					atomic.StoreUint64(&s.metricTerminationsInactivity, v)
				case "terminations_ttl_expired":
					atomic.StoreUint64(&s.metricTerminationsTTLExpired, v)
				}
			}
		}
	}

	s.mu.RLock()
	dRub := s.cfg.DonateAmountRub
	mSess := s.cfg.MaxSessions
	s.mu.RUnlock()
	log.Printf("[SETTINGS] Loaded live settings: Donate=%v, Voting=%v, DonateAmt=%d, MaxSessions=%d",
		s.enableDonate, s.enableVoting, dRub, mSess)
}

type AdminFeaturesPayload struct {
	EnableDonate        *bool `json:"enable_donate"`
	EnableVoting        *bool `json:"enable_voting"`
	EnableCommunityGoal *bool `json:"enable_community_goal"`
}

func (s *AppState) checkAdminAuth(r *http.Request) bool {
	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" || clientIP == ""
	if isLocal {
		return true
	}

	if s.cfg.DashboardKey == "" {
		return false
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.Header.Get("X-Dashboard-Key")
	}
	if key == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			key = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return key != "" && key == s.cfg.DashboardKey
}

func (s *AppState) handleAdminFeatures(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.featureMu.RLock()
		enDonate := s.enableDonate
		enVoting := s.enableVoting
		enCommunityGoal := s.enableCommunityGoal
		s.featureMu.RUnlock()

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":               true,
			"enable_donate":         enDonate,
			"enable_voting":         enVoting,
			"enable_community_goal": enCommunityGoal,
		})

	case http.MethodPost:
		var req AdminFeaturesPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
			return
		}

		s.featureMu.Lock()
		if req.EnableDonate != nil {
			s.enableDonate = *req.EnableDonate
		}
		if req.EnableVoting != nil {
			s.enableVoting = *req.EnableVoting
		}
		if req.EnableCommunityGoal != nil {
			s.enableCommunityGoal = *req.EnableCommunityGoal
		}
		currentDonate := s.enableDonate
		currentVoting := s.enableVoting
		currentGoal := s.enableCommunityGoal
		s.featureMu.Unlock()

		if s.db != nil {
			if req.EnableDonate != nil {
				val := "false"
				if *req.EnableDonate {
					val = "true"
				}
				_, _ = s.db.Exec(`
					INSERT INTO server_settings (key, value)
					VALUES ('enable_donate', $1)
					ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
				`, val)
			}
			if req.EnableVoting != nil {
				val := "false"
				if *req.EnableVoting {
					val = "true"
				}
				_, _ = s.db.Exec(`
					INSERT INTO server_settings (key, value)
					VALUES ('enable_voting', $1)
					ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
				`, val)
			}
			if req.EnableCommunityGoal != nil {
				val := "false"
				if *req.EnableCommunityGoal {
					val = "true"
				}
				_, _ = s.db.Exec(`
					INSERT INTO server_settings (key, value)
					VALUES ('enable_community_goal', $1)
					ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
				`, val)
			}
		}

		log.Printf("[ADMIN] Dynamic feature toggles updated: Donate=%v, Voting=%v, CommunityGoal=%v", currentDonate, currentVoting, currentGoal)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":               true,
			"enable_donate":         currentDonate,
			"enable_voting":         currentVoting,
			"enable_community_goal": currentGoal,
		})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

type AdminSettingsPayload struct {
	EnableDonate          *bool   `json:"enable_donate,omitempty"`
	EnableVoting          *bool   `json:"enable_voting,omitempty"`
	EnableCommunityGoal   *bool   `json:"enable_community_goal,omitempty"`
	DonateAmountRub       *int    `json:"donate_amount_rub,omitempty"`
	MaxSessions           *int    `json:"max_sessions,omitempty"`
	DedicatedSponsorSlots *int    `json:"dedicated_sponsor_slots,omitempty"`
	FreeSlotsLimit        *int    `json:"free_slots_limit,omitempty"`
	ServerName            *string `json:"server_name,omitempty"`
	ServerLocation        *string `json:"server_location,omitempty"`
	DonationsPaid         *uint64 `json:"donations_paid,omitempty"`
	DonationsRub          *uint64 `json:"donations_rub,omitempty"`
}

func (s *AppState) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Dashboard-Key, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.featureMu.RLock()
		enDonate := s.enableDonate
		enVoting := s.enableVoting
		enCommunityGoal := s.enableCommunityGoal
		s.featureMu.RUnlock()

		s.mu.RLock()
		donateAmt := s.cfg.DonateAmountRub
		maxSess := s.cfg.MaxSessions
		if maxSess <= 0 {
			maxSess = MaxActiveSessions
		}
		dedicatedSponsor := s.cfg.DedicatedSponsorSlots
		if dedicatedSponsor <= 0 {
			dedicatedSponsor = 10
		}
		dedicatedAdmin := 1
		freeSlotsLimit := maxSess - dedicatedSponsor - dedicatedAdmin
		if freeSlotsLimit < 0 {
			freeSlotsLimit = 0
		}
		srvName := s.cfg.ServerName
		srvLoc := s.cfg.ServerLocation
		activeSess := len(s.sessions)
		activeFreeCount := 0
		activeSponsorCount := 0
		for _, sess := range s.sessions {
			if sess.IsSponsor {
				activeSponsorCount++
			} else {
				activeFreeCount++
			}
		}
		s.mu.RUnlock()

		var dau, wau int
		if s.db != nil {
			_ = s.db.QueryRow("SELECT COUNT(*) FROM daily_active_devices WHERE seen_date = CURRENT_DATE").Scan(&dau)
			_ = s.db.QueryRow("SELECT COUNT(DISTINCT device_id) FROM daily_active_devices WHERE seen_date >= CURRENT_DATE - INTERVAL '7 days'").Scan(&wau)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":                 true,
			"enable_donate":           enDonate,
			"enable_voting":           enVoting,
			"enable_community_goal":   enCommunityGoal,
			"donate_amount_rub":       donateAmt,
			"max_sessions":            maxSess,
			"dedicated_sponsor_slots": dedicatedSponsor,
			"free_slots_limit":        freeSlotsLimit,
			"dedicated_admin_slots":   dedicatedAdmin,
			"active_sessions":         activeSess,
			"active_free_sessions":    activeFreeCount,
			"active_sponsor_sessions": activeSponsorCount,
			"server_name":             srvName,
			"server_location":         srvLoc,
			"invoices_created":        atomic.LoadUint64(&s.metricInvoicesCreated),
			"donations_paid":          atomic.LoadUint64(&s.metricDonationsPaid),
			"donations_rub":           atomic.LoadUint64(&s.metricDonationsRub),
			"dau":                     dau,
			"wau":                     wau,
		})

	case http.MethodPost:
		bodyBytes, _ := io.ReadAll(r.Body)
		var req AdminSettingsPayload
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			log.Printf("[ADMIN-SETTINGS] Decode error: %v, body was: %s", err, string(bodyBytes))
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request", "details": err.Error()})
			return
		}

		if req.EnableDonate != nil {
			s.featureMu.Lock()
			s.enableDonate = *req.EnableDonate
			s.featureMu.Unlock()
			if s.db != nil {
				val := "false"
				if *req.EnableDonate {
					val = "true"
				}
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('enable_donate', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
			}
		}

		if req.EnableVoting != nil {
			s.featureMu.Lock()
			s.enableVoting = *req.EnableVoting
			s.featureMu.Unlock()
			if s.db != nil {
				val := "false"
				if *req.EnableVoting {
					val = "true"
				}
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('enable_voting', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
			}
		}

		if req.EnableCommunityGoal != nil {
			s.featureMu.Lock()
			s.enableCommunityGoal = *req.EnableCommunityGoal
			s.featureMu.Unlock()
			if s.db != nil {
				val := "false"
				if *req.EnableCommunityGoal {
					val = "true"
				}
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('enable_community_goal', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
			}
		}

		if req.DonateAmountRub != nil && *req.DonateAmountRub > 0 {
			s.mu.Lock()
			s.cfg.DonateAmountRub = *req.DonateAmountRub
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('donate_amount_rub', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.DonateAmountRub))
			}
		}

		if req.MaxSessions != nil && *req.MaxSessions > 0 {
			s.mu.Lock()
			s.cfg.MaxSessions = *req.MaxSessions
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('max_sessions', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.MaxSessions))
			}
		}

		if req.DedicatedSponsorSlots != nil && *req.DedicatedSponsorSlots >= 0 {
			s.mu.Lock()
			s.cfg.DedicatedSponsorSlots = *req.DedicatedSponsorSlots
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('dedicated_sponsor_slots', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.DedicatedSponsorSlots))
			}
		}

		if req.FreeSlotsLimit != nil && *req.FreeSlotsLimit >= 0 {
			s.mu.Lock()
			maxS := s.cfg.MaxSessions
			if maxS <= 0 {
				maxS = MaxActiveSessions
			}
			newDedicated := maxS - *req.FreeSlotsLimit - 1
			if newDedicated < 0 {
				newDedicated = 0
			}
			s.cfg.DedicatedSponsorSlots = newDedicated
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('dedicated_sponsor_slots', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(newDedicated))
			}
		}

		if req.ServerName != nil && *req.ServerName != "" {
			s.mu.Lock()
			s.cfg.ServerName = *req.ServerName
			s.mu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('server_name', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, *req.ServerName)
			}
		}

		if req.DonationsPaid != nil {
			atomic.StoreUint64(&s.metricDonationsPaid, *req.DonationsPaid)
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO telemetry_counters (name, value) VALUES ('donations_paid', $1) ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, *req.DonationsPaid)
			}
		}

		if req.DonationsRub != nil {
			atomic.StoreUint64(&s.metricDonationsRub, *req.DonationsRub)
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO telemetry_counters (name, value) VALUES ('donations_rub', $1) ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value`, *req.DonationsRub)
			}
		}

		s.featureMu.RLock()
		enDonate := s.enableDonate
		enVoting := s.enableVoting
		enCommunityGoal := s.enableCommunityGoal
		s.featureMu.RUnlock()

		s.mu.RLock()
		donateAmt := s.cfg.DonateAmountRub
		maxSess := s.cfg.MaxSessions
		if maxSess <= 0 {
			maxSess = MaxActiveSessions
		}
		dedicatedSponsor := s.cfg.DedicatedSponsorSlots
		if dedicatedSponsor <= 0 {
			dedicatedSponsor = 10
		}
		dedicatedAdmin := 1
		freeSlotsLimit := maxSess - dedicatedSponsor - dedicatedAdmin
		if freeSlotsLimit < 0 {
			freeSlotsLimit = 0
		}
		srvName := s.cfg.ServerName
		srvLoc := s.cfg.ServerLocation
		activeSess := len(s.sessions)
		activeFreeCount := 0
		activeSponsorCount := 0
		for _, sess := range s.sessions {
			if sess.IsSponsor {
				activeSponsorCount++
			} else {
				activeFreeCount++
			}
		}
		s.mu.RUnlock()

		log.Printf("[ADMIN] Settings updated live: Donate=%v, Voting=%v, CommunityGoal=%v, MaxSessions=%d, DedicatedSponsorSlots=%d, FreeSlotsLimit=%d",
			enDonate, enVoting, enCommunityGoal, maxSess, dedicatedSponsor, freeSlotsLimit)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":                 true,
			"enable_donate":           enDonate,
			"enable_voting":           enVoting,
			"enable_community_goal":   enCommunityGoal,
			"donate_amount_rub":       donateAmt,
			"max_sessions":            maxSess,
			"dedicated_sponsor_slots": dedicatedSponsor,
			"free_slots_limit":        freeSlotsLimit,
			"dedicated_admin_slots":   dedicatedAdmin,
			"active_sessions":         activeSess,
			"active_free_sessions":    activeFreeCount,
			"active_sponsor_sessions": activeSponsorCount,
			"server_name":             srvName,
			"server_location":         srvLoc,
			"donations_paid":          atomic.LoadUint64(&s.metricDonationsPaid),
			"donations_rub":           atomic.LoadUint64(&s.metricDonationsRub),
		})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *AppState) handleMetrics(w http.ResponseWriter, r *http.Request) {
	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" || clientIP == ""
	key := r.URL.Query().Get("key")
	if !isLocal && (s.cfg.DashboardKey == "" || key != s.cfg.DashboardKey) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	s.mu.RLock()
	activeSessions := len(s.sessions)
	maxSessions := s.cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = MaxActiveSessions
	}
	dedicatedSponsor := s.cfg.DedicatedSponsorSlots
	if dedicatedSponsor <= 0 {
		dedicatedSponsor = 10
	}
	freeSlotsLimit := maxSessions - dedicatedSponsor - 1
	if freeSlotsLimit < 0 {
		freeSlotsLimit = 0
	}
	activeFreeSessions := 0
	activeSponsorSessions := 0
	for _, sess := range s.sessions {
		if sess.IsSponsor {
			activeSponsorSessions++
		} else {
			activeFreeSessions++
		}
	}
	due := s.cachedDue
	donateAmt := s.cfg.DonateAmountRub
	s.mu.RUnlock()

	daysLeft := 0
	if !due.IsZero() {
		dur := time.Until(due)
		daysLeft = int(dur.Hours() / 24)
		if daysLeft < 0 {
			daysLeft = 0
		}
	}

	s.featureMu.RLock()
	enDonate := 0
	if s.enableDonate {
		enDonate = 1
	}
	enVoting := 0
	if s.enableVoting {
		enVoting = 1
	}
	enCommunityGoal := 0
	if s.enableCommunityGoal {
		enCommunityGoal = 1
	}
	s.featureMu.RUnlock()

	var totalVotes, gamesVoting, gamesGraduated int
	var dau, wau int
	type gameVoteMetric struct {
		title  string
		status string
		votes  int
	}
	var gameVoteList []gameVoteMetric
	if s.db != nil {
		_ = s.db.QueryRow("SELECT COALESCE(SUM(votes_count), 0) FROM game_suggestions").Scan(&totalVotes)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM game_suggestions WHERE status = 'voting'").Scan(&gamesVoting)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM game_suggestions WHERE status = 'queue_integration'").Scan(&gamesGraduated)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM daily_active_devices WHERE seen_date = CURRENT_DATE").Scan(&dau)
		_ = s.db.QueryRow("SELECT COUNT(DISTINCT device_id) FROM daily_active_devices WHERE seen_date >= CURRENT_DATE - INTERVAL '7 days'").Scan(&wau)

		if vRows, err := s.db.Query("SELECT title, status, votes_count FROM game_suggestions ORDER BY votes_count DESC LIMIT 10"); err == nil {
			defer vRows.Close()
			for vRows.Next() {
				var gvm gameVoteMetric
				if err := vRows.Scan(&gvm.title, &gvm.status, &gvm.votes); err == nil {
					gameVoteList = append(gameVoteList, gvm)
				}
			}
		}
	}

	s.mu.RLock()
	sessionsByGame := map[string]int{
		"wardogs":          0,
		"wardogs (гибрид)": 0,
		"free_internet":    0,
	}
	geoCounts := make(map[GeoInfo]int)
	for _, sess := range s.sessions {
		g := sess.Game
		if g == "" {
			g = "wardogs"
		}
		sessionsByGame[g]++
		geo := s.resolveIPGeo(sess.ClientIP)
		geoCounts[geo]++
	}
	srvPrice := 1450
	if s.cachedPrice > 0 {
		srvPrice = s.cachedPrice
	}
	s.mu.RUnlock()

	s.loadMu.RLock()
	live := s.latestLoad
	s.loadMu.RUnlock()
	if live == nil {
		live = s.sampleLoad()
	}

	var sb strings.Builder
	sb.WriteString("# HELP warlink_active_sessions Active game sessions\n")
	sb.WriteString("# TYPE warlink_active_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_active_sessions %d\n\n", activeSessions))

	sb.WriteString("# HELP warlink_max_sessions Maximum configured sessions limit\n")
	sb.WriteString("# TYPE warlink_max_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_max_sessions %d\n\n", maxSessions))

	sb.WriteString("# HELP warlink_dedicated_sponsor_slots Dedicated sponsor slots reservation\n")
	sb.WriteString("# TYPE warlink_dedicated_sponsor_slots gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_dedicated_sponsor_slots %d\n\n", dedicatedSponsor))

	sb.WriteString("# HELP warlink_free_slots_limit Free sessions limit\n")
	sb.WriteString("# TYPE warlink_free_slots_limit gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_free_slots_limit %d\n\n", freeSlotsLimit))

	sb.WriteString("# HELP warlink_active_free_sessions Active free players sessions\n")
	sb.WriteString("# TYPE warlink_active_free_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_active_free_sessions %d\n\n", activeFreeSessions))

	sb.WriteString("# HELP warlink_active_sponsor_sessions Active sponsor sessions\n")
	sb.WriteString("# TYPE warlink_active_sponsor_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_active_sponsor_sessions %d\n\n", activeSponsorSessions))

	sb.WriteString("# HELP warlink_server_days_left Days remaining until server rent expiration\n")
	sb.WriteString("# TYPE warlink_server_days_left gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_days_left %d\n\n", daysLeft))

	sb.WriteString("# HELP warlink_donate_amount_rub Current donate amount in rubles\n")
	sb.WriteString("# TYPE warlink_donate_amount_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_donate_amount_rub %d\n\n", donateAmt))

	sb.WriteString("# HELP warlink_server_monthly_price_rub Monthly VPS rental price in rubles\n")
	sb.WriteString("# TYPE warlink_server_monthly_price_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_monthly_price_rub %d\n\n", srvPrice))

	aezaBalRub := atomic.LoadUint64(&s.metricAezaBalanceRub)
	aezaBonusRub := atomic.LoadUint64(&s.metricAezaBonusRub)
	aezaBalEurCents := atomic.LoadUint64(&s.metricAezaBalanceEurCents)
	aezaBonusEurCents := atomic.LoadUint64(&s.metricAezaBonusEurCents)

	sb.WriteString("# HELP warlink_aeza_balance_rub Current Aeza primary balance in rubles\n")
	sb.WriteString("# TYPE warlink_aeza_balance_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_aeza_balance_rub %d\n\n", aezaBalRub))

	sb.WriteString("# HELP warlink_aeza_bonus_rub Current Aeza bonus balance in rubles\n")
	sb.WriteString("# TYPE warlink_aeza_bonus_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_aeza_bonus_rub %d\n\n", aezaBonusRub))

	sb.WriteString("# HELP warlink_aeza_balance_eur_cents Current Aeza primary balance in EUR cents\n")
	sb.WriteString("# TYPE warlink_aeza_balance_eur_cents gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_aeza_balance_eur_cents %d\n\n", aezaBalEurCents))

	sb.WriteString("# HELP warlink_aeza_bonus_eur_cents Current Aeza bonus balance in EUR cents\n")
	sb.WriteString("# TYPE warlink_aeza_bonus_eur_cents gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_aeza_bonus_eur_cents %d\n\n", aezaBonusEurCents))

	sb.WriteString("# HELP warlink_server_version Server software release version info\n")
	sb.WriteString("# TYPE warlink_server_version gauge\n")
	sb.WriteString("warlink_server_version{version=\"v2.1.6\"} 1\n\n")

	clusterBudgetEur := 10
	clusterBudgetRub := 1300
	stockholmCostEur := 2
	stockholmCostRub := 260
	frankfurtCostEur := 6
	frankfurtCostRub := 780
	reserveCostEur := 2
	reserveCostRub := 260

	sb.WriteString("# HELP warlink_cluster_budget_eur Monthly cluster maintenance budget in EUR\n")
	sb.WriteString("# TYPE warlink_cluster_budget_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_budget_eur %d\n\n", clusterBudgetEur))

	sb.WriteString("# HELP warlink_cluster_budget_rub Monthly cluster maintenance budget in RUB\n")
	sb.WriteString("# TYPE warlink_cluster_budget_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_budget_rub %d\n\n", clusterBudgetRub))

	sb.WriteString("# HELP warlink_stockholm_cost_eur Stockholm node monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_stockholm_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_stockholm_cost_eur %d\n\n", stockholmCostEur))

	sb.WriteString("# HELP warlink_frankfurt_cost_eur Frankfurt node monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_frankfurt_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_frankfurt_cost_eur %d\n\n", frankfurtCostEur))

	sb.WriteString("# HELP warlink_frankfurt_cost_rub Frankfurt node monthly cost in RUB\n")
	sb.WriteString("# TYPE warlink_frankfurt_cost_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_frankfurt_cost_rub %d\n\n", frankfurtCostRub))

	sb.WriteString("# HELP warlink_reserve_cost_eur Cluster reserve monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_reserve_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_reserve_cost_eur %d\n\n", reserveCostEur))

	sb.WriteString("# HELP warlink_reserve_cost_rub Cluster reserve monthly cost in RUB\n")
	sb.WriteString("# TYPE warlink_reserve_cost_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_reserve_cost_rub %d\n\n", reserveCostRub))

	stockholmDailyPrice := float64(stockholmCostRub) / 30.0
	prepaidStockholmRub := float64(daysLeft) * stockholmDailyPrice
	totalAvailableRub := prepaidStockholmRub + float64(aezaBalRub+aezaBonusRub)

	clusterCoveragePercent := (totalAvailableRub / float64(clusterBudgetRub)) * 100.0
	donationsRubTotal := atomic.LoadUint64(&s.metricDonationsRub)
	donationsGoalPercent := (float64(donationsRubTotal) / float64(clusterBudgetRub)) * 100.0

	clusterDailyPrice := float64(clusterBudgetRub) / 30.0
	totalRunwayDays := totalAvailableRub / clusterDailyPrice

	sb.WriteString("# HELP warlink_server_runway_days Total days of cluster runway at 10 EUR / mo\n")
	sb.WriteString("# TYPE warlink_server_runway_days gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_runway_days %.1f\n\n", totalRunwayDays))

	sb.WriteString("# HELP warlink_server_coverage_percent Financial runway coverage percentage\n")
	sb.WriteString("# TYPE warlink_server_coverage_percent gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_coverage_percent %.1f\n\n", clusterCoveragePercent))

	sb.WriteString("# HELP warlink_cluster_coverage_percent Financial cluster coverage percentage (10 EUR target)\n")
	sb.WriteString("# TYPE warlink_cluster_coverage_percent gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_coverage_percent %.1f\n\n", clusterCoveragePercent))

	sb.WriteString("# HELP warlink_donations_goal_percent Community donations progress towards 10 EUR target\n")
	sb.WriteString("# TYPE warlink_donations_goal_percent gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_donations_goal_percent %.1f\n\n", donationsGoalPercent))

	sb.WriteString("# HELP warlink_invoices_created_total Total invoices created via Aeza\n")
	sb.WriteString("# TYPE warlink_invoices_created_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_invoices_created_total %d\n\n", atomic.LoadUint64(&s.metricInvoicesCreated)))

	sb.WriteString("# HELP warlink_donations_paid_total Total successful donations paid\n")
	sb.WriteString("# TYPE warlink_donations_paid_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_donations_paid_total %d\n\n", atomic.LoadUint64(&s.metricDonationsPaid)))

	sb.WriteString("# HELP warlink_donations_rub_total Total donation volume collected in rubles\n")
	sb.WriteString("# TYPE warlink_donations_rub_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_donations_rub_total %d\n\n", atomic.LoadUint64(&s.metricDonationsRub)))

	sb.WriteString("# HELP warlink_daily_active_users Unique active devices today (DAU)\n")
	sb.WriteString("# TYPE warlink_daily_active_users gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_daily_active_users %d\n\n", dau))

	sb.WriteString("# HELP warlink_weekly_active_users Unique active devices past 7 days (WAU)\n")
	sb.WriteString("# TYPE warlink_weekly_active_users gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_weekly_active_users %d\n\n", wau))

	sb.WriteString("# HELP warlink_session_requests_total Total session connection attempts\n")
	sb.WriteString("# TYPE warlink_session_requests_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_session_requests_total %d\n\n", atomic.LoadUint64(&s.metricRequestsTotal)))

	sb.WriteString("# HELP warlink_session_rejections_total Total rejected sessions by reason\n")
	sb.WriteString("# TYPE warlink_session_rejections_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_session_rejections_total{reason=\"rate_limit\"} %d\n", atomic.LoadUint64(&s.metricRejectionsRateLimit)))
	sb.WriteString(fmt.Sprintf("warlink_session_rejections_total{reason=\"ip_limit\"} %d\n", atomic.LoadUint64(&s.metricRejectionsIPLimit)))
	sb.WriteString(fmt.Sprintf("warlink_session_rejections_total{reason=\"bad_signature\"} %d\n", atomic.LoadUint64(&s.metricRejectionsBadSig)))
	sb.WriteString(fmt.Sprintf("warlink_session_rejections_total{reason=\"capacity\"} %d\n\n", atomic.LoadUint64(&s.metricRejectionsCapacity)))

	sb.WriteString("# HELP warlink_session_terminations_total Total session terminations by reason\n")
	sb.WriteString("# TYPE warlink_session_terminations_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_session_terminations_total{reason=\"user_release\"} %d\n", atomic.LoadUint64(&s.metricTerminationsUserRelease)))
	sb.WriteString(fmt.Sprintf("warlink_session_terminations_total{reason=\"inactivity\"} %d\n", atomic.LoadUint64(&s.metricTerminationsInactivity)))
	sb.WriteString(fmt.Sprintf("warlink_session_terminations_total{reason=\"ttl_expired\"} %d\n\n", atomic.LoadUint64(&s.metricTerminationsTTLExpired)))

	sb.WriteString("# HELP warlink_sessions_by_game Number of active sessions per game\n")
	sb.WriteString("# TYPE warlink_sessions_by_game gauge\n")
	if len(sessionsByGame) == 0 {
		sb.WriteString("warlink_sessions_by_game{game=\"wardogs\"} 0\n\n")
	} else {
		for g, cnt := range sessionsByGame {
			sb.WriteString(fmt.Sprintf("warlink_sessions_by_game{game=\"%s\"} %d\n", g, cnt))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("# HELP warlink_enable_donate Donate feature toggle\n")
	sb.WriteString("# TYPE warlink_enable_donate gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_enable_donate %d\n\n", enDonate))

	sb.WriteString("# HELP warlink_enable_voting Community voting feature toggle\n")
	sb.WriteString("# TYPE warlink_enable_voting gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_enable_voting %d\n\n", enVoting))

	sb.WriteString("# HELP warlink_enable_community_goal Community goal progress display feature toggle\n")
	sb.WriteString("# TYPE warlink_enable_community_goal gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_enable_community_goal %d\n\n", enCommunityGoal))

	sb.WriteString("# HELP warlink_total_votes Total community votes cast\n")
	sb.WriteString("# TYPE warlink_total_votes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_total_votes %d\n\n", totalVotes))

	sb.WriteString("# HELP warlink_games_in_voting Games currently in voting\n")
	sb.WriteString("# TYPE warlink_games_in_voting gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_games_in_voting %d\n\n", gamesVoting))

	sb.WriteString("# HELP warlink_games_graduated Games that won voting\n")
	sb.WriteString("# TYPE warlink_games_graduated gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_games_graduated %d\n\n", gamesGraduated))

	sb.WriteString("# HELP warlink_game_votes Number of community votes per game\n")
	sb.WriteString("# TYPE warlink_game_votes gauge\n")
	for _, gvm := range gameVoteList {
		cleanTitle := strings.ReplaceAll(gvm.title, "\"", "'")
		sb.WriteString(fmt.Sprintf("warlink_game_votes{title=\"%s\",status=\"%s\"} %d\n", cleanTitle, gvm.status, gvm.votes))
	}
	sb.WriteString("\n")

	sb.WriteString("# HELP process_start_time_seconds Start time of the process since unix epoch in seconds\n")
	sb.WriteString("# TYPE process_start_time_seconds gauge\n")
	sb.WriteString(fmt.Sprintf("process_start_time_seconds %d\n\n", s.startTime.Unix()))

	sb.WriteString("# HELP warlink_active_sessions_by_geo Active sessions grouped by geographic region\n")
	sb.WriteString("# TYPE warlink_active_sessions_by_geo gauge\n")
	if len(geoCounts) == 0 {
		sb.WriteString("warlink_active_sessions_by_geo{country=\"Unknown\",country_code=\"XX\",city=\"Unknown\"} 0\n\n")
	} else {
		for gk, cnt := range geoCounts {
			sb.WriteString(fmt.Sprintf("warlink_active_sessions_by_geo{country=\"%s\",country_code=\"%s\",city=\"%s\"} %d\n",
				gk.Country, gk.CountryCode, gk.City, cnt))
		}
		sb.WriteString("\n")
	}

	if live != nil {
		sb.WriteString("# HELP warlink_cpu_percent Server CPU usage percent\n")
		sb.WriteString("# TYPE warlink_cpu_percent gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_cpu_percent %.2f\n\n", live.CPUPercent))

		sb.WriteString("# HELP warlink_ram_used_mb Server RAM used in MB\n")
		sb.WriteString("# TYPE warlink_ram_used_mb gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_ram_used_mb %d\n\n", live.RAMUsedMB))

		sb.WriteString("# HELP warlink_ram_total_mb Server RAM total in MB\n")
		sb.WriteString("# TYPE warlink_ram_total_mb gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_ram_total_mb %d\n\n", live.RAMTotalMB))

		sb.WriteString("# HELP warlink_net_rx_rate_kbps Inbound network bitrate in kbps\n")
		sb.WriteString("# TYPE warlink_net_rx_rate_kbps gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_net_rx_rate_kbps %d\n\n", live.NetRxRateKbps))

		sb.WriteString("# HELP warlink_net_tx_rate_kbps Outbound network bitrate in kbps\n")
		sb.WriteString("# TYPE warlink_net_tx_rate_kbps gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_net_tx_rate_kbps %d\n\n", live.NetTxRateKbps))

		sb.WriteString("# HELP warlink_gateway_ping_ms Estimated ping to gateway in ms\n")
		sb.WriteString("# TYPE warlink_gateway_ping_ms gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_ping_ms %d\n\n", live.GatewayPingMs))
	}

	// Scrape Hysteria 2 trafficStats from 127.0.0.1:9090/traffic
	var hyTx, hyRx uint64
	hyUsers := 0
	hyClient := &http.Client{Timeout: 500 * time.Millisecond}
	if hyResp, err := hyClient.Get("http://127.0.0.1:9090/traffic"); err == nil {
		var hyData map[string]struct {
			Tx uint64 `json:"tx"`
			Rx uint64 `json:"rx"`
		}
		if err := json.NewDecoder(hyResp.Body).Decode(&hyData); err == nil {
			hyUsers = len(hyData)
			for _, v := range hyData {
				hyTx += v.Tx
				hyRx += v.Rx
			}
		}
		hyResp.Body.Close()
	}

	sb.WriteString("# HELP hysteria_online_users Number of online Hysteria users\n")
	sb.WriteString("# TYPE hysteria_online_users gauge\n")
	sb.WriteString(fmt.Sprintf("hysteria_online_users %d\n\n", hyUsers))

	sb.WriteString("# HELP hysteria_traffic_tx_bytes_total Total bytes sent through Hysteria\n")
	sb.WriteString("# TYPE hysteria_traffic_tx_bytes_total counter\n")
	sb.WriteString(fmt.Sprintf("hysteria_traffic_tx_bytes_total %d\n\n", hyTx))

	sb.WriteString("# HELP hysteria_traffic_rx_bytes_total Total bytes received through Hysteria\n")
	sb.WriteString("# TYPE hysteria_traffic_rx_bytes_total counter\n")
	sb.WriteString(fmt.Sprintf("hysteria_traffic_rx_bytes_total %d\n\n", hyRx))

	w.Write([]byte(sb.String()))
}

func (s *AppState) sampleLoad() *LoadSnapshot {
	now := time.Now()

	// 1. CPU load via /proc/stat
	var cpuPercent float64
	if statFile, err := os.Open("/proc/stat"); err == nil {
		scanner := bufio.NewScanner(statFile)
		if scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 5 && fields[0] == "cpu" {
				var user, nice, sys, idle, iowait, irq, softirq, steal uint64
				user, _ = strconv.ParseUint(fields[1], 10, 64)
				nice, _ = strconv.ParseUint(fields[2], 10, 64)
				sys, _ = strconv.ParseUint(fields[3], 10, 64)
				idle, _ = strconv.ParseUint(fields[4], 10, 64)
				if len(fields) > 5 {
					iowait, _ = strconv.ParseUint(fields[5], 10, 64)
				}
				if len(fields) > 6 {
					irq, _ = strconv.ParseUint(fields[6], 10, 64)
				}
				if len(fields) > 7 {
					softirq, _ = strconv.ParseUint(fields[7], 10, 64)
				}
				if len(fields) > 8 {
					steal, _ = strconv.ParseUint(fields[8], 10, 64)
				}

				total := user + nice + sys + idle + iowait + irq + softirq + steal
				idleTotal := idle + iowait

				s.loadMu.Lock()
				if s.prevCPUTotal > 0 && total > s.prevCPUTotal {
					deltaTotal := total - s.prevCPUTotal
					deltaIdle := idleTotal - s.prevCPUIdle
					if deltaTotal > 0 && deltaTotal >= deltaIdle {
						cpuPercent = float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
					}
				}
				s.prevCPUTotal = total
				s.prevCPUIdle = idleTotal
				s.loadMu.Unlock()
			}
		}
		statFile.Close()
	}

	// 2. RAM via /proc/meminfo
	var ramTotalMB, ramUsedMB int
	if memFile, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(memFile)
		var totalKB, availKB int
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					totalKB, _ = strconv.Atoi(f[1])
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					availKB, _ = strconv.Atoi(f[1])
				}
			}
		}
		memFile.Close()
		ramTotalMB = totalKB / 1024
		if totalKB > availKB {
			ramUsedMB = (totalKB - availKB) / 1024
		}
	}

	// 3. Network traffic via /proc/net/dev (interface net0)
	var rxBytes, txBytes int64
	var rxRateKbps, txRateKbps int
	if netFile, err := os.Open("/proc/net/dev"); err == nil {
		scanner := bufio.NewScanner(netFile)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "net0:") || (strings.Contains(line, ":") && !strings.HasPrefix(line, "lo:") && rxBytes == 0) {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					fields := strings.Fields(parts[1])
					if len(fields) >= 9 {
						rxBytes, _ = strconv.ParseInt(fields[0], 10, 64)
						txBytes, _ = strconv.ParseInt(fields[8], 10, 64)
						break
					}
				}
			}
		}
		netFile.Close()

		s.loadMu.Lock()
		if !s.prevNetTime.IsZero() && rxBytes >= s.prevNetRx && txBytes >= s.prevNetTx {
			elapsed := now.Sub(s.prevNetTime).Seconds()
			if elapsed > 0 {
				rxRateKbps = int(float64(rxBytes-s.prevNetRx) * 8 / elapsed / 1000)
				txRateKbps = int(float64(txBytes-s.prevNetTx) * 8 / elapsed / 1000)
			}
		}
		s.prevNetRx = rxBytes
		s.prevNetTx = txBytes
		s.prevNetTime = now
		s.loadMu.Unlock()
	}

	// 4. Active sessions
	s.mu.RLock()
	activeSess := 0
	for _, sess := range s.sessions {
		if now.Before(sess.ExpiresAt) {
			activeSess++
		}
	}
	s.mu.RUnlock()

	snap := &LoadSnapshot{
		RecordedAt:     now,
		ActiveSessions: activeSess,
		MaxSessions:    MaxActiveSessions,
		CPUPercent:     cpuPercent,
		RAMUsedMB:      ramUsedMB,
		RAMTotalMB:     ramTotalMB,
		NetBytesRecv:   rxBytes,
		NetBytesSent:   txBytes,
		NetRxRateKbps:  rxRateKbps,
		NetTxRateKbps:  txRateKbps,
		GatewayPingMs:  27,
	}

	s.loadMu.Lock()
	s.latestLoad = snap
	s.loadMu.Unlock()

	return snap
}

func (s *AppState) startAnalyticsCollector() {
	// Initial baseline sample
	s.sampleLoad()

	// Initial insert
	time.Sleep(1 * time.Second)
	snap := s.sampleLoad()
	if s.db != nil {
		_, _ = s.db.Exec(`
			INSERT INTO server_load_history (
				recorded_at, active_sessions, max_sessions, cpu_percent,
				ram_used_mb, ram_total_mb, net_bytes_recv, net_bytes_sent,
				net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, snap.RecordedAt, snap.ActiveSessions, snap.MaxSessions, snap.CPUPercent,
			snap.RAMUsedMB, snap.RAMTotalMB, snap.NetBytesRecv, snap.NetBytesSent,
			snap.NetRxRateKbps, snap.NetTxRateKbps, snap.GatewayPingMs)
	}

	ticker := time.NewTicker(1 * time.Minute)
	cleanTicker := time.NewTicker(24 * time.Hour)

	for {
		select {
		case <-ticker.C:
			snap := s.sampleLoad()
			if s.db != nil {
				_, err := s.db.Exec(`
					INSERT INTO server_load_history (
						recorded_at, active_sessions, max_sessions, cpu_percent,
						ram_used_mb, ram_total_mb, net_bytes_recv, net_bytes_sent,
						net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`, snap.RecordedAt, snap.ActiveSessions, snap.MaxSessions, snap.CPUPercent,
					snap.RAMUsedMB, snap.RAMTotalMB, snap.NetBytesRecv, snap.NetBytesSent,
					snap.NetRxRateKbps, snap.NetTxRateKbps, snap.GatewayPingMs)
				if err != nil {
					log.Printf("[ANALYTICS] Error saving load snapshot: %v", err)
				}
			}
		case <-cleanTicker.C:
			if s.db != nil {
				_, _ = s.db.Exec("DELETE FROM server_load_history WHERE recorded_at < NOW() - INTERVAL '90 days'")
			}
		}
	}
}

type GitHubReleaseItem struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	PublishedMs int64  `json:"published_ms"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
}

var (
	ghReleasesCache     []GitHubReleaseItem
	ghReleasesCacheTime time.Time
	ghReleasesMu        sync.Mutex
)

func (s *AppState) handleReleases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ghReleasesMu.Lock()
	defer ghReleasesMu.Unlock()

	if len(ghReleasesCache) > 0 && time.Since(ghReleasesCacheTime) < 10*time.Minute {
		_ = json.NewEncoder(w).Encode(ghReleasesCache)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com/repos/max-alekseyev/WarLink/releases?per_page=15", nil)
	if err == nil {
		req.Header.Set("User-Agent", "WarLink-Server")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			var raw []struct {
				TagName     string `json:"tag_name"`
				Name        string `json:"name"`
				PublishedAt string `json:"published_at"`
				Body        string `json:"body"`
				HTMLURL     string `json:"html_url"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&raw); err == nil {
				resp.Body.Close()
				var items []GitHubReleaseItem
				for _, it := range raw {
					t, _ := time.Parse(time.RFC3339, it.PublishedAt)
					ms := t.UnixMilli()
					body := strings.TrimSpace(it.Body)
					if len(body) > 300 {
						body = body[:300] + "..."
					}
					name := it.Name
					if name == "" {
						name = it.TagName
					}
					items = append(items, GitHubReleaseItem{
						Tag:         it.TagName,
						Name:        name,
						PublishedAt: it.PublishedAt,
						PublishedMs: ms,
						Body:        body,
						HTMLURL:     it.HTMLURL,
					})
				}
				ghReleasesCache = items
				ghReleasesCacheTime = time.Now()
				_ = json.NewEncoder(w).Encode(items)
				return
			}
			resp.Body.Close()
		}
	}

	if len(ghReleasesCache) > 0 {
		_ = json.NewEncoder(w).Encode(ghReleasesCache)
		return
	}

	_ = json.NewEncoder(w).Encode([]GitHubReleaseItem{
		{Tag: "v2.1.6", Name: "v2.1.6", PublishedAt: "2026-09-27T08:00:00Z", PublishedMs: 1790496000000, Body: "Anonymous Accounts, In-App Notifications, Dedicated Sponsor Slots, Profile Customization", HTMLURL: "https://github.com/max-alekseyev/WarLink/releases"},
		{Tag: "v2.0.6", Name: "v2.0.6", PublishedAt: "2026-09-27T02:00:00Z", PublishedMs: 1790474400000, Body: "Dark Cloudflare UI, Adaptive Engine, Zapret 2", HTMLURL: "https://github.com/max-alekseyev/WarLink/releases"},
	})
}

func (s *AppState) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" || clientIP == ""
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.Header.Get("X-Dashboard-Key")
	}
	if key == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			key = strings.TrimPrefix(auth, "Bearer ")
		}
	}

	if !isLocal && (s.cfg.DashboardKey == "" || key != s.cfg.DashboardKey) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	limit := 60
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	if hStr := r.URL.Query().Get("hours"); hStr != "" {
		if h, err := strconv.Atoi(hStr); err == nil && h > 0 {
			limit = h * 60
		}
	}
	if limit > 10080 {
		limit = 10080
	}

	s.loadMu.RLock()
	live := s.latestLoad
	s.loadMu.RUnlock()
	if live == nil {
		live = s.sampleLoad()
	}

	var history []LoadSnapshot
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT id, recorded_at, active_sessions, max_sessions, cpu_percent,
			       ram_used_mb, ram_total_mb, net_bytes_recv, net_bytes_sent,
			       net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms
			FROM server_load_history
			ORDER BY recorded_at DESC
			LIMIT $1
		`, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item LoadSnapshot
				if err := rows.Scan(
					&item.ID, &item.RecordedAt, &item.ActiveSessions, &item.MaxSessions,
					&item.CPUPercent, &item.RAMUsedMB, &item.RAMTotalMB,
					&item.NetBytesRecv, &item.NetBytesSent,
					&item.NetRxRateKbps, &item.NetTxRateKbps, &item.GatewayPingMs,
				); err == nil {
					history = append(history, item)
				}
			}
		}
	}

	// Reverse to chronological order (oldest to newest)
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	type ActivePlayerInfo struct {
		DeviceID     string  `json:"device_id"`
		Game         string  `json:"game"`
		ClientIP     string  `json:"client_ip"`
		ConnectedAt  string  `json:"connected_at"`
		DurationSec  int     `json:"duration_sec"`
		DurationDesc string  `json:"duration_desc"`
		Status       string  `json:"status"`
		Country      string  `json:"country"`
		City         string  `json:"city"`
		Lat          float64 `json:"lat"`
		Lon          float64 `json:"lon"`
	}

	type GeoPointInfo struct {
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		City        string  `json:"city"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		Count       int     `json:"count"`
	}

	activePlayers := make([]ActivePlayerInfo, 0)
	geoAgg := make(map[GeoInfo]int)
	now := time.Now()
	s.mu.RLock()
	for _, sess := range s.sessions {
		if now.Before(sess.ExpiresAt) {
			dur := int(now.Sub(sess.CreatedAt).Seconds())
			desc := fmt.Sprintf("%d мин", dur/60)
			if dur >= 3600 {
				desc = fmt.Sprintf("%d ч %d мин", dur/3600, (dur%3600)/60)
			} else if dur < 60 {
				desc = fmt.Sprintf("%d сек", dur)
			}

			devID := sess.DeviceID
			if len(devID) > 12 {
				devID = devID[:8] + "..." + devID[len(devID)-4:]
			}
			gameName := sess.Game
			if gameName == "" || gameName == "wardogs" {
				gameName = "WARDOGS"
			} else if gameName == "free_internet" {
				gameName = "Свободный интернет"
			}

			geo := s.resolveIPGeo(sess.ClientIP)
			geoAgg[geo]++

			activePlayers = append(activePlayers, ActivePlayerInfo{
				DeviceID:     devID,
				Game:         gameName,
				ClientIP:     sess.ClientIP,
				ConnectedAt:  sess.CreatedAt.Format(time.RFC3339),
				DurationSec:  dur,
				DurationDesc: desc,
				Status:       "АКТИВНА",
				Country:      geo.Country,
				City:         geo.City,
				Lat:          geo.Lat,
				Lon:          geo.Lon,
			})
		}
	}
	s.mu.RUnlock()

	geoPoints := make([]GeoPointInfo, 0)
	for gi, cnt := range geoAgg {
		geoPoints = append(geoPoints, GeoPointInfo{
			Country:     gi.Country,
			CountryCode: gi.CountryCode,
			City:        gi.City,
			Lat:         gi.Lat,
			Lon:         gi.Lon,
			Count:       cnt,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"live":           live,
		"history":        history,
		"active_players": activePlayers,
		"geo_points":     geoPoints,
	})
}

func resizeImage(src image.Image, targetWidth, targetHeight int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if srcW <= 0 || srcH <= 0 {
		return dst
	}

	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := srcBounds.Min.X + (x*srcW)/targetWidth
			srcY := srcBounds.Min.Y + (y*srcH)/targetHeight
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func (s *AppState) handleNotifications(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.db == nil {
		_ = json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	account := strings.TrimSpace(r.URL.Query().Get("account"))
	device := strings.TrimSpace(r.URL.Query().Get("device"))

	rows, err := s.db.Query(`
		SELECT n.id, n.target_type, n.title, n.message, n.severity, n.action_label, n.action_url,
		       TO_CHAR(n.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       EXISTS (
		           SELECT 1 FROM notification_reads nr 
		           WHERE nr.notification_id = n.id AND (nr.reader_id = $1 OR (nr.reader_id = $2 AND $2 != ''))
		       ) AS is_read
		FROM in_app_notifications n
		WHERE n.target_type = 'broadcast' 
		   OR (n.target_type = 'account' AND n.target_id = $1 AND $1 != '')
		   OR (n.target_type = 'device' AND n.target_id = $2 AND $2 != '')
		ORDER BY n.created_at DESC
		LIMIT 50
	`, account, device)
	if err != nil {
		log.Printf("[NOTIFICATIONS] Query error: %v", err)
		_ = json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	type NotifItem struct {
		ID          int64  `json:"id"`
		TargetType  string `json:"target_type"`
		Title       string `json:"title"`
		Message     string `json:"message"`
		Severity    string `json:"severity"`
		ActionLabel string `json:"action_label,omitempty"`
		ActionURL   string `json:"action_url,omitempty"`
		CreatedAt   string `json:"created_at"`
		IsRead      bool   `json:"is_read"`
	}

	items := make([]NotifItem, 0)
	for rows.Next() {
		var it NotifItem
		var actLabel, actURL sql.NullString
		if err := rows.Scan(&it.ID, &it.TargetType, &it.Title, &it.Message, &it.Severity, &actLabel, &actURL, &it.CreatedAt, &it.IsRead); err == nil {
			if actLabel.Valid {
				it.ActionLabel = actLabel.String
			}
			if actURL.Valid {
				it.ActionURL = actURL.String
			}
			items = append(items, it)
		}
	}
	_ = json.NewEncoder(w).Encode(items)
}

func (s *AppState) handleNotificationRead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NotificationID int64  `json:"notification_id"`
		AccountNumber  string `json:"account_number"`
		DeviceID       string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NotificationID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "bad_request"})
		return
	}

	readerID := strings.TrimSpace(req.AccountNumber)
	if readerID == "" {
		readerID = strings.TrimSpace(req.DeviceID)
	}
	if readerID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing_reader_id"})
		return
	}

	if s.db != nil {
		_, _ = s.db.Exec(`
			INSERT INTO notification_reads (notification_id, reader_id, read_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (notification_id, reader_id) DO NOTHING
		`, req.NotificationID, readerID)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *AppState) handleAdminNotifications(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Dashboard-Key, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		type AdminNotifItem struct {
			ID          int64  `json:"id"`
			TargetType  string `json:"target_type"`
			TargetID    string `json:"target_id"`
			Title       string `json:"title"`
			Message     string `json:"message"`
			Severity    string `json:"severity"`
			ActionLabel string `json:"action_label"`
			ActionURL   string `json:"action_url"`
			CreatedAt   string `json:"created_at"`
		}
		items := make([]AdminNotifItem, 0)
		if s.db != nil {
			rows, err := s.db.Query(`
				SELECT id, target_type, target_id, title, message, severity, COALESCE(action_label, ''), COALESCE(action_url, ''), TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
				FROM in_app_notifications
				ORDER BY created_at DESC
				LIMIT 30
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var item AdminNotifItem
					if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.Title, &item.Message, &item.Severity, &item.ActionLabel, &item.ActionURL, &item.CreatedAt); err == nil {
						items = append(items, item)
					}
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"notifications": items,
		})

	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			var dReq struct {
				ID int64 `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&dReq)
			if dReq.ID > 0 {
				idStr = strconv.FormatInt(dReq.ID, 10)
			}
		}
		if s.db != nil && idStr != "" {
			_, _ = s.db.Exec(`DELETE FROM in_app_notifications WHERE id = $1`, idStr)
			_, _ = s.db.Exec(`DELETE FROM notification_reads WHERE notification_id = $1`, idStr)
		}
		log.Printf("[ADMIN-NOTIF] Deleted notification #%s", idStr)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})

	case http.MethodPost:
		var req struct {
			Title       string `json:"title"`
			Message     string `json:"message"`
			Severity    string `json:"severity"`     // info | update | warning | urgent
			TargetType  string `json:"target_type"`  // broadcast | account | device
			TargetID    string `json:"target_id"`
			ActionLabel string `json:"action_label"`
			ActionURL   string `json:"action_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Message) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "title_and_message_required"})
			return
		}

		sev := strings.ToLower(strings.TrimSpace(req.Severity))
		if sev != "info" && sev != "update" && sev != "warning" && sev != "urgent" {
			sev = "info"
		}
		ttype := strings.ToLower(strings.TrimSpace(req.TargetType))
		if ttype != "broadcast" && ttype != "account" && ttype != "device" {
			ttype = "broadcast"
		}

		title := strings.TrimSpace(req.Title)
		if len(title) > 120 {
			title = title[:120]
		}
		msg := strings.TrimSpace(req.Message)
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		actLabel := strings.TrimSpace(req.ActionLabel)
		if len(actLabel) > 40 {
			actLabel = actLabel[:40]
		}
		actURL := strings.TrimSpace(req.ActionURL)
		if len(actURL) > 500 {
			actURL = actURL[:500]
		}
		if actURL != "" && !strings.HasPrefix(actURL, "http://") && !strings.HasPrefix(actURL, "https://") {
			actURL = ""
		}
		tgtID := strings.TrimSpace(req.TargetID)
		if len(tgtID) > 64 {
			tgtID = tgtID[:64]
		}

		var newID int64
		if s.db != nil {
			err := s.db.QueryRow(`
				INSERT INTO in_app_notifications (target_type, target_id, title, message, severity, action_label, action_url, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
				RETURNING id
			`, ttype, tgtID, title, msg, sev, actLabel, actURL).Scan(&newID)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
				return
			}
		}

		log.Printf("[ADMIN-NOTIF] Created notification #%d (%s, target: %s/%s): %s", newID, sev, ttype, tgtID, title)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"id":      newID,
		})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *AppState) handleSponsors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.db == nil {
		_ = json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	rows, err := s.db.Query(`
		SELECT account_number, nickname, avatar_url,
		       COALESCE(steam_id, ''), COALESCE(motto, ''),
		       TO_CHAR(created_at, 'YYYY-MM-DD'),
		       (sponsor_until IS NOT NULL AND sponsor_until > NOW()) AS is_active,
		       total_donated_rub,
		       COALESCE(hide_donation_amount, FALSE)
		FROM accounts
		WHERE (sponsor_until IS NOT NULL AND sponsor_until > NOW()) OR total_donated_rub > 0
		ORDER BY is_active DESC, sponsor_until DESC NULLS LAST, total_donated_rub DESC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("[SPONSORS] Query error: %v", err)
		_ = json.NewEncoder(w).Encode([]interface{}{})
		return
	}
	defer rows.Close()

	type SponsorCard struct {
		AccountNumber      string `json:"account_number"`
		Nickname           string `json:"nickname"`
		AvatarURL          string `json:"avatar_url"`
		SteamID            string `json:"steam_id"`
		Motto              string `json:"motto"`
		JoinedDate         string `json:"joined_date"`
		IsActive           bool   `json:"is_active"`
		TotalDonated       int64  `json:"total_donated_rub"`
		HideDonationAmount bool   `json:"hide_donation_amount"`
	}

	sponsors := make([]SponsorCard, 0)
	for rows.Next() {
		var sp SponsorCard
		var nick, av, st, mo sql.NullString
		if err := rows.Scan(&sp.AccountNumber, &nick, &av, &st, &mo, &sp.JoinedDate, &sp.IsActive, &sp.TotalDonated, &sp.HideDonationAmount); err == nil {
			if sp.HideDonationAmount {
				sp.TotalDonated = 0
			}
			if nick.Valid && nick.String != "" {
				sp.Nickname = nick.String
			} else {
				if len(sp.AccountNumber) >= 19 {
					sp.Nickname = sp.AccountNumber[:4] + "-****-****-" + sp.AccountNumber[15:]
				} else {
					sp.Nickname = "Спонсор WarLink"
				}
			}
			if av.Valid {
				sp.AvatarURL = av.String
			}
			if st.Valid {
				sp.SteamID = st.String
			}
			if mo.Valid {
				sp.Motto = mo.String
			}
			if len(sp.AccountNumber) >= 19 {
				sp.AccountNumber = sp.AccountNumber[:4] + "-****-****-" + sp.AccountNumber[15:]
			}
			sponsors = append(sponsors, sp)
		}
	}
	_ = json.NewEncoder(w).Encode(sponsors)
}

func validateServerNickname(nick string) error {
	return FastValidateNickname(nick)
}

func (s *AppState) handleProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_unavailable"})
		return
	}

	type DonationItem struct {
		InvoiceID int    `json:"invoice_id"`
		AmountRub int    `json:"amount_rub"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}

	type ProfileResp struct {
		AccountNumber      string         `json:"account_number"`
		Nickname           string         `json:"nickname"`
		AvatarURL          string         `json:"avatar_url"`
		SteamID            string         `json:"steam_id"`
		Motto              string         `json:"motto"`
		Tier               string         `json:"tier"`
		SponsorUntil       int64          `json:"sponsor_until"`
		DaysRemaining      int            `json:"days_remaining"`
		CreatedAt          string         `json:"created_at"`
		TotalDonatedRub    int            `json:"total_donated_rub"`
		HideDonationAmount bool           `json:"hide_donation_amount"`
		DeviceCount        int            `json:"device_count"`
		Donations          []DonationItem `json:"donations"`
	}

	if r.Method == http.MethodGet {
		acc := strings.TrimSpace(r.URL.Query().Get("account"))
		dev := strings.TrimSpace(r.URL.Query().Get("device"))
		if acc == "" && dev == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing_account_or_device"})
			return
		}

		var resp ProfileResp
		resp.Donations = []DonationItem{}
		var sponsorTime *time.Time
		var createdAt time.Time
		var totalDonated int
		var err error
		var st, mo sql.NullString

		if acc != "" {
			err = s.db.QueryRow(`
				SELECT account_number, nickname, avatar_url, steam_id, motto, tier, sponsor_until, total_donated_rub, created_at, COALESCE(hide_donation_amount, FALSE) 
				FROM accounts WHERE account_number = $1
			`, acc).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount)
		} else {
			err = s.db.QueryRow(`
				SELECT a.account_number, a.nickname, a.avatar_url, a.steam_id, a.motto, a.tier, a.sponsor_until, a.total_donated_rub, a.created_at, COALESCE(a.hide_donation_amount, FALSE) 
				FROM account_devices ad
				JOIN accounts a ON ad.account_number = a.account_number
				WHERE ad.device_id = $1
				ORDER BY a.sponsor_until DESC NULLS LAST LIMIT 1
			`, dev).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount)
		}

		if st.Valid {
			resp.SteamID = st.String
		}
		if mo.Valid {
			resp.Motto = mo.String
		}

		if err != nil {
			resp.AccountNumber = acc
			resp.Tier = "free"
			resp.CreatedAt = time.Now().Format("02.01.2006")
			resp.DeviceCount = 1
			if acc != "" {
				_, _ = s.db.Exec(`INSERT INTO accounts (account_number, created_at, updated_at) VALUES ($1, NOW(), NOW()) ON CONFLICT DO NOTHING`, acc)
				if dev != "" {
					_, _ = s.db.Exec(`INSERT INTO account_devices (account_number, device_id, created_at) VALUES ($1, $2, NOW()) ON CONFLICT DO NOTHING`, acc, dev)
				}
			}
		} else {
			resp.TotalDonatedRub = totalDonated
			if !createdAt.IsZero() {
				resp.CreatedAt = createdAt.Format("02.01.2006")
			} else {
				resp.CreatedAt = time.Now().Format("02.01.2006")
			}
		}

		if resp.AccountNumber != "" {
			var devCount int
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM account_devices WHERE account_number = $1`, resp.AccountNumber).Scan(&devCount)
			if devCount <= 0 {
				devCount = 1
			}
			resp.DeviceCount = devCount

			rows, qErr := s.db.Query(`
				SELECT invoice_id, amount_rub, status, created_at 
				FROM pending_donations 
				WHERE account_number = $1 
				ORDER BY created_at DESC LIMIT 20
			`, resp.AccountNumber)
			if qErr == nil {
				defer rows.Close()
				for rows.Next() {
					var item DonationItem
					var dTime time.Time
					if err := rows.Scan(&item.InvoiceID, &item.AmountRub, &item.Status, &dTime); err == nil {
						item.CreatedAt = dTime.Format("02.01.2006 15:04")
						resp.Donations = append(resp.Donations, item)
					}
				}
			}
		}

		if resp.AccountNumber == AdminAccountNumber {
			resp.Tier = "admin"
			resp.DaysRemaining = 9999
		} else if sponsorTime != nil && sponsorTime.After(time.Now()) {
			resp.Tier = "sponsor"
			resp.SponsorUntil = sponsorTime.Unix()
			resp.DaysRemaining = int(time.Until(*sponsorTime).Hours() / 24)
		} else {
			resp.Tier = "free"
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			AccountNumber      string `json:"account_number"`
			DeviceID           string `json:"device_id"`
			Nickname           string `json:"nickname"`
			SteamID            string `json:"steam_id"`
			Motto              string `json:"motto"`
			HideDonationAmount *bool  `json:"hide_donation_amount"`
			Action             string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
			return
		}

		acc := strings.TrimSpace(req.AccountNumber)
		dev := strings.TrimSpace(req.DeviceID)
		nick := strings.TrimSpace(req.Nickname)
		steamID := strings.TrimSpace(req.SteamID)
		motto := strings.TrimSpace(req.Motto)
		if len(nick) > 24 {
			nick = nick[:24]
		}
		nick = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(nick, "")
		nick = strings.TrimSpace(nick)

		if req.Action == "reset_devices" && acc != "" && dev != "" {
			_, _ = s.db.Exec(`DELETE FROM account_devices WHERE account_number = $1 AND device_id != $2`, acc, dev)
		}

		if nick != "" {
			if err := ValidateNicknameHybrid(r.Context(), nick, s.cfg.GeminiAPIKey, &s.nicknameCache); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "forbidden_nickname",
					"message": err.Error(),
				})
				return
			}
		}

		if acc == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "account_number_required"})
			return
		}

		_, _ = s.db.Exec(`
			INSERT INTO accounts (account_number, nickname, steam_id, motto, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (account_number) DO UPDATE
			SET nickname = CASE WHEN $2 != '' THEN $2 ELSE accounts.nickname END,
			    steam_id = CASE WHEN $3 != '' THEN $3 ELSE accounts.steam_id END,
			    motto = CASE WHEN $4 != '' THEN $4 ELSE accounts.motto END,
			    updated_at = NOW()
		`, acc, nick, steamID, motto)

		if req.HideDonationAmount != nil {
			_, _ = s.db.Exec(`UPDATE accounts SET hide_donation_amount = $1, updated_at = NOW() WHERE account_number = $2`, *req.HideDonationAmount, acc)
		}

		if dev != "" {
			_, _ = s.db.Exec(`
				INSERT INTO account_devices (account_number, device_id, created_at)
				VALUES ($1, $2, NOW())
				ON CONFLICT (account_number, device_id) DO NOTHING
			`, acc, dev)
		}

		var resp ProfileResp
		resp.Donations = []DonationItem{}
		var sponsorTime *time.Time
		var createdAt time.Time
		var totalDonated int
		var st, mo sql.NullString
		_ = s.db.QueryRow(`
			SELECT account_number, nickname, avatar_url, steam_id, motto, tier, sponsor_until, total_donated_rub, created_at, COALESCE(hide_donation_amount, FALSE) 
			FROM accounts WHERE account_number = $1
		`, acc).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount)
		if st.Valid {
			resp.SteamID = st.String
		}
		if mo.Valid {
			resp.Motto = mo.String
		}

		resp.TotalDonatedRub = totalDonated
		if !createdAt.IsZero() {
			resp.CreatedAt = createdAt.Format("02.01.2006")
		} else {
			resp.CreatedAt = time.Now().Format("02.01.2006")
		}

		var devCount int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM account_devices WHERE account_number = $1`, acc).Scan(&devCount)
		if devCount <= 0 {
			devCount = 1
		}
		resp.DeviceCount = devCount

		rows, qErr := s.db.Query(`
			SELECT invoice_id, amount_rub, status, created_at 
			FROM pending_donations 
			WHERE account_number = $1 
			ORDER BY created_at DESC LIMIT 20
		`, acc)
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var item DonationItem
				var dTime time.Time
				if err := rows.Scan(&item.InvoiceID, &item.AmountRub, &item.Status, &dTime); err == nil {
					item.CreatedAt = dTime.Format("02.01.2006 15:04")
					resp.Donations = append(resp.Donations, item)
				}
			}
		}

		if resp.AccountNumber == AdminAccountNumber {
			resp.Tier = "admin"
			resp.DaysRemaining = 9999
		} else if sponsorTime != nil && sponsorTime.After(time.Now()) {
			resp.Tier = "sponsor"
			resp.SponsorUntil = sponsorTime.Unix()
			resp.DaysRemaining = int(time.Until(*sponsorTime).Hours() / 24)
		} else {
			resp.Tier = "free"
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
}

func (s *AppState) handleProfileAvatar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_unavailable"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "file_too_large_max_4mb"})
		return
	}

	account := strings.TrimSpace(r.FormValue("account_number"))
	if account == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing_account_number"})
		return
	}

	file, _, err := r.FormFile("avatar")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing_avatar_file"})
		return
	}
	defer file.Close()

	srcImg, _, err := image.Decode(file)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid_image_format"})
		return
	}

	scaled := resizeImage(srcImg, 128, 128)

	avatarsDir := "/opt/warlink-server/avatars"
	if _, err := os.Stat("/opt/warlink-server"); os.IsNotExist(err) {
		avatarsDir = "./avatars"
	}
	_ = os.MkdirAll(avatarsDir, 0755)

	hash := sha256.Sum256([]byte(account))
	filename := fmt.Sprintf("%x.jpg", hash[:8])
	targetPath := filepath.Join(avatarsDir, filename)

	outF, err := os.Create(targetPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "failed_saving_avatar"})
		return
	}
	defer outF.Close()

	if err := jpeg.Encode(outF, scaled, &jpeg.Options{Quality: 85}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "failed_encoding_avatar"})
		return
	}

	avatarURL := "/avatars/" + filename
	_, _ = s.db.Exec(`
		UPDATE accounts 
		SET avatar_url = $1, updated_at = NOW() 
		WHERE account_number = $2
	`, avatarURL, account)

	log.Printf("[AVATAR] Saved 128x128 avatar for %s -> %s", account, avatarURL)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"avatar_url": avatarURL,
	})
}

func (s *AppState) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DashboardKey == "" || r.URL.Query().Get("key") != s.cfg.DashboardKey {
		http.NotFound(w, r)
		return
	}

	ip := s.getPublicIP(r)
	title := s.cfg.ServerName
	if title == "" {
		title = "WarLink • Stockholm GPN Telemetry"
	}
	ports := s.cfg.ServerPorts
	if ports == "" {
		ports = DefaultServerPorts
	}
	loc := s.cfg.ServerLocation
	if loc == "" {
		loc = "Stockholm, Sweden"
	}
	subtitle := fmt.Sprintf("%s • %s • Port %s", ip, loc, ports)

	rendered := dashboardHTML
	rendered = strings.ReplaceAll(rendered, "{{SERVER_TITLE}}", title)
	rendered = strings.ReplaceAll(rendered, "{{SERVER_SUBTITLE}}", subtitle)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(rendered))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{SERVER_TITLE}}</title>
    <style>
        :root {
            --bg-base: #0d0d0d;
            --bg-card: #151515;
            --bg-card-hover: #1a1a1a;
            --border: #242424;
            --border-light: #333333;
            --accent: #FF5E1F;
            --accent-glow: rgba(255, 94, 31, 0.15);
            --green: #22c55e;
            --blue: #3b82f6;
            --purple: #a855f7;
            --yellow: #eab308;
            --text-main: #f4f4f5;
            --text-muted: #888888;
            --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            background-color: var(--bg-base);
            color: var(--text-main);
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            font-size: 13px;
            line-height: 1.4;
            min-height: 100vh;
            padding: 24px;
        }
        .container {
            max-width: 1280px;
            margin: 0 auto;
        }
        .header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding-bottom: 20px;
            border-bottom: 1px solid var(--border);
            margin-bottom: 24px;
        }
        .brand {
            display: flex;
            align-items: center;
            gap: 12px;
        }
        .brand-icon {
            width: 34px;
            height: 34px;
            background: var(--accent);
            display: flex;
            align-items: center;
            justify-content: center;
            border-radius: 2px;
        }
        .brand-title {
            font-size: 16px;
            font-weight: 700;
            letter-spacing: 0.5px;
        }
        .brand-subtitle {
            font-size: 11px;
            color: var(--text-muted);
            font-family: var(--font-mono);
        }
        .header-meta {
            display: flex;
            align-items: center;
            gap: 16px;
        }
        .status-badge {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            background: #112211;
            border: 1px solid #224422;
            color: #4ade80;
            padding: 4px 10px;
            border-radius: 2px;
            font-size: 11px;
            font-weight: 600;
            letter-spacing: 0.5px;
        }
        .status-dot {
            width: 7px;
            height: 7px;
            border-radius: 50%;
            background: var(--green);
            box-shadow: 0 0 6px var(--green);
        }
        .controls {
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .btn {
            background: var(--bg-card);
            border: 1px solid var(--border);
            color: var(--text-main);
            padding: 6px 12px;
            border-radius: 2px;
            font-size: 11px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.15s ease;
        }
        .btn:hover {
            border-color: var(--border-light);
            background: var(--bg-card-hover);
        }
        .btn.active {
            background: var(--accent);
            color: #000;
            border-color: var(--accent);
        }
        .kpi-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
            gap: 16px;
            margin-bottom: 24px;
        }
        .kpi-card {
            background: var(--bg-card);
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 16px;
            display: flex;
            flex-direction: column;
            justify-content: space-between;
        }
        .kpi-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            color: var(--text-muted);
            font-size: 11px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            margin-bottom: 8px;
        }
        .kpi-value {
            font-size: 24px;
            font-weight: 700;
            font-family: var(--font-mono);
            letter-spacing: -0.5px;
            color: var(--text-main);
            margin-bottom: 6px;
        }
        .kpi-sub {
            font-size: 11px;
            color: var(--text-muted);
            display: flex;
            align-items: center;
            gap: 6px;
        }
        .progress-bar-bg {
            height: 4px;
            background: #222222;
            border-radius: 1px;
            overflow: hidden;
            margin-top: 8px;
        }
        .progress-bar-fill {
            height: 100%;
            background: var(--accent);
            transition: width 0.3s ease;
        }
        .chart-section {
            background: var(--bg-card);
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 18px;
            margin-bottom: 24px;
        }
        .chart-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 14px;
        }
        .chart-title {
            font-size: 13px;
            font-weight: 600;
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .chart-legend {
            display: flex;
            align-items: center;
            gap: 14px;
            font-size: 11px;
            color: var(--text-muted);
        }
        .legend-item {
            display: flex;
            align-items: center;
            gap: 6px;
        }
        .legend-color {
            width: 8px;
            height: 8px;
            border-radius: 1px;
        }
        .canvas-container {
            position: relative;
            width: 100%;
            height: 200px;
        }
        canvas {
            display: block;
            width: 100%;
            height: 100%;
        }
        .table-section {
            background: var(--bg-card);
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 16px;
        }
        .table-title {
            font-size: 13px;
            font-weight: 600;
            margin-bottom: 12px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            font-size: 12px;
            font-family: var(--font-mono);
        }
        th, td {
            text-align: left;
            padding: 8px 12px;
            border-bottom: 1px solid var(--border);
        }
        th {
            color: var(--text-muted);
            font-weight: 600;
            font-size: 11px;
            text-transform: uppercase;
        }
        tr:hover {
            background: var(--bg-card-hover);
        }
        .tooltip {
            position: absolute;
            background: #1e1e1e;
            border: 1px solid var(--border-light);
            padding: 6px 10px;
            border-radius: 2px;
            font-size: 11px;
            font-family: var(--font-mono);
            pointer-events: none;
            display: none;
            z-index: 10;
        }
    </style>
</head>
<body>
    <div class="container">
        <header class="header">
            <div class="brand">
                <div class="brand-icon">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
                    </svg>
                </div>
                <div>
                    <div class="brand-title">{{SERVER_TITLE}}</div>
                    <div class="brand-subtitle">{{SERVER_SUBTITLE}}</div>
                </div>
            </div>
            <div class="header-meta">
                <div class="status-badge">
                    <span class="status-dot"></span>
                    <span>ONLINE (27 MS)</span>
                </div>
                <div class="controls">
                    <button class="btn active" onclick="setRange(1, this)">1Ч</button>
                    <button class="btn" onclick="setRange(6, this)">6Ч</button>
                    <button class="btn" onclick="setRange(24, this)">24Ч</button>
                    <button class="btn" onclick="setRange(168, this)">7Д</button>
                    <button class="btn" onclick="loadData()">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -2px;">
                            <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
                        </svg>
                    </button>
                </div>
            </div>
        </header>

        <!-- WarLink Control Plane (Zero-Restart Dynamic Config) -->
        <div class="features-bar" style="background: var(--bg-card); border: 1px solid var(--border); border-radius: 2px; padding: 16px 20px; margin-bottom: 24px;">
            <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; border-bottom: 1px solid var(--border); padding-bottom: 10px;">
                <div style="display: flex; align-items: center; gap: 10px;">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="4" y1="21" x2="4" y2="14"></line>
                        <line x1="4" y1="10" x2="4" y2="3"></line>
                        <line x1="12" y1="21" x2="12" y2="12"></line>
                        <line x1="12" y1="8" x2="12" y2="3"></line>
                        <line x1="20" y1="21" x2="20" y2="16"></line>
                        <line x1="20" y1="12" x2="20" y2="3"></line>
                        <line x1="1" y1="14" x2="7" y2="14"></line>
                        <line x1="9" y1="8" x2="15" y2="8"></line>
                        <line x1="17" y1="16" x2="23" y2="16"></line>
                    </svg>
                    <span style="font-weight: 700; font-size: 12px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--text-main);">Панель управления шлюзом (WarLink Control Plane)</span>
                </div>
                <div id="ctrl-status-msg" style="font-size: 11px; font-weight: 600; font-family: var(--font-mono); color: var(--green);"></div>
            </div>
            <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; align-items: center;">
                <div style="background: #111111; border: 1px solid var(--border); border-radius: 2px; padding: 12px; display: flex; align-items: center; justify-content: space-between;">
                    <div>
                        <div style="font-size: 11px; text-transform: uppercase; color: var(--text-muted); font-weight: 600;">Приём донатов СБП</div>
                        <div style="font-size: 10px; color: var(--text-muted);">Кнопка в приложении</div>
                    </div>
                    <button id="toggle-donate-btn" class="btn" onclick="toggleFeature('enable_donate')" style="font-family: var(--font-mono); min-width: 90px; text-align: center; font-weight: 700; padding: 6px 12px;">ВКЛ</button>
                </div>
                <div style="background: #111111; border: 1px solid var(--border); border-radius: 2px; padding: 12px; display: flex; align-items: center; justify-content: space-between;">
                    <div>
                        <div style="font-size: 11px; text-transform: uppercase; color: var(--text-muted); font-weight: 600;">Голосование за игры</div>
                        <div style="font-size: 10px; color: var(--text-muted);">Каталог сообщества</div>
                    </div>
                    <button id="toggle-voting-btn" class="btn" onclick="toggleFeature('enable_voting')" style="font-family: var(--font-mono); min-width: 90px; text-align: center; font-weight: 700; padding: 6px 12px;">ВКЛ</button>
                </div>
                <div style="background: #111111; border: 1px solid var(--border); border-radius: 2px; padding: 12px; display: flex; align-items: center; justify-content: space-between;">
                    <div>
                        <div style="font-size: 11px; text-transform: uppercase; color: var(--text-muted); font-weight: 600;">Общий пул слотов</div>
                        <div style="font-size: 10px; color: var(--text-muted);">Всего подключений</div>
                    </div>
                    <div style="display: flex; gap: 6px; align-items: center;">
                        <input type="number" id="input-max-sess" value="100" min="5" max="500" style="background: #000; border: 1px solid var(--border); color: #fff; padding: 6px 8px; border-radius: 2px; width: 65px; font-weight: 700; font-family: var(--font-mono); text-align: right;">
                        <button class="btn" onclick="saveSetting('max_sessions', 'input-max-sess')" style="background: var(--accent); color: #000; border-color: var(--accent); font-weight: 700;">OK</button>
                    </div>
                </div>
                <div style="background: #111111; border: 1px solid var(--border); border-radius: 2px; padding: 12px; display: flex; align-items: center; justify-content: space-between;">
                    <div>
                        <div style="font-size: 11px; text-transform: uppercase; color: var(--text-muted); font-weight: 600;">Резерв спонсоров</div>
                        <div style="font-size: 10px; color: var(--text-muted);">Слоты поддержки</div>
                    </div>
                    <div style="display: flex; gap: 6px; align-items: center;">
                        <input type="number" id="input-sponsor-slots" value="10" min="0" max="500" style="background: #000; border: 1px solid var(--border); color: #fff; padding: 6px 8px; border-radius: 2px; width: 65px; font-weight: 700; font-family: var(--font-mono); text-align: right;">
                        <button class="btn" onclick="saveSetting('dedicated_sponsor_slots', 'input-sponsor-slots')" style="background: var(--accent); color: #000; border-color: var(--accent); font-weight: 700;">OK</button>
                    </div>
                </div>
                <div style="background: #111111; border: 1px solid var(--border); border-radius: 2px; padding: 12px; display: flex; align-items: center; justify-content: space-between;">
                    <div>
                        <div style="font-size: 11px; text-transform: uppercase; color: var(--text-muted); font-weight: 600;">Бесплатный пул</div>
                        <div style="font-size: 10px; color: var(--text-muted);">Лимит без статуса</div>
                    </div>
                    <div style="display: flex; gap: 6px; align-items: center;">
                        <input type="number" id="input-free-slots" value="89" min="1" max="500" style="background: #000; border: 1px solid var(--border); color: #fff; padding: 6px 8px; border-radius: 2px; width: 65px; font-weight: 700; font-family: var(--font-mono); text-align: right;">
                        <button class="btn" onclick="saveSetting('free_slots_limit', 'input-free-slots')" style="background: var(--accent); color: #000; border-color: var(--accent); font-weight: 700;">OK</button>
                    </div>
                </div>
            </div>
        </div>

        <!-- KPI Cards -->
        <div class="kpi-grid">
            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Активные сессии</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
                </div>
                <div class="kpi-value" id="kpi-sessions">0 / 100</div>
                <div class="kpi-sub" id="kpi-sessions-sub">0% емкости шлюза</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" id="kpi-sessions-bar" style="width: 0%;"></div></div>
            </div>

            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Сетевой битрейт</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
                </div>
                <div class="kpi-value" id="kpi-bandwidth">0 / 0 Кбит/с</div>
                <div class="kpi-sub" id="kpi-bandwidth-sub">Вход: 0 • Выход: 0</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" id="kpi-bandwidth-bar" style="width: 2%; background: var(--blue);"></div></div>
            </div>

            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Нагрузка CPU</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>
                </div>
                <div class="kpi-value" id="kpi-cpu">0.0%</div>
                <div class="kpi-sub" id="kpi-cpu-sub">AMD EPYC™ 4.2 GHz</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" id="kpi-cpu-bar" style="width: 0%; background: var(--yellow);"></div></div>
            </div>

            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Оперативная память</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 19v-3"/><path d="M10 19v-3"/><path d="M14 19v-3"/><path d="M18 19v-3"/><rect x="2" y="5" width="20" height="11" rx="2"/></svg>
                </div>
                <div class="kpi-value" id="kpi-ram">0 / 0 МБ</div>
                <div class="kpi-sub" id="kpi-ram-sub">0% использовано</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" id="kpi-ram-bar" style="width: 0%; background: var(--purple);"></div></div>
            </div>

            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Задержка шлюза</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                </div>
                <div class="kpi-value" id="kpi-ping">27 мс</div>
                <div class="kpi-sub">Стокгольм • Aeza DC</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" style="width: 100%; background: var(--green);"></div></div>
            </div>
        </div>

        <!-- Chart 1: Sessions & CPU -->
        <div class="chart-section">
            <div class="chart-header">
                <div class="chart-title">
                    <span>Сессии и Загрузка CPU</span>
                </div>
                <div class="chart-legend">
                    <div class="legend-item"><div class="legend-color" style="background: var(--accent);"></div><span>Сессии (0-100)</span></div>
                    <div class="legend-item"><div class="legend-color" style="background: var(--yellow);"></div><span>CPU (%)</span></div>
                </div>
            </div>
            <div class="canvas-container">
                <canvas id="chart-sessions"></canvas>
            </div>
        </div>

        <!-- Chart 2: Bandwidth -->
        <div class="chart-section">
            <div class="chart-header">
                <div class="chart-title">
                    <span>Сетевой трафик (Кбит/с)</span>
                </div>
                <div class="chart-legend">
                    <div class="legend-item"><div class="legend-color" style="background: var(--blue);"></div><span>Входящий (RX)</span></div>
                    <div class="legend-item"><div class="legend-color" style="background: var(--purple);"></div><span>Исходящий (TX)</span></div>
                </div>
            </div>
            <div class="canvas-container">
                <canvas id="chart-bandwidth"></canvas>
            </div>
        </div>

        <!-- Table of Active Players Online -->
        <div class="table-section" style="margin-bottom: 24px;">
            <div class="table-title" style="display: flex; justify-content: space-between; align-items: center;">
                <span>Игроки онлайн прямо сейчас</span>
                <span id="active-players-count" style="color: var(--accent); font-size: 11px; font-weight: 700; text-transform: none;">0 игроков</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>Устройство (ID)</th>
                        <th>Режим / Игра</th>
                        <th>IP-адрес</th>
                        <th>Подключен в</th>
                        <th>Время в сети</th>
                        <th>Статус сессии</th>
                    </tr>
                </thead>
                <tbody id="players-table-body">
                    <tr><td colspan="6" style="text-align: center; color: var(--text-muted);">Нет активных игроков онлайн</td></tr>
                </tbody>
            </table>
        </div>

        <!-- Table of Snapshots -->
        <div class="table-section">
            <div class="table-title">Последние снимки телеметрии</div>
            <table>
                <thead>
                    <tr>
                        <th>Время (МСК)</th>
                        <th>Сессии</th>
                        <th>CPU %</th>
                        <th>RAM</th>
                        <th>Входящий битрейт</th>
                        <th>Исходящий битрейт</th>
                        <th>Суммарный трафик (RX / TX)</th>
                    </tr>
                </thead>
                <tbody id="table-body">
                    <tr><td colspan="7" style="text-align: center; color: var(--text-muted);">Загрузка телеметрии...</td></tr>
                </tbody>
            </table>
        </div>
    </div>

    <script>
        let currentHours = 1;
        const urlParams = new URLSearchParams(window.location.search);
        const secretKey = urlParams.get('key') || '';

        function setRange(h, btn) {
            currentHours = h;
            document.querySelectorAll('.controls .btn').forEach(b => b.classList.remove('active'));
            if (btn) btn.classList.add('active');
            loadData();
        }

        async function loadData() {
            try {
                let url = '/api/v1/analytics?hours=' + currentHours;
                if (secretKey) {
                    url += '&key=' + encodeURIComponent(secretKey);
                }
                const res = await fetch(url);
                if (res.status === 404 || res.status === 403) {
                    document.body.innerHTML = '<div style="padding: 40px; text-align: center; color: #ff5555; font-family: monospace;">403 Forbidden: Доступ запрещен (неверный или отсутствующий ключ)</div>';
                    return;
                }
                const data = await res.json();
                if (data && data.success) {
                    updateDashboard(data);
                }
            } catch (err) {
                console.error('Error fetching analytics:', err);
            }
        }

        function formatBytes(bytes) {
            if (!bytes || bytes === 0) return '0 Б';
            const k = 1024;
            const sizes = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
        }

        function formatRate(kbps) {
            if (!kbps || kbps === 0) return '0 Кбит/с';
            if (kbps >= 1000) {
                return (kbps / 1000).toFixed(1) + ' Мбит/с';
            }
            return kbps + ' Кбит/с';
        }

        function updateDashboard(data) {
            const live = data.live || {};
            const history = data.history || [];

            // 1. Update KPI
            const sessEl = document.getElementById('kpi-sessions');
            const sessSubEl = document.getElementById('kpi-sessions-sub');
            const sessBar = document.getElementById('kpi-sessions-bar');
            if (sessEl) sessEl.textContent = live.active_sessions + ' / ' + (live.max_sessions || 100);
            const sessPct = Math.round((live.active_sessions / (live.max_sessions || 100)) * 100);
            if (sessSubEl) sessSubEl.textContent = sessPct + '% емкости (' + ((live.max_sessions || 100) - live.active_sessions) + ' доступно)';
            if (sessBar) sessBar.style.width = Math.max(2, sessPct) + '%';

            const bwEl = document.getElementById('kpi-bandwidth');
            const bwSubEl = document.getElementById('kpi-bandwidth-sub');
            if (bwEl) bwEl.textContent = formatRate(live.net_rx_rate_kbps) + ' / ' + formatRate(live.net_tx_rate_kbps);
            if (bwSubEl) bwSubEl.textContent = 'Всего: ' + formatBytes(live.net_bytes_recv) + ' / ' + formatBytes(live.net_bytes_sent);

            const cpuEl = document.getElementById('kpi-cpu');
            const cpuBar = document.getElementById('kpi-cpu-bar');
            if (cpuEl) cpuEl.textContent = (live.cpu_percent || 0).toFixed(1) + '%';
            if (cpuBar) cpuBar.style.width = Math.min(100, Math.max(2, live.cpu_percent || 0)) + '%';

            const ramEl = document.getElementById('kpi-ram');
            const ramSubEl = document.getElementById('kpi-ram-sub');
            const ramBar = document.getElementById('kpi-ram-bar');
            if (ramEl) ramEl.textContent = live.ram_used_mb + ' / ' + live.ram_total_mb + ' МБ';
            const ramPct = live.ram_total_mb > 0 ? Math.round((live.ram_used_mb / live.ram_total_mb) * 100) : 0;
            if (ramSubEl) ramSubEl.textContent = ramPct + '% памяти занято';
            if (ramBar) ramBar.style.width = ramPct + '%';

            // 2. Draw Charts
            drawSessionsChart(history);
            drawBandwidthChart(history);

            // 3. Populate Table
            const tbody = document.getElementById('table-body');
            if (tbody) {
                if (history.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="7" style="text-align: center; color: var(--text-muted);">История пока пуста</td></tr>';
                } else {
                    const recent = [...history].reverse().slice(0, 15);
                    tbody.innerHTML = recent.map(item => {
                        const d = new Date(item.recorded_at);
                        const timeStr = d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) + ' ' + d.toLocaleDateString('ru-RU');
                        return '<tr>' +
                            '<td>' + timeStr + '</td>' +
                            '<td><strong style="color: var(--accent);">' + item.active_sessions + '</strong> / ' + item.max_sessions + '</td>' +
                            '<td>' + (item.cpu_percent || 0).toFixed(1) + '%</td>' +
                            '<td>' + item.ram_used_mb + ' МБ</td>' +
                            '<td style="color: var(--blue);">' + formatRate(item.net_rx_rate_kbps) + '</td>' +
                            '<td style="color: var(--purple);">' + formatRate(item.net_tx_rate_kbps) + '</td>' +
                            '<td>' + formatBytes(item.net_bytes_recv) + ' / ' + formatBytes(item.net_bytes_sent) + '</td>' +
                        '</tr>';
                    }).join('');
                }
            }

            // 4. Populate Active Players Table
            const playersTbody = document.getElementById('players-table-body');
            const playersCountEl = document.getElementById('active-players-count');
            const players = data.active_players || [];
            if (playersCountEl) {
                playersCountEl.textContent = players.length + ' игроков онлайн';
            }
            if (playersTbody) {
                if (players.length === 0) {
                    playersTbody.innerHTML = '<tr><td colspan="6" style="text-align: center; color: var(--text-muted);">Нет активных игроков онлайн</td></tr>';
                } else {
                    playersTbody.innerHTML = players.map(p => {
                        const d = p.connected_at ? new Date(p.connected_at) : null;
                        const connTime = (d && !isNaN(d.getTime())) ? d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : (p.connected_at || '-');
                        let modeBadge = '';
                        const gLower = (p.game || '').toLowerCase();
                        if (gLower.includes('гибрид')) {
                            const cleanName = p.game.replace(/\(гибрид\)/gi, '').replace(/•/g, '').trim().toUpperCase() || 'WARDOGS';
                            modeBadge = '<strong style="color: var(--accent);">' + cleanName + '</strong> ' +
                                        '<span style="background: rgba(255, 94, 31, 0.18); color: #FF5E1F; font-size: 10px; padding: 2px 6px; border-radius: 2px; font-weight: 700; border: 1px solid rgba(255, 94, 31, 0.35); margin-left: 4px;">ГИБРИД</span>';
                        } else if (gLower === 'free_internet' || gLower.includes('свободный')) {
                            modeBadge = '<strong style="color: var(--blue);">СВОБОДНЫЙ ИНТЕРНЕТ</strong>';
                        } else {
                            modeBadge = '<strong style="color: var(--text-main);">' + (p.game || 'WARDOGS').toUpperCase() + '</strong> ' +
                                        '<span style="background: rgba(34, 197, 94, 0.15); color: #22c55e; font-size: 10px; padding: 2px 6px; border-radius: 2px; font-weight: 700; border: 1px solid rgba(34, 197, 94, 0.3); margin-left: 4px;">СОЛО</span>';
                        }
                        return '<tr>' +
                            '<td style="font-family: var(--font-mono); font-weight: 600;">' + p.device_id + '</td>' +
                            '<td>' + modeBadge + '</td>' +
                            '<td style="font-family: var(--font-mono); color: var(--blue);">' + p.client_ip + '</td>' +
                            '<td>' + connTime + '</td>' +
                            '<td>' + p.duration_desc + '</td>' +
                            '<td><span style="display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--green); margin-right: 6px;"></span>Активен</td>' +
                        '</tr>';
                    }).join('');
                }
            }
        }

        function setupCanvas(canvas) {
            const dpr = window.devicePixelRatio || 1;
            const rect = canvas.getBoundingClientRect();
            canvas.width = rect.width * dpr;
            canvas.height = rect.height * dpr;
            const ctx = canvas.getContext('2d');
            ctx.scale(dpr, dpr);
            return { ctx, width: rect.width, height: rect.height };
        }

        function drawSessionsChart(data) {
            const canvas = document.getElementById('chart-sessions');
            if (!canvas || !data || data.length === 0) return;
            const { ctx, width, height } = setupCanvas(canvas);

            ctx.clearRect(0, 0, width, height);

            const padLeft = 40;
            const padBottom = 24;
            const chartW = width - padLeft - 10;
            const chartH = height - padBottom - 10;

            // Draw Grid Lines
            ctx.strokeStyle = '#222222';
            ctx.lineWidth = 1;
            ctx.beginPath();
            for (let i = 0; i <= 4; i++) {
                const y = 10 + (chartH / 4) * i;
                ctx.moveTo(padLeft, y);
                ctx.lineTo(width - 10, y);
            }
            ctx.stroke();

            // Y-axis labels (0 to 100)
            ctx.fillStyle = '#666666';
            ctx.font = '10px monospace';
            ctx.textAlign = 'right';
            for (let i = 0; i <= 4; i++) {
                const val = 100 - (i * 25);
                const y = 10 + (chartH / 4) * i + 3;
                ctx.fillText(val, padLeft - 8, y);
            }

            const n = data.length;
            const stepX = chartW / Math.max(1, n - 1);

            // Draw CPU line (Yellow)
            ctx.strokeStyle = '#eab308';
            ctx.lineWidth = 1.5;
            ctx.beginPath();
            data.forEach((d, i) => {
                const x = padLeft + i * stepX;
                const y = 10 + chartH - (Math.min(100, d.cpu_percent || 0) / 100) * chartH;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            });
            ctx.stroke();

            // Draw Sessions line (Accent Orange)
            ctx.strokeStyle = '#FF5E1F';
            ctx.lineWidth = 2;
            ctx.beginPath();
            data.forEach((d, i) => {
                const x = padLeft + i * stepX;
                const y = 10 + chartH - (Math.min(100, d.active_sessions || 0) / 100) * chartH;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            });
            ctx.stroke();

            // Time labels
            ctx.textAlign = 'center';
            if (n > 1) {
                const dFirst = new Date(data[0].recorded_at);
                const dLast = new Date(data[n - 1].recorded_at);
                ctx.fillText(dFirst.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }), padLeft, height - 6);
                ctx.fillText(dLast.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }), width - 20, height - 6);
            }
        }

        function drawBandwidthChart(data) {
            const canvas = document.getElementById('chart-bandwidth');
            if (!canvas || !data || data.length === 0) return;
            const { ctx, width, height } = setupCanvas(canvas);

            ctx.clearRect(0, 0, width, height);

            const padLeft = 60;
            const padBottom = 24;
            const chartW = width - padLeft - 10;
            const chartH = height - padBottom - 10;

            let maxRate = 100;
            data.forEach(d => {
                if (d.net_rx_rate_kbps > maxRate) maxRate = d.net_rx_rate_kbps;
                if (d.net_tx_rate_kbps > maxRate) maxRate = d.net_tx_rate_kbps;
            });
            maxRate = Math.ceil(maxRate * 1.15 / 100) * 100;

            // Draw Grid Lines
            ctx.strokeStyle = '#222222';
            ctx.lineWidth = 1;
            ctx.beginPath();
            for (let i = 0; i <= 4; i++) {
                const y = 10 + (chartH / 4) * i;
                ctx.moveTo(padLeft, y);
                ctx.lineTo(width - 10, y);
            }
            ctx.stroke();

            // Y-axis labels
            ctx.fillStyle = '#666666';
            ctx.font = '10px monospace';
            ctx.textAlign = 'right';
            for (let i = 0; i <= 4; i++) {
                const val = maxRate - (i * (maxRate / 4));
                const y = 10 + (chartH / 4) * i + 3;
                ctx.fillText(formatRate(val), padLeft - 8, y);
            }

            const n = data.length;
            const stepX = chartW / Math.max(1, n - 1);

            // Draw RX (Blue)
            ctx.strokeStyle = '#3b82f6';
            ctx.lineWidth = 2;
            ctx.beginPath();
            data.forEach((d, i) => {
                const x = padLeft + i * stepX;
                const y = 10 + chartH - ((d.net_rx_rate_kbps || 0) / maxRate) * chartH;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            });
            ctx.stroke();

            // Draw TX (Purple)
            ctx.strokeStyle = '#a855f7';
            ctx.lineWidth = 2;
            ctx.beginPath();
            data.forEach((d, i) => {
                const x = padLeft + i * stepX;
                const y = 10 + chartH - ((d.net_tx_rate_kbps || 0) / maxRate) * chartH;
                if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
            });
            ctx.stroke();

            // Time labels
            ctx.textAlign = 'center';
            if (n > 1) {
                const dFirst = new Date(data[0].recorded_at);
                const dLast = new Date(data[n - 1].recorded_at);
                ctx.fillText(dFirst.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }), padLeft, height - 6);
                ctx.fillText(dLast.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }), width - 20, height - 6);
            }
        }

        let featuresState = { enable_donate: true, enable_voting: true, max_sessions: 100, dedicated_sponsor_slots: 10, free_slots_limit: 89 };

        async function loadFeatures() {
            try {
                let url = '/api/v1/admin/settings';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url);
                if (res.ok) {
                    const data = await res.json();
                    if (data && data.success) {
                        featuresState = data;
                        renderControlUI();
                    }
                }
            } catch (err) {
                console.error('Failed to load settings:', err);
            }
        }

        function renderControlUI() {
            const dBtn = document.getElementById('toggle-donate-btn');
            if (dBtn) {
                if (featuresState.enable_donate) {
                    dBtn.textContent = 'ВКЛЮЧЕНО';
                    dBtn.style.background = '#112211';
                    dBtn.style.borderColor = '#22c55e';
                    dBtn.style.color = '#4ade80';
                } else {
                    dBtn.textContent = 'ОТКЛЮЧЕНО';
                    dBtn.style.background = '#221111';
                    dBtn.style.borderColor = '#ef4444';
                    dBtn.style.color = '#f87171';
                }
            }
            const vBtn = document.getElementById('toggle-voting-btn');
            if (vBtn) {
                if (featuresState.enable_voting) {
                    vBtn.textContent = 'ВКЛЮЧЕНО';
                    vBtn.style.background = '#112211';
                    vBtn.style.borderColor = '#22c55e';
                    vBtn.style.color = '#4ade80';
                } else {
                    vBtn.textContent = 'ОТКЛЮЧЕНО';
                    vBtn.style.background = '#221111';
                    vBtn.style.borderColor = '#ef4444';
                    vBtn.style.color = '#f87171';
                }
            }
            const mSess = document.getElementById('input-max-sess');
            if (mSess && featuresState.max_sessions) {
                mSess.value = featuresState.max_sessions;
            }
            const sSponsor = document.getElementById('input-sponsor-slots');
            if (sSponsor && featuresState.dedicated_sponsor_slots !== undefined) {
                sSponsor.value = featuresState.dedicated_sponsor_slots;
            }
            const fSlots = document.getElementById('input-free-slots');
            if (fSlots && featuresState.free_slots_limit !== undefined) {
                fSlots.value = featuresState.free_slots_limit;
            }
        }

        async function toggleFeature(name) {
            const nextVal = !featuresState[name];
            const payload = {};
            payload[name] = nextVal;
            await sendAdminUpdate(payload);
        }

        async function saveSetting(name, inputId) {
            const input = document.getElementById(inputId);
            if (!input) return;
            const val = parseInt(input.value, 10);
            if (isNaN(val) || val <= 0) return;
            const payload = {};
            payload[name] = val;
            await sendAdminUpdate(payload);
        }

        async function sendAdminUpdate(payload) {
            const statusMsg = document.getElementById('ctrl-status-msg');
            if (statusMsg) statusMsg.textContent = 'Сохранение...';

            try {
                let url = '/api/v1/admin/settings';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                if (res.ok) {
                    const data = await res.json();
                    if (data && data.success) {
                        featuresState = data;
                        renderControlUI();
                        if (statusMsg) {
                            statusMsg.textContent = 'Успешно сохранено!';
                            statusMsg.style.color = 'var(--green)';
                            setTimeout(() => { if (statusMsg) statusMsg.textContent = ''; }, 3000);
                        }
                    }
                } else {
                    if (statusMsg) {
                        statusMsg.textContent = 'Ошибка доступа (403)';
                        statusMsg.style.color = '#ef4444';
                    }
                }
            } catch (err) {
                console.error('Failed to update setting:', err);
                if (statusMsg) {
                    statusMsg.textContent = 'Ошибка сети!';
                    statusMsg.style.color = '#ef4444';
                }
            }
        }

        // Initial fetch and auto-refresh
        loadData();
        loadFeatures();
        setInterval(loadData, 5000);
        setInterval(loadFeatures, 10000);
        window.addEventListener('resize', () => loadData());
    </script>
</body>
</html>
`

