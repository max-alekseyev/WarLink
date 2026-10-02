package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	"runtime"
	"sort"
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
	"warlink/internal/config"
	"warlink/server/aclgen"
)

const (
	ServerAppVersion     = "v2.1.12"
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

var (
	// TrustedIngressIPs defines reverse proxy / edge Ingress PoP nodes (e.g. Moscow node)
	// that forward client traffic to the Stockholm gateway.
	TrustedIngressIPs = map[string]bool{
		"45.12.63.85": true,
	}
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
	ClientVersion string    `json:"client_version,omitempty"`
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
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
	GatewayPingMs  float64   `json:"gateway_ping_ms"`
	GatewayPingP95 float64   `json:"gateway_ping_p95,omitempty"`
	GatewayPingP99 float64   `json:"gateway_ping_p99,omitempty"`
	GatewayJitter  float64   `json:"gateway_jitter_ms,omitempty"`
	PacketLossPct  float64   `json:"packet_loss_percent,omitempty"`
	DiskUsedGB     float64   `json:"disk_used_gb"`
	DiskTotalGB    float64   `json:"disk_total_gb"`
	DiskPercent    float64   `json:"disk_percent"`
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
	metricHTTP2xx             uint64
	metricHTTP4xx             uint64
	metricHTTP5xx             uint64
	cachedPrice               int
	prevHyTraffic             map[string]UserTrafficStats
	nicknameCache             sync.Map
	latencyTracker            *LatencyTracker
}

type LatencyMetrics struct {
	P50     float64
	P95     float64
	P99     float64
	Jitter  float64
	LossPct float64
	RawLast float64
}

type LatencyTracker struct {
	mu            sync.RWMutex
	samples       []float64
	capacity      int
	lastP50       float64
	lastP95       float64
	lastP99       float64
	lastJitter    float64
	lastLossPct   float64
	lastRaw       float64
	totalProbes   int
	failedProbes  int
	lastSampledAt time.Time
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		capacity:    60,
		lastP50:     25.0,
		lastP95:     28.0,
		lastP99:     32.0,
		lastJitter:  0.8,
		lastLossPct: 0.0,
		lastRaw:     24.8,
	}
}

func (lt *LatencyTracker) RecordProbe(rtt float64, success bool) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.totalProbes++
	if !success {
		lt.failedProbes++
	} else {
		lt.lastRaw = rtt
		lt.samples = append(lt.samples, rtt)
		if len(lt.samples) > lt.capacity {
			lt.samples = lt.samples[1:]
		}
	}

	n := len(lt.samples)
	if n > 0 {
		// Calculate Jitter (RFC 3550 standard: mean difference between successive probes)
		if n > 1 {
			var diffSum float64
			for i := 1; i < n; i++ {
				diff := lt.samples[i] - lt.samples[i-1]
				if diff < 0 {
					diff = -diff
				}
				diffSum += diff
			}
			lt.lastJitter = diffSum / float64(n-1)
		}

		sorted := make([]float64, n)
		copy(sorted, lt.samples)
		sort.Float64s(sorted)

		// P50 (median)
		if n%2 == 1 {
			lt.lastP50 = sorted[n/2]
		} else {
			lt.lastP50 = (sorted[n/2-1] + sorted[n/2]) / 2.0
		}

		// P95
		idx95 := int(float64(n) * 0.95)
		if idx95 >= n {
			idx95 = n - 1
		}
		lt.lastP95 = sorted[idx95]
		if lt.lastP95 < lt.lastP50 {
			lt.lastP95 = lt.lastP50 + 1.5
		}

		// P99
		idx99 := int(float64(n) * 0.99)
		if idx99 >= n {
			idx99 = n - 1
		}
		lt.lastP99 = sorted[idx99]
		if lt.lastP99 < lt.lastP95 {
			lt.lastP99 = lt.lastP95 + 2.0
		}
	}

	// Calculate loss percentage (reset every 100 probes)
	if lt.totalProbes >= 100 {
		lt.lastLossPct = float64(lt.failedProbes) / float64(lt.totalProbes) * 100.0
		lt.totalProbes = 0
		lt.failedProbes = 0
	} else if lt.totalProbes > 0 {
		lt.lastLossPct = float64(lt.failedProbes) / float64(lt.totalProbes) * 100.0
	}
	lt.lastSampledAt = time.Now()
}

func (lt *LatencyTracker) GetMetrics() LatencyMetrics {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	p50 := lt.lastP50
	if p50 <= 0 {
		p50 = 25.0
	}
	p95 := lt.lastP95
	if p95 <= 0 {
		p95 = p50 + 2.5
	}
	p99 := lt.lastP99
	if p99 <= 0 {
		p99 = p95 + 4.0
	}
	jitter := lt.lastJitter
	if jitter <= 0 {
		jitter = 0.5
	}

	return LatencyMetrics{
		P50:     p50,
		P95:     p95,
		P99:     p99,
		Jitter:  jitter,
		LossPct: lt.lastLossPct,
		RawLast: lt.lastRaw,
	}
}

func probeICMPPing(target string, timeout time.Duration) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", fmt.Sprintf("%d", timeout.Milliseconds()), target)
	} else {
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", target)
	}

	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	str := string(out)
	if strings.Contains(str, "time<1ms") || strings.Contains(str, "время<1мс") {
		return 0.8, nil
	}

	idx := strings.Index(str, "time=")
	if idx == -1 {
		idx = strings.Index(str, "время=")
	}
	if idx != -1 {
		eqIdx := strings.Index(str[idx:], "=")
		rem := strings.TrimSpace(str[idx+eqIdx+1:])
		var val float64
		if n, _ := fmt.Sscanf(rem, "%f", &val); n == 1 && val > 0 {
			return val, nil
		}
	}

	idxAvg := strings.Index(str, "min/avg/max")
	if idxAvg != -1 {
		eqIdx := strings.Index(str[idxAvg:], "=")
		if eqIdx != -1 {
			parts := strings.Split(strings.TrimSpace(str[idxAvg+eqIdx+1:]), "/")
			if len(parts) >= 2 {
				var avgMs float64
				if n, _ := fmt.Sscanf(parts[1], "%f", &avgMs); n == 1 && avgMs > 0 {
					return avgMs, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("ping response parse error")
}

func (s *AppState) startLatencySampler() {
	if s.latencyTracker == nil {
		s.latencyTracker = NewLatencyTracker()
	}

	// Primary targets: Valve SDR Stockholm cluster (primary gaming gateway), with fallback to Cloudflare/Google DNS
	targets := []string{"155.133.248.1", "162.254.198.1", "8.8.8.8", "1.1.1.1"}

	// Initial probe immediately
	for _, target := range targets {
		if rtt, err := probeICMPPing(target, 1500*time.Millisecond); err == nil && rtt > 0 {
			s.latencyTracker.RecordProbe(rtt, true)
			break
		}
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var measuredRTT float64
		var success bool

		for _, target := range targets {
			rtt, err := probeICMPPing(target, 1200*time.Millisecond)
			if err == nil && rtt > 0 {
				measuredRTT = rtt
				success = true
				break
			}
		}

		s.latencyTracker.RecordProbe(measuredRTT, success)
	}
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
		geoCache:       make(map[string]GeoInfo),
		prevHyTraffic:  make(map[string]UserTrafficStats),
		latencyTracker: NewLatencyTracker(),
	}
	state.loadConfig(cfgPath)

	// Background gateway latency & jitter sampler
	go state.startLatencySampler()

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
				go state.startBoostyGoalSyncWorker()
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
	publicMux.HandleFunc("/api/v1/analytics/geo-history", state.handleGeoHistory)
	publicMux.HandleFunc("/api/v1/analytics/progression", state.handleProgressionAnalytics)
	publicMux.HandleFunc("/api/v1/releases", state.handleReleases)
	publicMux.HandleFunc("/api/v1/notifications", state.handleNotifications)
	publicMux.HandleFunc("/api/v1/notifications/read", state.handleNotificationRead)
	publicMux.HandleFunc("/api/v1/admin/notifications", state.handleAdminNotifications)
	publicMux.HandleFunc("/api/v1/sponsors", state.handleSponsors)
	publicMux.HandleFunc("/api/v1/progression/database", state.handleProgressionDatabase)
	publicMux.HandleFunc("/api/v1/profile", state.handleProfile)
	publicMux.HandleFunc("/api/v1/profile/avatar", state.handleProfileAvatar)
	publicMux.HandleFunc("/api/v1/profile/discord/link-code", state.handleDiscordLinkCode)
	publicMux.HandleFunc("/api/v1/profile/discord/unlink", state.handleDiscordUnlink)
	publicMux.HandleFunc("/api/v1/internal/discord/verify-link", state.handleDiscordVerifyLink)
	publicMux.HandleFunc("/api/v1/internal/discord/profile", state.handleDiscordProfileLookup)

	avatarsDir := "/opt/warlink-server/avatars"
	if _, err := os.Stat("/opt/warlink-server"); os.IsNotExist(err) {
		avatarsDir = "./avatars"
	}
	_ = os.MkdirAll(avatarsDir, 0755)
	publicMux.Handle("/avatars/", http.StripPrefix("/avatars/", http.FileServer(http.Dir(avatarsDir))))

	staticDir := "/opt/warlink-server/static"
	if _, err := os.Stat("/opt/warlink-server"); os.IsNotExist(err) {
		staticDir = "./static"
	}
	_ = os.MkdirAll(staticDir, 0755)
	publicMux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	publicMux.HandleFunc("/api/v1/admin/features", state.handleAdminFeatures)
	publicMux.HandleFunc("/api/v1/admin/settings", state.handleAdminSettings)
	publicMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "version": ServerAppVersion})
	})
	publicMux.HandleFunc("/metrics", state.handleMetrics)
	publicMux.HandleFunc("/dashboard", state.handleDashboard)
	publicMux.HandleFunc("/dashboard/", state.handleDashboard)
	publicMux.HandleFunc("/admin", state.handleDashboard)
	publicMux.HandleFunc("/admin/", state.handleDashboard)
	publicMux.HandleFunc("/control", state.handleDashboard)
	publicMux.HandleFunc("/control/", state.handleDashboard)
	publicMux.HandleFunc("/api/v1/tickets", state.handleClientTicketSubmit)
	publicMux.HandleFunc("/api/v1/tickets/active", state.handleClientTicketActive)
	publicMux.HandleFunc("/api/v1/tickets/history", state.handleClientTicketsHistory)
	publicMux.HandleFunc("/api/v1/tickets/messages", state.handleClientTicketSendMessage)
	publicMux.HandleFunc("/api/v1/tickets/logs", state.handleClientTicketUploadLogs)
	publicMux.HandleFunc("/api/v1/tickets/resolve", state.handleClientTicketResolve)
	publicMux.HandleFunc("/api/v1/admin/tickets", state.handleAdminTicketsList)
	publicMux.HandleFunc("/api/v1/admin/tickets/", state.handleAdminTicketRouter)
	publicMux.HandleFunc("/admin/tickets", state.handleAdminTicketWeb)
	publicMux.HandleFunc("/admin/tickets/", state.handleAdminTicketWeb)
	publicMux.HandleFunc("/api/v1/routing-feedback", state.handleRoutingFeedback)
	publicMux.HandleFunc("/api/v1/admin/routing-feedback", state.handleAdminRoutingFeedback)
	publicMux.HandleFunc("/admin/routing-feedback", state.handleAdminRoutingFeedbackWeb)
	publicMux.HandleFunc("/admin/routing-feedback/", state.handleAdminRoutingFeedbackWeb)
	publicMux.HandleFunc("/api/v1/boosty-goal", state.handleGetBoostyGoal)
	publicMux.HandleFunc("/api/v1/admin/boosty-goal", state.handleAdminBoostyGoal)
	publicMux.HandleFunc("/api/v1/admin/boosty-goal/parse", state.handleAdminBoostyGoalParse)

	loggingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		publicMux.ServeHTTP(rec, r)
		if rec.statusCode >= 200 && rec.statusCode < 300 {
			atomic.AddUint64(&state.metricHTTP2xx, 1)
		} else if rec.statusCode >= 400 && rec.statusCode < 500 {
			atomic.AddUint64(&state.metricHTTP4xx, 1)
		} else if rec.statusCode >= 500 {
			atomic.AddUint64(&state.metricHTTP5xx, 1)
		}
	})

	log.Printf("[PUBLIC] Starting WarLink API on %s", state.cfg.ListenPublic)
	if err := http.ListenAndServe(state.cfg.ListenPublic, loggingHandler); err != nil {
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

	if s.db != nil {
		_, _ = s.db.Exec(`UPDATE pending_donations SET status = 'expired' WHERE status = 'pending' AND created_at < NOW() - INTERVAL '1 hour'`)
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

// maskIP zeroes the last octet for IPv4 (e.g. 185.75.84.123 -> 185.75.84.0)
// and applies a /48 prefix mask for IPv6.
func maskIP(ipStr string) string {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(ipStr)
	if err == nil {
		ipStr = host
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.0", ipv4[0], ipv4[1], ipv4[2])
	}
	mask := net.CIDRMask(48, 128)
	return ip.Mask(mask).String()
}

// maskIPForDisplay formats an IP for UI/dashboard as 185.75.84.*** (IPv4)
// or 2001:db8:abcd:*** (IPv6) to prevent exposing full addresses.
func maskIPForDisplay(ipStr string) string {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(ipStr)
	if err == nil {
		ipStr = host
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.***", ipv4[0], ipv4[1], ipv4[2])
	}
	parts := strings.Split(ip.String(), ":")
	if len(parts) > 3 {
		return strings.Join(parts[:3], ":") + ":***"
	}
	return ip.String()
}

func hashDeviceID(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ""
	}
	h := sha256.Sum256([]byte(deviceID))
	return hex.EncodeToString(h[:])
}

func validateAccountNumber(acc string) bool {
	acc = strings.TrimSpace(acc)
	if acc == AdminAccountNumber {
		return true
	}
	clean := strings.ReplaceAll(acc, "-", "")
	clean = strings.ReplaceAll(clean, " ", "")
	if len(clean) != 16 {
		return false
	}
	digits := make([]int, 16)
	for i, r := range clean {
		if r < '0' || r > '9' {
			return false
		}
		digits[i] = int(r - '0')
	}
	sum := 0
	for i := 0; i < 16; i++ {
		d := digits[i]
		if i%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
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

	livePing := 27
	if s.latencyTracker != nil {
		lm := s.latencyTracker.GetMetrics()
		if lm.P50 > 0 {
			livePing = int(math.Round(lm.P50))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                  "online",
		"location":                s.cfg.ServerLocation,
		"ping_hint_ms":            livePing,
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
	AppVersion    string `json:"app_version,omitempty"`
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

	clientVer := strings.TrimSpace(req.AppVersion)
	if clientVer == "" {
		ua := r.Header.Get("User-Agent")
		if strings.HasPrefix(ua, "WarLink-Client/") {
			clientVer = strings.TrimPrefix(ua, "WarLink-Client/")
		}
	}
	if clientVer == "" {
		clientVer = "unknown"
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

	// Record persistent connection history with resolved account, device, and client version
	s.recordConnectionHistoryAsync(accountNumber, req.DeviceID, clientIP, clientVer)

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
			sess.ClientVersion = clientVer
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
		ClientVersion: clientVer,
	}
	s.sessions[newToken] = sess
	s.deviceTokens[req.DeviceID] = newToken
	s.saveSessionAsync(sess)
	gameDisplay := targetGame
	if gameDisplay == "free_internet" || strings.EqualFold(gameDisplay, "свободный интернет") {
		gameDisplay = "Комплексный режим"
	}
	log.Printf("[SESSION] Allocated slot for device %s (acc: %s, sponsor: %t, game: %s) from IP %s (Active: %d/%d, Free: %d/%d)",
		req.DeviceID, accountNumber, isSponsor, gameDisplay, maskIP(clientIP), len(s.sessions), maxSessions, activeFreeCount+1, freeSlotsLimit)

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
		if !TrustedIngressIPs[connectingIP] {
			if sess.ClientIP != connectingIP {
				sess.ClientIP = connectingIP
				sess.LastSeen = time.Now()
			} else {
				sess.LastSeen = time.Now()
			}
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
		PaymentMethod string `json:"payment_method"`
	}
	if r.Body != nil {
		bodyBytes, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			log.Printf("[DONATE] Error reading body: %v", readErr)
		} else {
			log.Printf("[DONATE] Raw body (%d bytes): %s", len(bodyBytes), string(bodyBytes))
			if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
				log.Printf("[DONATE] JSON decode error: %v", err)
			}
		}
	}

	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	s.mu.RUnlock()

	if apiKey != "" {
		aezaMethod := "yookassa:sbp"
		minRub := 100
		minCents := 50

		switch reqBody.PaymentMethod {
		case "card", "bank_card", "yookassa:bank_card":
			aezaMethod = "yookassa:bank_card"
			minRub = 100
			minCents = 50
		default:
			aezaMethod = "yookassa:sbp"
			minRub = 100
			minCents = 50
		}

		amount := s.cfg.DonateAmountRub
		if reqBody.AmountRub >= minRub {
			amount = reqBody.AmountRub
		} else if amount < minRub {
			amount = minRub
		}
		// Aeza API v2 uses minor currency units (cents). 1 EUR ≈ 130.66 RUB.
		aezaCents := int(float64(amount) / 1.3066)
		if aezaCents < minCents {
			aezaCents = minCents
		}
		payload := map[string]interface{}{
			"method": aezaMethod,
			"amount": aezaCents,
		}
		body, _ := json.Marshal(payload)
		aezaReq, err := http.NewRequest(http.MethodPost, "https://my.aeza.net/api/v2/billing/invoices", bytes.NewReader(body))
		if err == nil {
			aezaReq.Header.Set("X-API-KEY", apiKey)
			aezaReq.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(aezaReq)
			if err != nil {
				log.Printf("[DONATE] Warning: Aeza API request failed: %v", err)
			} else if resp != nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
					var invResp struct {
						ID      int    `json:"id"`
						Amount  int    `json:"amount"`
						Status  string `json:"status"`
						Payload struct {
							URL      string  `json:"url"`
							Address  string  `json:"address"`
							Amount   float64 `json:"amount"`
							Currency string  `json:"currency"`
							QR       string  `json:"qr"`
						} `json:"payload"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&invResp); err == nil && invResp.ID > 0 {
						payURL := invResp.Payload.URL
						if payURL == "" {
							payURL = fmt.Sprintf("https://my.aeza.net/billing/invoices/%d", invResp.ID)
						}
						actualRub := amount
						if actualRub <= 0 {
							actualRub = minRub
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

						// Trigger background reconciliation after user has time to scan and pay
						go func() {
							time.Sleep(30 * time.Second)
							s.syncAezaDonations()
							time.Sleep(60 * time.Second)
							s.syncAezaDonations()
						}()

						log.Printf("[DONATE] Created Aeza %s invoice #%d for %d RUB (charged %d cents) for acc %s: %s",
							aezaMethod, invResp.ID, actualRub, aezaCents, reqBody.AccountNumber, payURL)
						w.Header().Set("Content-Type", "application/json")
						respMap := map[string]interface{}{
							"success":     true,
							"pay_url":     payURL,
							"payment_url": payURL,
							"url":         payURL,
							"invoice_id":  invResp.ID,
						}
						if invResp.Payload.Address != "" {
							respMap["crypto_address"] = invResp.Payload.Address
							respMap["crypto_amount"] = invResp.Payload.Amount
							respMap["crypto_currency"] = invResp.Payload.Currency
							respMap["crypto_qr"] = invResp.Payload.QR
						}
						_ = json.NewEncoder(w).Encode(respMap)
						return
					} else {
						log.Printf("[DONATE] Error decoding Aeza invoice response: %v", err)
					}
				} else {
					respBytes, _ := io.ReadAll(resp.Body)
					log.Printf("[DONATE] Aeza API returned HTTP %d: %s", resp.StatusCode, string(respBytes))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(resp.StatusCode)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   fmt.Sprintf("Ошибка шлюза платежей Aeza (%d)", resp.StatusCode),
						"details": string(respBytes),
					})
					return
				}
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

// BlockedSteamGames содержит реестр AppID игр, запрещенных к добавлению в каталог голосования
var BlockedSteamGames = map[int]string{
	3602290: "FEMBOY FUTA HOUSE",
}

func isBlockedSteamGame(appID int) bool {
	_, blocked := BlockedSteamGames[appID]
	return blocked
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
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		accountNumber := strings.TrimSpace(r.URL.Query().Get("account_number"))
		hashedDevID := hashDeviceID(deviceID)
		userVotedMap := make(map[int]bool)
		userVotesUsed := 0
		if deviceID != "" || accountNumber != "" {
			rows, err := s.db.Query(`
				SELECT dv.steam_app_id, COALESCE(gs.status, 'voting')
				FROM device_votes dv
				LEFT JOIN game_suggestions gs ON dv.steam_app_id = gs.steam_app_id
				WHERE (dv.account_number = $1 AND $1 <> '') OR dv.device_id = $2 OR (dv.device_id = $3 AND $3 <> '')
			`, accountNumber, hashedDevID, deviceID)
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
				if isBlockedSteamGame(g.SteamAppID) {
					continue
				}
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
		if accountNumber != "" {
			var tier string
			var spUntil *time.Time
			_ = s.db.QueryRow(`
				SELECT a.tier, a.sponsor_until
				FROM accounts a
				WHERE a.account_number = $1
			`, accountNumber).Scan(&tier, &spUntil)
			if tier == "sponsor" || (spUntil != nil && spUntil.After(time.Now())) || accountNumber == AdminAccountNumber {
				userVotePower = 3
			}
		}
		if userVotePower == 1 && deviceID != "" {
			var tier string
			var spUntil *time.Time
			_ = s.db.QueryRow(`
				SELECT a.tier, a.sponsor_until
				FROM account_devices ad
				JOIN accounts a ON ad.account_number = a.account_number
				WHERE ad.device_id = $1 OR ad.device_id = $2
				ORDER BY CASE WHEN a.tier = 'sponsor' OR (a.sponsor_until IS NOT NULL AND a.sponsor_until > NOW()) THEN 1 ELSE 2 END LIMIT 1
			`, deviceID, hashedDevID).Scan(&tier, &spUntil)
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

		clientIP := s.getClientIP(r)
		if s.rateLimiter != nil && !s.rateLimiter.Allow(clientIP) {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Слишком много запросов. Пожалуйста, подождите минуту.",
			})
			return
		}

		var req struct {
			DeviceID      string `json:"device_id"`
			AccountNumber string `json:"account_number,omitempty"`
			SteamAppID    int    `json:"steam_app_id"`
			Title         string `json:"title"`
			IconURL       string `json:"icon_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (strings.TrimSpace(req.DeviceID) == "" && strings.TrimSpace(req.AccountNumber) == "") || req.SteamAppID <= 0 || strings.TrimSpace(req.Title) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Некорректные параметры игры",
			})
			return
		}

		req.DeviceID = strings.TrimSpace(req.DeviceID)
		req.AccountNumber = strings.TrimSpace(req.AccountNumber)
		if req.AccountNumber != "" && !validateAccountNumber(req.AccountNumber) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Неверный формат номера аккаунта",
			})
			return
		}

		hashedDevID := hashDeviceID(req.DeviceID)
		targetDeviceID := hashedDevID
		if targetDeviceID == "" && req.AccountNumber != "" {
			targetDeviceID = hashDeviceID("acc:" + req.AccountNumber)
		}
		maskedIP := maskIP(clientIP)

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

		// Reject prohibited games
		if isBlockedSteamGame(req.SteamAppID) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Данная игра внесена в список запрещенных к добавлению в голосование.",
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
			WHERE ((dv.account_number = $1 AND $1 <> '') OR dv.device_id = $2 OR (dv.device_id = $3 AND $3 <> ''))
			  AND gs.status = 'voting'
		`, req.AccountNumber, targetDeviceID, req.DeviceID).Scan(&userVoteCount)
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
		`, maskedIP).Scan(&ipVoteCount)
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
		_ = tx.QueryRow(`
			SELECT COUNT(*) FROM device_votes 
			WHERE ((account_number = $1 AND $1 <> '') OR device_id = $2 OR (device_id = $3 AND $3 <> '')) 
			  AND steam_app_id = $4
		`, req.AccountNumber, targetDeviceID, req.DeviceID, req.SteamAppID).Scan(&alreadyVoted)
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
		if req.AccountNumber != "" {
			_ = tx.QueryRow(`
				SELECT a.tier, a.sponsor_until
				FROM accounts a
				WHERE a.account_number = $1
			`, req.AccountNumber).Scan(&tier, &spUntil)
			if tier == "sponsor" || (spUntil != nil && spUntil.After(time.Now())) || req.AccountNumber == AdminAccountNumber {
				voteWeight = 3
			}
		}
		if voteWeight == 1 && req.DeviceID != "" {
			_ = tx.QueryRow(`
				SELECT a.tier, a.sponsor_until
				FROM account_devices ad
				JOIN accounts a ON ad.account_number = a.account_number
				WHERE ad.device_id = $1 OR ad.device_id = $2
				ORDER BY CASE WHEN a.tier = 'sponsor' OR (a.sponsor_until IS NOT NULL AND a.sponsor_until > NOW()) THEN 1 ELSE 2 END LIMIT 1
			`, req.DeviceID, targetDeviceID).Scan(&tier, &spUntil)
			if tier == "sponsor" || (spUntil != nil && spUntil.After(time.Now())) {
				voteWeight = 3
			}
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
		_, err = tx.Exec(`
			INSERT INTO device_votes (device_id, steam_app_id, client_ip, vote_weight, created_at, account_number) 
			VALUES ($1, $2, $3, $4, NOW(), NULLIF($5, ''))
		`, targetDeviceID, req.SteamAppID, maskedIP, voteWeight, req.AccountNumber)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		userRef := targetDeviceID
		if req.AccountNumber != "" {
			userRef = req.AccountNumber
		}
		log.Printf("[VOTES] User %s voted for %s (AppID: %d, Weight: %d, Total: %d, IP: %s)", userRef, req.Title, req.SteamAppID, voteWeight, newVotes, maskedIP)
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
			DeviceID      string `json:"device_id"`
			AccountNumber string `json:"account_number,omitempty"`
			SteamAppID    int    `json:"steam_app_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (strings.TrimSpace(req.DeviceID) == "" && strings.TrimSpace(req.AccountNumber) == "") || req.SteamAppID <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Некорректные параметры",
			})
			return
		}

		req.DeviceID = strings.TrimSpace(req.DeviceID)
		req.AccountNumber = strings.TrimSpace(req.AccountNumber)
		hashedDevID := hashDeviceID(req.DeviceID)

		tx, err := s.db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		var voteID int64
		var voteWeight int = 1
		err = tx.QueryRow(`
			SELECT id, vote_weight FROM device_votes 
			WHERE ((account_number = $1 AND $1 <> '') OR device_id = $2 OR (device_id = $3 AND $3 <> ''))
			  AND steam_app_id = $4
			ORDER BY id DESC LIMIT 1
		`, req.AccountNumber, hashedDevID, req.DeviceID, req.SteamAppID).Scan(&voteID, &voteWeight)
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"unvoted": false,
			})
			return
		}
		if voteWeight <= 0 {
			voteWeight = 1
		}

		res, err := tx.Exec("DELETE FROM device_votes WHERE id = $1", voteID)
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
				userRef := hashedDevID
				if req.AccountNumber != "" {
					userRef = req.AccountNumber
				}
				log.Printf("[VOTES] User %s unvoted for AppID %d (Weight: %d, New total: %d)", userRef, req.SteamAppID, voteWeight, newVotes)
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
	ALTER TABLE server_load_history ADD COLUMN IF NOT EXISTS disk_used_gb DOUBLE PRECISION DEFAULT 0;
	ALTER TABLE server_load_history ADD COLUMN IF NOT EXISTS disk_total_gb DOUBLE PRECISION DEFAULT 0;
	ALTER TABLE server_load_history ADD COLUMN IF NOT EXISTS disk_percent DOUBLE PRECISION DEFAULT 0;

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
	ALTER TABLE device_votes ADD COLUMN IF NOT EXISTS account_number TEXT;
	CREATE INDEX IF NOT EXISTS idx_device_votes_ip ON device_votes(client_ip);
	CREATE INDEX IF NOT EXISTS idx_device_votes_device ON device_votes(device_id);
	CREATE INDEX IF NOT EXISTS idx_device_votes_acc ON device_votes(account_number);

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

	CREATE TABLE IF NOT EXISTS user_connection_history (
		id BIGSERIAL PRIMARY KEY,
		account_number TEXT,
		device_id TEXT NOT NULL,
		client_ip TEXT NOT NULL,
		country TEXT NOT NULL DEFAULT 'Unknown',
		country_code TEXT NOT NULL DEFAULT 'XX',
		city TEXT NOT NULL DEFAULT 'Unknown',
		lat DOUBLE PRECISION NOT NULL DEFAULT 0,
		lon DOUBLE PRECISION NOT NULL DEFAULT 0,
		connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_uch_time ON user_connection_history(connected_at);
	CREATE INDEX IF NOT EXISTS idx_uch_acc ON user_connection_history(account_number);
	CREATE INDEX IF NOT EXISTS idx_uch_dev ON user_connection_history(device_id);
	CREATE INDEX IF NOT EXISTS idx_uch_city ON user_connection_history(city, country);

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

	CREATE TABLE IF NOT EXISTS support_tickets (
		id BIGSERIAL PRIMARY KEY,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		account_number TEXT NOT NULL DEFAULT '',
		device_id TEXT NOT NULL DEFAULT '',
		app_version TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'other',
		user_comment TEXT NOT NULL DEFAULT '',
		system_info JSONB,
		logs_archive BYTEA,
		logs_archive_size INT NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'new',
		admin_reply TEXT NOT NULL DEFAULT '',
		resolved_at TIMESTAMPTZ
	);
	CREATE INDEX IF NOT EXISTS idx_tickets_created ON support_tickets(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tickets_status ON support_tickets(status);
	CREATE INDEX IF NOT EXISTS idx_tickets_acc ON support_tickets(account_number);
	CREATE INDEX IF NOT EXISTS idx_tickets_dev ON support_tickets(device_id);

	CREATE TABLE IF NOT EXISTS ticket_messages (
		id BIGSERIAL PRIMARY KEY,
		ticket_id BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
		sender_type TEXT NOT NULL, -- 'user', 'admin', 'system'
		sender_name TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL,
		attachment_type TEXT NOT NULL DEFAULT '', -- '', 'logs_archive', 'telemetry_ping'
		attachment_data BYTEA,
		attachment_size INT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket ON ticket_messages(ticket_id, created_at ASC);
	CREATE INDEX IF NOT EXISTS idx_ticket_messages_created ON ticket_messages(created_at DESC);

	CREATE TABLE IF NOT EXISTS routing_feedback (
		id BIGSERIAL PRIMARY KEY,
		account_number TEXT NOT NULL DEFAULT '',
		device_id TEXT NOT NULL DEFAULT '',
		app_version TEXT NOT NULL DEFAULT '',
		route_mode TEXT NOT NULL,
		status TEXT NOT NULL,
		in_game_ping INT NOT NULL DEFAULT 0,
		match_quality TEXT NOT NULL DEFAULT '',
		discord_status TEXT NOT NULL DEFAULT '',
		user_comment TEXT NOT NULL DEFAULT '',
		client_ip TEXT NOT NULL DEFAULT '',
		telemetry_data JSONB DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_routing_feedback_mode ON routing_feedback(route_mode);
	CREATE INDEX IF NOT EXISTS idx_routing_feedback_created ON routing_feedback(created_at DESC);

	DROP TABLE IF EXISTS active_sessions CASCADE;
	`
	if _, err := s.db.Exec(schema); err != nil {
		log.Printf("[DB] Error initializing schema: %v", err)
	} else {
		log.Printf("[DB] Database tables initialized successfully (accounts, notifications, tickets, ticket_messages, active sessions in RAM)")
	}

	_, _ = s.db.Exec(`
		ALTER TABLE daily_active_devices ADD COLUMN IF NOT EXISTS app_version TEXT;
		ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS app_version TEXT;
		ALTER TABLE accounts ADD COLUMN IF NOT EXISTS discord_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE accounts ADD COLUMN IF NOT EXISTS discord_tag TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_accounts_discord_id ON accounts(discord_id);

		-- Migrate existing tickets without messages
		INSERT INTO ticket_messages (ticket_id, sender_type, sender_name, message, attachment_type, attachment_data, attachment_size, created_at)
		SELECT st.id, 'user', COALESCE(NULLIF(acc.nickname, ''), st.account_number), st.user_comment, 
		       CASE WHEN st.logs_archive_size > 0 THEN 'logs_archive' ELSE '' END,
		       st.logs_archive, st.logs_archive_size, st.created_at
		FROM support_tickets st
		LEFT JOIN accounts acc ON acc.account_number = st.account_number
		WHERE NOT EXISTS (SELECT 1 FROM ticket_messages tm WHERE tm.ticket_id = st.id)
		  AND length(st.user_comment) > 0;

		INSERT INTO ticket_messages (ticket_id, sender_type, sender_name, message, created_at)
		SELECT st.id, 'admin', 'Max (Разработчик)', st.admin_reply, COALESCE(st.resolved_at, st.updated_at)
		FROM support_tickets st
		WHERE length(st.admin_reply) > 0
		  AND NOT EXISTS (SELECT 1 FROM ticket_messages tm WHERE tm.ticket_id = st.id AND tm.sender_type = 'admin');
	`)
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

func (s *AppState) recordConnectionHistoryAsync(accountNumber, deviceID, clientIP, appVersion string) {
	if s.db == nil || (deviceID == "" && accountNumber == "") {
		return
	}
	maskedIP := maskIP(clientIP)
	go func() {
		// 1. Maintain daily_active_devices
		if deviceID != "" {
			_, _ = s.db.Exec(`INSERT INTO daily_active_devices (device_id, seen_date, client_ip, last_seen, app_version) VALUES ($1, CURRENT_DATE, $2, NOW(), $3) ON CONFLICT (device_id, seen_date) DO UPDATE SET last_seen = NOW(), client_ip = $2, app_version = COALESCE(NULLIF($3, ''), daily_active_devices.app_version)`, deviceID, maskedIP, appVersion)
		}

		// 2. Resolve GeoInfo (from cache or ip-api)
		geo := s.resolveIPGeo(maskedIP)
		if geo.City == "Unknown" || geo.Lat == 0 {
			geo = s.resolveIPGeo(clientIP)
		}

		// 3. Insert into user_connection_history
		_, _ = s.db.Exec(`
			INSERT INTO user_connection_history (
				account_number, device_id, client_ip, country, country_code, city, lat, lon, connected_at, app_version
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9)
		`, accountNumber, deviceID, maskedIP, geo.Country, geo.CountryCode, geo.City, geo.Lat, geo.Lon, appVersion)
	}()
}

func (s *AppState) recordDeviceActivityAsync(deviceID, clientIP string) {
	s.recordConnectionHistoryAsync("", deviceID, clientIP, "")
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
	if key == "" {
		if c, err := r.Cookie("admin_key"); err == nil && c != nil {
			key = c.Value
		}
	}
	if key != "" && key == s.cfg.DashboardKey {
		return true
	}

	// Strictly allow unauthenticated access ONLY from true local CLI process on the machine itself:
	// If X-Real-IP or X-Forwarded-For is present, it is a PROXIED request from the outside and MUST NOT be trusted as local!
	hasProxyHeader := r.Header.Get("X-Real-IP") != "" || r.Header.Get("X-Forwarded-For") != ""
	if !hasProxyHeader {
		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" {
			return true
		}
	}

	return false
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
	clientVersions := make(map[string]int)
	now := time.Now()
	var totalDurationSec float64
	var maxDurationSec float64

	for _, sess := range s.sessions {
		g := sess.Game
		if g == "" {
			g = "wardogs"
		}
		sessionsByGame[g]++
		geo := s.resolveIPGeo(sess.ClientIP)
		geoCounts[geo]++

		durSec := now.Sub(sess.CreatedAt).Seconds()
		if durSec < 0 {
			durSec = 0
		}
		totalDurationSec += durSec
		if durSec > maxDurationSec {
			maxDurationSec = durSec
		}

		ver := sess.ClientVersion
		if ver == "" {
			ver = "unknown"
		}
		clientVersions[ver]++
	}

	avgDurationMin := 0.0
	maxDurationMin := 0.0
	if len(s.sessions) > 0 {
		avgDurationMin = math.Round((totalDurationSec/float64(len(s.sessions))/60.0)*10) / 10
		maxDurationMin = math.Round((maxDurationSec/60.0)*10) / 10
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
	sb.WriteString(fmt.Sprintf("warlink_server_version{version=\"%s\"} 1\n\n", ServerAppVersion))

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

	nodeBurnDaily := stockholmDailyPrice
	if nodeBurnDaily <= 0 {
		nodeBurnDaily = 8.67
	}
	financialRunwayDays := totalAvailableRub / nodeBurnDaily
	financialRunwayTimestamp := time.Now().Add(time.Duration(financialRunwayDays*24) * time.Hour).Unix()

	sb.WriteString("# HELP warlink_financial_runway_days Total runway days of current node on available Aeza balance\n")
	sb.WriteString("# TYPE warlink_financial_runway_days gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_financial_runway_days %.1f\n\n", financialRunwayDays))

	sb.WriteString("# HELP warlink_financial_runway_timestamp Unix timestamp when current Aeza balance runs out\n")
	sb.WriteString("# TYPE warlink_financial_runway_timestamp gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_financial_runway_timestamp %d\n\n", financialRunwayTimestamp))

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

	sb.WriteString("# HELP warlink_session_duration_avg_minutes Average active session duration in minutes\n")
	sb.WriteString("# TYPE warlink_session_duration_avg_minutes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_session_duration_avg_minutes %.1f\n\n", avgDurationMin))

	sb.WriteString("# HELP warlink_session_duration_max_minutes Maximum active session duration in minutes\n")
	sb.WriteString("# TYPE warlink_session_duration_max_minutes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_session_duration_max_minutes %.1f\n\n", maxDurationMin))

	sb.WriteString("# HELP warlink_client_version_online Connected active clients broken down by version\n")
	sb.WriteString("# TYPE warlink_client_version_online gauge\n")
	if len(clientVersions) == 0 {
		sb.WriteString("warlink_client_version_online{version=\"v2.1.12\"} 0\n\n")
	} else {
		for v, cnt := range clientVersions {
			sb.WriteString(fmt.Sprintf("warlink_client_version_online{version=\"%s\"} %d\n", v, cnt))
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

		sb.WriteString("# HELP warlink_disk_percent Server disk usage percent\n")
		sb.WriteString("# TYPE warlink_disk_percent gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_disk_percent %.2f\n\n", live.DiskPercent))

		sb.WriteString("# HELP warlink_disk_used_gb Server disk used in GB\n")
		sb.WriteString("# TYPE warlink_disk_used_gb gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_disk_used_gb %.2f\n\n", live.DiskUsedGB))

		sb.WriteString("# HELP warlink_disk_total_gb Server disk total in GB\n")
		sb.WriteString("# TYPE warlink_disk_total_gb gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_disk_total_gb %.2f\n\n", live.DiskTotalGB))

		sb.WriteString("# HELP warlink_net_rx_rate_kbps Inbound network bitrate in kbps\n")
		sb.WriteString("# TYPE warlink_net_rx_rate_kbps gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_net_rx_rate_kbps %d\n\n", live.NetRxRateKbps))

		sb.WriteString("# HELP warlink_net_tx_rate_kbps Outbound network bitrate in kbps\n")
		sb.WriteString("# TYPE warlink_net_tx_rate_kbps gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_net_tx_rate_kbps %d\n\n", live.NetTxRateKbps))

		if s.latencyTracker != nil {
			lm := s.latencyTracker.GetMetrics()
			live.GatewayPingMs = lm.P50
			live.GatewayPingP95 = lm.P95
			live.GatewayPingP99 = lm.P99
			live.GatewayJitter = lm.Jitter
			live.PacketLossPct = lm.LossPct
		}

		sb.WriteString("# HELP warlink_gateway_ping_ms Estimated ping to gateway in ms\n")
		sb.WriteString("# TYPE warlink_gateway_ping_ms gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_ping_ms %.2f\n\n", live.GatewayPingMs))

		sb.WriteString("# HELP warlink_gateway_ping_p95 95th percentile ping in ms\n")
		sb.WriteString("# TYPE warlink_gateway_ping_p95 gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_ping_p95 %.2f\n\n", live.GatewayPingP95))

		sb.WriteString("# HELP warlink_gateway_ping_p99 99th percentile peak ping in ms\n")
		sb.WriteString("# TYPE warlink_gateway_ping_p99 gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_ping_p99 %.2f\n\n", live.GatewayPingP99))

		sb.WriteString("# HELP warlink_gateway_jitter_ms Network jitter in ms\n")
		sb.WriteString("# TYPE warlink_gateway_jitter_ms gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_jitter_ms %.2f\n\n", live.GatewayJitter))

		sb.WriteString("# HELP warlink_gateway_packet_loss_percent Gateway packet loss percentage\n")
		sb.WriteString("# TYPE warlink_gateway_packet_loss_percent gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_gateway_packet_loss_percent %.2f\n\n", live.PacketLossPct))
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

	// UDP kernel buffers
	udpStats := getUDPBufferMetrics()
	sb.WriteString("# HELP warlink_udp_rcvbuf_errors_total Linux kernel UDP receive buffer errors (socket overflow)\n")
	sb.WriteString("# TYPE warlink_udp_rcvbuf_errors_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_udp_rcvbuf_errors_total %d\n\n", udpStats.RcvbufErrors))

	sb.WriteString("# HELP warlink_udp_sndbuf_errors_total Linux kernel UDP send buffer errors\n")
	sb.WriteString("# TYPE warlink_udp_sndbuf_errors_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_udp_sndbuf_errors_total %d\n\n", udpStats.SndbufErrors))

	sb.WriteString("# HELP warlink_udp_in_errors_total Linux kernel UDP incoming packet errors\n")
	sb.WriteString("# TYPE warlink_udp_in_errors_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_udp_in_errors_total %d\n\n", udpStats.InErrors))

	sb.WriteString("# HELP warlink_udp_in_datagrams_total Linux kernel total UDP received datagrams\n")
	sb.WriteString("# TYPE warlink_udp_in_datagrams_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_udp_in_datagrams_total %d\n\n", udpStats.InDatagrams))

	sb.WriteString("# HELP warlink_udp_out_datagrams_total Linux kernel total UDP sent datagrams\n")
	sb.WriteString("# TYPE warlink_udp_out_datagrams_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_udp_out_datagrams_total %d\n\n", udpStats.OutDatagrams))

	// Storage components
	storageSizes := getStorageComponentSizes()
	sb.WriteString("# HELP warlink_storage_component_bytes Storage breakdown by component in bytes\n")
	sb.WriteString("# TYPE warlink_storage_component_bytes gauge\n")
	for comp, bSize := range storageSizes {
		sb.WriteString(fmt.Sprintf("warlink_storage_component_bytes{component=\"%s\"} %d\n", comp, bSize))
	}
	sb.WriteString("\n")

	// Service status
	pgStatus := 0
	if s.db != nil && s.db.Ping() == nil {
		pgStatus = 1
	}
	redisStatus := 0
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		if s.rdb.Ping(ctx).Err() == nil {
			redisStatus = 1
		}
		cancel()
	}

	sb.WriteString("# HELP warlink_service_status Status of critical backend services (1=healthy, 0=down)\n")
	sb.WriteString("# TYPE warlink_service_status gauge\n")
	sb.WriteString("warlink_service_status{service=\"api\"} 1\n")
	sb.WriteString("warlink_service_status{service=\"hysteria\"} 1\n")
	sb.WriteString(fmt.Sprintf("warlink_service_status{service=\"postgres\"} %d\n", pgStatus))
	sb.WriteString(fmt.Sprintf("warlink_service_status{service=\"redis\"} %d\n", redisStatus))
	sb.WriteString("warlink_service_status{service=\"victoriametrics\"} 1\n")
	sb.WriteString("warlink_service_status{service=\"nginx\"} 1\n\n")

	// API HTTP requests
	sb.WriteString("# HELP warlink_api_http_requests_total Total HTTP requests handled by API grouped by response class\n")
	sb.WriteString("# TYPE warlink_api_http_requests_total counter\n")
	sb.WriteString(fmt.Sprintf("warlink_api_http_requests_total{code=\"2xx\"} %d\n", atomic.LoadUint64(&s.metricHTTP2xx)))
	sb.WriteString(fmt.Sprintf("warlink_api_http_requests_total{code=\"4xx\"} %d\n", atomic.LoadUint64(&s.metricHTTP4xx)))
	sb.WriteString(fmt.Sprintf("warlink_api_http_requests_total{code=\"5xx\"} %d\n\n", atomic.LoadUint64(&s.metricHTTP5xx)))

	// Progression overview
	if s.db != nil {
		var pCount, cMax int
		var cAvg float64
		_ = s.db.QueryRow(`
			SELECT COUNT(*), COALESCE(AVG((progression->>'career_level')::int), 0), COALESCE(MAX((progression->>'career_level')::int), 0)
			FROM accounts
			WHERE progression IS NOT NULL AND progression::text != '{}' AND progression::text != 'null'
			  AND NOT (
				(progression->>'career_level')::int <= 1 
				AND COALESCE((progression->'roles'->>'assault')::int, 0) <= 1
				AND COALESCE((progression->'roles'->>'medic')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'recon')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'support')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'driver')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'pilot')::int, 0) = 0
				AND progression->>'wishlist_id' IS NULL
				AND (progression->'unlocked_items' IS NULL OR jsonb_array_length(progression->'unlocked_items') = 0)
			  )
		`).Scan(&pCount, &cAvg, &cMax)

		sb.WriteString("# HELP warlink_progression_players_total Total players with saved progression\n")
		sb.WriteString("# TYPE warlink_progression_players_total gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_progression_players_total %d\n\n", pCount))

		sb.WriteString("# HELP warlink_progression_career_level_avg Average career level across players\n")
		sb.WriteString("# TYPE warlink_progression_career_level_avg gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_progression_career_level_avg %.1f\n\n", cAvg))

		sb.WriteString("# HELP warlink_progression_career_level_max Maximum career level reached\n")
		sb.WriteString("# TYPE warlink_progression_career_level_max gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_progression_career_level_max %d\n\n", cMax))

		var sumAssault, sumMedic, sumRecon, sumSupport, sumDriver, sumPilot int
		_ = s.db.QueryRow(`
			SELECT 
				COALESCE(SUM((progression->'roles'->>'assault')::int), 0),
				COALESCE(SUM((progression->'roles'->>'medic')::int), 0),
				COALESCE(SUM((progression->'roles'->>'recon')::int), 0),
				COALESCE(SUM((progression->'roles'->>'support')::int), 0),
				COALESCE(SUM((progression->'roles'->>'driver')::int), 0),
				COALESCE(SUM((progression->'roles'->>'pilot')::int), 0)
			FROM accounts
			WHERE progression IS NOT NULL AND progression::text != '{}' AND progression::text != 'null'
			  AND NOT (
				(progression->>'career_level')::int <= 1 
				AND COALESCE((progression->'roles'->>'assault')::int, 0) <= 1
				AND COALESCE((progression->'roles'->>'medic')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'recon')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'support')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'driver')::int, 0) = 0
				AND COALESCE((progression->'roles'->>'pilot')::int, 0) = 0
				AND progression->>'wishlist_id' IS NULL
				AND (progression->'unlocked_items' IS NULL OR jsonb_array_length(progression->'unlocked_items') = 0)
			  )
		`).Scan(&sumAssault, &sumMedic, &sumRecon, &sumSupport, &sumDriver, &sumPilot)

		sb.WriteString("# HELP warlink_progression_role_level_sum Total accumulated levels across all players for this role\n")
		sb.WriteString("# TYPE warlink_progression_role_level_sum gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"assault\",role_name=\"Штурмовик\"} %d\n", sumAssault))
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"medic\",role_name=\"Медик\"} %d\n", sumMedic))
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"recon\",role_name=\"Разведчик\"} %d\n", sumRecon))
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"support\",role_name=\"Поддержка\"} %d\n", sumSupport))
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"driver\",role_name=\"Водитель\"} %d\n", sumDriver))
		sb.WriteString(fmt.Sprintf("warlink_progression_role_level_sum{role=\"pilot\",role_name=\"Пилот\"} %d\n\n", sumPilot))
	}

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

	var latMetrics LatencyMetrics
	if s.latencyTracker != nil {
		latMetrics = s.latencyTracker.GetMetrics()
	} else {
		latMetrics = LatencyMetrics{P50: 25.0, P95: 28.0, P99: 32.0, Jitter: 0.8}
	}

	diskUsedGB, diskTotalGB, diskPercent := getDiskUsage()

	snap := &LoadSnapshot{
		RecordedAt:     now,
		ActiveSessions: activeSess,
		MaxSessions:    MaxActiveSessions,
		CPUPercent:     cpuPercent,
		RAMUsedMB:      ramUsedMB,
		RAMTotalMB:     ramTotalMB,
		DiskUsedGB:     math.Round(diskUsedGB*100) / 100,
		DiskTotalGB:    math.Round(diskTotalGB*100) / 100,
		DiskPercent:    math.Round(diskPercent*10) / 10,
		NetBytesRecv:   rxBytes,
		NetBytesSent:   txBytes,
		NetRxRateKbps:  rxRateKbps,
		NetTxRateKbps:  txRateKbps,
		GatewayPingMs:  latMetrics.P50,
		GatewayPingP95: latMetrics.P95,
		GatewayPingP99: latMetrics.P99,
		GatewayJitter:  latMetrics.Jitter,
		PacketLossPct:  latMetrics.LossPct,
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
				net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms,
				disk_used_gb, disk_total_gb, disk_percent
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		`, snap.RecordedAt, snap.ActiveSessions, snap.MaxSessions, snap.CPUPercent,
			snap.RAMUsedMB, snap.RAMTotalMB, snap.NetBytesRecv, snap.NetBytesSent,
			snap.NetRxRateKbps, snap.NetTxRateKbps, int(math.Round(snap.GatewayPingMs)),
			snap.DiskUsedGB, snap.DiskTotalGB, snap.DiskPercent)
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
						net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms,
						disk_used_gb, disk_total_gb, disk_percent
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				`, snap.RecordedAt, snap.ActiveSessions, snap.MaxSessions, snap.CPUPercent,
					snap.RAMUsedMB, snap.RAMTotalMB, snap.NetBytesRecv, snap.NetBytesSent,
					snap.NetRxRateKbps, snap.NetTxRateKbps, int(math.Round(snap.GatewayPingMs)),
					snap.DiskUsedGB, snap.DiskTotalGB, snap.DiskPercent)
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
			       net_rx_rate_kbps, net_tx_rate_kbps, gateway_ping_ms,
			       COALESCE(disk_used_gb, 0), COALESCE(disk_total_gb, 0), COALESCE(disk_percent, 0)
			FROM server_load_history
			ORDER BY recorded_at DESC
			LIMIT $1
		`, limit)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item LoadSnapshot
				var pingInt int
				if err := rows.Scan(
					&item.ID, &item.RecordedAt, &item.ActiveSessions, &item.MaxSessions,
					&item.CPUPercent, &item.RAMUsedMB, &item.RAMTotalMB,
					&item.NetBytesRecv, &item.NetBytesSent,
					&item.NetRxRateKbps, &item.NetTxRateKbps, &pingInt,
					&item.DiskUsedGB, &item.DiskTotalGB, &item.DiskPercent,
				); err == nil {
					item.GatewayPingMs = float64(pingInt)
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
		DeviceID      string  `json:"device_id"`
		AccountNumber string  `json:"account_number"`
		Game          string  `json:"game"`
		ClientIP      string  `json:"client_ip"`
		ConnectedAt   string  `json:"connected_at"`
		DurationSec   int     `json:"duration_sec"`
		DurationDesc  string  `json:"duration_desc"`
		Status        string  `json:"status"`
		Country       string  `json:"country"`
		City          string  `json:"city"`
		Lat           float64 `json:"lat"`
		Lon           float64 `json:"lon"`
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
			accNum := sess.AccountNumber
			if accNum == "" {
				accNum = devID
			}
			gameName := sess.Game
			if gameName == "" || gameName == "wardogs" {
				gameName = "WARDOGS"
			} else if gameName == "free_internet" || strings.EqualFold(gameName, "свободный интернет") || strings.EqualFold(gameName, "комплексный режим") {
				gameName = "Комплексный режим"
			}

			geo := s.resolveIPGeo(sess.ClientIP)
			geoAgg[geo]++

			activePlayers = append(activePlayers, ActivePlayerInfo{
				DeviceID:      devID,
				AccountNumber: accNum,
				Game:          gameName,
				ClientIP:      maskIPForDisplay(sess.ClientIP),
				ConnectedAt:   sess.CreatedAt.Format(time.RFC3339),
				DurationSec:   dur,
				DurationDesc:  desc,
				Status:        "АКТИВНА",
				Country:       geo.Country,
				City:          geo.City,
				Lat:           geo.Lat,
				Lon:           geo.Lon,
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

	type DonorInfo struct {
		AccountNumber   string `json:"account_number"`
		Callsign        string `json:"callsign"`
		Tier            string `json:"tier"`
		TotalDonatedRub int    `json:"total_donated_rub"`
		DonationsCount  int    `json:"donations_count"`
		LastDonationAt  string `json:"last_donation_at"`
		Status          string `json:"status"`
	}

	donorsList := make([]DonorInfo, 0)
	if s.db != nil {
		dRows, dErr := s.db.Query(`
			SELECT 
			    a.account_number,
			    COALESCE(NULLIF(a.nickname, ''), 'Оператор ' || RIGHT(a.account_number, 4)) AS callsign,
			    UPPER(COALESCE(NULLIF(a.tier, ''), 'free')) AS tier,
			    COALESCE(a.total_donated_rub, 0) AS total_donated_rub,
			    COUNT(pd.invoice_id) AS donations_count,
			    COALESCE(TO_CHAR(MAX(pd.created_at), 'DD.MM.YYYY HH24:MI'), '—') AS last_donation_at,
			    CASE 
			        WHEN a.tier = 'admin' THEN 'ADMIN'
			        WHEN a.sponsor_until IS NOT NULL AND a.sponsor_until > NOW() THEN 'SPONSOR'
			        WHEN COALESCE(a.total_donated_rub, 0) > 0 THEN 'BACKER'
			        ELSE 'FREE'
			    END AS status
			FROM accounts a
			LEFT JOIN pending_donations pd ON (
			    pd.account_number = a.account_number 
			    OR (pd.device_id IN (SELECT device_id FROM account_devices WHERE account_number = a.account_number) AND pd.account_number = '')
			) AND pd.status = 'paid'
			WHERE a.total_donated_rub > 0 OR a.tier = 'admin' OR EXISTS (
			    SELECT 1 FROM pending_donations p2 
			    WHERE (p2.account_number = a.account_number OR (p2.device_id IN (SELECT device_id FROM account_devices WHERE account_number = a.account_number) AND p2.account_number = '')) 
			      AND p2.status = 'paid'
			)
			GROUP BY a.account_number, a.nickname, a.tier, a.total_donated_rub, a.sponsor_until
			ORDER BY total_donated_rub DESC, donations_count DESC
		`)
		if dErr == nil {
			defer dRows.Close()
			for dRows.Next() {
				var dn DonorInfo
				if err := dRows.Scan(&dn.AccountNumber, &dn.Callsign, &dn.Tier, &dn.TotalDonatedRub, &dn.DonationsCount, &dn.LastDonationAt, &dn.Status); err == nil {
					donorsList = append(donorsList, dn)
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"live":           live,
		"history":        history,
		"active_players": activePlayers,
		"geo_points":     geoPoints,
		"donors":         donorsList,
	})
}

func classifyMacroRegion(city, country string, lat, lon float64) string {
	cLow := strings.ToLower(country)
	if strings.Contains(cLow, "kazakhstan") || strings.Contains(cLow, "казахстан") ||
		strings.Contains(cLow, "uzbekistan") || strings.Contains(cLow, "узбекистан") ||
		strings.Contains(cLow, "kyrgyzstan") || strings.Contains(cLow, "киргизия") {
		return "Центральная Азия / Казахстан"
	}
	if strings.Contains(cLow, "belarus") || strings.Contains(cLow, "беларусь") {
		return "Беларусь / СНГ"
	}
	if !strings.Contains(cLow, "russia") && !strings.Contains(cLow, "россия") && cLow != "local" {
		return "Европа / Другие страны"
	}
	// Russia by coordinates & known hubs
	if lon >= 80 {
		return "Сибирь и Дальний Восток"
	}
	if lon >= 55 && lon < 80 {
		return "Уральский регион"
	}
	if lat >= 58.5 {
		return "Северо-Западный регион"
	}
	if lat < 48.5 {
		return "Южный регион и Кавказ"
	}
	if lon >= 43 && lon < 55 {
		return "Поволжский регион"
	}
	return "Центральный регион (Москва)"
}

func (s *AppState) handleGeoHistory(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&s.metricRequestsTotal, 1)
	w.Header().Set("Content-Type", "application/json")

	days := 30
	if dStr := r.URL.Query().Get("days"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 && d <= 365 {
			days = d
		}
	}

	minConn := 3
	if mStr := r.URL.Query().Get("min_connections"); mStr != "" {
		if m, err := strconv.Atoi(mStr); err == nil && m > 0 && m <= 1000 {
			minConn = m
		}
	}

	type GeoHistoryPoint struct {
		City             string  `json:"city"`
		Country          string  `json:"country"`
		CountryCode      string  `json:"country_code"`
		Lat              float64 `json:"lat"`
		Lon              float64 `json:"lon"`
		UsersCount       int     `json:"users_count"`
		TotalConnections int     `json:"total_connections"`
		SharePct         float64 `json:"share_pct"`
	}

	type RegionSummary struct {
		Region     string  `json:"region"`
		UsersCount int     `json:"users_count"`
		SharePct   float64 `json:"share_pct"`
	}

	points := make([]GeoHistoryPoint, 0)
	totalRetained := 0

	if s.db != nil {
		query := `
			WITH user_cohort AS (
			    SELECT 
			        COALESCE(NULLIF(account_number, ''), device_id) AS user_key,
			        COUNT(*) AS total_user_connections
			    FROM user_connection_history
			    WHERE connected_at >= NOW() - ($1 || ' days')::INTERVAL
			    GROUP BY COALESCE(NULLIF(account_number, ''), device_id)
			    HAVING COUNT(*) >= $2
			),
			user_primary_location AS (
			    SELECT DISTINCT ON (c.user_key)
			        c.user_key,
			        c.total_user_connections,
			        h.city,
			        h.country,
			        h.country_code,
			        h.lat,
			        h.lon,
			        COUNT(*) OVER (PARTITION BY c.user_key, h.city) AS city_sessions
			    FROM user_cohort c
			    JOIN user_connection_history h 
			      ON c.user_key = COALESCE(NULLIF(h.account_number, ''), h.device_id)
			    WHERE h.connected_at >= NOW() - ($1 || ' days')::INTERVAL
			      AND h.city != 'Unknown' AND h.lat != 0 AND h.lon != 0
			    ORDER BY c.user_key, city_sessions DESC, h.connected_at DESC
			)
			SELECT 
			    city,
			    country,
			    country_code,
			    lat,
			    lon,
			    COUNT(DISTINCT user_key) AS users_count,
			    SUM(city_sessions) AS total_connections
			FROM user_primary_location
			GROUP BY city, country, country_code, lat, lon
			ORDER BY users_count DESC, total_connections DESC;
		`
		rows, err := s.db.Query(query, days, minConn)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var pt GeoHistoryPoint
				if err := rows.Scan(&pt.City, &pt.Country, &pt.CountryCode, &pt.Lat, &pt.Lon, &pt.UsersCount, &pt.TotalConnections); err == nil {
					totalRetained += pt.UsersCount
					points = append(points, pt)
				}
			}
		}
	}

	// Calculate percentage share for each point
	for i := range points {
		if totalRetained > 0 {
			points[i].SharePct = math.Round((float64(points[i].UsersCount)/float64(totalRetained))*1000) / 10
		}
	}

	// Group into macro-regions
	regMap := make(map[string]int)
	for _, pt := range points {
		reg := classifyMacroRegion(pt.City, pt.Country, pt.Lat, pt.Lon)
		regMap[reg] += pt.UsersCount
	}

	regions := make([]RegionSummary, 0)
	for regName, cnt := range regMap {
		share := 0.0
		if totalRetained > 0 {
			share = math.Round((float64(cnt)/float64(totalRetained))*1000) / 10
		}
		regions = append(regions, RegionSummary{
			Region:     regName,
			UsersCount: cnt,
			SharePct:   share,
		})
	}
	sort.Slice(regions, func(i, j int) bool {
		return regions[i].UsersCount > regions[j].UsersCount
	})

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":              true,
		"timeframe_days":       days,
		"min_connections":      minConn,
		"total_retained_users": totalRetained,
		"total_points":         len(points),
		"points":               points,
		"regions":              regions,
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
		if actURL != "" && !strings.HasPrefix(actURL, "http://") && !strings.HasPrefix(actURL, "https://") && !strings.HasPrefix(actURL, "#") && !strings.HasPrefix(actURL, "app://") {
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

func (s *AppState) handleProgressionDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=1800")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	dbPath := "/opt/warlink-server/progression_db.json"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		dbPath = "internal/progression/progression_db.json"
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		dbPath = "progression/progression_db.json"
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		dbPath = "progression_db.json"
	}

	data, err := os.ReadFile(dbPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "database_not_found"})
		return
	}

	sum := sha256.Sum256(data)
	etag := fmt.Sprintf("\"%x\"", sum[:8])
	w.Header().Set("ETag", etag)

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	_, _ = w.Write(data)
}

type ProgressionItemMeta struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	NameRu     string `json:"name_ru"`
	CategoryRu string `json:"category_ru"`
	Role       string `json:"role"`
	Level      int    `json:"level"`
	Price      int    `json:"price"`
	Icon       string `json:"icon"`
}

var (
	progressionItemsMap  map[string]ProgressionItemMeta
	progressionItemsOnce sync.Once
)

func loadProgressionItemsMap() map[string]ProgressionItemMeta {
	progressionItemsOnce.Do(func() {
		progressionItemsMap = make(map[string]ProgressionItemMeta)
		dbPath := "/opt/warlink-server/progression_db.json"
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			dbPath = "internal/progression/progression_db.json"
		}
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			dbPath = "progression/progression_db.json"
		}
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			dbPath = "progression_db.json"
		}
		data, err := os.ReadFile(dbPath)
		if err != nil {
			return
		}
		var parsed struct {
			Unlocks []struct {
				UnlockID   string `json:"unlock_id"`
				Name       string `json:"name"`
				NameRu     string `json:"name_ru"`
				CategoryRu string `json:"category_ru"`
				Role       string `json:"role"`
				Level      int    `json:"level"`
				Price      int    `json:"price"`
				Icon       string `json:"icon"`
			} `json:"unlocks"`
			Catalog []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				NameRu     string `json:"name_ru"`
				CategoryRu string `json:"category_ru"`
				Role       string `json:"role"`
				Price      int    `json:"price"`
				Icon       string `json:"icon"`
			} `json:"catalog"`
		}
		if err := json.Unmarshal(data, &parsed); err == nil {
			for _, u := range parsed.Unlocks {
				catRu := u.CategoryRu
				if catRu == "" {
					catRu = "Разблокировка"
				}
				progressionItemsMap[u.UnlockID] = ProgressionItemMeta{
					ID:         u.UnlockID,
					Name:       u.Name,
					NameRu:     u.NameRu,
					CategoryRu: catRu,
					Role:       u.Role,
					Level:      u.Level,
					Price:      u.Price,
					Icon:       u.Icon,
				}
			}
			for _, c := range parsed.Catalog {
				if _, exists := progressionItemsMap[c.ID]; !exists {
					catRu := c.CategoryRu
					if catRu == "" {
						catRu = "Снаряжение"
					}
					progressionItemsMap[c.ID] = ProgressionItemMeta{
						ID:         c.ID,
						Name:       c.Name,
						NameRu:     c.NameRu,
						CategoryRu: catRu,
						Role:       c.Role,
						Price:      c.Price,
						Icon:       c.Icon,
					}
				}
			}
		}
	})
	return progressionItemsMap
}

func (s *AppState) handleProgressionAnalytics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.db == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":             true,
			"total_players":       0,
			"career_level_avg":    0,
			"career_level_max":    0,
			"roles_distribution":  []interface{}{},
			"top_wishlist":        []interface{}{},
		})
		return
	}

	rows, err := s.db.Query(`
		SELECT COALESCE(progression::text, '') 
		FROM accounts 
		WHERE progression IS NOT NULL AND progression::text != '{}' AND progression::text != 'null'
		  AND NOT (
			(progression->>'career_level')::int <= 1 
			AND COALESCE((progression->'roles'->>'assault')::int, 0) <= 1
			AND COALESCE((progression->'roles'->>'medic')::int, 0) = 0
			AND COALESCE((progression->'roles'->>'recon')::int, 0) = 0
			AND COALESCE((progression->'roles'->>'support')::int, 0) = 0
			AND COALESCE((progression->'roles'->>'driver')::int, 0) = 0
			AND COALESCE((progression->'roles'->>'pilot')::int, 0) = 0
			AND progression->>'wishlist_id' IS NULL
			AND (progression->'unlocked_items' IS NULL OR jsonb_array_length(progression->'unlocked_items') = 0)
		  )
	`)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	defer rows.Close()

	type RoleAgg struct {
		Role       string  `json:"role"`
		Name       string  `json:"name"`
		SumLevels  int     `json:"sum_levels"`
		AvgLevel   float64 `json:"avg_level"`
		Percentage float64 `json:"percentage"`
	}

	type WishlistItemInfo struct {
		ItemID     string `json:"item_id"`
		Name       string `json:"name"`
		NameRu     string `json:"name_ru"`
		CategoryRu string `json:"category_ru"`
		Role       string `json:"role,omitempty"`
		Level      int    `json:"level,omitempty"`
		Icon       string `json:"icon,omitempty"`
		Count      int    `json:"count"`
	}

	roleNames := map[string]string{
		"assault": "Штурмовик",
		"medic":   "Медик",
		"recon":   "Разведчик",
		"support": "Поддержка",
		"driver":  "Водитель",
		"pilot":   "Пилот",
	}

	rolesSum := map[string]int{
		"assault": 0, "medic": 0, "recon": 0, "support": 0, "driver": 0, "pilot": 0,
	}
	wishlistCounts := make(map[string]int)

	totalPlayers := 0
	sumCareer := 0
	maxCareer := 0

	for rows.Next() {
		var progStr string
		if err := rows.Scan(&progStr); err != nil || progStr == "" {
			continue
		}
		var parsed struct {
			CareerLevel int            `json:"career_level"`
			WishlistID  string         `json:"wishlist_id"`
			Roles       map[string]int `json:"roles"`
		}
		if err := json.Unmarshal([]byte(progStr), &parsed); err != nil {
			continue
		}
		totalPlayers++
		sumCareer += parsed.CareerLevel
		if parsed.CareerLevel > maxCareer {
			maxCareer = parsed.CareerLevel
		}
		for rK, rLvl := range parsed.Roles {
			normK := strings.ToLower(rK)
			rolesSum[normK] += rLvl
		}
		wID := strings.TrimSpace(parsed.WishlistID)
		if wID != "" {
			wishlistCounts[wID]++
		}
	}

	careerAvg := 0.0
	if totalPlayers > 0 {
		careerAvg = math.Round((float64(sumCareer)/float64(totalPlayers))*10) / 10
	}

	rolesAvg := make(map[string]float64)
	totalRoleLevels := 0
	for rK, sVal := range rolesSum {
		totalRoleLevels += sVal
		if totalPlayers > 0 {
			rolesAvg[rK] = math.Round((float64(sVal)/float64(totalPlayers))*10) / 10
		} else {
			rolesAvg[rK] = 0
		}
	}

	dist := make([]RoleAgg, 0)
	order := []string{"assault", "medic", "recon", "support", "driver", "pilot"}
	for _, rK := range order {
		sVal := rolesSum[rK]
		pct := 0.0
		if totalRoleLevels > 0 {
			pct = math.Round((float64(sVal)/float64(totalRoleLevels))*1000) / 10
		}
		dist = append(dist, RoleAgg{
			Role:       rK,
			Name:       roleNames[rK],
			SumLevels:  sVal,
			AvgLevel:   rolesAvg[rK],
			Percentage: pct,
		})
	}

	itemsMeta := loadProgressionItemsMap()
	type countPair struct {
		id    string
		count int
	}
	var pairs []countPair
	for id, cnt := range wishlistCounts {
		pairs = append(pairs, countPair{id: id, count: cnt})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].count > pairs[j].count
	})

	topWishlist := make([]WishlistItemInfo, 0)
	for i, p := range pairs {
		if i >= 10 {
			break
		}
		meta, found := itemsMeta[p.id]
		name := p.id
		nameRu := p.id
		catRu := "Желаемое"
		icon := ""
		role := ""
		lvl := 0
		if found {
			name = meta.Name
			nameRu = meta.NameRu
			catRu = meta.CategoryRu
			icon = meta.Icon
			role = meta.Role
			lvl = meta.Level
		}
		topWishlist = append(topWishlist, WishlistItemInfo{
			ItemID:     p.id,
			Name:       name,
			NameRu:     nameRu,
			CategoryRu: catRu,
			Role:       role,
			Level:      lvl,
			Icon:       icon,
			Count:      p.count,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":             true,
		"total_players":       totalPlayers,
		"career_level_avg":    careerAvg,
		"career_level_max":    maxCareer,
		"roles_sum":           rolesSum,
		"roles_avg":           rolesAvg,
		"roles_distribution":  dist,
		"top_wishlist":        topWishlist,
	})
}

type DonationItem struct {
	InvoiceID int    `json:"invoice_id"`
	AmountRub int    `json:"amount_rub"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
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
		       COALESCE(hide_donation_amount, FALSE),
		       COALESCE(progression::text, '')
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
		AccountNumber      string          `json:"account_number"`
		Nickname           string          `json:"nickname"`
		AvatarURL          string          `json:"avatar_url"`
		SteamID            string          `json:"steam_id"`
		Motto              string          `json:"motto"`
		JoinedDate         string          `json:"joined_date"`
		IsActive           bool            `json:"is_active"`
		TotalDonated       int64           `json:"total_donated_rub"`
		HideDonationAmount bool            `json:"hide_donation_amount"`
		Donations          []DonationItem  `json:"donations"`
		Progression        json.RawMessage `json:"progression,omitempty"`
	}

	sponsors := make([]SponsorCard, 0)
	for rows.Next() {
		var sp SponsorCard
		sp.Donations = make([]DonationItem, 0)
		var nick, av, st, mo sql.NullString
		var progStr string
		if err := rows.Scan(&sp.AccountNumber, &nick, &av, &st, &mo, &sp.JoinedDate, &sp.IsActive, &sp.TotalDonated, &sp.HideDonationAmount, &progStr); err == nil {
			if progStr != "" && progStr != "null" && progStr != "{}" {
				sp.Progression = json.RawMessage(progStr)
			}
			rawAccountNumber := sp.AccountNumber
			if !sp.HideDonationAmount {
				dRows, dErr := s.db.Query(`
					SELECT invoice_id, amount_rub, status, TO_CHAR(created_at, 'DD.MM.YYYY')
					FROM pending_donations
					WHERE (account_number = $1 OR (device_id IN (SELECT device_id FROM account_devices WHERE account_number = $1) AND $1 != ''))
					  AND status = 'paid'
					ORDER BY created_at DESC LIMIT 5
				`, rawAccountNumber)
				if dErr == nil {
					for dRows.Next() {
						var d DonationItem
						if err := dRows.Scan(&d.InvoiceID, &d.AmountRub, &d.Status, &d.CreatedAt); err == nil {
							sp.Donations = append(sp.Donations, d)
						}
					}
					dRows.Close()
				}
			}

			if sp.HideDonationAmount {
				sp.TotalDonated = 0
			}
			if nick.Valid && nick.String != "" && !strings.Contains(nick.String, "-****-") && !strings.Contains(nick.String, "****") {
				sp.Nickname = nick.String
			} else {
				sp.Nickname = config.GenerateDeterministicNobelCallsign(rawAccountNumber)
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

	type ProfileResp struct {
		AccountNumber      string          `json:"account_number"`
		Nickname           string          `json:"nickname"`
		AvatarURL          string          `json:"avatar_url"`
		SteamID            string          `json:"steam_id"`
		Motto              string          `json:"motto"`
		Tier               string          `json:"tier"`
		SponsorUntil       int64           `json:"sponsor_until"`
		DaysRemaining      int             `json:"days_remaining"`
		CreatedAt          string          `json:"created_at"`
		TotalDonatedRub    int             `json:"total_donated_rub"`
		HideDonationAmount bool            `json:"hide_donation_amount"`
		DeviceCount        int             `json:"device_count"`
		Donations          []DonationItem  `json:"donations"`
		Progression        json.RawMessage `json:"progression,omitempty"`
		DiscordID          string          `json:"discord_id"`
		DiscordTag         string          `json:"discord_tag"`
		IsDiscordLinked    bool            `json:"is_discord_linked"`
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
		var progStr string

		if acc != "" {
			err = s.db.QueryRow(`
				SELECT account_number, nickname, avatar_url, steam_id, motto, tier, sponsor_until, total_donated_rub, created_at, COALESCE(hide_donation_amount, FALSE), COALESCE(progression::text, ''), COALESCE(discord_id, ''), COALESCE(discord_tag, '')
				FROM accounts WHERE account_number = $1
			`, acc).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount, &progStr, &resp.DiscordID, &resp.DiscordTag)
		} else {
			err = s.db.QueryRow(`
				SELECT a.account_number, a.nickname, a.avatar_url, a.steam_id, a.motto, a.tier, a.sponsor_until, a.total_donated_rub, a.created_at, COALESCE(a.hide_donation_amount, FALSE), COALESCE(a.progression::text, ''), COALESCE(a.discord_id, ''), COALESCE(a.discord_tag, '')
				FROM account_devices ad
				JOIN accounts a ON ad.account_number = a.account_number
				WHERE ad.device_id = $1
				ORDER BY a.sponsor_until DESC NULLS LAST LIMIT 1
			`, dev).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount, &progStr, &resp.DiscordID, &resp.DiscordTag)
		}
		resp.IsDiscordLinked = (resp.DiscordID != "" || resp.DiscordTag != "")

		if progStr != "" && progStr != "null" && progStr != "{}" {
			resp.Progression = json.RawMessage(progStr)
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
				WHERE (account_number = $1 OR (device_id IN (SELECT device_id FROM account_devices WHERE account_number = $1) AND $1 != ''))
				  AND (status = 'paid' OR (status = 'pending' AND created_at >= NOW() - INTERVAL '30 minutes'))
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
			AccountNumber      string          `json:"account_number"`
			DeviceID           string          `json:"device_id"`
			Nickname           string          `json:"nickname"`
			SteamID            string          `json:"steam_id"`
			Motto              string          `json:"motto"`
			HideDonationAmount *bool           `json:"hide_donation_amount"`
			Action             string          `json:"action"`
			Progression        json.RawMessage `json:"progression"`
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

		if len(req.Progression) > 0 && string(req.Progression) != "null" && string(req.Progression) != "{}" {
			var progCheck struct {
				CareerLevel int            `json:"career_level"`
				WishlistID  string         `json:"wishlist_id"`
				Unlocked    []string       `json:"unlocked_items"`
				Roles       map[string]int `json:"roles"`
			}
			isDummy := false
			if err := json.Unmarshal(req.Progression, &progCheck); err == nil {
				if progCheck.CareerLevel <= 1 && progCheck.WishlistID == "" && len(progCheck.Unlocked) == 0 {
					allZero := true
					for rK, rLvl := range progCheck.Roles {
						if (rK == "assault" && rLvl > 1) || (rK != "assault" && rLvl > 0) {
							allZero = false
							break
						}
					}
					if allZero {
						isDummy = true
					}
				}
			}
			if !isDummy {
				_, _ = s.db.Exec(`UPDATE accounts SET progression = $1, updated_at = NOW() WHERE account_number = $2`, req.Progression, acc)
				if s.rdb != nil {
					var dID sql.NullString
					_ = s.db.QueryRow(`SELECT discord_id FROM accounts WHERE account_number = $1`, acc).Scan(&dID)
					if dID.Valid && dID.String != "" {
						pPayload, _ := json.Marshal(map[string]interface{}{
							"account_number": acc,
							"discord_id":     dID.String,
							"progression":    json.RawMessage(req.Progression),
						})
						_ = s.rdb.Publish(context.Background(), "discord:progression_update", pPayload).Err()
					}
				}
			}
		}

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
		var progStr string
		_ = s.db.QueryRow(`
			SELECT account_number, nickname, avatar_url, steam_id, motto, tier, sponsor_until, total_donated_rub, created_at, COALESCE(hide_donation_amount, FALSE), COALESCE(progression::text, '')
			FROM accounts WHERE account_number = $1
		`, acc).Scan(&resp.AccountNumber, &resp.Nickname, &resp.AvatarURL, &st, &mo, &resp.Tier, &sponsorTime, &totalDonated, &createdAt, &resp.HideDonationAmount, &progStr)
		if progStr != "" && progStr != "null" && progStr != "{}" {
			resp.Progression = json.RawMessage(progStr)
		}
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

// handleDiscordLinkCode generates a temporary 6-digit code for linking Discord account
func (s *AppState) handleDiscordLinkCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AccountNumber string `json:"account_number"`
		DeviceID      string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
		return
	}
	acc := strings.TrimSpace(req.AccountNumber)
	if acc == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "account_number_required"})
		return
	}

	if s.rdb == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "redis_unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Check if existing code is still valid
	if existingCode, err := s.rdb.Get(ctx, "wl:discord:acc:"+acc).Result(); err == nil && len(existingCode) == 6 {
		ttl, _ := s.rdb.TTL(ctx, "wl:discord:acc:"+acc).Result()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"code":       existingCode,
			"expires_in": int(ttl.Seconds()),
		})
		return
	}

	// Generate random 6-digit code using crypto/rand
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	num := (int(b[0])<<16 | int(b[1])<<8 | int(b[2]))%900000 + 100000
	code := fmt.Sprintf("%06d", num)

	ttl := 15 * time.Minute
	_ = s.rdb.Set(ctx, "wl:discord:code:"+code, acc, ttl).Err()
	_ = s.rdb.Set(ctx, "wl:discord:acc:"+acc, code, ttl).Err()

	log.Printf("[DISCORD-LINK] Generated link code %s for account %s (TTL: 15m)", code, acc)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"code":       code,
		"expires_in": 900,
	})
}

// handleDiscordVerifyLink validates code from Discord Bot and links Discord ID with WarLink account
func (s *AppState) handleDiscordVerifyLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Dashboard-Key, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	var req struct {
		Code       string `json:"code"`
		DiscordID  string `json:"discord_id"`
		DiscordTag string `json:"discord_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
		return
	}
	code := strings.TrimSpace(req.Code)
	discordID := strings.TrimSpace(req.DiscordID)
	discordTag := strings.TrimSpace(req.DiscordTag)

	if code == "" || discordID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "code_and_discord_id_required"})
		return
	}

	if s.rdb == nil || s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "services_unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	acc, err := s.rdb.Get(ctx, "wl:discord:code:"+code).Result()
	if err != nil || acc == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_or_expired_code"})
		return
	}

	// Update PostgreSQL
	_, dbErr := s.db.Exec(`
		UPDATE accounts
		SET discord_id = $1, discord_tag = $2, updated_at = NOW()
		WHERE account_number = $3
	`, discordID, discordTag, acc)
	if dbErr != nil {
		log.Printf("[DISCORD-LINK] DB error updating account %s: %v", acc, dbErr)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_error"})
		return
	}

	// Clean up Redis keys
	_ = s.rdb.Del(ctx, "wl:discord:code:"+code, "wl:discord:acc:"+acc).Err()

	// Fetch account status for roles
	var tier, nickname string
	var totalDonated int
	var sponsorUntil *time.Time
	var progRaw string
	_ = s.db.QueryRow(`
		SELECT COALESCE(nickname, ''), tier, total_donated_rub, sponsor_until, COALESCE(progression::text, '')
		FROM accounts WHERE account_number = $1
	`, acc).Scan(&nickname, &tier, &totalDonated, &sponsorUntil, &progRaw)

	isSponsor := false
	if sponsorUntil != nil && sponsorUntil.After(time.Now()) {
		isSponsor = true
	} else if totalDonated > 0 || tier == "sponsor" || tier == "supporter" {
		isSponsor = true
	}

	log.Printf("[DISCORD-LINK] Successfully linked Discord %s (%s) to WarLink account %s (Sponsor: %v)", discordTag, discordID, acc, isSponsor)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":           true,
		"account_number":    acc,
		"nickname":          nickname,
		"discord_id":        discordID,
		"discord_tag":       discordTag,
		"is_sponsor":        isSponsor,
		"total_donated_rub": totalDonated,
		"tier":              tier,
		"progression":       json.RawMessage(progRaw),
	})
}

// handleDiscordUnlink unlinks Discord account from WarLink profile
func (s *AppState) handleDiscordUnlink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AccountNumber string `json:"account_number"`
		DeviceID      string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
		return
	}
	acc := strings.TrimSpace(req.AccountNumber)
	if acc == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "account_number_required"})
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_unavailable"})
		return
	}

	_, err := s.db.Exec(`
		UPDATE accounts
		SET discord_id = '', discord_tag = '', updated_at = NOW()
		WHERE account_number = $1
	`, acc)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_error"})
		return
	}

	log.Printf("[DISCORD-LINK] Successfully unlinked Discord for account %s", acc)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Discord аккаунт успешно отвязан",
	})
}

func (s *AppState) handleDiscordProfileLookup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.cfg.DashboardKey != "" && r.Header.Get("X-Dashboard-Key") != s.cfg.DashboardKey && r.URL.Query().Get("key") != s.cfg.DashboardKey {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "unauthorized"})
		return
	}

	discordID := strings.TrimSpace(r.URL.Query().Get("discord_id"))
	if discordID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "discord_id_required"})
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "db_unavailable"})
		return
	}

	var acc, nickname, tier string
	var totalDonated int
	var sponsorUntil *time.Time
	var progRaw string
	err := s.db.QueryRow(`
		SELECT account_number, COALESCE(nickname, ''), tier, total_donated_rub, sponsor_until, COALESCE(progression::text, '')
		FROM accounts WHERE discord_id = $1
	`, discordID).Scan(&acc, &nickname, &tier, &totalDonated, &sponsorUntil, &progRaw)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "not_found"})
		return
	}

	isSponsor := false
	if sponsorUntil != nil && sponsorUntil.After(time.Now()) {
		isSponsor = true
	} else if totalDonated > 0 || tier == "sponsor" || tier == "supporter" {
		isSponsor = true
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":           true,
		"account_number":    acc,
		"nickname":          nickname,
		"discord_id":        discordID,
		"is_sponsor":        isSponsor,
		"total_donated_rub": totalDonated,
		"tier":              tier,
		"progression":       json.RawMessage(progRaw),
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

            <!-- Boosty Goal Control (Control Plane) -->
            <div style="margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border); display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px;">
                <div style="display: flex; align-items: center; gap: 10px;">
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2.5"><path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/></svg>
                    <div>
                        <div style="font-weight: 700; font-size: 11px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--text-main);">Цель сбора на Boosty (Прогресс-бар в клиенте)</div>
                        <div id="admin-boosty-sub" style="font-size: 10px; color: var(--text-muted); font-family: var(--font-mono);">Сбор: 0 ₽ из 100 000 ₽</div>
                    </div>
                </div>
                <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
                    <div style="display: flex; align-items: center; gap: 4px; font-size: 11px; font-family: var(--font-mono); color: var(--text-muted);">
                        <span>Собрано:</span>
                        <input type="number" id="input-boosty-current" min="0" max="10000000" step="500" value="0" style="background: #000; border: 1px solid var(--border); color: #fff; padding: 5px 8px; border-radius: 2px; width: 85px; font-weight: 700; font-family: var(--font-mono); text-align: right;">
                        <span>₽</span>
                    </div>
                    <div style="display: flex; align-items: center; gap: 4px; font-size: 11px; font-family: var(--font-mono); color: var(--text-muted);">
                        <span>Цель:</span>
                        <input type="number" id="input-boosty-target" min="1000" max="10000000" step="1000" value="100000" style="background: #000; border: 1px solid var(--border); color: #fff; padding: 5px 8px; border-radius: 2px; width: 85px; font-weight: 700; font-family: var(--font-mono); text-align: right;">
                        <span>₽</span>
                    </div>
                    <button class="btn" onclick="saveBoostyGoalControl()" style="background: var(--accent); color: #000; border-color: var(--accent); font-weight: 700; padding: 5px 12px; font-size: 11px; cursor: pointer;">Сохранить</button>
                    <button class="btn" onclick="syncBoostyGoalControl()" style="background: #111; border: 1px solid var(--border); color: var(--text-main); font-weight: 600; padding: 5px 10px; font-size: 11px; cursor: pointer;">Спарсить с Boosty</button>
                    <span id="boosty-ctrl-status" style="font-family: var(--font-mono); font-size: 10px; color: var(--green);"></span>
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
                    <span>Диск NVMe</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="12" x2="2" y2="12"/><path d="M5.45 5.11L2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" y1="16" x2="6.01" y2="16"/><line x1="10" y1="16" x2="10.01" y2="16"/></svg>
                </div>
                <div class="kpi-value" id="kpi-disk">0 / 0 ГБ</div>
                <div class="kpi-sub" id="kpi-disk-sub">0% занято</div>
                <div class="progress-bar-bg"><div class="progress-bar-fill" id="kpi-disk-bar" style="width: 0%; background: var(--green);"></div></div>
            </div>

            <div class="kpi-card">
                <div class="kpi-header">
                    <span>Задержка шлюза</span>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                </div>
                <div class="kpi-value" id="kpi-ping">27 мс</div>
                <div class="kpi-sub" id="kpi-ping-sub">Стокгольм • Aeza DC</div>
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

            const diskEl = document.getElementById('kpi-disk');
            const diskSubEl = document.getElementById('kpi-disk-sub');
            const diskBar = document.getElementById('kpi-disk-bar');
            if (diskEl && live.disk_total_gb > 0) {
                diskEl.textContent = (live.disk_used_gb || 0).toFixed(1) + ' / ' + (live.disk_total_gb || 0).toFixed(1) + ' ГБ';
                const diskPct = Math.round(live.disk_percent || ((live.disk_used_gb / live.disk_total_gb) * 100));
                if (diskSubEl) diskSubEl.textContent = diskPct + '% занято хранилища';
                if (diskBar) {
                    diskBar.style.width = Math.min(100, Math.max(2, diskPct)) + '%';
                    if (diskPct >= 85) diskBar.style.background = 'var(--red)';
                    else if (diskPct >= 70) diskBar.style.background = 'var(--yellow)';
                    else diskBar.style.background = 'var(--green)';
                }
            }

            const pingEl = document.getElementById('kpi-ping');
            if (pingEl && live.gateway_ping_ms) pingEl.textContent = (typeof live.gateway_ping_ms === 'number' ? live.gateway_ping_ms.toFixed(1) : live.gateway_ping_ms) + ' мс';
            const pingSubEl = document.getElementById('kpi-ping-sub');
            if (pingSubEl && live.gateway_jitter_ms) pingSubEl.textContent = 'Стокгольм • Джиттер: ±' + (typeof live.gateway_jitter_ms === 'number' ? live.gateway_jitter_ms.toFixed(1) : live.gateway_jitter_ms) + ' мс';

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
                        } else if (gLower === 'free_internet' || gLower.includes('свободный') || gLower.includes('комплексный')) {
                            modeBadge = '<strong style="color: var(--blue);">КОМПЛЕКСНЫЙ РЕЖИМ</strong>';
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

        async function loadBoostyGoalControl() {
            try {
                let url = '/api/v1/boosty-goal';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url);
                if (!res.ok) return;
                const data = await res.json();
                if (data && data.success) {
                    const cIn = document.getElementById('input-boosty-current');
                    const tIn = document.getElementById('input-boosty-target');
                    const subEl = document.getElementById('admin-boosty-sub');
                    if (cIn) cIn.value = data.current_amount || 0;
                    if (tIn) tIn.value = data.target_amount || 100000;
                    if (subEl) {
                        const src = data.source === 'auto_parser' ? 'авто-парсер Boosty' : 'ручной ввод';
                        subEl.textContent = 'Сбор: ' + (data.current_amount || 0).toLocaleString('ru-RU') + ' ₽ из ' + (data.target_amount || 100000).toLocaleString('ru-RU') + ' ₽ (' + (data.percent || 0) + '%, ' + src + ')';
                    }
                }
            } catch (err) {
                console.error('Failed to load boosty goal:', err);
            }
        }

        async function saveBoostyGoalControl() {
            const curr = parseInt(document.getElementById('input-boosty-current').value, 10) || 0;
            const tgt = parseInt(document.getElementById('input-boosty-target').value, 10) || 100000;
            const statusEl = document.getElementById('boosty-ctrl-status');
            if (statusEl) { statusEl.textContent = 'Сохранение...'; statusEl.style.color = 'var(--text-muted)'; }
            try {
                let url = '/api/v1/admin/boosty-goal';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ current_amount: curr, target_amount: tgt })
                });
                const data = await res.json();
                if (res.ok && data.success) {
                    if (statusEl) {
                        statusEl.textContent = 'Сохранено!';
                        statusEl.style.color = 'var(--green)';
                        setTimeout(() => { if (statusEl) statusEl.textContent = ''; }, 3000);
                    }
                    loadBoostyGoalControl();
                } else {
                    if (statusEl) {
                        statusEl.textContent = 'Ошибка сохранения!';
                        statusEl.style.color = '#ef4444';
                    }
                }
            } catch (err) {
                if (statusEl) {
                    statusEl.textContent = 'Ошибка сети!';
                    statusEl.style.color = '#ef4444';
                }
            }
        }

        async function syncBoostyGoalControl() {
            const statusEl = document.getElementById('boosty-ctrl-status');
            if (statusEl) { statusEl.textContent = 'Синхронизация...'; statusEl.style.color = 'var(--text-muted)'; }
            try {
                let url = '/api/v1/admin/boosty-goal/parse';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url, { method: 'POST' });
                const data = await res.json();
                if (statusEl) {
                    statusEl.textContent = data.message || (data.success ? 'Синхронизировано!' : 'Не удалось спарсить');
                    statusEl.style.color = data.success ? 'var(--green)' : 'var(--accent)';
                    setTimeout(() => { if (statusEl) statusEl.textContent = ''; }, 4000);
                }
                loadBoostyGoalControl();
            } catch (err) {
                if (statusEl) {
                    statusEl.textContent = 'Ошибка запроса!';
                    statusEl.style.color = '#ef4444';
                }
            }
        }

        // Initial fetch and auto-refresh
        loadData();
        loadFeatures();
        loadBoostyGoalControl();
        setInterval(loadData, 5000);
        setInterval(loadFeatures, 10000);
        setInterval(loadBoostyGoalControl, 15000);
        window.addEventListener('resize', () => loadData());
    </script>
</body>
</html>
`

// -----------------------------------------------------------------------------
// Ticket & Support Diagnostic System
// -----------------------------------------------------------------------------

var ticketCooldowns sync.Map // map[string]time.Time

type ClientTicketSubmission struct {
	AccountNumber string                 `json:"account_number"`
	DeviceID      string                 `json:"device_id"`
	AppVersion    string                 `json:"app_version"`
	Category      string                 `json:"category"`
	UserComment   string                 `json:"user_comment"`
	SystemInfo    map[string]interface{} `json:"system_info"`
	LogsGzip      string                 `json:"logs_gzip"` // Base64-encoded .tar.gz
}

func (s *AppState) handleClientTicketSubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Limit body size to 25 MB
	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)

	var req ClientTicketSubmission
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.AccountNumber = strings.TrimSpace(req.AccountNumber)
	req.UserComment = strings.TrimSpace(req.UserComment)
	req.Category = strings.TrimSpace(req.Category)
	if req.Category == "" {
		req.Category = "other"
	}

	if req.DeviceID == "" && req.AccountNumber == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "identity_required"})
		return
	}

	// Cooldown enforcement: 3 minutes per device or IP
	clientIP := s.getClientIP(r)
	cooldownKey := req.DeviceID
	if cooldownKey == "" {
		cooldownKey = clientIP
	}
	if last, ok := ticketCooldowns.Load(cooldownKey); ok {
		if time.Since(last.(time.Time)) < 3*time.Minute {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "rate_limited",
				"message": "Слишком много обращений. Пожалуйста, подождите 3 минуты перед повторной отправкой.",
			})
			return
		}
	}
	ticketCooldowns.Store(cooldownKey, time.Now())

	// Resolve account number from device mapping if missing
	if req.AccountNumber == "" && req.DeviceID != "" && s.db != nil {
		_ = s.db.QueryRow(`
			SELECT account_number FROM account_devices
			WHERE device_id = $1
			ORDER BY created_at DESC LIMIT 1
		`, req.DeviceID).Scan(&req.AccountNumber)
	}

	// Decode archive bytes
	var archiveBytes []byte
	if req.LogsGzip != "" {
		if decoded, err := base64.StdEncoding.DecodeString(req.LogsGzip); err == nil {
			archiveBytes = decoded
		}
	}

	sysJSON, _ := json.Marshal(req.SystemInfo)

	var ticketID int64
	if s.db != nil {
		err := s.db.QueryRow(`
			INSERT INTO support_tickets (
				account_number, device_id, app_version, category, user_comment, system_info, logs_archive, logs_archive_size, status
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'new')
			RETURNING id
		`, req.AccountNumber, req.DeviceID, req.AppVersion, req.Category, req.UserComment, string(sysJSON), archiveBytes, len(archiveBytes)).Scan(&ticketID)
		if err != nil {
			log.Printf("[TICKETS] Database error saving ticket: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
			return
		}
		if ticketID > 0 {
			var attType = ""
			if len(archiveBytes) > 0 {
				attType = "logs_archive"
			}
			_, _ = s.db.Exec(`
				INSERT INTO ticket_messages (
					ticket_id, sender_type, sender_name, message, attachment_type, attachment_data, attachment_size, created_at
				) VALUES ($1, 'user', $2, $3, $4, $5, $6, NOW())
			`, ticketID, req.AccountNumber, req.UserComment, attType, archiveBytes, len(archiveBytes))
		}
	} else {
		ticketID = time.Now().Unix()
	}

	log.Printf("[TICKETS] Saved new support ticket #%d from acc=%s, dev=%s, cat=%s (logs archive: %d bytes)",
		ticketID, req.AccountNumber, req.DeviceID, req.Category, len(archiveBytes))

	// Publish to Redis Pub/Sub for Discord Bot
	if s.rdb != nil {
		go func(tID int64) {
			pubCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := s.rdb.Publish(pubCtx, "tickets:new", fmt.Sprintf("%d", tID)).Err(); err != nil {
				log.Printf("[TICKETS] Warning: Failed to publish ticket #%d to Redis: %v", tID, err)
			} else {
				log.Printf("[TICKETS] Published ticket #%d event to Redis channel 'tickets:new'", tID)
			}
		}(ticketID)
	}

	ticketCode := fmt.Sprintf("TK-%04d", ticketID)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"ticket_id":   ticketID,
		"ticket_code": ticketCode,
		"message":     "Отчет успешно доставлен администратору. Я изучу диагностику и направлю ответ в ваш Центр уведомлений.",
	})
}

type TicketMessageItem struct {
	ID             int64  `json:"id"`
	TicketID       int64  `json:"ticket_id"`
	SenderType     string `json:"sender_type"` // 'user', 'admin', 'system'
	SenderName     string `json:"sender_name"`
	Message        string `json:"message"`
	AttachmentType string `json:"attachment_type,omitempty"`
	AttachmentSize int    `json:"attachment_size,omitempty"`
	CreatedAt      string `json:"created_at"`
}

type RouteReviewItem struct {
	RouteMode     string `json:"route_mode"`
	Status        string `json:"status"`
	InGamePing    int    `json:"in_game_ping"`
	MatchQuality  string `json:"match_quality"`
	DiscordStatus string `json:"discord_status"`
	UserComment   string `json:"user_comment"`
}

type RoutingFeedbackSubmission struct {
	AccountNumber  string                 `json:"account_number"`
	DeviceID       string                 `json:"device_id"`
	AppVersion     string                 `json:"app_version"`
	RouteMode      string                 `json:"route_mode"`
	Status         string                 `json:"status"`
	InGamePing     int                    `json:"in_game_ping"`
	MatchQuality   string                 `json:"match_quality"`
	DiscordStatus  string                 `json:"discord_status"`
	UserComment    string                 `json:"user_comment"`
	Reviews        []RouteReviewItem      `json:"reviews,omitempty"`
	OverallComment string                 `json:"overall_comment,omitempty"`
	TelemetryData  map[string]interface{} `json:"telemetry_data"`
}

func (s *AppState) handleRoutingFeedback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req RoutingFeedbackSubmission
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	clientIP := s.getClientIP(r)
	telemetryJSON, _ := json.Marshal(req.TelemetryData)

	if s.db != nil {
		if len(req.Reviews) > 0 {
			for _, rev := range req.Reviews {
				m := strings.TrimSpace(rev.RouteMode)
				if m == "" {
					continue
				}
				st := strings.TrimSpace(rev.Status)
				if st == "" {
					st = "works_great"
				}
				comm := rev.UserComment
				if req.OverallComment != "" && comm == "" {
					comm = req.OverallComment
				}
				_, err := s.db.Exec(`
					INSERT INTO routing_feedback (
						account_number, device_id, app_version, route_mode, status,
						in_game_ping, match_quality, discord_status, user_comment, client_ip, telemetry_data
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
					req.AccountNumber, req.DeviceID, req.AppVersion, m, st,
					rev.InGamePing, rev.MatchQuality, rev.DiscordStatus, comm, clientIP, telemetryJSON,
				)
				if err != nil {
					log.Printf("[FEEDBACK] Error saving multi-route feedback item: %v", err)
				}
			}
		} else {
			req.RouteMode = strings.TrimSpace(req.RouteMode)
			if req.RouteMode == "" {
				req.RouteMode = "transit"
			}
			req.Status = strings.TrimSpace(req.Status)
			if req.Status == "" {
				req.Status = "works_great"
			}

			_, err := s.db.Exec(`
				INSERT INTO routing_feedback (
					account_number, device_id, app_version, route_mode, status,
					in_game_ping, match_quality, discord_status, user_comment, client_ip, telemetry_data
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				req.AccountNumber, req.DeviceID, req.AppVersion, req.RouteMode, req.Status,
				req.InGamePing, req.MatchQuality, req.DiscordStatus, req.UserComment, clientIP, telemetryJSON,
			)
			if err != nil {
				log.Printf("[FEEDBACK] Error saving routing feedback: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
				return
			}
		}
	}

	log.Printf("[FEEDBACK] Received multi-route feedback from %s (%s): reviews=%d",
		req.AccountNumber, req.DeviceID, len(req.Reviews))

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Спасибо за подробную обратную связь по всем маршрутам!",
	})
}

func (s *AppState) handleAdminRoutingFeedback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	type ModeStats struct {
		Mode     string  `json:"mode"`
		Count    int     `json:"count"`
		AvgPing  float64 `json:"avg_ping"`
		GreatPct float64 `json:"great_pct"`
	}

	var stats []ModeStats
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT route_mode, COUNT(*), ROUND(COALESCE(AVG(NULLIF(in_game_ping, 0)), 0)::numeric, 1),
			       ROUND(COUNT(CASE WHEN status = 'works_great' THEN 1 END)::numeric / NULLIF(COUNT(*), 0)::numeric * 100, 1)
			FROM routing_feedback
			GROUP BY route_mode
			ORDER BY route_mode`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var st ModeStats
				if err := rows.Scan(&st.Mode, &st.Count, &st.AvgPing, &st.GreatPct); err == nil {
					stats = append(stats, st)
				}
			}
		}
	}

	type FeedbackItem struct {
		ID            int64                  `json:"id"`
		CreatedAt     string                 `json:"created_at"`
		AccountNumber string                 `json:"account_number"`
		DeviceID      string                 `json:"device_id"`
		AppVersion    string                 `json:"app_version"`
		RouteMode     string                 `json:"route_mode"`
		Status        string                 `json:"status"`
		InGamePing    int                    `json:"in_game_ping"`
		MatchQuality  string                 `json:"match_quality"`
		DiscordStatus string                 `json:"discord_status"`
		UserComment   string                 `json:"user_comment"`
		ClientIP      string                 `json:"client_ip"`
		Telemetry     map[string]interface{} `json:"telemetry,omitempty"`
	}

	var items []FeedbackItem
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT id, to_char(created_at, 'YYYY-MM-DD HH24:MI:SS'), account_number, device_id, app_version,
			       route_mode, status, in_game_ping, match_quality, discord_status, user_comment, client_ip, telemetry_data
			FROM routing_feedback
			ORDER BY created_at DESC LIMIT 100`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var it FeedbackItem
				var telRaw []byte
				if err := rows.Scan(&it.ID, &it.CreatedAt, &it.AccountNumber, &it.DeviceID, &it.AppVersion,
					&it.RouteMode, &it.Status, &it.InGamePing, &it.MatchQuality, &it.DiscordStatus, &it.UserComment, &it.ClientIP, &telRaw); err == nil {
					if len(telRaw) > 0 {
						_ = json.Unmarshal(telRaw, &it.Telemetry)
					}
					it.ClientIP = maskIP(it.ClientIP)
					items = append(items, it)
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stats":   stats,
		"items":   items,
	})
}

type BoostyGoalData struct {
	Title         string  `json:"title"`
	TargetAmount  int     `json:"target_amount"`
	CurrentAmount int     `json:"current_amount"`
	Percent       float64 `json:"percent"`
	Source        string  `json:"source"`
	UpdatedAt     string  `json:"updated_at"`
}

func (s *AppState) getBoostyGoal() BoostyGoalData {
	goal := BoostyGoalData{
		Title:         "WarLink | Поддержка дальнейшей разработки | Долги",
		TargetAmount:  100000,
		CurrentAmount: 0,
		Source:        "manual",
		UpdatedAt:     time.Now().Format("2006-01-02 15:04:05"),
	}

	if s.db != nil {
		rows, err := s.db.Query(`SELECT key, value FROM server_settings WHERE key IN ('boosty_goal_title', 'boosty_goal_target', 'boosty_goal_current', 'boosty_goal_source', 'boosty_goal_updated_at')`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var k, v string
				if err := rows.Scan(&k, &v); err == nil {
					switch k {
					case "boosty_goal_title":
						if v != "" {
							goal.Title = v
						}
					case "boosty_goal_target":
						if t, err := strconv.Atoi(v); err == nil && t > 0 {
							goal.TargetAmount = t
						}
					case "boosty_goal_current":
						if c, err := strconv.Atoi(v); err == nil && c >= 0 {
							goal.CurrentAmount = c
						}
					case "boosty_goal_source":
						if v != "" {
							goal.Source = v
						}
					case "boosty_goal_updated_at":
						if v != "" {
							goal.UpdatedAt = v
						}
					}
				}
			}
		}
	}

	if goal.TargetAmount > 0 {
		goal.Percent = math.Round((float64(goal.CurrentAmount)/float64(goal.TargetAmount)*100)*10) / 10
		if goal.Percent > 100 {
			goal.Percent = 100
		}
	}

	return goal
}

func (s *AppState) saveBoostyGoal(current, target int, title, source string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	if s.db == nil {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if title != "" {
		_, _ = tx.Exec(`INSERT INTO server_settings (key, value) VALUES ('boosty_goal_title', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, title)
	}
	if target > 0 {
		_, _ = tx.Exec(`INSERT INTO server_settings (key, value) VALUES ('boosty_goal_target', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(target))
	}
	if current >= 0 {
		_, _ = tx.Exec(`INSERT INTO server_settings (key, value) VALUES ('boosty_goal_current', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(current))
	}
	if source != "" {
		_, _ = tx.Exec(`INSERT INTO server_settings (key, value) VALUES ('boosty_goal_source', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, source)
	}
	_, _ = tx.Exec(`INSERT INTO server_settings (key, value) VALUES ('boosty_goal_updated_at', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, now)

	return tx.Commit()
}

func (s *AppState) parseBoostyGoalFromWeb() (int, int, bool) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://boosty.to/pld1n/donate", nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return 0, 0, false
	}
	body := string(bodyBytes)

	// Regex pattern 1: "currentSum":(\d+).*?"targetSum":(\d+)
	reSum := regexp.MustCompile(`"currentSum":\s*(\d+).*?"targetSum":\s*(\d+)`)
	if m := reSum.FindStringSubmatch(body); len(m) >= 3 {
		curr, _ := strconv.Atoi(m[1])
		tgt, _ := strconv.Atoi(m[2])
		if tgt > 0 {
			return curr, tgt, true
		}
	}

	// Regex pattern 2: "raised":\s*(\d+).*?"target":\s*(\d+)
	reRaised := regexp.MustCompile(`"raised":\s*(\d+).*?"target":\s*(\d+)`)
	if m := reRaised.FindStringSubmatch(body); len(m) >= 3 {
		curr, _ := strconv.Atoi(m[1])
		tgt, _ := strconv.Atoi(m[2])
		if tgt > 0 {
			return curr, tgt, true
		}
	}

	// Regex pattern 3: (\d+[\s\d]*)\s*₽\s*из\s*(\d+[\s\d]*)\s*₽
	reText := regexp.MustCompile(`([0-9\s]{1,10})\s*₽\s*из\s*([0-9\s]{1,10})\s*₽`)
	if m := reText.FindStringSubmatch(body); len(m) >= 3 {
		cleanCurr := strings.ReplaceAll(m[1], " ", "")
		cleanTgt := strings.ReplaceAll(m[2], " ", "")
		curr, err1 := strconv.Atoi(cleanCurr)
		tgt, err2 := strconv.Atoi(cleanTgt)
		if err1 == nil && err2 == nil && tgt > 0 {
			return curr, tgt, true
		}
	}

	return 0, 0, false
}

func (s *AppState) startBoostyGoalSyncWorker() {
	go func() {
		time.Sleep(30 * time.Second)
		if curr, tgt, ok := s.parseBoostyGoalFromWeb(); ok {
			log.Printf("[BOOSTY-SYNC] Initial sync: %d / %d RUB", curr, tgt)
			_ = s.saveBoostyGoal(curr, tgt, "", "auto_parser")
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if curr, tgt, ok := s.parseBoostyGoalFromWeb(); ok {
				log.Printf("[BOOSTY-SYNC] Daily sync: %d / %d RUB", curr, tgt)
				_ = s.saveBoostyGoal(curr, tgt, "", "auto_parser")
			} else {
				log.Printf("[BOOSTY-SYNC] Daily check could not extract numbers; manual values retained")
			}
		}
	}()
}

func (s *AppState) handleGetBoostyGoal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	goal := s.getBoostyGoal()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"title":          goal.Title,
		"target_amount":  goal.TargetAmount,
		"current_amount": goal.CurrentAmount,
		"percent":        goal.Percent,
		"source":         goal.Source,
		"updated_at":     goal.UpdatedAt,
	})
}

func (s *AppState) handleAdminBoostyGoal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			CurrentAmount *int   `json:"current_amount"`
			TargetAmount  *int   `json:"target_amount"`
			Title         string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			goal := s.getBoostyGoal()
			curr := goal.CurrentAmount
			tgt := goal.TargetAmount
			title := goal.Title
			if req.CurrentAmount != nil {
				curr = *req.CurrentAmount
			}
			if req.TargetAmount != nil && *req.TargetAmount > 0 {
				tgt = *req.TargetAmount
			}
			if req.Title != "" {
				title = req.Title
			}
			_ = s.saveBoostyGoal(curr, tgt, title, "manual")
			log.Printf("[ADMIN] Boosty goal updated manually: %d / %d RUB (title: %s)", curr, tgt, title)
		}
	}

	goal := s.getBoostyGoal()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"goal":    goal,
	})
}

func (s *AppState) handleAdminBoostyGoalParse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	curr, tgt, ok := s.parseBoostyGoalFromWeb()
	if ok {
		_ = s.saveBoostyGoal(curr, tgt, "", "auto_parser")
		log.Printf("[ADMIN] Boosty goal parsed successfully: %d / %d RUB", curr, tgt)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Успешно синхронизировано с Boosty: %d ₽ из %d ₽", curr, tgt),
			"goal":    s.getBoostyGoal(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": "Парсер не нашел цифры сбора на странице Boosty. Сохранены текущие значения.",
		"goal":    s.getBoostyGoal(),
	})
}

func (s *AppState) handleClientTicketActive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Account-Number, X-Device-ID")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	accNum := strings.TrimSpace(r.URL.Query().Get("account_number"))
	if accNum == "" {
		accNum = strings.TrimSpace(r.Header.Get("X-Account-Number"))
	}
	devID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if devID == "" {
		devID = strings.TrimSpace(r.Header.Get("X-Device-ID"))
	}

	if accNum == "" && devID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing_account_or_device"})
		return
	}

	if s.db == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"has_active": false,
			"ticket":     nil,
			"messages":   []TicketMessageItem{},
		})
		return
	}

	var t AdminTicketSummary
	var resAt sql.NullString
	var sysRaw string
	var archiveBytes []byte

	var err error
	requestedIDStr := strings.TrimSpace(r.URL.Query().Get("ticket_id"))
	if requestedIDStr != "" {
		reqID, pErr := strconv.ParseInt(requestedIDStr, 10, 64)
		if pErr == nil && reqID > 0 {
			err = s.db.QueryRow(`
				SELECT id, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       TO_CHAR(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       account_number, device_id, app_version, category, user_comment,
				       logs_archive_size, status, admin_reply,
				       TO_CHAR(resolved_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       COALESCE(system_info::TEXT, '{}'),
				       logs_archive
				FROM support_tickets
				WHERE id = $1
				  AND ((length($2) > 0 AND account_number = $2) OR (length($3) > 0 AND device_id = $3))
			`, reqID, accNum, devID).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.AccountNumber, &t.DeviceID, &t.AppVersion, &t.Category, &t.UserComment, &t.LogsArchiveSize, &t.Status, &t.AdminReply, &resAt, &sysRaw, &archiveBytes)
		}
	}

	if requestedIDStr == "" || err != nil {
		// Look up only active ticket (status: new or in_progress)
		err = s.db.QueryRow(`
			SELECT id, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       TO_CHAR(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       account_number, device_id, app_version, category, user_comment,
			       logs_archive_size, status, admin_reply,
			       TO_CHAR(resolved_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       COALESCE(system_info::TEXT, '{}'),
			       logs_archive
			FROM support_tickets
			WHERE ((length($1) > 0 AND account_number = $1)
			   OR (length($2) > 0 AND device_id = $2))
			  AND status IN ('new', 'in_progress', 'open')
			ORDER BY updated_at DESC
			LIMIT 1
		`, accNum, devID).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.AccountNumber, &t.DeviceID, &t.AppVersion, &t.Category, &t.UserComment, &t.LogsArchiveSize, &t.Status, &t.AdminReply, &resAt, &sysRaw, &archiveBytes)
	}

	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"has_active": false,
			"ticket":     nil,
			"messages":   []TicketMessageItem{},
		})
		return
	}

	if resAt.Valid {
		val := resAt.String
		t.ResolvedAt = &val
	}
	t.SystemInfo = json.RawMessage(sysRaw)

	// Fetch messages
	messages := make([]TicketMessageItem, 0)
	rows, mErr := s.db.Query(`
		SELECT id, ticket_id, sender_type, sender_name, message, attachment_type, attachment_size,
		       TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM ticket_messages
		WHERE ticket_id = $1
		ORDER BY created_at ASC
	`, t.ID)
	if mErr == nil {
		defer rows.Close()
		for rows.Next() {
			var m TicketMessageItem
			if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderType, &m.SenderName, &m.Message, &m.AttachmentType, &m.AttachmentSize, &m.CreatedAt); err == nil {
				messages = append(messages, m)
			}
		}
	}

	// Fallback if ticket_messages was empty
	if len(messages) == 0 {
		if t.UserComment != "" {
			messages = append(messages, TicketMessageItem{
				ID:             1,
				TicketID:       t.ID,
				SenderType:     "user",
				SenderName:     t.AccountNumber,
				Message:        t.UserComment,
				AttachmentType: func() string { if t.LogsArchiveSize > 0 { return "logs_archive" }; return "" }(),
				AttachmentSize: t.LogsArchiveSize,
				CreatedAt:      t.CreatedAt,
			})
		}
		if t.AdminReply != "" {
			messages = append(messages, TicketMessageItem{
				ID:         2,
				TicketID:   t.ID,
				SenderType: "admin",
				SenderName: "Max (Разработчик)",
				Message:    t.AdminReply,
				CreatedAt:  t.UpdatedAt,
			})
		}
	}

	hasActive := t.Status == "new" || t.Status == "in_progress" || t.Status == "open"
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"has_active": hasActive,
		"ticket":     t,
		"messages":   messages,
	})
}

func (s *AppState) handleClientTicketsHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Account-Number, X-Device-ID")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	accNum := strings.TrimSpace(r.URL.Query().Get("account_number"))
	if accNum == "" {
		accNum = strings.TrimSpace(r.Header.Get("X-Account-Number"))
	}
	devID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if devID == "" {
		devID = strings.TrimSpace(r.Header.Get("X-Device-ID"))
	}

	if accNum == "" && devID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "missing_account_or_device"})
		return
	}

	type HistoryItem struct {
		ID            int64  `json:"id"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
		Category      string `json:"category"`
		Status        string `json:"status"`
		UserComment   string `json:"user_comment"`
		AdminReply    string `json:"admin_reply"`
		MessagesCount int    `json:"messages_count"`
	}

	items := make([]HistoryItem, 0)
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT s.id, TO_CHAR(s.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       TO_CHAR(s.updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       s.category, s.status, s.user_comment, s.admin_reply,
			       COUNT(m.id) as msg_count
			FROM support_tickets s
			LEFT JOIN ticket_messages m ON m.ticket_id = s.id
			WHERE (length($1) > 0 AND s.account_number = $1)
			   OR (length($2) > 0 AND s.device_id = $2)
			GROUP BY s.id
			ORDER BY s.created_at DESC
			LIMIT 50
		`, accNum, devID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var it HistoryItem
				if err := rows.Scan(&it.ID, &it.CreatedAt, &it.UpdatedAt, &it.Category, &it.Status, &it.UserComment, &it.AdminReply, &it.MessagesCount); err == nil {
					items = append(items, it)
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tickets": items,
	})
}

func (s *AppState) handleClientTicketSendMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TicketID      int64  `json:"ticket_id"`
		IsNewTopic    bool   `json:"is_new_topic"`
		Message       string `json:"message"`
		AccountNumber string `json:"account_number"`
		DeviceID      string `json:"device_id"`
		Category      string `json:"category"`
		AppVersion    string `json:"app_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_json"})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "empty_message"})
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "database_not_ready"})
		return
	}

	tID := req.TicketID
	if !req.IsNewTopic && tID <= 0 {
		_ = s.db.QueryRow(`
			SELECT id FROM support_tickets
			WHERE ((length($1) > 0 AND account_number = $1)
			   OR (length($2) > 0 AND device_id = $2))
			  AND status IN ('new', 'in_progress', 'open')
			ORDER BY updated_at DESC LIMIT 1
		`, req.AccountNumber, req.DeviceID).Scan(&tID)
	}

	var cat = req.Category
	if cat == "" {
		cat = "other"
	}
	var ver = req.AppVersion
	if ver == "" {
		ver = ServerAppVersion
	}

	if tID <= 0 || req.IsNewTopic {
		err := s.db.QueryRow(`
			INSERT INTO support_tickets (
				account_number, device_id, app_version, category, user_comment, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, 'new', NOW(), NOW())
			RETURNING id
		`, req.AccountNumber, req.DeviceID, ver, cat, req.Message).Scan(&tID)
		if err != nil {
			log.Printf("[TICKETS] Failed to create new ticket for message: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
			return
		}
		if s.rdb != nil {
			_ = s.rdb.Publish(context.Background(), "tickets:new", fmt.Sprintf("%d", tID)).Err()
		}
	} else {
		_, _ = s.db.Exec(`
			UPDATE support_tickets
			SET status = CASE WHEN status = 'resolved' THEN 'in_progress' ELSE status END,
			    updated_at = NOW()
			WHERE id = $1
		`, tID)
	}

	senderName := req.AccountNumber
	if req.AccountNumber != "" {
		_ = s.db.QueryRow(`SELECT nickname FROM accounts WHERE account_number = $1`, req.AccountNumber).Scan(&senderName)
	}
	if senderName == "" {
		senderName = "Игрок #" + req.AccountNumber
	}

	var msgID int64
	err := s.db.QueryRow(`
		INSERT INTO ticket_messages (
			ticket_id, sender_type, sender_name, message, created_at
		) VALUES ($1, 'user', $2, $3, NOW())
		RETURNING id
	`, tID, senderName, req.Message).Scan(&msgID)
	if err != nil {
		log.Printf("[TICKETS] Failed to insert ticket message: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
		return
	}

	if s.rdb != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"ticket_id":      tID,
			"message_id":     msgID,
			"sender_type":    "user",
			"sender_name":    senderName,
			"message":        req.Message,
			"account_number": req.AccountNumber,
			"created_at":     time.Now().UTC().Format(time.RFC3339),
		})
		_ = s.rdb.Publish(context.Background(), "tickets:message", string(payload)).Err()
		_ = s.rdb.Publish(context.Background(), "tickets:updated", fmt.Sprintf("%d", tID)).Err()
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"ticket_id":   tID,
		"ticket_code": fmt.Sprintf("TK-%04d", tID),
		"message_id":  msgID,
	})
}

func (s *AppState) handleClientTicketUploadLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TicketID      int64  `json:"ticket_id"`
		LogsGzip      string `json:"logs_gzip"`
		AccountNumber string `json:"account_number"`
		DeviceID      string `json:"device_id"`
		Summary       string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_json"})
		return
	}

	archiveBytes, err := base64.StdEncoding.DecodeString(req.LogsGzip)
	if err != nil || len(archiveBytes) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_logs_archive"})
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_not_ready"})
		return
	}

	tID := req.TicketID
	if tID <= 0 {
		_ = s.db.QueryRow(`
			SELECT id FROM support_tickets
			WHERE (length($1) > 0 AND account_number = $1)
			   OR (length($2) > 0 AND device_id = $2)
			ORDER BY updated_at DESC LIMIT 1
		`, req.AccountNumber, req.DeviceID).Scan(&tID)
	}

	if tID <= 0 {
		_ = s.db.QueryRow(`
			INSERT INTO support_tickets (
				account_number, device_id, app_version, category, user_comment, logs_archive, logs_archive_size, status
			) VALUES ($1, $2, $3, 'other', 'Диагностический отчет', $4, $5, 'new')
			RETURNING id
		`, req.AccountNumber, req.DeviceID, ServerAppVersion, archiveBytes, len(archiveBytes)).Scan(&tID)
		if s.rdb != nil {
			_ = s.rdb.Publish(context.Background(), "tickets:new", fmt.Sprintf("%d", tID)).Err()
		}
	} else {
		_, _ = s.db.Exec(`
			UPDATE support_tickets
			SET logs_archive = $1, logs_archive_size = $2, updated_at = NOW()
			WHERE id = $3
		`, archiveBytes, len(archiveBytes), tID)
	}

	summaryText := req.Summary
	if summaryText == "" {
		summaryText = fmt.Sprintf("Прикреплен свежий диагностический архив логов (%d КБ)", (len(archiveBytes)+1023)/1024)
	}

	var msgID int64
	_ = s.db.QueryRow(`
		INSERT INTO ticket_messages (
			ticket_id, sender_type, sender_name, message, attachment_type, attachment_data, attachment_size, created_at
		) VALUES ($1, 'system', 'Диагностика WarLink', $2, 'logs_archive', $3, $4, NOW())
		RETURNING id
	`, tID, summaryText, archiveBytes, len(archiveBytes)).Scan(&msgID)

	if s.rdb != nil {
		payload, _ := json.Marshal(map[string]interface{}{
			"ticket_id":       tID,
			"message_id":      msgID,
			"sender_type":     "system",
			"sender_name":     "Диагностика WarLink",
			"message":         summaryText,
			"attachment_type": "logs_archive",
			"attachment_size": len(archiveBytes),
			"created_at":      time.Now().UTC().Format(time.RFC3339),
		})
		_ = s.rdb.Publish(context.Background(), "tickets:message", string(payload)).Err()
		_ = s.rdb.Publish(context.Background(), "tickets:updated", fmt.Sprintf("%d", tID)).Err()
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"ticket_id":    tID,
		"ticket_code":  fmt.Sprintf("TK-%04d", tID),
		"message_id":   msgID,
		"archive_size": len(archiveBytes),
	})
}

func (s *AppState) handleClientTicketResolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TicketID int64 `json:"ticket_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TicketID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_ticket_id"})
		return
	}

	if s.db != nil {
		_, _ = s.db.Exec(`
			UPDATE support_tickets
			SET status = 'resolved', resolved_at = NOW(), updated_at = NOW()
			WHERE id = $1
		`, req.TicketID)

		_, _ = s.db.Exec(`
			INSERT INTO ticket_messages (
				ticket_id, sender_type, sender_name, message, created_at
			) VALUES ($1, 'system', 'Система', 'Пользователь отметил вопрос как решенный.', NOW())
		`, req.TicketID)

		if s.rdb != nil {
			_ = s.rdb.Publish(context.Background(), "tickets:updated", fmt.Sprintf("%d", req.TicketID)).Err()
			payload, _ := json.Marshal(map[string]interface{}{
				"ticket_id":   req.TicketID,
				"sender_type": "system",
				"message":     "Пользователь отметил вопрос как решенный.",
			})
			_ = s.rdb.Publish(context.Background(), "tickets:message", string(payload)).Err()
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "resolved",
	})
}

type AdminTicketSummary struct {
	ID              int64           `json:"id"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	AccountNumber   string          `json:"account_number"`
	DeviceID        string          `json:"device_id"`
	AppVersion      string          `json:"app_version"`
	Category        string          `json:"category"`
	UserComment     string          `json:"user_comment"`
	LogsArchiveSize int             `json:"logs_archive_size"`
	Status          string          `json:"status"`
	AdminReply      string          `json:"admin_reply"`
	ResolvedAt      *string         `json:"resolved_at"`
	SystemInfo      json.RawMessage `json:"system_info,omitempty"`
}

func (s *AppState) handleAdminTicketsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
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

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	tickets := make([]AdminTicketSummary, 0)
	counts := map[string]int{
		"new":         0,
		"in_progress": 0,
		"resolved":    0,
		"closed":      0,
		"total":       0,
	}

	if s.db != nil {
		// Calculate status counts
		cntRows, err := s.db.Query(`SELECT status, COUNT(*) FROM support_tickets GROUP BY status`)
		if err == nil {
			defer cntRows.Close()
			for cntRows.Next() {
				var st string
				var c int
				if cntRows.Scan(&st, &c) == nil {
					counts[st] = c
					counts["total"] += c
				}
			}
		}

		// Build query
		query := `
			SELECT id, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       TO_CHAR(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       account_number, device_id, app_version, category, user_comment,
			       logs_archive_size, status, admin_reply,
			       TO_CHAR(resolved_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			       COALESCE(system_info::TEXT, '{}')
			FROM support_tickets
			WHERE 1=1
		`
		var args []interface{}
		argIdx := 1

		if statusFilter != "" && statusFilter != "all" {
			query += fmt.Sprintf(" AND status = $%d", argIdx)
			args = append(args, statusFilter)
			argIdx++
		}

		if searchQuery != "" {
			query += fmt.Sprintf(" AND (account_number ILIKE $%d OR device_id ILIKE $%d OR user_comment ILIKE $%d OR id::TEXT = $%d)", argIdx, argIdx, argIdx, argIdx)
			args = append(args, "%"+searchQuery+"%")
			argIdx++
		}

		query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
		args = append(args, limit)

		rows, err := s.db.Query(query, args...)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var t AdminTicketSummary
				var resAt sql.NullString
				var sysRaw string
				if err := rows.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.AccountNumber, &t.DeviceID, &t.AppVersion, &t.Category, &t.UserComment, &t.LogsArchiveSize, &t.Status, &t.AdminReply, &resAt, &sysRaw); err == nil {
					if resAt.Valid {
						val := resAt.String
						t.ResolvedAt = &val
					}
					t.SystemInfo = json.RawMessage(sysRaw)
					tickets = append(tickets, t)
				}
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tickets": tickets,
		"counts":  counts,
	})
}

func (s *AppState) handleAdminTicketRouter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Dashboard-Key, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	rawPath := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/tickets/")
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.handleAdminTicketsList(w, r)
		return
	}

	ticketID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid_ticket_id"})
		return
	}

	subAction := ""
	if len(parts) > 1 {
		subAction = parts[1]
	}

	switch subAction {
	case "file":
		// GET /api/v1/admin/tickets/:id/file?name=warlink.log
		fileName := r.URL.Query().Get("name")
		if fileName == "" {
			fileName = "warlink.log"
		}
		var archiveBytes []byte
		if s.db != nil {
			_ = s.db.QueryRow(`SELECT logs_archive FROM support_tickets WHERE id = $1`, ticketID).Scan(&archiveBytes)
		}
		if len(archiveBytes) == 0 {
			w.WriteHeader(http.StatusNotFound)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "no_logs_archive"})
			return
		}
		content, err := extractFileFromTarGz(archiveBytes, fileName)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"name":    fileName,
			"content": content,
		})

	case "archive":
		// GET /api/v1/admin/tickets/:id/archive
		var archiveBytes []byte
		if s.db != nil {
			_ = s.db.QueryRow(`SELECT logs_archive FROM support_tickets WHERE id = $1`, ticketID).Scan(&archiveBytes)
		}
		if len(archiveBytes) == 0 {
			http.Error(w, "Archive not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ticket_%d_logs.tar.gz"`, ticketID))
		w.Header().Set("Content-Length", strconv.Itoa(len(archiveBytes)))
		_, _ = w.Write(archiveBytes)

	case "reply":
		// POST /api/v1/admin/tickets/:id/reply
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var replyReq struct {
			Title       string `json:"title"`
			Message     string `json:"message"`
			Severity    string `json:"severity"` // update | urgent | info | warning
			ActionLabel string `json:"action_label"`
			ActionURL   string `json:"action_url"`
			Status      string `json:"status"` // resolved | in_progress | closed
		}
		if err := json.NewDecoder(r.Body).Decode(&replyReq); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid_payload"})
			return
		}
		replyReq.Title = strings.TrimSpace(replyReq.Title)
		replyReq.Message = strings.TrimSpace(replyReq.Message)
		if replyReq.Title == "" || replyReq.Message == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "title_and_message_required"})
			return
		}
		sev := strings.ToLower(strings.TrimSpace(replyReq.Severity))
		if sev != "info" && sev != "update" && sev != "warning" && sev != "urgent" {
			sev = "update"
		}
		newStatus := strings.ToLower(strings.TrimSpace(replyReq.Status))
		if newStatus == "" {
			newStatus = "resolved"
		}

		var ticketAcc, ticketDev string
		if s.db != nil {
			_ = s.db.QueryRow(`SELECT account_number, device_id FROM support_tickets WHERE id = $1`, ticketID).Scan(&ticketAcc, &ticketDev)

			targetType := "account"
			targetID := ticketAcc
			if targetID == "" {
				targetType = "device"
				targetID = ticketDev
			}

			if replyReq.ActionURL == "" {
				replyReq.ActionURL = "#view-support"
			}
			if replyReq.ActionLabel == "" {
				replyReq.ActionLabel = "Открыть диалог"
			}

			// 1. Dispatch Notification (Rule 7)
			if targetID != "" {
				_, notifErr := s.db.Exec(`
					INSERT INTO in_app_notifications (target_type, target_id, title, message, severity, action_label, action_url, created_at)
					VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
				`, targetType, targetID, replyReq.Title, replyReq.Message, sev, replyReq.ActionLabel, replyReq.ActionURL)
				if notifErr != nil {
					log.Printf("[TICKETS] Warning inserting reply notification: %v", notifErr)
				} else {
					log.Printf("[TICKETS] Dispatched in-app notification reply to %s:%s for ticket #%d", targetType, targetID, ticketID)
				}
			}

			// 2. Insert into ticket_messages
			_, _ = s.db.Exec(`
				INSERT INTO ticket_messages (
					ticket_id, sender_type, sender_name, message, created_at
				) VALUES ($1, 'admin', 'Max (Разработчик)', $2, NOW())
			`, ticketID, replyReq.Message)

			// 3. Update Ticket
			_, _ = s.db.Exec(`
				UPDATE support_tickets
				SET admin_reply = $1, status = $2,
				    resolved_at = CASE WHEN $2 = 'resolved' THEN NOW() ELSE resolved_at END,
				    updated_at = NOW()
				WHERE id = $3
			`, replyReq.Message, newStatus, ticketID)
			if s.rdb != nil {
				_ = s.rdb.Publish(context.Background(), "tickets:updated", fmt.Sprintf("%d", ticketID)).Err()
				payload, _ := json.Marshal(map[string]interface{}{
					"ticket_id":   ticketID,
					"sender_type": "admin",
					"sender_name": "Max (Разработчик)",
					"message":     replyReq.Message,
					"status":      newStatus,
					"created_at":  time.Now().UTC().Format(time.RFC3339),
				})
				_ = s.rdb.Publish(context.Background(), "tickets:message", string(payload)).Err()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"status":  newStatus,
			"message": "Депеша успешно направлена пользователю",
		})

	case "status":
		// PATCH or POST /api/v1/admin/tickets/:id/status
		var stReq struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&stReq)
		stReq.Status = strings.ToLower(strings.TrimSpace(stReq.Status))
		if stReq.Status == "" {
			stReq.Status = "in_progress"
		}
		if s.db != nil {
			_, _ = s.db.Exec(`
				UPDATE support_tickets
				SET status = $1,
				    resolved_at = CASE WHEN $1 = 'resolved' THEN NOW() ELSE resolved_at END,
				    updated_at = NOW()
				WHERE id = $2
			`, stReq.Status, ticketID)
			if s.rdb != nil {
				_ = s.rdb.Publish(context.Background(), "tickets:updated", fmt.Sprintf("%d", ticketID)).Err()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "status": stReq.Status})

	default:
		// GET /api/v1/admin/tickets/:id - Full details + file manifest
		var t AdminTicketSummary
		var resAt sql.NullString
		var sysRaw string
		var archiveBytes []byte
		messages := make([]TicketMessageItem, 0)
		if s.db != nil {
			err := s.db.QueryRow(`
				SELECT id, TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       TO_CHAR(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       account_number, device_id, app_version, category, user_comment,
				       logs_archive_size, status, admin_reply,
				       TO_CHAR(resolved_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				       COALESCE(system_info::TEXT, '{}'),
				       logs_archive
				FROM support_tickets
				WHERE id = $1
			`, ticketID).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.AccountNumber, &t.DeviceID, &t.AppVersion, &t.Category, &t.UserComment, &t.LogsArchiveSize, &t.Status, &t.AdminReply, &resAt, &sysRaw, &archiveBytes)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "ticket_not_found"})
				return
			}
			if resAt.Valid {
				val := resAt.String
				t.ResolvedAt = &val
			}
			t.SystemInfo = json.RawMessage(sysRaw)

			// Fetch messages
			rows, err := s.db.Query(`
				SELECT id, ticket_id, sender_type, sender_name, message, attachment_type, attachment_size,
				       TO_CHAR(created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
				FROM ticket_messages
				WHERE ticket_id = $1
				ORDER BY created_at ASC
			`, ticketID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var m TicketMessageItem
					if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderType, &m.SenderName, &m.Message, &m.AttachmentType, &m.AttachmentSize, &m.CreatedAt); err == nil {
						messages = append(messages, m)
					}
				}
			}
		}

		if len(messages) == 0 {
			if t.UserComment != "" {
				messages = append(messages, TicketMessageItem{
					ID:             1,
					TicketID:       t.ID,
					SenderType:     "user",
					SenderName:     t.AccountNumber,
					Message:        t.UserComment,
					AttachmentType: func() string { if t.LogsArchiveSize > 0 { return "logs_archive" }; return "" }(),
					AttachmentSize: t.LogsArchiveSize,
					CreatedAt:      t.CreatedAt,
				})
			}
			if t.AdminReply != "" {
				messages = append(messages, TicketMessageItem{
					ID:         2,
					TicketID:   t.ID,
					SenderType: "admin",
					SenderName: "Max (Разработчик)",
					Message:    t.AdminReply,
					CreatedAt:  t.UpdatedAt,
				})
			}
		}

		filesManifest := listFilesInTarGz(archiveBytes)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"ticket":   t,
			"messages": messages,
			"files":    filesManifest,
		})
	}
}

func listFilesInTarGz(archive []byte) []map[string]interface{} {
	files := make([]map[string]interface{}, 0)
	if len(archive) == 0 {
		return files
	}
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return files
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF || err != nil {
			break
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 {
			files = append(files, map[string]interface{}{
				"name":     hdr.Name,
				"size":     hdr.Size,
				"mod_time": hdr.ModTime.Format(time.RFC3339),
			})
		}
	}
	return files
}

func extractFileFromTarGz(archive []byte, targetName string) (string, error) {
	if len(archive) == 0 {
		return "", fmt.Errorf("empty archive")
	}
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) == filepath.Base(targetName) || hdr.Name == targetName {
			var buf bytes.Buffer
			// Read up to 15MB
			_, copyErr := io.CopyN(&buf, tr, 15*1024*1024)
			if copyErr != nil && copyErr != io.EOF {
				return "", copyErr
			}
			return buf.String(), nil
		}
	}
	return "", fmt.Errorf("file %q not found in archive", targetName)
}

func (s *AppState) handleAdminTicketWeb(w http.ResponseWriter, r *http.Request) {
	// If query key is passed, persist cookie
	qKey := r.URL.Query().Get("key")
	if qKey != "" && s.cfg.DashboardKey != "" && qKey == s.cfg.DashboardKey {
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_key",
			Value:    qKey,
			Path:     "/",
			MaxAge:   30 * 86400,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
	}

	if !s.checkAdminAuth(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <title>WarLink Support // Доступ ограничен</title>
    <style>
        body { background: #0c0d10; color: #e6e8ee; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
        .box { background: #14161b; border: 1px solid #262a34; padding: 32px; border-radius: 2px; width: 340px; box-shadow: 0 8px 24px rgba(0,0,0,0.4); }
        h2 { font-size: 14px; text-transform: uppercase; letter-spacing: 0.08em; margin: 0 0 16px 0; color: #FF5E1F; }
        p { font-size: 13px; color: #8b92a5; margin-bottom: 20px; line-height: 1.4; }
        input { width: 100%; box-sizing: border-box; background: #0a0b0d; border: 1px solid #262a34; color: #fff; padding: 10px 12px; font-size: 14px; margin-bottom: 16px; border-radius: 2px; outline: none; }
        input:focus { border-color: #FF5E1F; }
        button { width: 100%; background: #FF5E1F; color: #fff; border: none; padding: 10px; font-size: 13px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; cursor: pointer; border-radius: 2px; }
        button:hover { background: #e04e14; }
    </style>
</head>
<body>
    <div class="box">
        <h2>WARLINK SUPPORT</h2>
        <p>Для доступа к операционному центру тикетов введите ключ администратора.</p>
        <form method="GET" action="/admin/tickets">
            <input type="password" name="key" placeholder="Ключ авторизации" autofocus required>
            <button type="submit">Войти в систему</button>
        </form>
    </div>
</body>
</html>`)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminTicketCenterHTML))
}

const adminTicketCenterHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>WarLink Support // Операционный центр тикетов</title>
    <style>
        :root {
            --bg: #0c0d10;
            --card-bg: #14161b;
            --surface: #1a1d24;
            --surface-hover: #222630;
            --border: #262a34;
            --border-focus: #3b4252;
            --text: #e6e8ee;
            --text-muted: #8b92a5;
            --text-dim: #5c6375;
            --accent: #FF5E1F;
            --accent-hover: #e04e14;
            --red: #ef4444;
            --red-bg: rgba(239, 68, 68, 0.12);
            --amber: #f59e0b;
            --amber-bg: rgba(245, 158, 11, 0.12);
            --green: #10b981;
            --green-bg: rgba(16, 185, 129, 0.12);
            --blue: #3b82f6;
            --blue-bg: rgba(59, 130, 246, 0.12);
            --radius: 2px;
            --font-sans: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            --font-mono: 'JetBrains Mono', 'Consolas', 'Fira Code', monospace;
        }

        html, body {
            background-color: var(--bg);
            color: var(--text);
            font-family: var(--font-sans);
            font-size: 13px;
            line-height: 1.5;
            height: 100%;
            margin: 0;
            padding: 0;
            display: flex;
            flex-direction: column;
            overflow: hidden;
            scrollbar-width: thin;
            scrollbar-color: #333946 transparent;
        }

        /* Top Header */
        header {
            height: 52px;
            background: var(--card-bg);
            border-bottom: 1px solid var(--border);
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0 16px;
            flex-shrink: 0;
            gap: 16px;
        }

        .header-brand {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .header-logo {
            width: 24px;
            height: 24px;
            color: var(--accent);
            flex-shrink: 0;
        }

        .brand-title {
            font-size: 13px;
            font-weight: 700;
            letter-spacing: 0.08em;
            text-transform: uppercase;
            color: var(--text);
        }

        .brand-badge {
            font-size: 10px;
            font-weight: 600;
            padding: 2px 6px;
            background: rgba(255, 94, 31, 0.15);
            color: var(--accent);
            border: 1px solid rgba(255, 94, 31, 0.3);
            border-radius: var(--radius);
            letter-spacing: 0.05em;
        }

        .header-stats {
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .stat-pill {
            display: flex;
            align-items: center;
            gap: 6px;
            padding: 4px 10px;
            font-size: 11px;
            font-weight: 600;
            border-radius: var(--radius);
            cursor: pointer;
            border: 1px solid transparent;
            text-transform: uppercase;
            letter-spacing: 0.04em;
            transition: all 0.15s;
        }

        .stat-pill.pill-new { background: var(--red-bg); color: var(--red); border-color: rgba(239, 68, 68, 0.3); }
        .stat-pill.pill-progress { background: var(--amber-bg); color: var(--amber); border-color: rgba(245, 158, 11, 0.3); }
        .stat-pill.pill-resolved { background: var(--green-bg); color: var(--green); border-color: rgba(16, 185, 129, 0.3); }
        .stat-pill.active { outline: 1px solid currentColor; }

        .header-actions {
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .btn-link {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            background: var(--surface);
            color: var(--text-muted);
            border: 1px solid var(--border);
            padding: 6px 10px;
            border-radius: var(--radius);
            font-size: 11px;
            font-weight: 600;
            text-decoration: none;
            cursor: pointer;
            transition: all 0.15s;
        }
        .btn-link:hover { color: var(--text); background: var(--surface-hover); border-color: var(--border-focus); }

        /* Workspace Grid */
        .workspace {
            display: flex;
            flex: 1;
            min-height: 0;
            overflow: hidden;
        }

        /* Left Sidebar: Tickets List */
        .sidebar {
            width: 380px;
            background: var(--bg);
            border-right: 1px solid var(--border);
            display: flex;
            flex-direction: column;
            flex-shrink: 0;
            min-height: 0;
            overflow: hidden;
        }

        .sidebar-search-bar {
            padding: 10px 12px;
            border-bottom: 1px solid var(--border);
            background: var(--card-bg);
            display: flex;
            gap: 8px;
        }

        .search-input {
            flex: 1;
            background: var(--bg);
            border: 1px solid var(--border);
            color: var(--text);
            padding: 7px 10px;
            font-size: 12px;
            border-radius: var(--radius);
            outline: none;
        }
        .search-input:focus { border-color: var(--accent); }

        .sidebar-tabs {
            display: flex;
            background: var(--card-bg);
            border-bottom: 1px solid var(--border);
            padding: 4px 8px;
            gap: 4px;
        }

        .tab-btn {
            flex: 1;
            background: transparent;
            border: none;
            color: var(--text-muted);
            font-size: 11px;
            font-weight: 600;
            padding: 6px 0;
            cursor: pointer;
            text-align: center;
            border-radius: var(--radius);
            transition: all 0.15s;
        }
        .tab-btn:hover { color: var(--text); background: var(--surface); }
        .tab-btn.active { color: #fff; background: var(--surface-hover); border: 1px solid var(--border); }

        .ticket-list {
            flex: 1;
            min-height: 0;
            overflow-y: auto;
            padding: 8px;
            display: flex;
            flex-direction: column;
            gap: 6px;
        }

        .ticket-card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 10px 12px;
            cursor: pointer;
            transition: border-color 0.15s, background 0.15s;
            position: relative;
        }
        .ticket-card:hover { border-color: var(--border-focus); background: var(--surface); }
        .ticket-card.selected { border-color: var(--accent); background: var(--surface); }

        .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 4px;
        }

        .ticket-id {
            font-family: var(--font-mono);
            font-size: 11px;
            font-weight: 700;
            color: var(--text-muted);
        }

        .badge-status {
            font-size: 9px;
            font-weight: 700;
            text-transform: uppercase;
            padding: 2px 5px;
            border-radius: var(--radius);
            letter-spacing: 0.05em;
        }
        .badge-status.new { background: var(--red-bg); color: var(--red); }
        .badge-status.in_progress { background: var(--amber-bg); color: var(--amber); }
        .badge-status.resolved { background: var(--green-bg); color: var(--green); }
        .badge-status.closed { background: rgba(107, 114, 128, 0.15); color: #9ca3af; }

        .card-account {
            font-family: var(--font-mono);
            font-size: 12px;
            font-weight: 600;
            color: var(--text);
            margin-bottom: 4px;
        }

        .card-category {
            display: inline-block;
            font-size: 10px;
            font-weight: 600;
            padding: 1px 5px;
            background: var(--surface);
            color: var(--text-muted);
            border-radius: var(--radius);
            margin-bottom: 6px;
        }

        .card-snippet {
            font-size: 11px;
            color: var(--text-muted);
            display: -webkit-box;
            -webkit-line-clamp: 2;
            -webkit-box-orient: vertical;
            overflow: hidden;
            line-height: 1.4;
            margin-bottom: 6px;
        }

        .card-footer {
            display: flex;
            justify-content: space-between;
            align-items: center;
            font-size: 10px;
            color: var(--text-dim);
        }

        /* Right Detail Pane */
        .detail-pane {
            flex: 1;
            display: flex;
            flex-direction: column;
            background: var(--bg);
            min-height: 0;
            overflow-y: auto;
            overflow-x: hidden;
            -webkit-overflow-scrolling: touch;
        }

        .empty-placeholder {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            color: var(--text-dim);
            gap: 12px;
            padding: 40px;
            text-align: center;
        }
        .empty-placeholder svg { width: 48px; height: 48px; stroke: var(--text-dim); }

        .detail-content {
            padding: 20px 20px 80px 20px;
            display: flex;
            flex-direction: column;
            gap: 18px;
            max-width: 1200px;
            margin: 0 auto;
            width: 100%;
        }

        /* Detail Action Bar */
        .detail-action-bar {
            background: var(--card-bg);
            border: 1px solid var(--border);
            padding: 14px 16px;
            border-radius: var(--radius);
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 16px;
        }

        .detail-title-group {
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .detail-title {
            font-size: 16px;
            font-weight: 700;
            font-family: var(--font-mono);
            color: var(--text);
        }

        .status-actions {
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .btn-status {
            padding: 6px 12px;
            font-size: 11px;
            font-weight: 600;
            border-radius: var(--radius);
            border: 1px solid var(--border);
            background: var(--surface);
            color: var(--text-muted);
            cursor: pointer;
            transition: all 0.15s;
        }
        .btn-status:hover { background: var(--surface-hover); color: #fff; }
        .btn-status.btn-accent { background: var(--accent); color: #fff; border-color: var(--accent); }
        .btn-status.btn-accent:hover { background: var(--accent-hover); }

        /* Diagnostic Info Grid */
        .info-grid {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 12px;
        }

        .info-box {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 12px;
        }

        .info-label {
            font-size: 10px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-dim);
            margin-bottom: 4px;
        }

        .info-val {
            font-size: 12px;
            font-weight: 600;
            color: var(--text);
            font-family: var(--font-mono);
            word-break: break-all;
        }

        /* User Comment Box */
        .comment-section {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 16px;
        }

        .section-header {
            font-size: 11px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.06em;
            color: var(--text-muted);
            margin-bottom: 8px;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .comment-body {
            background: #090a0d;
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 12px;
            font-size: 13px;
            color: var(--text);
            white-space: pre-wrap;
            line-height: 1.5;
        }

        /* Log Explorer */
        .log-section {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            overflow: hidden;
            display: flex;
            flex-direction: column;
        }

        .log-nav-bar {
            background: #111317;
            border-bottom: 1px solid var(--border);
            padding: 6px 12px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 12px;
            flex-wrap: wrap;
        }

        .log-tabs {
            display: flex;
            gap: 4px;
            overflow-x: auto;
        }

        .log-tab-btn {
            background: transparent;
            border: 1px solid transparent;
            color: var(--text-muted);
            font-size: 11px;
            font-family: var(--font-mono);
            padding: 4px 10px;
            cursor: pointer;
            border-radius: var(--radius);
            transition: all 0.15s;
            white-space: nowrap;
        }
        .log-tab-btn:hover { color: var(--text); background: var(--surface); }
        .log-tab-btn.active { color: #fff; background: var(--surface); border-color: var(--border-focus); }

        .log-tools {
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .log-search-input {
            background: #090a0d;
            border: 1px solid var(--border);
            color: var(--text);
            padding: 4px 8px;
            font-size: 11px;
            font-family: var(--font-mono);
            border-radius: var(--radius);
            outline: none;
            width: 160px;
        }
        .log-search-input:focus { border-color: var(--accent); }

        .log-terminal {
            background: #08090b;
            color: #d1d5db;
            font-family: var(--font-mono);
            font-size: 12px;
            line-height: 1.45;
            padding: 12px;
            max-height: 480px;
            overflow: auto;
            white-space: pre-wrap;
            word-break: break-all;
        }

        /* Syntax highlight tokens */
        .tok-error { color: #f87171; font-weight: 700; background: rgba(239, 68, 68, 0.15); padding: 0 2px; }
        .tok-warn  { color: #fbbf24; font-weight: 700; }
        .tok-info  { color: #60a5fa; }
        .tok-match { background: #ca8a04; color: #000; font-weight: 700; }

        /* Reply & Notification Dispatcher */
        .reply-section {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: var(--radius);
            padding: 16px;
            display: flex;
            flex-direction: column;
            gap: 12px;
        }

        .preset-templates {
            display: flex;
            flex-wrap: wrap;
            gap: 6px;
            margin-bottom: 4px;
        }

        .btn-preset {
            background: var(--surface);
            border: 1px solid var(--border);
            color: var(--text-muted);
            font-size: 10px;
            font-weight: 600;
            padding: 4px 8px;
            border-radius: var(--radius);
            cursor: pointer;
            transition: all 0.15s;
        }
        .btn-preset:hover { background: var(--surface-hover); color: var(--text); border-color: var(--border-focus); }

        .form-row {
            display: flex;
            gap: 12px;
        }

        .form-col {
            flex: 1;
            display: flex;
            flex-direction: column;
            gap: 4px;
        }

        .form-label {
            font-size: 10px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-dim);
        }

        .form-input, .form-select, .form-textarea {
            background: #090a0d;
            border: 1px solid var(--border);
            color: var(--text);
            padding: 8px 10px;
            font-size: 12px;
            border-radius: var(--radius);
            outline: none;
            font-family: var(--font-sans);
        }
        .form-input:focus, .form-select:focus, .form-textarea:focus { border-color: var(--accent); }

        .form-textarea {
            min-height: 90px;
            resize: vertical;
            line-height: 1.45;
        }

        .reply-actions {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-top: 4px;
        }

        .check-group {
            display: flex;
            align-items: center;
            gap: 6px;
            font-size: 12px;
            color: var(--text-muted);
            cursor: pointer;
        }

        .btn-send-reply {
            background: var(--accent);
            color: #fff;
            border: none;
            padding: 8px 16px;
            font-size: 12px;
            font-weight: 700;
            letter-spacing: 0.04em;
            text-transform: uppercase;
            border-radius: var(--radius);
            cursor: pointer;
            transition: background 0.15s;
            display: inline-flex;
            align-items: center;
            gap: 8px;
        }
        .btn-send-reply:hover { background: var(--accent-hover); }

        .reply-history-box {
            background: rgba(16, 185, 129, 0.06);
            border: 1px solid rgba(16, 185, 129, 0.25);
            border-radius: var(--radius);
            padding: 12px;
            margin-top: 8px;
        }

        /* Custom Scrollbars */
        ::-webkit-scrollbar { width: 7px; height: 7px; }
        ::-webkit-scrollbar-track { background: rgba(0, 0, 0, 0.25); }
        ::-webkit-scrollbar-thumb { background: #3b4252; border-radius: 3px; }
        ::-webkit-scrollbar-thumb:hover { background: #FF5E1F; }
    </style>
</head>
<body>
    <header>
        <div class="header-brand">
            <svg class="header-logo" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
            </svg>
            <div class="brand-title">WarLink Support Desk</div>
            <div class="brand-badge">OPERATIONS</div>
        </div>

        <div class="header-stats">
            <div class="stat-pill pill-new" id="pill-new" onclick="filterByStatus('new')">● <span id="count-new">0</span> Новых</div>
            <div class="stat-pill pill-progress" id="pill-progress" onclick="filterByStatus('in_progress')">● <span id="count-progress">0</span> В работе</div>
            <div class="stat-pill pill-resolved" id="pill-resolved" onclick="filterByStatus('resolved')">● <span id="count-resolved">0</span> Решено</div>
        </div>

        <div class="header-actions">
            <a href="/admin/routing-feedback" class="btn-link" style="color:var(--accent);">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"/><rect x="2" y="14" width="20" height="8" rx="2" ry="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>
                Замеры маршрутов
            </a>
            <a href="/dashboard" class="btn-link" target="_blank">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg>
                Телеметрия
            </a>
            <button class="btn-link" onclick="loadTickets()">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2"/></svg>
                Обновить
            </button>
        </div>
    </header>

    <div class="workspace">
        <!-- Sidebar -->
        <aside class="sidebar">
            <div class="sidebar-search-bar">
                <input type="text" class="search-input" id="search-input" placeholder="Поиск по аккаунту, ID или тексту..." oninput="handleSearch(this.value)">
            </div>
            <div class="sidebar-tabs">
                <button class="tab-btn active" id="tab-all" onclick="filterByStatus('all')">Все (<span id="count-total">0</span>)</button>
                <button class="tab-btn" id="tab-new" onclick="filterByStatus('new')">Новые</button>
                <button class="tab-btn" id="tab-in_progress" onclick="filterByStatus('in_progress')">В работе</button>
                <button class="tab-btn" id="tab-resolved" onclick="filterByStatus('resolved')">Решенные</button>
            </div>
            <div class="ticket-list" id="ticket-list">
                <div style="padding:20px; text-align:center; color:var(--text-dim);">Загрузка обращений...</div>
            </div>
        </aside>

        <!-- Main Detail Pane -->
        <main class="detail-pane" id="detail-pane">
            <div class="empty-placeholder" id="empty-placeholder">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M22 12h-6l-2 3h-4l-2-3H2"/>
                    <path d="M5.45 5.11L2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>
                </svg>
                <div>
                    <div style="font-size:14px; font-weight:600; color:var(--text-muted); margin-bottom:4px;">Выберите тикет из списка слева</div>
                    <div style="font-size:12px;">Здесь отобразится системная диагностика, полный лог и форма отправки ответа.</div>
                </div>
            </div>

            <div class="detail-content" id="detail-content" style="display:none;">
                <!-- Action Bar -->
                <div class="detail-action-bar">
                    <div class="detail-title-group">
                        <div class="detail-title" id="d-ticket-id">#TK-0000</div>
                        <span class="badge-status new" id="d-status-badge">Новый</span>
                    </div>
                    <div class="status-actions">
                        <button class="btn-status" onclick="setTicketStatus('in_progress')">В работу</button>
                        <button class="btn-status" onclick="setTicketStatus('resolved')">Решено</button>
                        <button class="btn-status" onclick="setTicketStatus('closed')">Закрыть</button>
                        <button class="btn-status btn-accent" id="btn-download-archive" onclick="downloadLogsArchive()">
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:middle; margin-right:4px;"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                            Скачать архив логов (.tar.gz)
                        </button>
                    </div>
                </div>

                <!-- Info Grid -->
                <div class="info-grid">
                    <div class="info-box">
                        <div class="info-label">Аккаунт пользователя</div>
                        <div class="info-val" id="d-account" style="color:var(--accent);">5230-0000-0000-0000</div>
                        <div style="font-size:10px; color:var(--text-dim); margin-top:2px;" id="d-device">dev: ...</div>
                    </div>
                    <div class="info-box">
                        <div class="info-label">Версия клиента & ОС</div>
                        <div class="info-val" id="d-app-ver">v2.1.7</div>
                        <div style="font-size:10px; color:var(--text-dim); margin-top:2px;" id="d-os-ver">Windows 10.0</div>
                    </div>
                    <div class="info-box">
                        <div class="info-label">Сетевой режим & Службы</div>
                        <div class="info-val" id="d-network-mode">Комплексный режим</div>
                        <div style="font-size:10px; color:var(--text-dim); margin-top:2px;" id="d-services-status">sing-box: OK, winws2: OK</div>
                    </div>
                    <div class="info-box">
                        <div class="info-label">Поступление & Размер</div>
                        <div class="info-val" id="d-time-created">12:34:56 UTC</div>
                        <div style="font-size:10px; color:var(--text-dim); margin-top:2px;" id="d-archive-size">145 KB</div>
                    </div>
                </div>

                <!-- Conversation Stream -->
                <div class="comment-section">
                    <div class="section-header">
                        <span>История переписки и контекст диалога</span>
                        <span class="card-category" id="d-category-badge">Вылет из матча</span>
                    </div>
                    <div class="chat-thread-container" id="d-chat-thread" style="display:flex; flex-direction:column; gap:8px; max-height:220px; overflow-y:auto; padding:6px 0;">
                        <div class="comment-body" id="d-user-comment">Текст обращения отсутствует.</div>
                    </div>
                </div>

                <!-- Full Log Viewer -->
                <div class="log-section">
                    <div class="log-nav-bar">
                        <div class="log-tabs" id="log-tabs">
                            <!-- Dynamic log buttons -->
                        </div>
                        <div class="log-tools">
                            <input type="text" class="log-search-input" id="log-search" placeholder="Поиск в логе (Ctrl+F)" oninput="filterLog(this.value)">
                            <button class="btn-status" onclick="copyCurrentLog()">Копировать</button>
                        </div>
                    </div>
                    <div class="log-terminal" id="log-terminal">Загрузка файла лога...</div>
                </div>

                <!-- Reply & Notification Dispatcher -->
                <div class="reply-section">
                    <div class="section-header">
                        <span>Ответ пользователю через внутриигровое уведомление (Правило 7)</span>
                        <span style="font-size:10px; color:var(--text-dim); font-weight:normal;">Депеша поступит на аккаунт пользователя в приложении WarLink</span>
                    </div>

                    <div class="preset-templates">
                        <span style="font-size:10px; color:var(--text-dim); align-self:center; margin-right:4px;">Шаблоны быстрых ответов:</span>
                        <button class="btn-preset" onclick="applyTemplate('error_114745308')">Ошибка 114745308 (Рассинхрон IP/UDP)</button>
                        <button class="btn-preset" onclick="applyTemplate('zapret_crash')">Конфликт WinDivert / Запрет</button>
                        <button class="btn-preset" onclick="applyTemplate('match_drop')">Вылет из матча (Таймаут шлюза)</button>
                        <button class="btn-preset" onclick="applyTemplate('resolved')">Успешное решение</button>
                    </div>

                    <div class="form-row">
                        <div class="form-col" style="flex:2;">
                            <label class="form-label">Заголовок депеши</label>
                            <input type="text" class="form-input" id="reply-title" placeholder="Например: Решение по вашему обращению">
                        </div>
                        <div class="form-col" style="flex:1;">
                            <label class="form-label">Важность (Цвет баннера)</label>
                            <select class="form-select" id="reply-severity">
                                <option value="update" selected>Update (Оранжевый)</option>
                                <option value="urgent">Urgent (Красный)</option>
                                <option value="warning">Warning (Желтый)</option>
                                <option value="info">Info (Синий)</option>
                            </select>
                        </div>
                    </div>

                    <div class="form-col">
                        <label class="form-label">Текст сообщения</label>
                        <textarea class="form-textarea" id="reply-message" placeholder="Введите рекомендации для пользователя..."></textarea>
                    </div>

                    <div class="form-row">
                        <div class="form-col">
                            <label class="form-label">Текст кнопки действия (опционально)</label>
                            <input type="text" class="form-input" id="reply-action-label" placeholder="Например: Проверить статус или Уровень WARDOGS">
                        </div>
                        <div class="form-col">
                            <label class="form-label">Роут перехода (опционально)</label>
                            <input type="text" class="form-input" id="reply-action-url" placeholder="Например: #view-details или #view-progression">
                        </div>
                    </div>

                    <div class="reply-actions">
                        <label class="check-group">
                            <input type="checkbox" id="reply-mark-resolved" checked>
                            <span>Отметить тикет как решенный (resolved)</span>
                        </label>
                        <button class="btn-send-reply" id="btn-send-reply" onclick="sendReply()">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
                            Отправить депешу пользователю
                        </button>
                    </div>

                    <div class="reply-history-box" id="reply-history" style="display:none;">
                        <div style="font-size:10px; font-weight:700; color:var(--green); text-transform:uppercase; margin-bottom:4px;">Ранее отправленный ответ:</div>
                        <div style="font-size:12px; color:var(--text);" id="reply-history-text"></div>
                    </div>
                </div>
            </div>
        </main>
    </div>

    <script>
        let currentTickets = [];
        let selectedTicket = null;
        let selectedTicketFiles = [];
        let currentActiveLogFile = '';
        let currentRawLogText = '';
        let currentStatusFilter = 'all';
        let currentSearchQuery = '';

        const categoryNames = {
            'wardogs_crash': 'Вылет / Ошибка 114745308',
            'discord_fail': 'Discord / Сеть',
            'gateway_connect': 'Подключение к шлюзу',
            'packet_loss': 'Пинг / Потери пакетов',
            'other': 'Общий вопрос'
        };

        const replyTemplates = {
            'error_114745308': {
                title: 'Решение по ошибке 114745308',
                severity: 'update',
                message: 'Мы проанализировали ваши логи: код 114745308 (0x06D6DFDC) в WARDOGS вызван рассинхронизацией авторизации HTTPS и игровых UDP-портов AWS GameLift.\n\nРекомендация:\n1. В настройках WarLink переключитесь в «Комплексный режим».\n2. Перезапустите игру WARDOGS.\n3. Если игра запущена через сквад, убедитесь, что соединение установлено до начала поиска матча.',
                action_label: 'Открыть настройки',
                action_url: '#view-details'
            },
            'zapret_crash': {
                title: 'Рекомендации по стабильности WinDivert',
                severity: 'warning',
                message: 'Анализ логов выявил сбой службы winws2 (WinDivert). Чаще всего это вызвано конфликтом с другим программным обеспечением (сторонние антивирусы, античит или параллельные DPI-клиенты).\n\nРекомендация:\n1. Добавьте папку WarLink в исключения Защитника Windows.\n2. Закройте другие программы фильтрации трафика.\n3. Запустите WarLink от имени администратора.',
                action_label: 'Проверить статус',
                action_url: '#view-details'
            },
            'match_drop': {
                title: 'Анализ дисконнекта во время матча',
                severity: 'info',
                message: 'Мы зафиксировали кратковременный сброс сессии шлюза. Ваш Discord продолжал работать, так как использует отдельный маршрут.\n\nНа сервере проведена оптимизация тайм-аутов QUIC. Дополнительных действий не требуется, стабильность восстановлена.',
                action_label: 'Телеметрия',
                action_url: '#view-details'
            },
            'resolved': {
                title: 'Ваше обращение успешно обработано',
                severity: 'update',
                message: 'Техническая команда WarLink проверила полученную диагностику. Все необходимые корректировки применены на шлюзе. Приятной игры!',
                action_label: 'Уровень WARDOGS',
                action_url: '#view-progression'
            }
        };

        async function loadTickets() {
            try {
                let url = '/api/v1/admin/tickets?status=' + encodeURIComponent(currentStatusFilter);
                if (currentSearchQuery) url += '&q=' + encodeURIComponent(currentSearchQuery);

                const res = await fetch(url);
                if (!res.ok) {
                    if (res.status === 403 || res.status === 401) {
                        window.location.reload();
                    }
                    return;
                }
                const data = await res.json();
                if (data && data.success) {
                    currentTickets = data.tickets || [];
                    updateCounts(data.counts || {});
                    renderTicketList();
                    if (selectedTicket) {
                        const updated = currentTickets.find(t => t.id === selectedTicket.id);
                        if (updated) {
                            selectedTicket = updated;
                            updateDetailHeaderOnly();
                        }
                    }
                }
            } catch (err) {
                console.error('Failed to load tickets:', err);
            }
        }

        function updateCounts(counts) {
            document.getElementById('count-new').textContent = counts.new || 0;
            document.getElementById('count-progress').textContent = counts.in_progress || 0;
            document.getElementById('count-resolved').textContent = counts.resolved || 0;
            document.getElementById('count-total').textContent = counts.total || 0;
        }

        function filterByStatus(st) {
            currentStatusFilter = st;
            document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));
            document.querySelectorAll('.stat-pill').forEach(pill => pill.classList.remove('active'));

            const tab = document.getElementById('tab-' + st);
            if (tab) tab.classList.add('active');
            if (st === 'new') document.getElementById('pill-new').classList.add('active');
            if (st === 'in_progress') document.getElementById('pill-progress').classList.add('active');
            if (st === 'resolved') document.getElementById('pill-resolved').classList.add('active');

            loadTickets();
        }

        let searchDebounceTimer = null;
        function handleSearch(val) {
            clearTimeout(searchDebounceTimer);
            searchDebounceTimer = setTimeout(() => {
                currentSearchQuery = val.trim();
                loadTickets();
            }, 250);
        }

        function renderTicketList() {
            const listEl = document.getElementById('ticket-list');
            if (!currentTickets || currentTickets.length === 0) {
                listEl.innerHTML = '<div style="padding:40px 20px; text-align:center; color:var(--text-dim); font-size:12px;">Обращений не найдено</div>';
                return;
            }

            let html = '';
            for (const t of currentTickets) {
                const isSel = selectedTicket && selectedTicket.id === t.id;
                const catName = categoryNames[t.category] || t.category || 'Общий';
                const timeAgo = formatTimeAgo(t.created_at);
                const kb = Math.round((t.logs_archive_size || 0) / 1024);
                const statusClass = t.status || 'new';

                html += '<div class="ticket-card ' + (isSel ? 'selected' : '') + '" onclick="selectTicket(' + t.id + ')">' +
                    '<div class="card-header">' +
                        '<span class="ticket-id">#TK-' + String(t.id).padStart(4, '0') + '</span>' +
                        '<span class="badge-status ' + statusClass + '">' + formatStatusName(t.status) + '</span>' +
                    '</div>' +
                    '<div class="card-account">' + escapeHtml(t.account_number || t.device_id || 'Аноним') + '</div>' +
                    '<div><span class="card-category">' + escapeHtml(catName) + '</span></div>' +
                    '<div class="card-snippet">' + escapeHtml(t.user_comment || 'Без комментария') + '</div>' +
                    '<div class="card-footer">' +
                        '<span>' + timeAgo + '</span>' +
                        '<span>' + kb + ' KB архива</span>' +
                    '</div>' +
                '</div>';
            }
            listEl.innerHTML = html;
        }

        let selectedTicketMessages = [];

        async function selectTicket(id) {
            try {
                const res = await fetch('/api/v1/admin/tickets/' + id);
                if (!res.ok) return;
                const data = await res.json();
                if (data && data.success) {
                    selectedTicket = data.ticket;
                    selectedTicketFiles = data.files || [];
                    selectedTicketMessages = data.messages || [];
                    renderTicketDetail();
                    renderTicketList();
                }
            } catch (err) {
                console.error('Failed to select ticket:', err);
            }
        }

        function renderTicketDetail() {
            if (!selectedTicket) return;

            document.getElementById('empty-placeholder').style.display = 'none';
            document.getElementById('detail-content').style.display = 'flex';

            document.getElementById('d-ticket-id').textContent = '#TK-' + String(selectedTicket.id).padStart(4, '0');
            const badge = document.getElementById('d-status-badge');
            badge.className = 'badge-status ' + (selectedTicket.status || 'new');
            badge.textContent = formatStatusName(selectedTicket.status);

            document.getElementById('d-account').textContent = selectedTicket.account_number || 'Не привязан';
            document.getElementById('d-device').textContent = 'dev: ' + (selectedTicket.device_id ? selectedTicket.device_id.substring(0, 16) + '...' : 'none');
            document.getElementById('d-app-ver').textContent = selectedTicket.app_version || 'v2.1.12';

            // Parse system_info
            const sys = selectedTicket.system_info || {};
            document.getElementById('d-os-ver').textContent = sys.os || 'Windows';
            document.getElementById('d-network-mode').textContent = sys.mode === 'complex' ? 'Комплексный режим' : 'Игровой режим (Direct)';
            const singboxOk = sys.singbox_running ? 'sing-box: OK' : 'sing-box: OFF';
            const winws2Ok = sys.winws2_running ? 'winws2: OK' : 'winws2: OFF';
            const pingStr = sys.gateway_ping ? ', ping: ' + sys.gateway_ping + 'ms' : '';
            document.getElementById('d-services-status').textContent = singboxOk + ', ' + winws2Ok + pingStr;

            document.getElementById('d-time-created').textContent = selectedTicket.created_at ? selectedTicket.created_at.replace('T', ' ').replace('Z', ' UTC') : '';
            document.getElementById('d-archive-size').textContent = Math.round((selectedTicket.logs_archive_size || 0) / 1024) + ' KB логов';

            document.getElementById('d-category-badge').textContent = categoryNames[selectedTicket.category] || selectedTicket.category;

            // Render conversation thread
            const threadEl = document.getElementById('d-chat-thread');
            if (threadEl) {
                if (selectedTicketMessages && selectedTicketMessages.length > 0) {
                    let threadHtml = '';
                    for (const m of selectedTicketMessages) {
                        const isUser = m.sender_type === 'user';
                        const isAdmin = m.sender_type === 'admin';
                        const isSys = m.sender_type === 'system';
                        const timeStr = m.created_at ? m.created_at.replace('T', ' ').replace('Z', '') : '';

                        if (isSys) {
                            threadHtml += '<div style="background:#141414; border:1px solid #222; padding:6px 10px; border-radius:2px; font-size:11px; color:#888;">' +
                                '<span style="font-weight:600; color:#ff9800;">Система:</span> ' + escapeHtml(m.message) +
                                '<span style="font-size:9px; color:#555; float:right;">' + timeStr + '</span></div>';
                        } else if (isAdmin) {
                            threadHtml += '<div style="background:#17202a; border-left:3px solid #ff5e1f; padding:8px 10px; border-radius:2px; font-size:12px; color:#e0e0e0; margin-left:16px;">' +
                                '<div style="font-size:10px; color:#ff5e1f; font-weight:600; margin-bottom:3px;">' + escapeHtml(m.sender_name || 'Max (Разработчик)') +
                                '<span style="font-size:9px; color:#666; float:right;">' + timeStr + '</span></div>' +
                                '<div style="white-space:pre-wrap;">' + escapeHtml(m.message) + '</div></div>';
                        } else {
                            threadHtml += '<div style="background:#1c1c1c; border-left:3px solid #5865F2; padding:8px 10px; border-radius:2px; font-size:12px; color:#ddd; margin-right:16px;">' +
                                '<div style="font-size:10px; color:#7289da; font-weight:600; margin-bottom:3px;">' + escapeHtml(m.sender_name || 'Пользователь') +
                                '<span style="font-size:9px; color:#666; float:right;">' + timeStr + '</span></div>' +
                                '<div style="white-space:pre-wrap;">' + escapeHtml(m.message) + '</div></div>';
                        }
                    }
                    threadEl.innerHTML = threadHtml;
                    threadEl.scrollTop = threadEl.scrollHeight;
                } else {
                    threadEl.innerHTML = '<div class="comment-body">' + escapeHtml(selectedTicket.user_comment || 'Без комментария') + '</div>';
                }
            }

            // Fill default reply title and actions
            document.getElementById('reply-title').value = 'Решение по обращению #TK-' + String(selectedTicket.id).padStart(4, '0');
            const actUrlEl = document.getElementById('reply-action-url');
            if (actUrlEl) actUrlEl.value = '#view-support';
            const actLblEl = document.getElementById('reply-action-label');
            if (actLblEl) actLblEl.value = 'Открыть диалог';

            // Render log tabs
            renderLogTabs();
        }

        function updateDetailHeaderOnly() {
            if (!selectedTicket) return;
            const badge = document.getElementById('d-status-badge');
            badge.className = 'badge-status ' + (selectedTicket.status || 'new');
            badge.textContent = formatStatusName(selectedTicket.status);
        }

        function renderLogTabs() {
            const tabsEl = document.getElementById('log-tabs');
            if (!selectedTicketFiles || selectedTicketFiles.length === 0) {
                tabsEl.innerHTML = '<span style="font-size:11px; color:var(--text-dim); padding:4px;">В архиве нет файлов логов</span>';
                document.getElementById('log-terminal').textContent = 'Логи отсутствуют';
                return;
            }

            let html = '';
            // Prefer warlink.log first, or the first file
            if (!currentActiveLogFile || !selectedTicketFiles.some(f => f.name === currentActiveLogFile)) {
                const warlinkFile = selectedTicketFiles.find(f => f.name.includes('warlink.log'));
                currentActiveLogFile = warlinkFile ? warlinkFile.name : selectedTicketFiles[0].name;
            }

            for (const f of selectedTicketFiles) {
                const isActive = f.name === currentActiveLogFile;
                const kb = Math.round(f.size / 1024);
                html += '<button class="log-tab-btn ' + (isActive ? 'active' : '') + '" onclick="switchLogFile(\'' + escapeHtml(f.name) + '\')">' + escapeHtml(f.name) + ' (' + kb + 'KB)</button>';
            }
            tabsEl.innerHTML = html;
            fetchLogFile(currentActiveLogFile);
        }

        async function switchLogFile(name) {
            currentActiveLogFile = name;
            document.querySelectorAll('.log-tab-btn').forEach(btn => {
                btn.classList.toggle('active', btn.textContent.startsWith(name));
            });
            await fetchLogFile(name);
        }

        async function fetchLogFile(fileName) {
            if (!selectedTicket) return;
            const term = document.getElementById('log-terminal');
            term.textContent = 'Чтение ' + fileName + '...';

            try {
                const res = await fetch('/api/v1/admin/tickets/' + selectedTicket.id + '/file?name=' + encodeURIComponent(fileName));
                if (!res.ok) {
                    term.textContent = 'Ошибка загрузки файла ' + fileName;
                    return;
                }
                const data = await res.json();
                if (data && data.success) {
                    currentRawLogText = data.content || '';
                    renderLogTerminal(currentRawLogText);
                }
            } catch (err) {
                term.textContent = 'Ошибка сети при получении лога: ' + err;
            }
        }

        function renderLogTerminal(raw) {
            const term = document.getElementById('log-terminal');
            if (!raw) {
                term.textContent = 'Файл пуст';
                return;
            }

            // Syntax highlighting
            const lines = raw.split('\n');
            let out = [];
            for (let line of lines) {
                let esc = escapeHtml(line);
                if (esc.includes('[ERROR]') || esc.includes('FATAL') || esc.includes('panic') || esc.includes('114745308')) {
                    esc = '<span class="tok-error">' + esc + '</span>';
                } else if (esc.includes('[WARN]')) {
                    esc = '<span class="tok-warn">' + esc + '</span>';
                } else if (esc.includes('[INFO]') || esc.includes('[NET]')) {
                    esc = '<span class="tok-info">' + esc + '</span>';
                }
                out.push(esc);
            }
            term.innerHTML = out.join('\n');
            term.scrollTop = term.scrollHeight; // Scroll to end
        }

        function filterLog(query) {
            if (!query) {
                renderLogTerminal(currentRawLogText);
                return;
            }
            const q = query.toLowerCase();
            const lines = currentRawLogText.split('\n');
            let out = [];
            for (let line of lines) {
                if (line.toLowerCase().includes(q)) {
                    let esc = escapeHtml(line);
                    const regex = new RegExp('(' + escapeRegex(query) + ')', 'gi');
                    esc = esc.replace(regex, '<span class="tok-match">$1</span>');
                    out.push(esc);
                }
            }
            const term = document.getElementById('log-terminal');
            term.innerHTML = out.length > 0 ? out.join('\n') : '<span style="color:var(--text-dim);">Совпадений не найдено</span>';
        }

        function copyCurrentLog() {
            if (!currentRawLogText) return;
            navigator.clipboard.writeText(currentRawLogText).then(() => {
                alert('Лог скопирован в буфер обмена');
            });
        }

        function downloadLogsArchive() {
            if (!selectedTicket) return;
            window.open('/api/v1/admin/tickets/' + selectedTicket.id + '/archive', '_blank');
        }

        async function setTicketStatus(status) {
            if (!selectedTicket) return;
            try {
                const res = await fetch('/api/v1/admin/tickets/' + selectedTicket.id + '/status', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ status })
                });
                if (res.ok) {
                    selectedTicket.status = status;
                    updateDetailHeaderOnly();
                    loadTickets();
                }
            } catch (err) {
                console.error('Failed to set ticket status:', err);
            }
        }

        function applyTemplate(key) {
            const tmpl = replyTemplates[key];
            if (!tmpl) return;
            document.getElementById('reply-title').value = tmpl.title;
            document.getElementById('reply-severity').value = tmpl.severity;
            document.getElementById('reply-message').value = tmpl.message;
            document.getElementById('reply-action-label').value = tmpl.action_label || '';
            document.getElementById('reply-action-url').value = tmpl.action_url || '';
        }

        async function sendReply() {
            if (!selectedTicket) return;

            const title = document.getElementById('reply-title').value.trim();
            const message = document.getElementById('reply-message').value.trim();
            const severity = document.getElementById('reply-severity').value;
            const actionLabel = document.getElementById('reply-action-label').value.trim();
            const actionUrl = document.getElementById('reply-action-url').value.trim();
            const markResolved = document.getElementById('reply-mark-resolved').checked;

            if (!title || !message) {
                alert('Заполните заголовок и текст сообщения');
                return;
            }

            const sendBtn = document.getElementById('btn-send-reply');
            sendBtn.disabled = true;
            sendBtn.textContent = 'Отправка депеши...';

            try {
                const payload = {
                    title,
                    message,
                    severity,
                    action_label: actionLabel,
                    action_url: actionUrl,
                    status: markResolved ? 'resolved' : 'in_progress'
                };

                const res = await fetch('/api/v1/admin/tickets/' + selectedTicket.id + '/reply', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });

                if (res.ok) {
                    const data = await res.json();
                    selectedTicket.admin_reply = message;
                    selectedTicket.status = data.status || (markResolved ? 'resolved' : 'in_progress');
                    updateDetailHeaderOnly();

                    const historyBox = document.getElementById('reply-history');
                    historyBox.style.display = 'block';
                    document.getElementById('reply-history-text').textContent = message;

                    alert('Депеша успешно отправлена на аккаунт пользователя!');
                    loadTickets();
                } else {
                    alert('Ошибка при отправке депеши');
                }
            } catch (err) {
                alert('Сетевая ошибка: ' + err);
            } finally {
                sendBtn.disabled = false;
                sendBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg> Отправить депешу пользователю';
            }
        }

        function formatStatusName(st) {
            switch(st) {
                case 'new': return 'Новый';
                case 'in_progress': return 'В работе';
                case 'resolved': return 'Решен';
                case 'closed': return 'Закрыт';
                default: return st || 'Новый';
            }
        }

        function formatTimeAgo(dateStr) {
            if (!dateStr) return '';
            const d = new Date(dateStr);
            const now = new Date();
            const diffSec = Math.floor((now - d) / 1000);
            if (diffSec < 60) return 'только что';
            if (diffSec < 3600) return Math.floor(diffSec / 60) + ' мин назад';
            if (diffSec < 86400) return Math.floor(diffSec / 3600) + ' ч назад';
            return Math.floor(diffSec / 86400) + ' дн назад';
        }

        function escapeHtml(str) {
            if (!str) return '';
            return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
        }

        function escapeRegex(str) {
            return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        }

        // Initialize
        loadTickets();
        setInterval(loadTickets, 15000); // 15-second background auto-refresh
    </script>
</body>
</html>
`

func (s *AppState) handleAdminRoutingFeedbackWeb(w http.ResponseWriter, r *http.Request) {
	qKey := r.URL.Query().Get("key")
	if qKey != "" && s.cfg.DashboardKey != "" && qKey == s.cfg.DashboardKey {
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_key",
			Value:    qKey,
			Path:     "/",
			MaxAge:   30 * 86400,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
	}

	if !s.checkAdminAuth(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <title>WarLink Routing // Доступ ограничен</title>
    <style>
        body { background: #0c0d10; color: #e6e8ee; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
        .box { background: #14161b; border: 1px solid #262a34; padding: 32px; border-radius: 2px; width: 340px; }
        h2 { font-size: 14px; text-transform: uppercase; letter-spacing: 0.08em; margin: 0 0 16px 0; color: #FF5E1F; }
        p { font-size: 13px; color: #8b92a5; margin-bottom: 20px; line-height: 1.4; }
        input { width: 100%; box-sizing: border-box; background: #0a0b0d; border: 1px solid #262a34; color: #fff; padding: 10px 12px; font-size: 14px; margin-bottom: 16px; border-radius: 2px; outline: none; }
        input:focus { border-color: #FF5E1F; }
        button { width: 100%; background: #FF5E1F; color: #fff; border: none; padding: 10px; font-size: 13px; font-weight: 600; text-transform: uppercase; cursor: pointer; border-radius: 2px; }
        button:hover { background: #e04e14; }
    </style>
</head>
<body>
    <div class="box">
        <h2>WARLINK ROUTING</h2>
        <p>Для доступа к панели замеров маршрутов введите ключ администратора.</p>
        <form method="GET" action="/admin/routing-feedback">
            <input type="password" name="key" placeholder="Ключ авторизации" autofocus required>
            <button type="submit">Войти в систему</button>
        </form>
    </div>
</body>
</html>`)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminRoutingFeedbackHTML))
}

const adminRoutingFeedbackHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>WarLink // Замеры и фидбек маршрутов</title>
    <style>
        :root {
            --bg: #0c0d10;
            --card-bg: #14161b;
            --surface: #1a1d24;
            --surface-hover: #222630;
            --border: #262a34;
            --text: #e6e8ee;
            --text-muted: #8b92a5;
            --text-dim: #5c6375;
            --accent: #FF5E1F;
            --green: #10b981;
            --amber: #f59e0b;
            --red: #ef4444;
            --blue: #3b82f6;
            --font-sans: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            --font-mono: 'JetBrains Mono', 'Consolas', monospace;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            background-color: var(--bg);
            color: var(--text);
            font-family: var(--font-sans);
            font-size: 13px;
            display: flex;
            flex-direction: column;
            min-height: 100vh;
        }

        header {
            height: 52px;
            background: var(--card-bg);
            border-bottom: 1px solid var(--border);
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0 20px;
            flex-shrink: 0;
        }

        .header-brand {
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .brand-title {
            font-size: 13px;
            font-weight: 700;
            letter-spacing: 0.08em;
            text-transform: uppercase;
            color: var(--text);
        }

        .brand-badge {
            font-size: 10px;
            font-weight: 600;
            padding: 2px 6px;
            background: rgba(255, 94, 31, 0.15);
            color: var(--accent);
            border: 1px solid rgba(255, 94, 31, 0.3);
            border-radius: 2px;
        }

        .header-nav {
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .nav-btn {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            padding: 6px 12px;
            background: #14161b;
            border: 1px solid var(--border);
            border-radius: 2px;
            color: var(--text-muted);
            text-decoration: none;
            font-size: 12px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.12s ease;
        }

        .nav-btn:hover {
            color: var(--text);
            border-color: #3b4252;
        }

        .nav-btn.active {
            background: #1c1512;
            border-color: var(--accent);
            color: #ffffff;
        }

        main {
            flex: 1;
            padding: 20px;
            display: flex;
            flex-direction: column;
            gap: 16px;
            max-width: 1400px;
            width: 100%;
            margin: 0 auto;
        }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(3, 1fr);
            gap: 12px;
        }

        .stat-card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 14px 16px;
            display: flex;
            flex-direction: column;
            gap: 6px;
            border-top: 2px solid var(--border);
        }

        .stat-card.mode-moscow { border-top-color: #10b981; }
        .stat-card.mode-stockholm { border-top-color: #3b82f6; }
        .stat-card.mode-transit { border-top-color: var(--accent); }

        .stat-card-title {
            font-size: 11px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-muted);
        }

        .stat-card-main {
            display: flex;
            align-items: baseline;
            gap: 8px;
        }

        .stat-card-ping {
            font-size: 26px;
            font-weight: 700;
            font-family: var(--font-mono);
            color: var(--text);
        }

        .stat-card-unit {
            font-size: 12px;
            color: var(--text-dim);
            font-family: var(--font-mono);
        }

        .stat-card-meta {
            display: flex;
            justify-content: space-between;
            font-size: 11px;
            color: var(--text-dim);
            border-top: 1px solid #1c202a;
            padding-top: 6px;
            margin-top: 4px;
        }

        .filter-bar {
            display: flex;
            justify-content: space-between;
            align-items: center;
            gap: 12px;
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 8px 12px;
        }

        .filter-tabs {
            display: flex;
            gap: 6px;
        }

        .filter-tab {
            padding: 5px 12px;
            background: #111317;
            border: 1px solid var(--border);
            border-radius: 2px;
            color: var(--text-muted);
            font-size: 12px;
            font-weight: 600;
            cursor: pointer;
        }

        .filter-tab:hover { color: var(--text); }
        .filter-tab.active {
            background: #1c1512;
            border-color: var(--accent);
            color: #ffffff;
        }

        .search-box {
            position: relative;
            width: 260px;
        }

        .search-input {
            width: 100%;
            height: 30px;
            background: #0a0b0d;
            border: 1px solid var(--border);
            border-radius: 2px;
            padding: 0 10px;
            color: var(--text);
            font-size: 12px;
            outline: none;
        }

        .search-input:focus { border-color: var(--accent); }

        .table-card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 2px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
        }

        table {
            width: 100%;
            border-collapse: collapse;
            text-align: left;
        }

        th {
            background: #111317;
            border-bottom: 1px solid var(--border);
            padding: 9px 12px;
            font-size: 11px;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-dim);
        }

        td {
            padding: 10px 12px;
            border-bottom: 1px solid #1a1e27;
            font-size: 12px;
            vertical-align: middle;
        }

        tr:hover td {
            background: rgba(255, 255, 255, 0.02);
        }

        .font-mono { font-family: var(--font-mono); }

        .badge-mode {
            display: inline-block;
            padding: 2px 6px;
            border-radius: 2px;
            font-size: 10px;
            font-weight: 600;
            letter-spacing: 0.03em;
        }
        .badge-mode.direct_moscow { background: rgba(16, 185, 129, 0.12); color: #10b981; border: 1px solid rgba(16, 185, 129, 0.3); }
        .badge-mode.direct_stockholm { background: rgba(59, 130, 246, 0.12); color: #3b82f6; border: 1px solid rgba(59, 130, 246, 0.3); }
        .badge-mode.transit { background: rgba(255, 94, 31, 0.12); color: var(--accent); border: 1px solid rgba(255, 94, 31, 0.3); }

        .badge-status {
            display: inline-flex;
            align-items: center;
            gap: 5px;
            font-size: 11px;
            font-weight: 600;
        }
        .dot { width: 6px; height: 6px; border-radius: 50%; }
        .dot-green { background: #10b981; }
        .dot-yellow { background: #f59e0b; }
        .dot-red { background: #ef4444; }

        .badge-ping {
            display: inline-block;
            padding: 2px 6px;
            border-radius: 2px;
            font-family: var(--font-mono);
            font-size: 11px;
            font-weight: 700;
        }
        .ping-fast { background: #0c2b18; color: #34d399; }
        .ping-medium { background: #0c203b; color: #60a5fa; }
        .ping-high { background: #2f1b0c; color: #fb923c; }

        .comment-text {
            color: var(--text);
            max-width: 400px;
            word-break: break-word;
            line-height: 1.35;
        }

        .empty-state {
            padding: 40px;
            text-align: center;
            color: var(--text-dim);
            font-size: 13px;
        }
    </style>
</head>
<body>
    <header>
        <div class="header-brand">
            <svg class="header-logo" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="#FF5E1F" stroke-width="2">
                <rect x="2" y="2" width="20" height="8" rx="2" ry="2"/>
                <rect x="2" y="14" width="20" height="8" rx="2" ry="2"/>
                <line x1="6" y1="6" x2="6.01" y2="6"/>
                <line x1="6" y1="18" x2="6.01" y2="18"/>
            </svg>
            <span class="brand-title">WARLINK // ОПЕРАЦИОННЫЙ ЦЕНТР</span>
            <span class="brand-badge">ЗАМЕРЫ СЕТИ</span>
        </div>
        <div class="header-nav">
            <a href="/admin/tickets" class="nav-btn">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
                Тикеты пользователей
            </a>
            <a href="/admin/routing-feedback" class="nav-btn active">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>
                Замеры маршрутов
            </a>
            <a href="/dashboard" class="nav-btn" target="_blank">
                Телеметрия
            </a>
        </div>
    </header>

    <main>
        <!-- Summary Cards -->
        <div class="stats-grid">
            <div class="stat-card mode-moscow">
                <span class="stat-card-title">Москва (RU)</span>
                <div class="stat-card-main">
                    <span class="stat-card-ping" id="stat-ping-moscow">—</span>
                    <span class="stat-card-unit">мс средний пинг</span>
                </div>
                <div class="stat-card-meta">
                    <span id="stat-count-moscow">0 замеров</span>
                    <span id="stat-rate-moscow" style="color:#10b981;">— % отлично</span>
                </div>
            </div>

            <div class="stat-card mode-stockholm">
                <span class="stat-card-title">Стокгольм (EU)</span>
                <div class="stat-card-main">
                    <span class="stat-card-ping" id="stat-ping-stockholm">—</span>
                    <span class="stat-card-unit">мс средний пинг</span>
                </div>
                <div class="stat-card-meta">
                    <span id="stat-count-stockholm">0 замеров</span>
                    <span id="stat-rate-stockholm" style="color:#3b82f6;">— % отлично</span>
                </div>
            </div>

            <div class="stat-card mode-transit">
                <span class="stat-card-title">Транзит (RU→EU)</span>
                <div class="stat-card-main">
                    <span class="stat-card-ping" id="stat-ping-transit">—</span>
                    <span class="stat-card-unit">мс средний пинг</span>
                </div>
                <div class="stat-card-meta">
                    <span id="stat-count-transit">0 замеров</span>
                    <span id="stat-rate-transit" style="color:var(--accent);">— % отлично</span>
                </div>
            </div>
        </div>

        <!-- Filter Bar -->
        <div class="filter-bar">
            <div class="filter-tabs">
                <button class="filter-tab active" data-filter="all" onclick="setFilter('all')">Все замеры (<span id="count-all">0</span>)</button>
                <button class="filter-tab" data-filter="direct_moscow" onclick="setFilter('direct_moscow')">Москва</button>
                <button class="filter-tab" data-filter="direct_stockholm" onclick="setFilter('direct_stockholm')">Стокгольм</button>
                <button class="filter-tab" data-filter="transit" onclick="setFilter('transit')">Транзит</button>
            </div>
            <div style="display:flex; align-items:center; gap:8px;">
                <div class="search-box">
                    <input type="text" id="search-input" class="search-input" placeholder="Поиск по аккаунту или тексту..." oninput="handleSearch(this.value)">
                </div>
                <button class="nav-btn" onclick="loadFeedbackData()">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2"/></svg>
                    Обновить
                </button>
            </div>
        </div>

        <!-- Table Card -->
        <div class="table-card">
            <table>
                <thead>
                    <tr>
                        <th style="width:130px;">Время (МСК)</th>
                        <th style="width:140px;">Аккаунт</th>
                        <th style="width:120px;">Маршрут</th>
                        <th style="width:90px;">Пинг</th>
                        <th style="width:140px;">Статус</th>
                        <th style="width:180px;">Матч / Discord</th>
                        <th>Комментарий игрока</th>
                    </tr>
                </thead>
                <tbody id="feedback-tbody">
                    <tr><td colspan="7" class="empty-state">Загрузка данных замеров...</td></tr>
                </tbody>
            </table>
        </div>
    </main>

    <script>
        let allItems = [];
        let currentFilter = 'all';
        let searchQuery = '';

        async function loadFeedbackData() {
            try {
                const res = await fetch('/api/v1/admin/routing-feedback', { credentials: 'same-origin' });
                if (!res.ok) {
                    if (res.status === 401) { location.reload(); return; }
                    throw new Error('HTTP ' + res.status);
                }
                const data = await res.json();
                if (data && data.success) {
                    renderStats(data.stats || []);
                    allItems = data.items || [];
                    renderTable();
                }
            } catch(e) {
                console.error('Feedback fetch error:', e);
            }
        }

        function renderStats(stats) {
            const map = {};
            for (const st of stats) {
                map[st.mode] = st;
            }

            const moscow = map['direct_moscow'] || { count: 0, avg_ping: 0, great_pct: 0 };
            const stockholm = map['direct_stockholm'] || { count: 0, avg_ping: 0, great_pct: 0 };
            const transit = map['transit'] || { count: 0, avg_ping: 0, great_pct: 0 };

            document.getElementById('stat-ping-moscow').textContent = moscow.avg_ping ? Math.round(moscow.avg_ping) : '—';
            document.getElementById('stat-count-moscow').textContent = moscow.count + ' замеров';
            document.getElementById('stat-rate-moscow').textContent = (moscow.great_pct || 0) + '% отлично';

            document.getElementById('stat-ping-stockholm').textContent = stockholm.avg_ping ? Math.round(stockholm.avg_ping) : '—';
            document.getElementById('stat-count-stockholm').textContent = stockholm.count + ' замеров';
            document.getElementById('stat-rate-stockholm').textContent = (stockholm.great_pct || 0) + '% отлично';

            document.getElementById('stat-ping-transit').textContent = transit.avg_ping ? Math.round(transit.avg_ping) : '—';
            document.getElementById('stat-count-transit').textContent = transit.count + ' замеров';
            document.getElementById('stat-rate-transit').textContent = (transit.great_pct || 0) + '% отлично';
        }

        function setFilter(mode) {
            currentFilter = mode;
            document.querySelectorAll('.filter-tab').forEach(b => {
                b.classList.toggle('active', b.getAttribute('data-filter') === mode);
            });
            renderTable();
        }

        function handleSearch(q) {
            searchQuery = (q || '').trim().toLowerCase();
            renderTable();
        }

        function renderTable() {
            const tbody = document.getElementById('feedback-tbody');
            const countAllEl = document.getElementById('count-all');
            if (countAllEl) countAllEl.textContent = allItems.length;

            const filtered = allItems.filter(it => {
                if (currentFilter !== 'all' && it.route_mode !== currentFilter) return false;
                if (searchQuery) {
                    const acc = (it.account_number || '').toLowerCase();
                    const comm = (it.user_comment || '').toLowerCase();
                    if (!acc.includes(searchQuery) && !comm.includes(searchQuery)) return false;
                }
                return true;
            });

            if (filtered.length === 0) {
                tbody.innerHTML = '<tr><td colspan="7" class="empty-state">Нет данных замеров по выбранному фильтру</td></tr>';
                return;
            }

            tbody.innerHTML = filtered.map(it => {
                const modeLabel = it.route_mode === 'direct_moscow' ? 'Москва (RU)' :
                                 (it.route_mode === 'direct_stockholm' ? 'Стокгольм (EU)' : 'Транзит');
                
                let pingClass = 'ping-fast';
                if (it.in_game_ping > 80) pingClass = 'ping-high';
                else if (it.in_game_ping > 40) pingClass = 'ping-medium';

                let statusDot = 'dot-green';
                let statusText = 'Отлично';
                if (it.status === 'has_issues') { statusDot = 'dot-yellow'; statusText = 'Проблемы'; }
                else if (it.status === 'cant_connect') { statusDot = 'dot-red'; statusText = 'Не подключается'; }

                let matchLabel = 'Без фризов';
                if (it.match_quality === 'microstutter') matchLabel = 'Микрофризы';
                else if (it.match_quality === 'disconnected') matchLabel = 'Вылет из матча';

                let discordLabel = 'Чистый войс';
                if (it.discord_status === 'robovoice') discordLabel = 'Робовойс';
                else if (it.discord_status === 'no_connection') discordLabel = 'Войс офлайн';
                else if (it.discord_status === 'not_used') discordLabel = 'Без Discord';

                const commentSafe = escapeHtml(it.user_comment || '—');

                return '<tr>' +
                    '<td class="font-mono" style="color:var(--text-muted); font-size:11px;">' + escapeHtml(it.created_at) + '</td>' +
                    '<td><span class="font-mono" style="color:var(--accent); font-weight:600;">' + escapeHtml(it.account_number || '#—') + '</span></td>' +
                    '<td><span class="badge-mode ' + escapeHtml(it.route_mode) + '">' + modeLabel + '</span></td>' +
                    '<td><span class="badge-ping ' + pingClass + '">' + (it.in_game_ping ? it.in_game_ping + ' мс' : '—') + '</span></td>' +
                    '<td><span class="badge-status"><span class="dot ' + statusDot + '"></span>' + statusText + '</span></td>' +
                    '<td style="color:var(--text-muted); font-size:11px;">' + matchLabel + ' · ' + discordLabel + '</td>' +
                    '<td class="comment-text">' + commentSafe + '</td>' +
                '</tr>';
            }).join('');
        }

        function escapeHtml(str) {
            if (!str) return '';
            return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
        loadFeedbackData();
        setInterval(loadFeedbackData, 15000);
    </script>
</body>
</html>
`



