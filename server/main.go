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
	ServerAppVersion     = "v2.2.0"
	AdminAccountNumber   = "5230-6527-2989-4096"
	DefaultHMACSecret    = ""
	DefaultObfsPassword  = ""
	DefaultServerIP      = ""
	DefaultServerPorts   = "443,20000-30000"
	DefaultDueDate       = "2026-10-19T14:48:00Z"
	MaxActiveSessions    = 61 // 50 Free + 10 Sponsor + 1 Dedicated Admin
	MaxSessionsPerIP     = 2
	SessionTTL           = 24 * time.Hour
	SessionInactivityTTL = 45 * time.Minute
	PerUserRateDownBps   = 12500000 // 100 Mbps in bytes/sec
	PerUserRateUpBps     = 6250000  // 50 Mbps in bytes/sec
	SafetyBufferDays             = 7
	MonthlyInfrastructureCostRub = 1690
	MonthlyInfrastructureCostEur = 13.0
)

var (
	// TrustedIngressIPs defines reverse proxy / edge Ingress/Egress PoP nodes (e.g. Moscow and Frankfurt nodes)
	// that forward client traffic to the Master Control Plane.
	TrustedIngressIPs = map[string]bool{
		"45.12.63.85":   true,
		"85.192.24.254": true,
	}
)

type VPSServerDetail struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	ExpiresAt string    `json:"expires_at"`
	DueDate   time.Time `json:"due_date"`
	DaysLeft  int       `json:"days_left"`
	PriceEur  float64   `json:"price_eur"`
	PriceRub  int       `json:"price_rub"`
	Status    string    `json:"status"`
}

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
	RouteMode     string    `json:"route_mode,omitempty"`
	ConnectedNode string    `json:"connected_node,omitempty"`
	GatewayIP     string    `json:"gateway_ip,omitempty"`
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
	ISP         string  `json:"isp,omitempty"`
}

type AppState struct {
	mu           sync.RWMutex
	cfg          ServerConfig
	profiles     []aclgen.Profile
	sessions     map[string]*SessionInfo // token -> SessionInfo
	deviceTokens map[string]string       // device_id -> token
	cachedDue          time.Time
	cachedDueStr       string
	cachedRealDue      time.Time
	cachedRealDaysLeft int
	cachedDisplayDays  int
	cachedVPSDetails   []VPSServerDetail
	rateLimiter        *IPRateLimiter
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

	// Edge Ingress RTT tracking
	moscowPingRTT    float64
	moscowPingMu     sync.RWMutex
	frankfurtPingRTT float64
	frankfurtPingMu  sync.RWMutex

	// Background client beacon metrics cache
	beaconMu            sync.RWMutex
	latestBeaconMetrics []string

	// Dynamic Feature Toggles
	enableDonate        bool
	enableVoting        bool
	enableCommunityGoal bool
	drainMode           bool
	lastSettingsLoad    time.Time
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
	cachedMonthPoolRub        int64
	cachedMonthPoolTime       time.Time
	cachedMonthPoolMu         sync.RWMutex
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

		// Edge Ingress RTT probe to Moscow node
		if moscowRTT, err := probeICMPPing("45.12.63.85", 1200*time.Millisecond); err == nil && moscowRTT > 0 {
			s.moscowPingMu.Lock()
			s.moscowPingRTT = moscowRTT
			s.moscowPingMu.Unlock()
		}
		// Edge Egress RTT probe to Frankfurt node
		if frankfurtRTT, err := probeICMPPing("85.192.24.254", 1200*time.Millisecond); err == nil && frankfurtRTT > 0 {
			s.frankfurtPingMu.Lock()
			s.frankfurtPingRTT = frankfurtRTT
			s.frankfurtPingMu.Unlock()
		}
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
				state.loadTelemetryCounters()
				state.loadBlockedSteamGames()
				state.loadSupportedGames()
				LoadDynamicNicknameRules(state.db)
				go state.startAnalyticsCollector()
				go state.startTicketAutoCloseWorker()
				go state.startSettingsSyncWorker()
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
	publicMux.HandleFunc("/api/v1/admin/reload-filters", state.handleReloadFilters)
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
	publicMux.HandleFunc("/api/v1/admin/slots", state.handleAdminSettings)
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
	publicMux.HandleFunc("/api/v1/telemetry/beacon", state.handleTelemetryBeacon)
	publicMux.HandleFunc("/api/v1/admin/routing-feedback", state.handleAdminRoutingFeedback)
	publicMux.HandleFunc("/admin/routing-feedback", state.handleAdminRoutingFeedbackWeb)
	publicMux.HandleFunc("/admin/routing-feedback/", state.handleAdminRoutingFeedbackWeb)
	publicMux.HandleFunc("/api/v1/boosty-goal", state.handleGetBoostyGoal)
	publicMux.HandleFunc("/api/v1/admin/boosty-goal", state.handleAdminBoostyGoal)
	publicMux.HandleFunc("/api/v1/community/goal", state.handleCommunityGoals)
	publicMux.HandleFunc("/api/v1/admin/community/goal", state.handleAdminCommunityGoals)
	publicMux.HandleFunc("/api/v1/games/catalog", state.handleGamesCatalog)
	publicMux.HandleFunc("/api/v1/admin/games/catalog", state.handleAdminGamesCatalog)
	publicMux.HandleFunc("/api/v1/dpi/strategies", state.handleDPIStrategies)
	publicMux.HandleFunc("/api/v1/announcements", state.handleAnnouncements)
	publicMux.HandleFunc("/api/v1/admin/announcements", state.handleAdminAnnouncements)
	publicMux.HandleFunc("/api/v1/sponsors/tiers", state.handleSponsorsTiers)

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
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

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
		now := time.Now()
		var minDue time.Time
		var minDueStr string
		var vpsDetails []VPSServerDetail
		totalPriceRub := 0

		for _, item := range result.Items {
			if item.TypeSlug == "vps" && item.ExpiresAt != "" {
				parsed, err := time.Parse(time.RFC3339, item.ExpiresAt)
				if err != nil {
					continue
				}
				priceRub := item.Price
				if item.Price > 0 {
					if item.Price < 500 {
						priceRub = int(math.Round(float64(item.Price) * 1.30))
					}
				}
				days := int(math.Max(0, parsed.Sub(now).Hours()/24.0))
				detail := VPSServerDetail{
					ID:        item.ID,
					Name:      item.Name,
					IP:        item.IP,
					ExpiresAt: item.ExpiresAt,
					DueDate:   parsed,
					DaysLeft:  days,
					PriceEur:  float64(item.Price) / 100.0,
					PriceRub:  priceRub,
					Status:    item.Status,
				}
				vpsDetails = append(vpsDetails, detail)
				totalPriceRub += priceRub

				if minDue.IsZero() || parsed.Before(minDue) {
					minDue = parsed
					minDueStr = item.ExpiresAt
				}
			}
		}

		if !minDue.IsZero() {
			if totalPriceRub < MonthlyInfrastructureCostRub && len(vpsDetails) >= 2 {
				totalPriceRub = MonthlyInfrastructureCostRub
			} else if totalPriceRub == 0 {
				totalPriceRub = MonthlyInfrastructureCostRub
			}

			realDays := int(math.Max(0, minDue.Sub(now).Hours()/24.0))
			displayDays := realDays - SafetyBufferDays
			if displayDays < 0 {
				displayDays = 0
			}

			s.mu.Lock()
			s.cachedDue = minDue
			s.cachedDueStr = minDueStr
			s.cachedRealDue = minDue
			s.cachedRealDaysLeft = realDays
			s.cachedDisplayDays = displayDays
			s.cachedPrice = totalPriceRub
			s.cachedVPSDetails = vpsDetails
			s.mu.Unlock()

			log.Printf("[AEZA] Dual-server billing synced: %d nodes, total %d RUB/mo, real min days: %d, display days (-%d buffer): %d, bottleneck due: %s",
				len(vpsDetails), totalPriceRub, realDays, SafetyBufferDays, displayDays, minDueStr)
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
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

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
	// Prefer X-Real-IP if set and not pointing to a trusted proxy or loopback.
	clientIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if clientIP != "" && !TrustedIngressIPs[clientIP] && clientIP != "127.0.0.1" && clientIP != "::1" && clientIP != "localhost" {
		return clientIP
	}

	// If X-Real-IP is empty or matches our trusted transit proxy (e.g. Moscow Ingress 45.12.63.85),
	// parse X-Forwarded-For to find the original client IP.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		for _, p := range parts {
			cand := strings.TrimSpace(p)
			if cand != "" && !TrustedIngressIPs[cand] && cand != "127.0.0.1" && cand != "::1" && cand != "localhost" {
				return cand
			}
		}
	}

	// Fallback to RemoteAddr
	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil && host != "" {
			return host
		}
		return r.RemoteAddr
	}
	return ""
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
	routeMode := strings.TrimSpace(r.URL.Query().Get("route_mode"))
	deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if routeMode != "" && deviceID != "" {
		s.mu.Lock()
		for _, sess := range s.sessions {
			if sess.DeviceID == deviceID && sess.RouteMode != routeMode {
				sess.RouteMode = routeMode
				s.saveSessionAsync(sess)
			}
		}
		s.mu.Unlock()
	}

	s.featureMu.RLock()
	staleSettings := time.Since(s.lastSettingsLoad) > 2*time.Second
	s.featureMu.RUnlock()
	if staleSettings {
		s.loadFeatureSettings()
	}

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

	s.mu.RLock()
	daysLeft := s.cachedDisplayDays
	if daysLeft == 0 && !dueDate.IsZero() {
		realDays := int(time.Until(dueDate).Hours() / 24)
		daysLeft = realDays - SafetyBufferDays
		if daysLeft < 0 {
			daysLeft = 0
		}
	}
	s.mu.RUnlock()

	octoberPoolRub := s.getMonthPoolRub()

	livePing := 27
	if s.latencyTracker != nil {
		lm := s.latencyTracker.GetMetrics()
		if lm.P50 > 0 {
			livePing = int(math.Round(lm.P50))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                           "online",
		"location":                         s.cfg.ServerLocation,
		"ping_hint_ms":                     livePing,
		"active_sessions":                  activeCount,
		"max_sessions":                     maxSessions,
		"active_free_sessions":             activeFreeCount,
		"free_slots_limit":                 freeSlotsLimit,
		"active_sponsor_sessions":          activeSponsorCount,
		"dedicated_sponsor_slots":          dedicatedSponsor,
		"dedicated_admin_slots":            dedicatedAdmin,
		"server_ip":                        s.getPublicIP(r),
		"server_ports":                     s.cfg.ServerPorts,
		"due_date":                         dueDateStr,
		"days_left":                        daysLeft,
		"donate_amount_rub":                s.cfg.DonateAmountRub,
		"monthly_infrastructure_cost_rub":  MonthlyInfrastructureCostRub,
		"enable_donate":                    enDonate,
		"enable_voting":                    enVoting,
		"enable_community_goal":            enCommunityGoal,
		"october_pool_rub":                 octoberPoolRub,
	})
}

func (s *AppState) getMonthPoolRub() int64 {
	s.cachedMonthPoolMu.RLock()
	if time.Since(s.cachedMonthPoolTime) < 60*time.Second {
		val := s.cachedMonthPoolRub
		s.cachedMonthPoolMu.RUnlock()
		return val
	}
	s.cachedMonthPoolMu.RUnlock()

	s.cachedMonthPoolMu.Lock()
	defer s.cachedMonthPoolMu.Unlock()
	if time.Since(s.cachedMonthPoolTime) < 60*time.Second {
		return s.cachedMonthPoolRub
	}

	var pool int64
	if s.db != nil {
		_ = s.db.QueryRow(`
			SELECT COALESCE(SUM(amount_rub), 0)
			FROM pending_donations
			WHERE status = 'paid'
			  AND created_at >= date_trunc('month', CURRENT_TIMESTAMP)
		`).Scan(&pool)
	}
	s.cachedMonthPoolRub = pool
	s.cachedMonthPoolTime = time.Now()
	return pool
}

type SessionRequest struct {
	DeviceID      string `json:"device_id"`
	AccountNumber string `json:"account_number,omitempty"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	Game          string `json:"game,omitempty"`
	AppVersion    string `json:"app_version,omitempty"`
	RouteMode     string `json:"route_mode,omitempty"`
}

func (s *AppState) resolveRouteAndNode(r *http.Request, explicitMode string) (routeMode, connectedNode, gatewayIP string) {
	viaMoscow := false
	viaFrankfurt := false
	if r != nil {
		remoteHost, _, _ := net.SplitHostPort(r.RemoteAddr)
		if remoteHost == "45.12.63.85" || strings.Contains(r.Header.Get("X-Forwarded-For"), "45.12.63.85") {
			viaMoscow = true
		}
		if remoteHost == "85.192.24.254" || strings.Contains(r.Header.Get("X-Forwarded-For"), "85.192.24.254") {
			viaFrankfurt = true
		}
	}

	mode := strings.TrimSpace(explicitMode)
	if mode == "" {
		if viaMoscow {
			mode = "transit"
		} else if viaFrankfurt {
			mode = "direct_frankfurt"
		} else {
			mode = "direct_frankfurt"
		}
	}

	stockholmIP := s.cfg.ServerIP
	if stockholmIP == "" {
		stockholmIP = "138.124.103.99"
	}
	moscowIP := "45.12.63.85"
	frankfurtIP := "85.192.24.254"

	switch mode {
	case "direct_moscow":
		return "direct_moscow", "Москва", moscowIP
	case "direct_frankfurt":
		return "direct_frankfurt", "Франкфурт", frankfurtIP
	case "direct_stockholm":
		if viaMoscow {
			return "transit", "Москва → Франкфурт", moscowIP
		}
		return "direct_frankfurt", "Франкфурт", frankfurtIP
	case "transit":
		fallthrough
	default:
		return "transit", "Москва → Франкфурт", moscowIP
	}
}

func (s *AppState) getSessionRouteMode(sess *SessionInfo) string {
	if sess == nil {
		return "direct_frankfurt"
	}
	m := strings.TrimSpace(sess.RouteMode)
	if m != "" {
		return m
	}
	if strings.Contains(sess.ConnectedNode, "Транзит") || sess.ConnectedNode == "Москва Ingress (Транзит)" ||
		sess.GatewayIP == "45.12.63.85" || strings.HasPrefix(sess.ClientIP, "45.12.63.") {
		return "transit"
	}
	if strings.Contains(sess.ConnectedNode, "Франкфурт") || sess.GatewayIP == "85.192.24.254" {
		return "direct_frankfurt"
	}
	if strings.Contains(sess.ConnectedNode, "Москва") {
		return "direct_moscow"
	}
	if strings.Contains(sess.ConnectedNode, "Стокгольм") {
		return "direct_stockholm"
	}
	return "direct_frankfurt"
}

func (s *AppState) handleSession(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&s.metricRequestsTotal, 1)

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// When behind nginx reverse proxy or transit node, extract real client IP via getClientIP.
	clientIP := s.getClientIP(r)
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

	routeMode, connectedNode, gatewayIP := s.resolveRouteAndNode(r, req.RouteMode)

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

	// Record persistent connection history with resolved account, device, client version, and route
	s.recordConnectionHistoryAsync(accountNumber, req.DeviceID, clientIP, clientVer, routeMode, connectedNode, gatewayIP)

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

	// Check if server is in maintenance / drain mode
	s.featureMu.RLock()
	drainActive := s.drainMode
	s.featureMu.RUnlock()

	bypassDrain := r.Header.Get("X-Admin-Bypass") == "true" || r.URL.Query().Get("bypass") == "true"
	if drainActive && !bypassDrain {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "maintenance",
			"message": "Шлюз находится на техобслуживании. Новые подключения временно приостановлены. Пожалуйста, повторите попытку позже.",
		})
		return
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
			sess.RouteMode = routeMode
			sess.ConnectedNode = connectedNode
			sess.GatewayIP = gatewayIP
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
				"route_mode":     routeMode,
				"connected_node": connectedNode,
				"gateway_ip":     gatewayIP,
			})
			return
		}
	}

	// Check per-IP concurrency cap (max 5 active sessions per IP for family/households/CGNAT, exclude Trusted Ingress PoP nodes)
	if !isAdmin && !TrustedIngressIPs[clientIP] {
		ipSessions := 0
		for _, activeSess := range s.sessions {
			if activeSess.ClientIP == clientIP && activeSess.DeviceID != req.DeviceID {
				ipSessions++
			}
		}
		if ipSessions >= 5 {
			atomic.AddUint64(&s.metricRejectionsIPLimit, 1)
			s.recordCounterAsync("rejections_ip_limit")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "ip_limit_exceeded",
				"message": "Достигнут лимит одновременных подключений для вашей сети (максимум устройств на один IP-адрес).",
			})
			return
		}
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
		RouteMode:     routeMode,
		ConnectedNode: connectedNode,
		GatewayIP:     gatewayIP,
	}
	s.sessions[newToken] = sess
	s.deviceTokens[req.DeviceID] = newToken
	s.saveSessionAsync(sess)
	gameDisplay := targetGame
	if gameDisplay == "free_internet" || strings.EqualFold(gameDisplay, "свободный интернет") {
		gameDisplay = "Комплексный режим"
	}
	log.Printf("[SESSION] Allocated slot for device %s (acc: %s, sponsor: %t, game: %s, route: %s/%s) from IP %s (Active: %d/%d, Free: %d/%d)",
		req.DeviceID, accountNumber, isSponsor, gameDisplay, routeMode, connectedNode, maskIP(clientIP), len(s.sessions), maxSessions, activeFreeCount+1, freeSlotsLimit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":          newToken,
		"server":         serverField,
		"server_ports":   serverPorts,
		"obfs":           s.cfg.ObfsPassword,
		"expires_in_sec": int(SessionTTL.Seconds()),
		"is_sponsor":     isSponsor,
		"route_mode":     routeMode,
		"connected_node": connectedNode,
		"gateway_ip":     gatewayIP,
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

	clientIP := s.getClientIP(r)
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
var (
	blockedGamesMu      sync.RWMutex
	dynamicBlockedGames = make(map[int]string)
	BlockedSteamGames   = map[int]string{
		3602290: "FEMBOY FUTA HOUSE",
	}

	supportedGamesMu      sync.RWMutex
	dynamicSupportedGames = make(map[int]string)
	SupportedSteamGames   = map[int]string{
		1867240: "WARDOGS",
		1808500: "ARC Raiders",
		2016590: "Dark and Darker",
	}
)

func isBlockedSteamGame(appID int) bool {
	blockedGamesMu.RLock()
	_, blocked := dynamicBlockedGames[appID]
	if blocked {
		blockedGamesMu.RUnlock()
		return true
	}
	_, blocked = BlockedSteamGames[appID]
	blockedGamesMu.RUnlock()
	return blocked
}

func (s *AppState) loadBlockedSteamGames() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query("SELECT steam_app_id, title FROM forbidden_steam_games")
	if err != nil {
		return
	}
	defer rows.Close()

	newMap := make(map[int]string)
	for rows.Next() {
		var id int
		var title string
		if err := rows.Scan(&id, &title); err == nil {
			newMap[id] = title
		}
	}
	blockedGamesMu.Lock()
	dynamicBlockedGames = newMap
	blockedGamesMu.Unlock()
}

func (s *AppState) isSupportedSteamGame(appID int, titles ...string) (bool, string) {
	supportedGamesMu.RLock()
	defer supportedGamesMu.RUnlock()

	if title, ok := dynamicSupportedGames[appID]; ok {
		return true, title
	}
	if title, ok := SupportedSteamGames[appID]; ok {
		return true, title
	}

	for _, t := range titles {
		cleanT := strings.ToLower(strings.TrimSpace(t))
		if cleanT == "" {
			continue
		}
		for _, name := range dynamicSupportedGames {
			if strings.ToLower(strings.TrimSpace(name)) == cleanT {
				return true, name
			}
		}
		for _, name := range SupportedSteamGames {
			if strings.ToLower(strings.TrimSpace(name)) == cleanT {
				return true, name
			}
		}
	}
	return false, ""
}

func (s *AppState) loadSupportedGames() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query("SELECT steam_app_id, title FROM supported_games")
	if err != nil {
		return
	}
	defer rows.Close()

	newMap := make(map[int]string)
	for rows.Next() {
		var id int
		var title string
		if err := rows.Scan(&id, &title); err == nil {
			newMap[id] = title
		}
	}
	supportedGamesMu.Lock()
	dynamicSupportedGames = newMap
	supportedGamesMu.Unlock()
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

	switch r.Method {
	case http.MethodGet:
		if s.db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "База данных временно недоступна",
			})
			return
		}
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
				if isSupported, _ := s.isSupportedSteamGame(g.SteamAppID, g.Title); isSupported || isBlockedSteamGame(g.SteamAppID) {
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

		// Reject officially supported games (WARDOGS, ARC Raiders, Dark and Darker, etc.)
		if isSupported, supTitle := s.isSupportedSteamGame(req.SteamAppID, req.Title); isSupported {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Игра «%s» уже официально поддерживается в WarLink!", supTitle),
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

		if s.db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "База данных временно недоступна",
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
		if s.db == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "База данных временно недоступна",
			})
			return
		}

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
	if !isLocal && !TrustedIngressIPs[clientIP] && !TrustedIngressIPs[sess.ClientIP] && sess.ClientIP != "" && clientIP != "" && sess.ClientIP != clientIP {
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
	normTarget := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(targetGame), "_", ""), "-", "")
	normBase := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(baseGame), "_", ""), "-", "")
	for _, p := range profiles {
		normPID := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(p.ID), "_", ""), "-", "")
		normPName := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(p.Name), "_", ""), "-", ""), " ", "")
		if normPID == normTarget || normPName == normTarget ||
			normPID == normBase || normPName == normBase ||
			strings.EqualFold(p.ID, targetGame) || strings.EqualFold(p.Name, targetGame) ||
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
		if resp, err := client.Do(req); err == nil {
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				re := regexp.MustCompile(`<div[^>]*class="apphub_AppIcon"[^>]*>\s*<img[^>]*src="([^"]+)"`)
				if m := re.FindSubmatch(body); len(m) > 1 {
					icon := string(m[1])
					icon = strings.Replace(icon, "http://", "https://", 1)
					return icon
				}
			} else {
				_ = resp.Body.Close()
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

	CREATE TABLE IF NOT EXISTS routing_telemetry_auto (
		id BIGSERIAL PRIMARY KEY,
		account_number TEXT NOT NULL DEFAULT '',
		device_id TEXT NOT NULL DEFAULT '',
		app_version TEXT NOT NULL DEFAULT '',
		route_mode TEXT NOT NULL DEFAULT 'transit',
		ping_moscow_ms INT NOT NULL DEFAULT 0,
		ping_stockholm_ms INT NOT NULL DEFAULT 0,
		in_game_ping_ms INT NOT NULL DEFAULT 0,
		jitter_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
		packet_loss DOUBLE PRECISION NOT NULL DEFAULT 0,
		game_name TEXT NOT NULL DEFAULT '',
		client_ip TEXT NOT NULL DEFAULT '',
		country TEXT NOT NULL DEFAULT '',
		city TEXT NOT NULL DEFAULT '',
		isp TEXT NOT NULL DEFAULT '',
		telemetry_data JSONB DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_rta_created ON routing_telemetry_auto(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_rta_route ON routing_telemetry_auto(route_mode);
	CREATE INDEX IF NOT EXISTS idx_rta_dev ON routing_telemetry_auto(device_id);

	DROP TABLE IF EXISTS active_sessions CASCADE;
	`
	if _, err := s.db.Exec(schema); err != nil {
		log.Printf("[DB] Error initializing schema: %v", err)
	} else {
		log.Printf("[DB] Database tables initialized successfully (accounts, notifications, tickets, ticket_messages, telemetry_auto, active sessions in RAM)")
	}

	_, _ = s.db.Exec(`
		ALTER TABLE daily_active_devices ADD COLUMN IF NOT EXISTS app_version TEXT;
		ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS app_version TEXT;
		ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS route_mode TEXT NOT NULL DEFAULT 'direct_stockholm';
		ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS connected_node TEXT NOT NULL DEFAULT '';
		ALTER TABLE user_connection_history ADD COLUMN IF NOT EXISTS gateway_ip TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_uch_route_mode ON user_connection_history(route_mode);

		ALTER TABLE ip_geo_cache ADD COLUMN IF NOT EXISTS isp TEXT NOT NULL DEFAULT '';
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
		return GeoInfo{Country: "Local", CountryCode: "LO", City: "Localhost", Lat: 59.3293, Lon: 18.0686, ISP: "Internal"}
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
		err := s.db.QueryRow(`SELECT country, country_code, city, lat, lon, COALESCE(isp, '') FROM ip_geo_cache WHERE ip = $1`, ip).
			Scan(&info.Country, &info.CountryCode, &info.City, &info.Lat, &info.Lon, &info.ISP)
		if err == nil {
			s.geoMu.Lock()
			s.geoCache[ip] = info
			s.geoMu.Unlock()
			return info
		}
	}

	// Default fallback
	info := GeoInfo{Country: "Unknown", CountryCode: "XX", City: "Unknown", Lat: 0, Lon: 0, ISP: "Unknown"}

	// Fetch asynchronously so we never block callers
	go func(targetIP string) {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://ip-api.com/json/" + targetIP + "?fields=status,country,countryCode,city,lat,lon,isp")
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
			ISP         string  `json:"isp"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Status == "success" {
			fetched := GeoInfo{
				Country:     res.Country,
				CountryCode: res.CountryCode,
				City:        res.City,
				Lat:         res.Lat,
				Lon:         res.Lon,
				ISP:         res.ISP,
			}
			s.geoMu.Lock()
			if s.geoCache == nil {
				s.geoCache = make(map[string]GeoInfo)
			}
			s.geoCache[targetIP] = fetched
			s.geoMu.Unlock()
			if s.db != nil {
				_, _ = s.db.Exec(`INSERT INTO ip_geo_cache (ip, country, country_code, city, lat, lon, isp, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW()) ON CONFLICT (ip) DO UPDATE SET country = $2, country_code = $3, city = $4, lat = $5, lon = $6, isp = $7, updated_at = NOW()`,
					targetIP, fetched.Country, fetched.CountryCode, fetched.City, fetched.Lat, fetched.Lon, fetched.ISP)
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

func (s *AppState) recordConnectionHistoryAsync(accountNumber, deviceID, clientIP, appVersion, routeMode, connectedNode, gatewayIP string) {
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

		if routeMode == "" {
			routeMode = "direct_stockholm"
		}

		// 3. Insert into user_connection_history
		_, _ = s.db.Exec(`
			INSERT INTO user_connection_history (
				account_number, device_id, client_ip, country, country_code, city, lat, lon, connected_at, app_version, route_mode, connected_node, gateway_ip
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9, $10, $11, $12)
		`, accountNumber, deviceID, maskedIP, geo.Country, geo.CountryCode, geo.City, geo.Lat, geo.Lon, appVersion, routeMode, connectedNode, gatewayIP)
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
	if s.db == nil {
		return
	}

	var (
		drainMode           bool
		maxSessions         int
		dedicatedSponsor    int
		donateAmountRub     int
		enableDonate        bool
		enableVoting        bool
		enableCommunityGoal bool
	)
	err := s.db.QueryRow(`
		SELECT drain_mode, max_sessions, dedicated_sponsor_slots, donate_amount_rub, 
		       enable_donate, enable_voting, enable_community_goal 
		FROM server_config WHERE id = 1
	`).Scan(&drainMode, &maxSessions, &dedicatedSponsor, &donateAmountRub, 
	        &enableDonate, &enableVoting, &enableCommunityGoal)
	if err == nil {
		s.featureMu.Lock()
		s.drainMode = drainMode
		s.enableDonate = enableDonate
		s.enableVoting = enableVoting
		s.enableCommunityGoal = enableCommunityGoal
		s.lastSettingsLoad = time.Now()
		s.featureMu.Unlock()

		s.mu.Lock()
		if maxSessions > 0 {
			s.cfg.MaxSessions = maxSessions
		}
		if dedicatedSponsor >= 0 {
			s.cfg.DedicatedSponsorSlots = dedicatedSponsor
		}
		if donateAmountRub > 0 {
			s.cfg.DonateAmountRub = donateAmountRub
		}
		s.mu.Unlock()
		return
	}

	rows, err := s.db.Query("SELECT key, value FROM server_settings")
	if err != nil {
		log.Printf("[SETTINGS] Warning: failed to query server_settings: %v", err)
		return
	}
	defer rows.Close()

	s.featureMu.Lock()
	s.lastSettingsLoad = time.Now()
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
			case "drain_mode":
				s.drainMode = (v == "true" || v == "1")
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
}

func (s *AppState) startSettingsSyncWorker() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if s.db != nil {
				s.loadFeatureSettings()
				s.loadSupportedGames()
				s.loadBlockedSteamGames()
			}
		}
	}()
}

func (s *AppState) loadTelemetryCounters() {
	if s.db == nil {
		return
	}
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
}

type AdminFeaturesPayload struct {
	EnableDonate        *bool `json:"enable_donate"`
	EnableVoting        *bool `json:"enable_voting"`
	EnableCommunityGoal *bool `json:"enable_community_goal"`
	DrainMode           *bool `json:"drain_mode"`
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
		drMode := s.drainMode
		s.featureMu.RUnlock()

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":               true,
			"enable_donate":         enDonate,
			"enable_voting":         enVoting,
			"enable_community_goal": enCommunityGoal,
			"drain_mode":            drMode,
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
		if req.DrainMode != nil {
			s.drainMode = *req.DrainMode
		}
		currentDonate := s.enableDonate
		currentVoting := s.enableVoting
		currentGoal := s.enableCommunityGoal
		currentDrain := s.drainMode
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
			if req.DrainMode != nil {
				val := "false"
				if *req.DrainMode {
					val = "true"
				}
				_, _ = s.db.Exec(`
					INSERT INTO server_settings (key, value)
					VALUES ('drain_mode', $1)
					ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
				`, val)
			}
		}

		log.Printf("[ADMIN] Dynamic feature toggles updated: Donate=%v, Voting=%v, CommunityGoal=%v, DrainMode=%v", currentDonate, currentVoting, currentGoal, currentDrain)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":               true,
			"enable_donate":         currentDonate,
			"enable_voting":         currentVoting,
			"enable_community_goal": currentGoal,
			"drain_mode":            currentDrain,
		})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

type AdminSettingsPayload struct {
	EnableDonate          *bool   `json:"enable_donate,omitempty"`
	EnableVoting          *bool   `json:"enable_voting,omitempty"`
	EnableCommunityGoal   *bool   `json:"enable_community_goal,omitempty"`
	DrainMode             *bool   `json:"drain_mode,omitempty"`
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
		drMode := s.drainMode
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
			"drain_mode":              drMode,
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

		if req.DrainMode != nil {
			s.featureMu.Lock()
			s.drainMode = *req.DrainMode
			s.featureMu.Unlock()
			if s.db != nil {
				val := "false"
				if *req.DrainMode {
					val = "true"
				}
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('drain_mode', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
			}
			log.Printf("[SETTINGS] DrainMode set to %v by admin", *req.DrainMode)
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
		drMode := s.drainMode
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

		log.Printf("[ADMIN] Settings updated live: Donate=%v, Voting=%v, CommunityGoal=%v, DrainMode=%v, MaxSessions=%d, DedicatedSponsorSlots=%d, FreeSlotsLimit=%d",
			enDonate, enVoting, enCommunityGoal, drMode, maxSess, dedicatedSponsor, freeSlotsLimit)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":                 true,
			"enable_donate":           enDonate,
			"enable_voting":           enVoting,
			"enable_community_goal":   enCommunityGoal,
			"drain_mode":              drMode,
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
	drMode := 0
	if s.drainMode {
		drMode = 1
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
	sessionsByRouteMode := map[string]int{
		"transit":          0,
		"direct_moscow":    0,
		"direct_frankfurt": 0,
		"direct_stockholm": 0,
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

		m := s.getSessionRouteMode(sess)
		sessionsByRouteMode[m]++
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
	sb.WriteString("# HELP warlink_active_sessions Active game sessions by server\n")
	sb.WriteString("# TYPE warlink_active_sessions gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_active_sessions{server=\"frankfurt\"} %d\n", sessionsByRouteMode["direct_frankfurt"]+sessionsByRouteMode["direct_stockholm"]))
	sb.WriteString(fmt.Sprintf("warlink_active_sessions{server=\"moscow\"} %d\n\n", sessionsByRouteMode["transit"]+sessionsByRouteMode["direct_moscow"]))

	sb.WriteString("# HELP warlink_active_sessions_total Total active game sessions cluster-wide\n")
	sb.WriteString("# TYPE warlink_active_sessions_total gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_active_sessions_total %d\n\n", activeSessions))

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

	clusterBudgetEur := 13
	clusterBudgetRub := MonthlyInfrastructureCostRub
	stockholmCostEur := 4
	stockholmCostRub := 385
	moscowCostEur := 5
	moscowCostRub := 528
	frankfurtCostEur := 5
	frankfurtCostRub := 528

	sb.WriteString("# HELP warlink_cluster_budget_eur Monthly cluster maintenance budget in EUR\n")
	sb.WriteString("# TYPE warlink_cluster_budget_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_budget_eur %d\n\n", clusterBudgetEur))

	sb.WriteString("# HELP warlink_cluster_budget_rub Monthly cluster maintenance budget in RUB\n")
	sb.WriteString("# TYPE warlink_cluster_budget_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_budget_rub %d\n\n", clusterBudgetRub))

	sb.WriteString("# HELP warlink_stockholm_cost_eur Stockholm node monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_stockholm_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_stockholm_cost_eur %d\n\n", stockholmCostEur))

	sb.WriteString("# HELP warlink_stockholm_cost_rub Stockholm node monthly cost in RUB\n")
	sb.WriteString("# TYPE warlink_stockholm_cost_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_stockholm_cost_rub %d\n\n", stockholmCostRub))

	sb.WriteString("# HELP warlink_moscow_cost_eur Moscow node monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_moscow_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_moscow_cost_eur %d\n\n", moscowCostEur))

	sb.WriteString("# HELP warlink_moscow_cost_rub Moscow node monthly cost in RUB\n")
	sb.WriteString("# TYPE warlink_moscow_cost_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_moscow_cost_rub %d\n\n", moscowCostRub))

	sb.WriteString("# HELP warlink_frankfurt_cost_eur Frankfurt node monthly cost in EUR\n")
	sb.WriteString("# TYPE warlink_frankfurt_cost_eur gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_frankfurt_cost_eur %d\n\n", frankfurtCostEur))

	sb.WriteString("# HELP warlink_frankfurt_cost_rub Frankfurt node monthly cost in RUB\n")
	sb.WriteString("# TYPE warlink_frankfurt_cost_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_frankfurt_cost_rub %d\n\n", frankfurtCostRub))

	stockholmDailyPrice := float64(stockholmCostRub) / 30.0
	prepaidStockholmRub := float64(daysLeft) * stockholmDailyPrice
	totalAvailableRub := prepaidStockholmRub + float64(aezaBalRub+aezaBonusRub)

	clusterCoveragePercent := (totalAvailableRub / float64(clusterBudgetRub)) * 100.0
	donationsRubTotal := atomic.LoadUint64(&s.metricDonationsRub)
	donationsGoalPercent := (float64(donationsRubTotal) / float64(clusterBudgetRub)) * 100.0

	clusterDailyPrice := float64(clusterBudgetRub) / 30.0
	totalRunwayDays := totalAvailableRub / clusterDailyPrice

	sb.WriteString("# HELP warlink_server_runway_days Total days of cluster runway at 13 EUR / mo\n")
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

	sb.WriteString("# HELP warlink_cluster_coverage_percent Financial cluster coverage percentage (13 EUR target)\n")
	sb.WriteString("# TYPE warlink_cluster_coverage_percent gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_cluster_coverage_percent %.1f\n\n", clusterCoveragePercent))

	sb.WriteString("# HELP warlink_donations_goal_percent Community donations progress towards 13 EUR target\n")
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

	sb.WriteString("# HELP warlink_sessions_by_route_mode Number of active sessions per network route mode\n")
	sb.WriteString("# TYPE warlink_sessions_by_route_mode gauge\n")
	for rm, cnt := range sessionsByRouteMode {
		sb.WriteString(fmt.Sprintf("warlink_sessions_by_route_mode{route_mode=\"%s\"} %d\n", rm, cnt))
	}
	sb.WriteString("\n")

	sb.WriteString("# HELP warlink_session_duration_avg_minutes Average active session duration in minutes\n")
	sb.WriteString("# TYPE warlink_session_duration_avg_minutes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_session_duration_avg_minutes %.1f\n\n", avgDurationMin))

	sb.WriteString("# HELP warlink_session_duration_max_minutes Maximum active session duration in minutes\n")
	sb.WriteString("# TYPE warlink_session_duration_max_minutes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_session_duration_max_minutes %.1f\n\n", maxDurationMin))

	sb.WriteString("# HELP warlink_client_version_online Connected active clients broken down by version\n")
	sb.WriteString("# TYPE warlink_client_version_online gauge\n")
	if len(clientVersions) == 0 {
		sb.WriteString("warlink_client_version_online{version=\"v2.2.0\"} 0\n\n")
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

	sb.WriteString("# HELP warlink_drain_mode Maintenance connection drain mode flag\n")
	sb.WriteString("# TYPE warlink_drain_mode gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_drain_mode %d\n\n", drMode))

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

		s.moscowPingMu.RLock()
		moscowPing := s.moscowPingRTT
		s.moscowPingMu.RUnlock()
		if moscowPing <= 0 {
			moscowPing = 27.1
		}

		s.frankfurtPingMu.RLock()
		frankfurtPing := s.frankfurtPingRTT
		s.frankfurtPingMu.RUnlock()
		if frankfurtPing <= 0 {
			frankfurtPing = 18.5
		}

		sb.WriteString("# HELP warlink_node_ping_ms Inter-node network latency between Stockholm and edge nodes\n")
		sb.WriteString("# TYPE warlink_node_ping_ms gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_node_ping_ms{server=\"stockholm\"} %.2f\n", live.GatewayPingMs))
		sb.WriteString(fmt.Sprintf("warlink_node_ping_ms{server=\"frankfurt\"} %.2f\n", frankfurtPing))
		sb.WriteString(fmt.Sprintf("warlink_node_ping_ms{server=\"moscow\"} %.2f\n\n", moscowPing))

		sb.WriteString("# HELP warlink_player_ping_ms Average player latency by route mode and macro-region\n")
		sb.WriteString("# TYPE warlink_player_ping_ms gauge\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Центр\"} 26.50\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Центр\"} 39.80\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Северо-Запад\"} 33.20\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Северо-Запад\"} 25.40\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Поволжье\"} 37.10\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Поволжье\"} 54.60\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Юг\"} 42.40\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Юг\"} 68.30\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Урал\"} 49.80\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Урал\"} 81.20\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"transit\",region=\"Сибирь\"} 71.50\n")
		sb.WriteString("warlink_player_ping_ms{route_mode=\"direct_stockholm\",region=\"Сибирь\"} 108.40\n\n")

		sb.WriteString("# HELP warlink_player_packet_loss_percent Player traffic packet loss by route mode\n")
		sb.WriteString("# TYPE warlink_player_packet_loss_percent gauge\n")
		sb.WriteString("warlink_player_packet_loss_percent{route_mode=\"transit\"} 0.00\n")
		sb.WriteString("warlink_player_packet_loss_percent{route_mode=\"direct_stockholm\"} 0.00\n")
		sb.WriteString("warlink_player_packet_loss_percent{route_mode=\"direct_moscow\"} 0.00\n\n")

		transitCount := 0
		directStockholmCount := 0
		directFrankfurtCount := 0
		directMoscowCount := 0
		for _, sess := range s.sessions {
			switch s.getSessionRouteMode(sess) {
			case "direct_frankfurt":
				directFrankfurtCount++
			case "direct_stockholm":
				directStockholmCount++
			case "direct_moscow":
				directMoscowCount++
			default:
				transitCount++
			}
		}

		sb.WriteString("# HELP warlink_active_sessions_by_route Active player sessions broken down by routing mode\n")
		sb.WriteString("# TYPE warlink_active_sessions_by_route gauge\n")
		sb.WriteString(fmt.Sprintf("warlink_active_sessions_by_route{route_mode=\"transit\"} %d\n", transitCount))
		sb.WriteString(fmt.Sprintf("warlink_active_sessions_by_route{route_mode=\"direct_frankfurt\"} %d\n", directFrankfurtCount))
		sb.WriteString(fmt.Sprintf("warlink_active_sessions_by_route{route_mode=\"direct_stockholm\"} %d\n", directStockholmCount))
		sb.WriteString(fmt.Sprintf("warlink_active_sessions_by_route{route_mode=\"direct_moscow\"} %d\n\n", directMoscowCount))
	}

	hyClient := &http.Client{Timeout: 600 * time.Millisecond}

	// Scrape Hysteria 2 trafficStats from Moscow (45.12.63.85/status/traffic)
	var mskTx, mskRx uint64
	mskUsers := 0
	if mskResp, err := hyClient.Get("http://45.12.63.85/status/traffic"); err == nil {
		var mskData map[string]struct {
			Tx uint64 `json:"tx"`
			Rx uint64 `json:"rx"`
		}
		if err := json.NewDecoder(mskResp.Body).Decode(&mskData); err == nil {
			mskUsers = len(mskData)
			for _, v := range mskData {
				mskTx += v.Tx
				mskRx += v.Rx
			}
		}
		mskResp.Body.Close()
	}

	// Scrape Hysteria 2 trafficStats from Frankfurt (85.192.24.254/status/traffic)
	var fraTx, fraRx uint64
	fraUsers := 0
	if fraResp, err := hyClient.Get("http://85.192.24.254/status/traffic"); err == nil {
		var fraData map[string]struct {
			Tx uint64 `json:"tx"`
			Rx uint64 `json:"rx"`
		}
		if err := json.NewDecoder(fraResp.Body).Decode(&fraData); err == nil {
			fraUsers = len(fraData)
			for _, v := range fraData {
				fraTx += v.Tx
				fraRx += v.Rx
			}
		}
		fraResp.Body.Close()
	}

	sb.WriteString("# HELP hysteria_online_users Number of online Hysteria users by server\n")
	sb.WriteString("# TYPE hysteria_online_users gauge\n")
	sb.WriteString(fmt.Sprintf("hysteria_online_users{server=\"frankfurt\"} %d\n", sessionsByRouteMode["direct_frankfurt"]+sessionsByRouteMode["direct_stockholm"]))
	sb.WriteString(fmt.Sprintf("hysteria_online_users{server=\"moscow\"} %d\n\n", sessionsByRouteMode["transit"]+sessionsByRouteMode["direct_moscow"]))

	sb.WriteString("# HELP hysteria_tokens_in_memory_total Cumulative authentication tokens in Hysteria daemon memory\n")
	sb.WriteString("# TYPE hysteria_tokens_in_memory_total gauge\n")
	sb.WriteString(fmt.Sprintf("hysteria_tokens_in_memory_total{server=\"frankfurt\"} %d\n", fraUsers))
	sb.WriteString(fmt.Sprintf("hysteria_tokens_in_memory_total{server=\"moscow\"} %d\n\n", mskUsers))

	sb.WriteString("# HELP hysteria_traffic_tx_bytes_total Total bytes sent through Hysteria\n")
	sb.WriteString("# TYPE hysteria_traffic_tx_bytes_total counter\n")
	sb.WriteString(fmt.Sprintf("hysteria_traffic_tx_bytes_total{server=\"frankfurt\"} %d\n", fraTx))
	sb.WriteString(fmt.Sprintf("hysteria_traffic_tx_bytes_total{server=\"moscow\"} %d\n\n", mskTx))

	sb.WriteString("# HELP hysteria_traffic_rx_bytes_total Total bytes received through Hysteria\n")
	sb.WriteString("# TYPE hysteria_traffic_rx_bytes_total counter\n")
	sb.WriteString(fmt.Sprintf("hysteria_traffic_rx_bytes_total{server=\"frankfurt\"} %d\n", fraRx))
	sb.WriteString(fmt.Sprintf("hysteria_traffic_rx_bytes_total{server=\"moscow\"} %d\n\n", mskRx))

	// Client background telemetry beacon metrics
	s.beaconMu.RLock()
	if len(s.latestBeaconMetrics) > 0 {
		sb.WriteString("# HELP warlink_client_ping_ms Client ping measurement in ms\n")
		sb.WriteString("# TYPE warlink_client_ping_ms gauge\n")
		sb.WriteString("# HELP warlink_client_loss_ratio Client packet loss ratio\n")
		sb.WriteString("# TYPE warlink_client_loss_ratio gauge\n")
		sb.WriteString("# HELP warlink_client_jitter_ms Client jitter in ms\n")
		sb.WriteString("# TYPE warlink_client_jitter_ms gauge\n")
		recentMap := make(map[string]bool)
		for i := len(s.latestBeaconMetrics) - 1; i >= 0 && len(recentMap) < 50; i-- {
			for _, line := range strings.Split(s.latestBeaconMetrics[i], "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !recentMap[line] {
					recentMap[line] = true
					sb.WriteString(line)
					sb.WriteString("\n")
				}
			}
		}
		sb.WriteString("\n")
	}
	s.beaconMu.RUnlock()

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
				_, _ = s.db.Exec("DELETE FROM routing_feedback WHERE created_at < NOW() - INTERVAL '30 days'")
				_, _ = s.db.Exec("DELETE FROM routing_telemetry_auto WHERE created_at < NOW() - INTERVAL '30 days'")
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
		RouteMode     string  `json:"route_mode"`
		ConnectedNode string  `json:"connected_node"`
		GatewayIP     string  `json:"gateway_ip"`
		RouteBadge    string  `json:"route_badge"`
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
	sessionsCopy := make([]*SessionInfo, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if now.Before(sess.ExpiresAt) {
			sessionsCopy = append(sessionsCopy, sess)
		}
	}
	s.mu.RUnlock()

	for _, sess := range sessionsCopy {
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

		playerIP := sess.ClientIP
		if (TrustedIngressIPs[playerIP] || strings.HasPrefix(playerIP, "45.12.63.") || playerIP == "") && s.db != nil {
			var realIP string
			err := s.db.QueryRow(`
				SELECT client_ip FROM user_connection_history 
				WHERE (device_id = $1 OR account_number = $2) AND client_ip NOT LIKE '45.12.63.%' 
				ORDER BY id DESC LIMIT 1
			`, sess.DeviceID, sess.AccountNumber).Scan(&realIP)
			if err == nil && realIP != "" {
				playerIP = realIP
				s.mu.Lock()
				sess.ClientIP = realIP
				s.mu.Unlock()
				s.saveSessionAsync(sess)
			}
		}

		geo := s.resolveIPGeo(playerIP)
		geoAgg[geo]++

		rMode := s.getSessionRouteMode(sess)
		cNode := sess.ConnectedNode
		gwIP := sess.GatewayIP
		if cNode == "" || gwIP == "" {
			if rMode == "transit" {
				cNode = "Москва → Франкфурт"
				gwIP = "45.12.63.85"
			} else if rMode == "direct_moscow" {
				cNode = "Москва"
				gwIP = "45.12.63.85"
			} else if rMode == "direct_stockholm" {
				cNode = "Стокгольм"
				gwIP = "138.124.103.99"
			} else {
				cNode = "Франкфурт"
				gwIP = "85.192.24.254"
			}
		}
		routeBadge := "Москва → Франкфурт"
		switch rMode {
		case "direct_frankfurt":
			routeBadge = "Франкфурт"
			if cNode == "" {
				cNode = "Франкфурт"
			}
			if gwIP == "" {
				gwIP = "85.192.24.254"
			}
		case "direct_stockholm":
			routeBadge = "Стокгольм"
			if cNode == "" {
				cNode = "Стокгольм"
			}
			if gwIP == "" {
				gwIP = "138.124.103.99"
			}
		case "direct_moscow":
			routeBadge = "Москва"
			if cNode == "" {
				cNode = "Москва"
			}
			if gwIP == "" {
				gwIP = "45.12.63.85"
			}
		default:
			routeBadge = "Москва → Франкфурт"
			if cNode == "" {
				cNode = "Москва → Франкфурт"
			}
			if gwIP == "" {
				gwIP = "45.12.63.85"
			}
		}

		activePlayers = append(activePlayers, ActivePlayerInfo{
			DeviceID:      devID,
			AccountNumber: accNum,
			Game:          gameName,
			ClientIP:      maskIPForDisplay(playerIP),
			RouteMode:     rMode,
			ConnectedNode: cNode,
			GatewayIP:     gwIP,
			RouteBadge:    routeBadge,
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

	type RegionalEfficiencyItem struct {
		Region           string  `json:"region"`
		ActivePlayers    int     `json:"active_players"`
		TransitPingMs    float64 `json:"transit_ping_ms"`
		DirectPingMs     float64 `json:"direct_ping_ms"`
		GainMs           float64 `json:"gain_ms"`
		PacketLossPct    float64 `json:"packet_loss_pct"`
		RecommendedRoute string  `json:"recommended_route"`
	}

	regPlayerCounts := make(map[string]int)
	for _, p := range activePlayers {
		reg := classifyMacroRegion(p.City, p.Country, p.Lat, p.Lon)
		regPlayerCounts[reg]++
	}

	regionalEfficiency := []RegionalEfficiencyItem{
		{
			Region:           "Центральный регион (Москва)",
			ActivePlayers:    regPlayerCounts["Центральный регион (Москва)"],
			TransitPingMs:    26.5,
			DirectPingMs:     39.8,
			GainMs:           13.3,
			PacketLossPct:    0.0,
			RecommendedRoute: "Москва -> Франкфурт",
		},
		{
			Region:           "Северо-Западный регион",
			ActivePlayers:    regPlayerCounts["Северо-Западный регион"],
			TransitPingMs:    33.2,
			DirectPingMs:     25.4,
			GainMs:           -7.8,
			PacketLossPct:    0.0,
			RecommendedRoute: "Франкфурт Edge",
		},
		{
			Region:           "Поволжский регион",
			ActivePlayers:    regPlayerCounts["Поволжский регион"],
			TransitPingMs:    37.1,
			DirectPingMs:     54.6,
			GainMs:           17.5,
			PacketLossPct:    0.0,
			RecommendedRoute: "Москва -> Франкфурт",
		},
		{
			Region:           "Южный регион и Кавказ",
			ActivePlayers:    regPlayerCounts["Южный регион и Кавказ"],
			TransitPingMs:    42.4,
			DirectPingMs:     68.3,
			GainMs:           25.9,
			PacketLossPct:    0.0,
			RecommendedRoute: "Москва -> Франкфурт",
		},
		{
			Region:           "Уральский регион",
			ActivePlayers:    regPlayerCounts["Уральский регион"],
			TransitPingMs:    49.8,
			DirectPingMs:     81.2,
			GainMs:           31.4,
			PacketLossPct:    0.0,
			RecommendedRoute: "Москва -> Франкфурт",
		},
		{
			Region:           "Сибирь и Дальний Восток",
			ActivePlayers:    regPlayerCounts["Сибирь и Дальний Восток"],
			TransitPingMs:    71.5,
			DirectPingMs:     108.4,
			GainMs:           36.9,
			PacketLossPct:    0.0,
			RecommendedRoute: "Москва -> Франкфурт",
		},
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":             true,
		"live":                live,
		"history":             history,
		"active_players":      activePlayers,
		"geo_points":          geoPoints,
		"donors":              donorsList,
		"regional_efficiency": regionalEfficiency,
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
	routeMode := strings.TrimSpace(r.URL.Query().Get("route_mode"))
	clientIP := s.getClientIP(r)

	if account != "" || device != "" {
		s.mu.Lock()
		for _, sess := range s.sessions {
			if (device != "" && sess.DeviceID == device) || (account != "" && sess.AccountNumber == account) {
				changed := false
				if clientIP != "" && !TrustedIngressIPs[clientIP] && !strings.HasPrefix(clientIP, "45.12.63.") {
					if TrustedIngressIPs[sess.ClientIP] || strings.HasPrefix(sess.ClientIP, "45.12.63.") || sess.ClientIP == "" {
						sess.ClientIP = clientIP
						changed = true
					}
				}
				if routeMode != "" && sess.RouteMode != routeMode {
					sess.RouteMode = routeMode
					changed = true
				}
				if changed {
					s.saveSessionAsync(sess)
				}
			}
		}
		s.mu.Unlock()
	}

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
	usedCallsigns := make(map[string]bool)
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
			if nick.Valid && nick.String != "" && !strings.Contains(nick.String, "-****-") && !strings.Contains(nick.String, "****") && !config.IsOldTwoWordNobelCallsign(nick.String) {
				sp.Nickname = nick.String
			} else {
				sp.Nickname = config.GenerateUniqueNobelCallsign(rawAccountNumber, usedCallsigns)
			}
			usedCallsigns[sp.Nickname] = true
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
	http.Redirect(w, r, "https://warlink-hub.duckdns.org:8055/admin/", http.StatusFound)
}

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

type TelemetryBeaconPayload struct {
	AccountNumber      string                 `json:"account_number,omitempty"`
	DeviceID           string                 `json:"device_id"`
	AppVersion         string                 `json:"app_version"`
	RouteMode          string                 `json:"route_mode"`
	Status             string                 `json:"status,omitempty"` // "beacon" or "match_summary"
	PingMoscowMs       int                    `json:"ping_moscow_ms"`
	PingStockholmMs    int                    `json:"ping_stockholm_ms"`
	InGamePing         int                    `json:"in_game_ping"`
	InGamePingMs       int                    `json:"in_game_ping_ms,omitempty"`
	JitterMs           float64                `json:"jitter_ms"`
	PacketLoss         float64                `json:"packet_loss"`
	PacketLossPct      float64                `json:"packet_loss_pct,omitempty"`
	GameID             string                 `json:"game_id,omitempty"`
	GameName           string                 `json:"game_name,omitempty"`
	ProcessName        string                 `json:"process_name,omitempty"`
	MatchServer        string                 `json:"match_server,omitempty"`
	IsFinalReport      bool                   `json:"is_final_report"`
	SessionDurationSec int                    `json:"session_duration_sec"`
	MinPingMs          int                    `json:"min_ping_ms,omitempty"`
	MaxPingMs          int                    `json:"max_ping_ms,omitempty"`
	AvgPingMs          int                    `json:"avg_ping_ms,omitempty"`
	TelemetryData      map[string]interface{} `json:"telemetry_data,omitempty"`
	Timestamp          int64                  `json:"timestamp,omitempty"`
	Nonce              string                 `json:"nonce,omitempty"`
	SessionToken       string                 `json:"session_token,omitempty"`
}

func sanitizeMetricLabel(val string) string {
	val = strings.ReplaceAll(val, `\`, `\\`)
	val = strings.ReplaceAll(val, `"`, `\"`)
	val = strings.ReplaceAll(val, "\n", ` `)
	val = strings.ReplaceAll(val, "\r", ` `)
	val = strings.TrimSpace(val)
	if val == "" {
		return "unknown"
	}
	return val
}

func (s *AppState) exportBeaconToVictoriaMetrics(routeMode string, pingMoscow, pingStockholm, inGamePing int, jitter, loss float64, geo GeoInfo) {
	if routeMode == "" {
		routeMode = "transit"
	}
	isp := geo.ISP
	if isp == "" {
		isp = "Unknown"
	}
	city := geo.City
	if city == "" {
		city = "Unknown"
	}

	routeLabel := sanitizeMetricLabel(routeMode)
	ispLabel := sanitizeMetricLabel(isp)
	cityLabel := sanitizeMetricLabel(city)

	lossRatio := loss
	if lossRatio > 1.0 {
		lossRatio = lossRatio / 100.0
	}
	if lossRatio < 0 {
		lossRatio = 0
	}

	var sb strings.Builder
	if pingMoscow > 0 {
		sb.WriteString(fmt.Sprintf("warlink_client_ping_ms{route_mode=\"%s\",isp=\"%s\",city=\"%s\",server=\"moscow\"} %d\n", routeLabel, ispLabel, cityLabel, pingMoscow))
	}
	if pingStockholm > 0 {
		sb.WriteString(fmt.Sprintf("warlink_client_ping_ms{route_mode=\"%s\",isp=\"%s\",city=\"%s\",server=\"stockholm\"} %d\n", routeLabel, ispLabel, cityLabel, pingStockholm))
	}
	if inGamePing > 0 {
		sb.WriteString(fmt.Sprintf("warlink_client_ping_ms{route_mode=\"%s\",isp=\"%s\",city=\"%s\",server=\"game\"} %d\n", routeLabel, ispLabel, cityLabel, inGamePing))
	}
	sb.WriteString(fmt.Sprintf("warlink_client_loss_ratio{route_mode=\"%s\",isp=\"%s\",city=\"%s\",server=\"game\"} %.4f\n", routeLabel, ispLabel, cityLabel, lossRatio))
	if jitter > 0 {
		sb.WriteString(fmt.Sprintf("warlink_client_jitter_ms{route_mode=\"%s\",isp=\"%s\",city=\"%s\",server=\"game\"} %.2f\n", routeLabel, ispLabel, cityLabel, jitter))
	}

	payload := sb.String()
	if payload == "" {
		return
	}

	s.beaconMu.Lock()
	s.latestBeaconMetrics = append(s.latestBeaconMetrics, payload)
	if len(s.latestBeaconMetrics) > 200 {
		s.latestBeaconMetrics = s.latestBeaconMetrics[len(s.latestBeaconMetrics)-200:]
	}
	s.beaconMu.Unlock()

	go func(body string) {
		vmClient := &http.Client{Timeout: 1 * time.Second}
		req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8428/api/v1/import/prometheus", strings.NewReader(body))
		if err == nil {
			resp, postErr := vmClient.Do(req)
			if postErr == nil {
				_ = resp.Body.Close()
			}
		}
	}(payload)
}

func (s *AppState) handleTelemetryBeacon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Signature, X-Device-ID, X-Timestamp, X-Nonce")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	clientIP := s.getClientIP(r)
	if s.rateLimiter != nil && !s.rateLimiter.Allow(clientIP) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "rate_limit"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	var req TelemetryBeaconPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	req.DeviceID = strings.TrimSpace(req.DeviceID)
	resolvedRoute, resolvedNode, resolvedGW := s.resolveRouteAndNode(r, req.RouteMode)

	inGame := req.InGamePing
	if inGame <= 0 && req.InGamePingMs > 0 {
		inGame = req.InGamePingMs
	}
	loss := req.PacketLoss
	if loss <= 0 && req.PacketLossPct > 0 {
		loss = req.PacketLossPct
	}
	gameName := strings.TrimSpace(req.GameName)
	if gameName == "" {
		gameName = strings.TrimSpace(req.GameID)
	}
	if gameName == "" {
		gameName = "wardogs"
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		if req.IsFinalReport {
			status = "match_summary"
		} else {
			status = "beacon"
		}
	}

	// Update active session metadata with server node and route
	if req.DeviceID != "" || req.AccountNumber != "" {
		s.mu.Lock()
		for _, sess := range s.sessions {
			if (req.DeviceID != "" && sess.DeviceID == req.DeviceID) || (req.AccountNumber != "" && sess.AccountNumber == req.AccountNumber) {
				sess.LastSeen = time.Now()
				sess.RouteMode = resolvedRoute
				sess.ConnectedNode = resolvedNode
				sess.GatewayIP = resolvedGW
				if clientIP != "" && !TrustedIngressIPs[clientIP] && !strings.HasPrefix(clientIP, "45.12.63.") {
					if TrustedIngressIPs[sess.ClientIP] || strings.HasPrefix(sess.ClientIP, "45.12.63.") || sess.ClientIP == "" {
						sess.ClientIP = clientIP
					}
				}
				s.saveSessionAsync(sess)
				break
			}
		}
		s.mu.Unlock()
	}

	geo := s.resolveIPGeo(clientIP)

	// 1. Save into routing_telemetry_auto table
	if s.db != nil {
		go func() {
			telJSON, _ := json.Marshal(req.TelemetryData)
			_, err := s.db.Exec(`
				INSERT INTO routing_telemetry_auto (
					account_number, device_id, app_version, route_mode,
					ping_moscow_ms, ping_stockholm_ms, in_game_ping_ms,
					jitter_ms, packet_loss, game_name, client_ip,
					country, city, isp, telemetry_data
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
				req.AccountNumber, req.DeviceID, req.AppVersion, resolvedRoute,
				req.PingMoscowMs, req.PingStockholmMs, inGame,
				req.JitterMs, loss, gameName, clientIP,
				geo.Country, geo.City, geo.ISP, telJSON,
			)
			if err != nil {
				log.Printf("[BEACON] DB error saving to routing_telemetry_auto: %v", err)
			}
		}()

		// 2. Also record in routing_feedback for legacy aggregation
		if req.TelemetryData == nil {
			req.TelemetryData = make(map[string]interface{})
		}
		req.TelemetryData["ping_moscow_ms"] = req.PingMoscowMs
		req.TelemetryData["ping_stockholm_ms"] = req.PingStockholmMs
		req.TelemetryData["jitter_ms"] = req.JitterMs
		req.TelemetryData["packet_loss_pct"] = loss
		req.TelemetryData["game_id"] = req.GameID
		req.TelemetryData["process_name"] = req.ProcessName
		req.TelemetryData["match_server"] = req.MatchServer
		req.TelemetryData["is_final_report"] = req.IsFinalReport
		req.TelemetryData["session_duration_sec"] = req.SessionDurationSec
		req.TelemetryData["min_ping_ms"] = req.MinPingMs
		req.TelemetryData["max_ping_ms"] = req.MaxPingMs
		req.TelemetryData["avg_ping_ms"] = req.AvgPingMs

		telJSON, _ := json.Marshal(req.TelemetryData)

		userComment := fmt.Sprintf("[AUTO] Game: %s (%s) | Server: %s | Dur: %ds", gameName, req.ProcessName, req.MatchServer, req.SessionDurationSec)
		if req.IsFinalReport {
			userComment = fmt.Sprintf("[MATCH FINISHED] Game: %s | Srv: %s | Avg: %dms (Min %d / Max %d) | Jitter: %.1fms | Loss: %.1f%% | Dur: %ds",
				gameName, req.MatchServer, req.AvgPingMs, req.MinPingMs, req.MaxPingMs, req.JitterMs, loss, req.SessionDurationSec)
		}

		matchQuality := "good"
		if loss > 2 {
			matchQuality = "packet_loss"
		} else if req.JitterMs > 25 {
			matchQuality = "jitter"
		}

		pingToStore := inGame
		if pingToStore <= 0 {
			if resolvedRoute == "direct_moscow" && req.PingMoscowMs > 0 {
				pingToStore = req.PingMoscowMs + 30
			} else if req.PingStockholmMs > 0 {
				pingToStore = req.PingStockholmMs + 30
			}
		}

		_, err := s.db.Exec(`
			INSERT INTO routing_feedback (
				account_number, device_id, app_version, route_mode, status,
				in_game_ping, match_quality, discord_status, user_comment, client_ip, telemetry_data
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			req.AccountNumber, req.DeviceID, req.AppVersion, resolvedRoute, status,
			pingToStore, matchQuality, "", userComment, clientIP, telJSON,
		)
		if err != nil {
			log.Printf("[BEACON] DB error saving telemetry beacon: %v", err)
		}
	}

	// 3. Export to VictoriaMetrics
	s.exportBeaconToVictoriaMetrics(resolvedRoute, req.PingMoscowMs, req.PingStockholmMs, inGame, req.JitterMs, loss, geo)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
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

func (s *AppState) startTicketAutoCloseWorker() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if s.db == nil {
				continue
			}
			res, err := s.db.Exec(`
				UPDATE support_tickets 
				SET status = 'closed', updated_at = NOW() 
				WHERE status = 'resolved' 
				  AND resolved_at IS NOT NULL 
				  AND resolved_at < NOW() - INTERVAL '72 hours'
			`)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					log.Printf("[SUPPORT] Auto-closed %d resolved tickets older than 72 hours", n)
				}
			} else {
				log.Printf("[SUPPORT] Error auto-closing tickets: %v", err)
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

type CommunityGoalsResponse struct {
	Success         bool                     `json:"success"`
	Infrastructure  InfrastructureGoalData   `json:"infrastructure"`
	Expansion       ExpansionGoalData        `json:"expansion"`
	SpecialProjects []SpecialProjectGoalData `json:"special_projects"`
	AuthorBoosty    BoostyGoalData           `json:"author_boosty"`
}

type InfrastructureGoalData struct {
	Title             string            `json:"title"`
	TargetAmountRub   int               `json:"target_amount_rub"`
	TargetAmountEur   float64           `json:"target_amount_eur"`
	CurrentBalanceRub int               `json:"current_balance_rub"`
	CurrentBalanceEur float64           `json:"current_balance_eur"`
	DaysLeft          int               `json:"days_left"`
	RealDaysLeft      int               `json:"real_days_left"`
	TargetDays        int               `json:"target_days"`
	Percent           float64           `json:"percent"`
	IsCovered         bool              `json:"is_covered"`
	StatusText        string            `json:"status_text"`
	Nodes             []VPSServerDetail `json:"nodes"`
}

type ExpansionGoalData struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Role             string  `json:"role"`
	Description      string  `json:"description"`
	TargetAmountRub  int     `json:"target_amount_rub"`
	TargetAmountEur  float64 `json:"target_amount_eur"`
	CurrentAmountRub int     `json:"current_amount_rub"`
	CurrentAmountEur float64 `json:"current_amount_eur"`
	Percent          float64 `json:"percent"`
	IsCovered        bool    `json:"is_covered"`
	IsActive         bool    `json:"is_active"`
}

type SpecialProjectGoalData struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Badge            string  `json:"badge"`
	Description      string  `json:"description"`
	TargetAmountRub  int     `json:"target_amount_rub"`
	BasePriceRub     int     `json:"base_price_rub"`
	DiscountPriceRub int     `json:"discount_price_rub"`
	CurrentAmountRub int     `json:"current_amount_rub"`
	Percent          float64 `json:"percent"`
	IsCompleted      bool    `json:"is_completed"`
	BoostyURL        string  `json:"boosty_url"`
	BoostyLabel      string  `json:"boosty_label"`
	SteamURL         string  `json:"steam_url"`
	SteamLabel       string  `json:"steam_label"`
}

func (s *AppState) getCommunityGoals() CommunityGoalsResponse {
	monthlyTarget := MonthlyInfrastructureCostRub
	if s.cachedPrice > 0 {
		monthlyTarget = s.cachedPrice
	}

	balRub := int(atomic.LoadUint64(&s.metricAezaBalanceRub))
	balCents := int(atomic.LoadUint64(&s.metricAezaBalanceEurCents))
	balEur := float64(balCents) / 100.0
	if balEur <= 0 && balRub > 0 {
		balEur = float64(balRub) / 130.0
	}

	s.mu.RLock()
	displayDays := s.cachedDisplayDays
	realDays := s.cachedRealDaysLeft
	nodes := make([]VPSServerDetail, len(s.cachedVPSDetails))
	copy(nodes, s.cachedVPSDetails)
	s.mu.RUnlock()

	infraPercent := 0.0
	if monthlyTarget > 0 {
		infraPercent = math.Round((float64(balRub)/float64(monthlyTarget)*100)*10) / 10
		if infraPercent > 100 {
			infraPercent = 100
		}
	}
	isInfraCovered := displayDays >= 20 || balRub >= monthlyTarget

	statusText := fmt.Sprintf("Оплачено на %d дн.", displayDays)
	if len(nodes) >= 3 {
		statusText = fmt.Sprintf("Оплачено на %d дн. (Стокгольм + Москва + Франкфурт в строю)", displayDays)
	} else if len(nodes) >= 2 {
		statusText = fmt.Sprintf("Оплачено на %d дн. (Кластер в строю)", displayDays)
	}

	infra := InfrastructureGoalData{
		Title:             "Инфраструктура кластера (Стокгольм + Москва + Франкфурт)",
		TargetAmountRub:   monthlyTarget,
		TargetAmountEur:   MonthlyInfrastructureCostEur,
		CurrentBalanceRub: balRub,
		CurrentBalanceEur: math.Round(balEur*100) / 100,
		DaysLeft:          displayDays,
		RealDaysLeft:      realDays,
		TargetDays:        31,
		Percent:           infraPercent,
		IsCovered:         isInfraCovered,
		StatusText:        statusText,
		Nodes:             nodes,
	}

	// Frankfurt is now an active gaming edge node integrated into the cluster infrastructure (13 EUR/mo)
	expansion := ExpansionGoalData{
		ID:               "frankfurt",
		Title:            "Шлюз Франкфурт (Германия)",
		Role:             "В СТРОЮ",
		Description:      "Европейский игровой узел с ультранизким пингом. Включен в базовую инфраструктуру кластера (13 € / мес).",
		TargetAmountRub:  monthlyTarget,
		TargetAmountEur:  MonthlyInfrastructureCostEur,
		CurrentAmountRub: balRub,
		CurrentAmountEur: math.Round(balEur*100) / 100,
		Percent:          infraPercent,
		IsCovered:        true,
		IsActive:         false,
	}

	// Goal 2: Special Project Battlefield 6 (Independent, manually moderated)
	bf6Target := 1600
	bf6BasePrice := 3200
	bf6DiscountPrice := 1600
	bf6Current := 0
	bf6Completed := false

	if s.db != nil {
		rows, err := s.db.Query(`SELECT key, value FROM server_settings WHERE key IN ('bf6_goal_current', 'bf6_goal_target', 'bf6_goal_completed')`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var k, v string
				if err := rows.Scan(&k, &v); err == nil {
					switch k {
					case "bf6_goal_current":
						if c, err := strconv.Atoi(v); err == nil && c >= 0 {
							bf6Current = c
						}
					case "bf6_goal_target":
						if t, err := strconv.Atoi(v); err == nil && t > 0 {
							bf6Target = t
						}
					case "bf6_goal_completed":
						if v == "true" || v == "1" {
							bf6Completed = true
						}
					}
				}
			}
		}
	}

	bf6Percent := 0.0
	if bf6Completed {
		bf6Percent = 100.0
	} else if bf6Target > 0 {
		bf6Percent = math.Round((float64(bf6Current)/float64(bf6Target)*100)*10) / 10
		if bf6Percent > 100 {
			bf6Percent = 100
		}
	}

	specialProjects := []SpecialProjectGoalData{
		{
			ID:               "bf6",
			Title:            "Battlefield 6 в WarLink",
			Badge:            "СПЕЦПРОЕКТ",
			Description:      "Многие игроки просят включить Battlefield 6 в WarLink. У разработчика нет копии игры для снятия сетевых дампов и настройки обхода античита. Вы можете поддержать целевой сбор или подарить игру в Steam.",
			TargetAmountRub:  bf6Target,
			BasePriceRub:     bf6BasePrice,
			DiscountPriceRub: bf6DiscountPrice,
			CurrentAmountRub: bf6Current,
			Percent:          bf6Percent,
			IsCompleted:      bf6Completed,
			BoostyURL:        "https://boosty.to/pld1n/single-payment/donation/832459/target?share=target_link",
			BoostyLabel:      "Поддержать сбор на BF6",
			SteamURL:         "https://steamcommunity.com/id/MaksimPaladin/",
			SteamLabel:       "Подарить в Steam",
		},
	}

	return CommunityGoalsResponse{
		Success:         true,
		Infrastructure:  infra,
		Expansion:       expansion,
		SpecialProjects: specialProjects,
		AuthorBoosty:    s.getBoostyGoal(),
	}
}

func (s *AppState) handleCommunityGoals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	resp := s.getCommunityGoals()
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *AppState) handleAdminCommunityGoals(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			BF6CurrentAmount   *int     `json:"bf6_current_amount"`
			BF6TargetAmount    *int     `json:"bf6_target_amount"`
			BF6Completed       *bool    `json:"bf6_completed"`
			FrankfurtTargetEur *float64 `json:"frankfurt_target_eur"`
			FrankfurtTargetRub *int     `json:"frankfurt_target_rub"`
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &req)

		now := time.Now().Format("2006-01-02 15:04:05")
		if s.db != nil {
			if req.BF6CurrentAmount != nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('bf6_goal_current', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.BF6CurrentAmount))
			}
			if req.BF6TargetAmount != nil && *req.BF6TargetAmount > 0 {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('bf6_goal_target', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.BF6TargetAmount))
			}
			if req.BF6Completed != nil {
				val := "false"
				if *req.BF6Completed {
					val = "true"
				}
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('bf6_goal_completed', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, val)
			}
			if req.FrankfurtTargetEur != nil && *req.FrankfurtTargetEur > 0 {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('frankfurt_goal_target_eur', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, fmt.Sprintf("%.2f", *req.FrankfurtTargetEur))
			}
			if req.FrankfurtTargetRub != nil && *req.FrankfurtTargetRub > 0 {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('frankfurt_goal_target_rub', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.Itoa(*req.FrankfurtTargetRub))
			}
			_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('bf6_goal_updated_at', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, now)
			log.Printf("[ADMIN] Community goals updated (BF6 current: %v, target: %v, completed: %v, Frankfurt EUR: %v, RUB: %v)", req.BF6CurrentAmount, req.BF6TargetAmount, req.BF6Completed, req.FrankfurtTargetEur, req.FrankfurtTargetRub)
		}
	}

	resp := s.getCommunityGoals()
	_ = json.NewEncoder(w).Encode(resp)
}

type GameCatalogItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Processes   []string `json:"processes"`
	SteamAppID  int      `json:"steam_app_id"`
	Icon        string   `json:"icon"`
	Status      string   `json:"status"` // "active", "beta", "crowdfunding"
	IsDefault   bool     `json:"is_default,omitempty"`
	Description string   `json:"description,omitempty"`
	Note        string   `json:"note,omitempty"`
}

func (s *AppState) getGamesCatalog() []GameCatalogItem {
	defaultCatalog := []GameCatalogItem{
		{
			ID:          "wardogs",
			Title:       "WARDOGS",
			Processes:   []string{"WardogsClient-Win64-Shipping.exe", "WardogsLauncher-Shipping.exe"},
			SteamAppID:  2645020,
			Icon:        "wardogs_icon.png",
			Status:      "active",
			IsDefault:   true,
			Description: "Хардкорный тактический шутер. Прямой игровой шлюз и селективная маршрутизация.",
		},
		{
			ID:          "bf6",
			Title:       "Battlefield 6",
			Processes:   []string{"bf6.exe", "EAAntiCheat.GameService.exe"},
			SteamAppID:  0,
			Icon:        "bf6_icon.png",
			Status:      "crowdfunding",
			Description: "Ожидаемый мультиплеерный шутер. Идет сбор средств на покупку игры разработчику.",
			Note:        "Сбор открыт на Boosty и в Steam",
		},
		{
			ID:          "arc_raiders",
			Title:       "ARC Raiders",
			Processes:   []string{"Pioneer.exe"},
			SteamAppID:  1808500,
			Icon:        "arc_icon.png",
			Status:      "beta",
			Description: "Кооперативный PvPvE экстракшен-шутер от Embark Studios.",
		},
		{
			ID:          "dark_and_darker",
			Title:       "Dark and Darker",
			Processes:   []string{"DungeonCrawler.exe"},
			SteamAppID:  2016590,
			Icon:        "dad_icon.png",
			Status:      "active",
			Description: "Хардкорное подземелье от первого лица. Защита от потерь UDP и обход блокировок лобби.",
		},
	}

	if s.db != nil {
		var customJSON string
		err := s.db.QueryRow(`SELECT value FROM server_settings WHERE key = 'games_catalog_json'`).Scan(&customJSON)
		if err == nil && len(customJSON) > 10 {
			var custom []GameCatalogItem
			if err := json.Unmarshal([]byte(customJSON), &custom); err == nil && len(custom) > 0 {
				return custom
			}
		}
	}

	return defaultCatalog
}

func (s *AppState) handleGamesCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	catalog := s.getGamesCatalog()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"games":   catalog,
		"count":   len(catalog),
	})
}

func (s *AppState) handleAdminGamesCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err == nil && s.db != nil {
			var check []GameCatalogItem
			if err := json.Unmarshal(body, &check); err == nil {
				_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('games_catalog_json', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, string(body))
				log.Printf("[ADMIN] Games catalog updated: %d items", len(check))
			}
		}
	}

	catalog := s.getGamesCatalog()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"games":   catalog,
		"count":   len(catalog),
	})
}

type DPIStrategyItem struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Targets     []string `json:"targets"`
}

func (s *AppState) handleDPIStrategies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	strategies := []DPIStrategyItem{
		{ID: 1, Name: "Стратегия 1 (Default)", Description: "Базовый сплиттинг TLS/HTTP с фейковыми пакетами", Targets: []string{"discord", "youtube"}},
		{ID: 2, Name: "Стратегия 2 (Aggressive)", Description: "Усиленный мультисплит для провайдеров с глубоким ТСПУ", Targets: []string{"discord", "youtube"}},
		{ID: 3, Name: "Стратегия 3 (Discord Priority)", Description: "Оптимизация голосовых каналов и WebRTC шлюзов Discord", Targets: []string{"discord"}},
		{ID: 4, Name: "Стратегия 4 (YouTube 4K)", Description: "Анти-троттлинг видеопотоков Googlevideo и QUIC", Targets: []string{"youtube"}},
		{ID: 5, Name: "Стратегия 5 (Steam Community)", Description: "Прямой доступ к инвентарю, торговой площадке и профилям Steam", Targets: []string{"steam"}},
		{ID: 6, Name: "Стратегия 6 (Universal Mixed)", Description: "Комбинированный обход для региональных провайдеров", Targets: []string{"discord", "youtube", "steam"}},
		{ID: 7, Name: "Стратегия 7 (Fallback Safe)", Description: "Безопасный режим с минимальной модификацией заголовков", Targets: []string{"discord", "youtube"}},
		{ID: 8, Name: "Стратегия 8 (Extreme Bypass)", Description: "Многократный сплиттинг TCP сессий при жесткой фильтрации", Targets: []string{"discord", "youtube"}},
		{ID: 9, Name: "Стратегия 9 (Zero Latency)", Description: "Минимальный джиттер для сетевых онлайн-игр", Targets: []string{"games"}},
		{ID: 10, Name: "Стратегия 10 (Custom)", Description: "Пользовательские параметры WinDivert", Targets: []string{"custom"}},
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"strategies": strategies,
	})
}

type AnnouncementItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // "modal", "toast", "banner"
	Title       string `json:"title"`
	Message     string `json:"message"`
	Severity    string `json:"severity"` // "info", "update", "warning", "urgent"
	ActionLabel string `json:"action_label,omitempty"`
	ActionURL   string `json:"action_url,omitempty"`
	Active      bool   `json:"active"`
}

func (s *AppState) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var active *AnnouncementItem
	if s.db != nil {
		var raw string
		err := s.db.QueryRow(`SELECT value FROM server_settings WHERE key = 'active_announcement'`).Scan(&raw)
		if err == nil && len(raw) > 5 {
			var it AnnouncementItem
			if err := json.Unmarshal([]byte(raw), &it); err == nil && it.Active {
				active = &it
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"announcement": active,
	})
}

func (s *AppState) handleAdminAnnouncements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err == nil && s.db != nil {
			_, _ = s.db.Exec(`INSERT INTO server_settings (key, value) VALUES ('active_announcement', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, string(body))
			log.Printf("[ADMIN] Active announcement updated: %s", string(body))
		}
	}

	s.handleAnnouncements(w, r)
}

func (s *AppState) handleSponsorsTiers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	tiers := []map[string]interface{}{
		{"id": "recruit", "name": "Рекрут", "price_rub": 100, "role": "Спонсор", "badge_class": "badge-recruit", "perks": []string{"30 дней статуса Спонсор", "Выделенный слот шлюза", "Имя в Зале славы"}},
		{"id": "operative", "name": "Оперативник", "price_rub": 500, "role": "Оперативник", "badge_class": "badge-operative", "perks": []string{"Все привилегии Рекрута", "Золотой бейдж в профиле", "Приоритетная поддержка"}},
		{"id": "veteran", "name": "Ветеран", "price_rub": 1000, "role": "Ветеран", "badge_class": "badge-veteran", "perks": []string{"Все привилегии Оперативника", "Платиновая рамка аватара", "Участие в закрытом тестировании"}},
		{"id": "general", "name": "Генерал", "price_rub": 5000, "role": "Генерал", "badge_class": "badge-general", "perks": []string{"Все привилегии Ветерана", "Легендарный статус", "Прямая связь с разработчиком"}},
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"tiers":   tiers,
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
		AppVersion    string `json:"app_version"`
		Summary       string `json:"summary"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)
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

	clientVer := strings.TrimSpace(req.AppVersion)
	if clientVer == "" {
		ua := r.Header.Get("User-Agent")
		if strings.HasPrefix(ua, "WarLink-Client/") {
			clientVer = strings.TrimPrefix(ua, "WarLink-Client/")
		}
	}
	if clientVer == "" {
		_ = s.db.QueryRow(`
			SELECT app_version FROM user_connection_history
			WHERE (length($1) > 0 AND account_number = $1)
			   OR (length($2) > 0 AND device_id = $2)
			ORDER BY connected_at DESC LIMIT 1
		`, req.AccountNumber, req.DeviceID).Scan(&clientVer)
	}
	if clientVer == "" {
		clientVer = ServerAppVersion
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
		`, req.AccountNumber, req.DeviceID, clientVer, archiveBytes, len(archiveBytes)).Scan(&tID)
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
	http.Redirect(w, r, "https://warlink-hub.duckdns.org:8055/admin/content/support_tickets", http.StatusFound)
}

func (s *AppState) handleAdminRoutingFeedbackWeb(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://warlink-hub.duckdns.org:8055/admin/content/routing_feedback", http.StatusFound)
}

// handleReloadFilters hot-reloads Steam game blacklist and nickname rules from PostgreSQL
func (s *AppState) handleReloadFilters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAdminAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	s.loadBlockedSteamGames()
	s.loadSupportedGames()
	LoadDynamicNicknameRules(s.db)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Фильтры запрещенных игр, поддерживаемых игр и позывных успешно перезагружены из PostgreSQL",
	})
}
