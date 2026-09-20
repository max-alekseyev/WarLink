package main

import (
	"bufio"
	"bytes"
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

	_ "github.com/lib/pq"
	"warlink/server/aclgen"
)

const (
	DefaultHMACSecret    = ""
	DefaultObfsPassword  = ""
	DefaultServerIP      = ""
	DefaultServerPorts   = "443,20000-30000"
	DefaultDueDate       = "2026-10-19T14:48:00Z"
	MaxActiveSessions    = 100
	MaxSessionsPerIP     = 2
	SessionTTL           = 24 * time.Hour
	SessionInactivityTTL = 5 * time.Minute
	PerUserRateDownBps   = 12500000 // 100 Mbps in bytes/sec
	PerUserRateUpBps     = 6250000  // 50 Mbps in bytes/sec
)

type ServerConfig struct {
	ServerIP        string `json:"server_ip"`
	ServerName      string `json:"server_name"`
	ServerLocation  string `json:"server_location"`
	ServerPorts     string `json:"server_ports"`
	AezaAPIKey      string `json:"aeza_api_key"`
	HMACSecret      string `json:"hmac_secret"`
	ObfsPassword    string `json:"obfs_password"`
	DashboardKey    string `json:"dashboard_key"`
	FallbackDueDate string `json:"fallback_due_date"`
	DonateURL       string `json:"donate_url"`
	DonateAmountRub int    `json:"donate_amount_rub"`
	MaxSessions     int    `json:"max_sessions"`
	ListenPublic    string `json:"listen_public"`
	ListenInternal  string `json:"listen_internal"`
	DatabaseURL     string `json:"database_url"`
}

type SessionInfo struct {
	DeviceID  string    `json:"device_id"`
	Token     string    `json:"token"`
	ClientIP  string    `json:"client_ip"`
	Game      string    `json:"game"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
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

	// Server load telemetry
	prevCPUTotal uint64
	prevCPUIdle  uint64
	prevNetRx    int64
	prevNetTx    int64
	prevNetTime  time.Time
	latestLoad   *LoadSnapshot
	loadMu       sync.RWMutex

	// Dynamic Feature Toggles
	enableDonate bool
	enableVoting bool
	featureMu    sync.RWMutex

	// Telemetry and Analytics Counters
	metricRequestsTotal       uint64
	metricRejectionsRateLimit uint64
	metricRejectionsIPLimit   uint64
	metricRejectionsBadSig    uint64
	metricRejectionsCapacity  uint64
	metricInvoicesCreated     uint64
	metricDonationsPaid       uint64
	metricDonationsRub        uint64
	cachedPrice               int
	prevHyTraffic             map[string]UserTrafficStats
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
		rateLimiter:   NewIPRateLimiter(5, 1*time.Minute),
		enableDonate:  true,
		enableVoting:  true,
		prevHyTraffic: make(map[string]UserTrafficStats),
	}
	state.loadConfig(cfgPath)

	// Sync ACL and load profiles on startup
	if err := state.syncACL(listsDir); err != nil {
		log.Printf("[ACL] Warning: startup ACL sync: %v", err)
	}

	// Background session cleaner
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			state.cleanupExpiredSessions()
		}
	}()

	// Background Aeza due date updater
	go func() {
		state.updateDueDateFromAeza()
		ticker := time.NewTicker(3 * time.Hour)
		for range ticker.C {
			state.updateDueDateFromAeza()
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
	publicMux.HandleFunc("/api/v1/admin/features", state.handleAdminFeatures)
	publicMux.HandleFunc("/api/v1/admin/settings", state.handleAdminSettings)
	publicMux.HandleFunc("/metrics", state.handleMetrics)
	publicMux.HandleFunc("/dashboard", state.handleDashboard)
	publicMux.HandleFunc("/dashboard/", state.handleDashboard)

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
					if item.Price > 0 {
						s.cachedPrice = item.Price
					}
					s.mu.Unlock()
					log.Printf("[AEZA] Updated due date from API: %s (status: %s, price: %d RUB)", item.ExpiresAt, item.Status, item.Price)
					break
				}
			}
		}
	}
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

func (s *AppState) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	activeCount := len(s.sessions)
	dueDate := s.cachedDue
	dueDateStr := s.cachedDueStr
	s.mu.RUnlock()

	s.featureMu.RLock()
	enDonate := s.enableDonate
	enVoting := s.enableVoting
	s.featureMu.RUnlock()

	daysLeft := int(time.Until(dueDate).Hours() / 24)
	if daysLeft < 0 {
		daysLeft = 0
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "online",
		"location":          s.cfg.ServerLocation,
		"ping_hint_ms":      27,
		"active_sessions":   activeCount,
		"max_sessions":      s.cfg.MaxSessions,
		"server_ip":         s.getPublicIP(r),
		"server_ports":      s.cfg.ServerPorts,
		"due_date":          dueDateStr,
		"days_left":         daysLeft,
		"donate_amount_rub": s.cfg.DonateAmountRub,
		"enable_donate":     enDonate,
		"enable_voting":     enVoting,
	})
}

type SessionRequest struct {
	DeviceID  string `json:"device_id"`
	Timestamp int64  `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Game      string `json:"game,omitempty"`
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

	s.mu.Lock()
	defer s.mu.Unlock()

	pubIP := s.getPublicIP(r)
	serverField := ":443"
	if pubIP != "" {
		serverField = fmt.Sprintf("%s:443", pubIP)
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
			sess.ExpiresAt = time.Now().Add(SessionTTL)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":          sess.Token,
				"server":         serverField,
				"server_ports":   serverPorts,
				"obfs":           s.cfg.ObfsPassword,
				"expires_in_sec": int(SessionTTL.Seconds()),
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
	if ipSessions >= MaxSessionsPerIP {
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

	// Check global concurrency cap
	if len(s.sessions) >= MaxActiveSessions {
		atomic.AddUint64(&s.metricRejectionsCapacity, 1)
		s.recordCounterAsync("rejections_capacity")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "server_full",
			"message": "Все 100 слотов шлюза заняты. Пожалуйста, подождите освобождения места.",
		})
		return
	}

	// Generate new secure session token
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	newToken := "wl_tok_" + hex.EncodeToString(tokenBytes)

	sess := &SessionInfo{
		DeviceID:  req.DeviceID,
		Token:     newToken,
		ClientIP:  clientIP,
		Game:      targetGame,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		ExpiresAt: time.Now().Add(SessionTTL),
	}
	s.sessions[newToken] = sess
	s.deviceTokens[req.DeviceID] = newToken

	log.Printf("[SESSION] Allocated slot for device %s (game: %s) from IP %s (Active: %d/%d, IP sessions: %d/%d)",
		req.DeviceID, targetGame, clientIP, len(s.sessions), MaxActiveSessions, ipSessions+1, MaxSessionsPerIP)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":          newToken,
		"server":         serverField,
		"server_ports":   serverPorts,
		"obfs":           s.cfg.ObfsPassword,
		"expires_in_sec": int(SessionTTL.Seconds()),
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
		// Verify connecting client IP matches the session's recorded IP
		connectingIP, _, _ := net.SplitHostPort(req.Addr)
		if connectingIP == "" {
			connectingIP = req.Addr
		}
		if sess.ClientIP != "" && connectingIP != "" && sess.ClientIP != connectingIP {
			s.mu.Unlock()
			log.Printf("[AUTH] REJECTED token %s: IP mismatch (issued for %s, connected from %s)", req.Auth, sess.ClientIP, connectingIP)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false})
			return
		}

		sess.LastSeen = time.Now()
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

	s.mu.RLock()
	apiKey := s.cfg.AezaAPIKey
	s.mu.RUnlock()

	if apiKey != "" {
		amount := s.cfg.DonateAmountRub
		if amount <= 0 {
			amount = 98
		}
		// Aeza API v2 uses minor currency units (cents). 75 cents EUR ≈ 98 RUB.
		aezaCents := 75
		if amount != 98 {
			// If admin configured custom RUB amount, scale cents proportionally (approx 1 EUR ≈ 130 RUB)
			aezaCents = int(float64(amount) / 1.3066)
			if aezaCents < 50 {
				aezaCents = 50
			}
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
					actualRub := invResp.Amount
					if actualRub <= 0 {
						actualRub = amount
					}

					atomic.AddUint64(&s.metricInvoicesCreated, 1)
					s.recordCounterAsync("invoices_created")

					log.Printf("[DONATE] Created Aeza SBP invoice #%d for %d RUB (charged %d cents): %s",
						invResp.ID, actualRub, aezaCents, invResp.Payload.URL)
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"success": true,
						"pay_url": invResp.Payload.URL,
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

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"target_votes":    50,
			"max_user_votes":  3,
			"user_votes_used": userVotesUsed,
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

		// Upsert game_suggestions
		var newVotes int
		var currStatus string
		err = tx.QueryRow(`
			INSERT INTO game_suggestions (steam_app_id, title, icon_url, votes_count, status, created_at, updated_at)
			VALUES ($1, $2, $3, 1, 'voting', NOW(), NOW())
			ON CONFLICT (steam_app_id) DO UPDATE
			SET votes_count = game_suggestions.votes_count + 1,
			    title = EXCLUDED.title,
			    icon_url = CASE WHEN EXCLUDED.icon_url <> '' THEN EXCLUDED.icon_url ELSE game_suggestions.icon_url END,
			    updated_at = NOW()
			RETURNING votes_count, status
		`, req.SteamAppID, req.Title, iconURL).Scan(&newVotes, &currStatus)
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
		_, err = tx.Exec("INSERT INTO device_votes (device_id, steam_app_id, client_ip, created_at) VALUES ($1, $2, $3, NOW())", req.DeviceID, req.SteamAppID, clientIP)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[VOTES] Device %s voted for %s (AppID: %d, Total: %d)", req.DeviceID, req.Title, req.SteamAppID, newVotes)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"votes_count": newVotes,
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
				SET votes_count = GREATEST(0, votes_count - 1),
				    status = CASE WHEN votes_count - 1 < 50 AND status = 'queue_integration' THEN 'voting' ELSE status END,
				    updated_at = NOW()
				WHERE steam_app_id = $1
				RETURNING votes_count
			`, req.SteamAppID).Scan(&newVotes)
			if err == nil {
				log.Printf("[VOTES] Device %s unvoted for AppID %d (New total: %d)", req.DeviceID, req.SteamAppID, newVotes)
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
	if hyResp, err := hyClient.Get("http://127.0.0.1:9090/traffic"); err == nil {
		var hyData map[string]UserTrafficStats
		if err := json.NewDecoder(hyResp.Body).Decode(&hyData); err == nil {
			s.mu.Lock()
			if s.prevHyTraffic == nil {
				s.prevHyTraffic = make(map[string]UserTrafficStats)
			}
			for devID, cur := range hyData {
				prev, exists := s.prevHyTraffic[devID]
				// A device is considered actively transmitting ONLY if its byte count increased
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
	defer s.mu.Unlock()

	now := time.Now()
	for token, sess := range s.sessions {
		if activeDevices[sess.DeviceID] {
			sess.LastSeen = now
			continue
		}
		if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeen) > SessionInactivityTTL {
			delete(s.sessions, token)
			delete(s.deviceTokens, sess.DeviceID)
			log.Printf("[CLEANUP] Released inactive slot for device %s (Active: %d/%d)", sess.DeviceID, len(s.sessions), MaxActiveSessions)
		}
	}
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

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}

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

	// Verify connecting client IP matches session IP
	if sess.ClientIP != "" && clientIP != "" && sess.ClientIP != clientIP {
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

	var activeProfiles []aclgen.Profile
	for _, p := range profiles {
		if strings.EqualFold(p.ID, targetGame) || strings.EqualFold(p.Name, targetGame) || p.ID == "socials" || p.ID == "wardogs" {
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

	CREATE TABLE IF NOT EXISTS daily_active_devices (
		device_id TEXT NOT NULL,
		seen_date DATE NOT NULL DEFAULT CURRENT_DATE,
		client_ip TEXT,
		last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY(device_id, seen_date)
	);
	CREATE INDEX IF NOT EXISTS idx_daily_active_date ON daily_active_devices(seen_date);
	`
	if _, err := s.db.Exec(schema); err != nil {
		log.Printf("[DB] Error initializing schema: %v", err)
	} else {
		log.Printf("[DB] Database tables initialized successfully")
	}
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
	EnableDonate *bool `json:"enable_donate"`
	EnableVoting *bool `json:"enable_voting"`
}

func (s *AppState) handleAdminFeatures(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	key := r.URL.Query().Get("key")
	if s.cfg.DashboardKey == "" || key != s.cfg.DashboardKey {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.featureMu.RLock()
		enDonate := s.enableDonate
		enVoting := s.enableVoting
		s.featureMu.RUnlock()

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"enable_donate": enDonate,
			"enable_voting": enVoting,
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
		currentDonate := s.enableDonate
		currentVoting := s.enableVoting
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
		}

		log.Printf("[ADMIN] Dynamic feature toggles updated: Donate=%v, Voting=%v", currentDonate, currentVoting)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"enable_donate": currentDonate,
			"enable_voting": currentVoting,
		})

	default:
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
	}
}

type AdminSettingsPayload struct {
	EnableDonate    *bool   `json:"enable_donate,omitempty"`
	EnableVoting    *bool   `json:"enable_voting,omitempty"`
	DonateAmountRub *int    `json:"donate_amount_rub,omitempty"`
	MaxSessions     *int    `json:"max_sessions,omitempty"`
	ServerName      *string `json:"server_name,omitempty"`
	ServerLocation  *string `json:"server_location,omitempty"`
	DonationsPaid   *uint64 `json:"donations_paid,omitempty"`
	DonationsRub    *uint64 `json:"donations_rub,omitempty"`
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

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	isLocal := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" || clientIP == ""
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.Header.Get("X-Dashboard-Key")
	}

	if !isLocal && (s.cfg.DashboardKey == "" || key != s.cfg.DashboardKey) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.featureMu.RLock()
		enDonate := s.enableDonate
		enVoting := s.enableVoting
		s.featureMu.RUnlock()

		s.mu.RLock()
		donateAmt := s.cfg.DonateAmountRub
		maxSess := s.cfg.MaxSessions
		srvName := s.cfg.ServerName
		srvLoc := s.cfg.ServerLocation
		activeSess := len(s.sessions)
		s.mu.RUnlock()

		var dau, wau int
		if s.db != nil {
			_ = s.db.QueryRow("SELECT COUNT(*) FROM daily_active_devices WHERE seen_date = CURRENT_DATE").Scan(&dau)
			_ = s.db.QueryRow("SELECT COUNT(DISTINCT device_id) FROM daily_active_devices WHERE seen_date >= CURRENT_DATE - INTERVAL '7 days'").Scan(&wau)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           true,
			"enable_donate":     enDonate,
			"enable_voting":     enVoting,
			"donate_amount_rub": donateAmt,
			"max_sessions":      maxSess,
			"active_sessions":   activeSess,
			"server_name":       srvName,
			"server_location":   srvLoc,
			"invoices_created":  atomic.LoadUint64(&s.metricInvoicesCreated),
			"donations_paid":    atomic.LoadUint64(&s.metricDonationsPaid),
			"donations_rub":     atomic.LoadUint64(&s.metricDonationsRub),
			"dau":               dau,
			"wau":               wau,
		})

	case http.MethodPost:
		var req AdminSettingsPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad_request"})
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
		s.featureMu.RUnlock()

		s.mu.RLock()
		donateAmt := s.cfg.DonateAmountRub
		maxSess := s.cfg.MaxSessions
		srvName := s.cfg.ServerName
		srvLoc := s.cfg.ServerLocation
		activeSess := len(s.sessions)
		s.mu.RUnlock()

		log.Printf("[ADMIN] Settings updated live: Donate=%v, Voting=%v, DonateAmt=%d, MaxSessions=%d",
			enDonate, enVoting, donateAmt, maxSess)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":           true,
			"enable_donate":     enDonate,
			"enable_voting":     enVoting,
			"donate_amount_rub": donateAmt,
			"max_sessions":      maxSess,
			"active_sessions":   activeSess,
			"server_name":       srvName,
			"server_location":   srvLoc,
			"donations_paid":    atomic.LoadUint64(&s.metricDonationsPaid),
			"donations_rub":     atomic.LoadUint64(&s.metricDonationsRub),
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
	s.featureMu.RUnlock()

	var totalVotes, gamesVoting, gamesGraduated int
	var dau, wau int
	if s.db != nil {
		_ = s.db.QueryRow("SELECT COALESCE(SUM(votes_count), 0) FROM game_suggestions").Scan(&totalVotes)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM game_suggestions WHERE status = 'voting'").Scan(&gamesVoting)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM game_suggestions WHERE status = 'queue_integration'").Scan(&gamesGraduated)
		_ = s.db.QueryRow("SELECT COUNT(*) FROM daily_active_devices WHERE seen_date = CURRENT_DATE").Scan(&dau)
		_ = s.db.QueryRow("SELECT COUNT(DISTINCT device_id) FROM daily_active_devices WHERE seen_date >= CURRENT_DATE - INTERVAL '7 days'").Scan(&wau)
	}

	s.mu.RLock()
	sessionsByGame := make(map[string]int)
	for _, sess := range s.sessions {
		g := sess.Game
		if g == "" {
			g = "wardogs"
		}
		sessionsByGame[g]++
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

	sb.WriteString("# HELP warlink_server_days_left Days remaining until server rent expiration\n")
	sb.WriteString("# TYPE warlink_server_days_left gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_days_left %d\n\n", daysLeft))

	sb.WriteString("# HELP warlink_donate_amount_rub Current donate amount in rubles\n")
	sb.WriteString("# TYPE warlink_donate_amount_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_donate_amount_rub %d\n\n", donateAmt))

	sb.WriteString("# HELP warlink_server_monthly_price_rub Monthly VPS rental price in rubles\n")
	sb.WriteString("# TYPE warlink_server_monthly_price_rub gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_server_monthly_price_rub %d\n\n", srvPrice))

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

	sb.WriteString("# HELP warlink_total_votes Total community votes cast\n")
	sb.WriteString("# TYPE warlink_total_votes gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_total_votes %d\n\n", totalVotes))

	sb.WriteString("# HELP warlink_games_in_voting Games currently in voting\n")
	sb.WriteString("# TYPE warlink_games_in_voting gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_games_in_voting %d\n\n", gamesVoting))

	sb.WriteString("# HELP warlink_games_graduated Games that won voting\n")
	sb.WriteString("# TYPE warlink_games_graduated gauge\n")
	sb.WriteString(fmt.Sprintf("warlink_games_graduated %d\n\n", gamesGraduated))

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

func (s *AppState) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DashboardKey == "" || r.URL.Query().Get("key") != s.cfg.DashboardKey {
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
		DeviceID     string `json:"device_id"`
		Game         string `json:"game"`
		ClientIP     string `json:"client_ip"`
		ConnectedAt  string `json:"connected_at"`
		DurationSec  int    `json:"duration_sec"`
		DurationDesc string `json:"duration_desc"`
	}

	activePlayers := make([]ActivePlayerInfo, 0)
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

			activePlayers = append(activePlayers, ActivePlayerInfo{
				DeviceID:     devID,
				Game:         gameName,
				ClientIP:     sess.ClientIP,
				ConnectedAt:  sess.CreatedAt.Format(time.RFC3339),
				DurationSec:  dur,
				DurationDesc: desc,
			})
		}
	}
	s.mu.RUnlock()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"live":           live,
		"history":        history,
		"active_players": activePlayers,
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

        <!-- Dynamic Client Feature Controls (Live Zero Restart) -->
        <div class="features-bar" style="background: var(--bg-card); border: 1px solid var(--border); border-radius: 2px; padding: 12px 18px; margin-bottom: 24px; display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px;">
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
                <span style="font-weight: 600; font-size: 11px; letter-spacing: 0.5px; text-transform: uppercase; color: var(--text-muted);">Переключатели функций клиента (Zero-Restart)</span>
            </div>
            <div style="display: flex; align-items: center; gap: 16px;">
                <div style="display: flex; align-items: center; gap: 10px;">
                    <span style="font-size: 12px; color: var(--text-main);">Кнопка доната:</span>
                    <button id="toggle-donate-btn" class="btn" onclick="toggleFeature('enable_donate')" style="font-family: var(--font-mono); min-width: 80px; text-align: center; font-weight: 700;">ВКЛ</button>
                </div>
                <div style="width: 1px; height: 18px; background: var(--border);"></div>
                <div style="display: flex; align-items: center; gap: 10px;">
                    <span style="font-size: 12px; color: var(--text-main);">Голосование игр:</span>
                    <button id="toggle-voting-btn" class="btn" onclick="toggleFeature('enable_voting')" style="font-family: var(--font-mono); min-width: 80px; text-align: center; font-weight: 700;">ВКЛ</button>
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
                        return '<tr>' +
                            '<td style="font-family: var(--font-mono); font-weight: 600;">' + p.device_id + '</td>' +
                            '<td><strong style="color: var(--accent);">' + p.game + '</strong></td>' +
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

        let featuresState = { enable_donate: true, enable_voting: true };

        async function loadFeatures() {
            try {
                let url = '/api/v1/admin/features';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const res = await fetch(url);
                if (res.ok) {
                    const data = await res.json();
                    if (data && data.success) {
                        featuresState.enable_donate = data.enable_donate;
                        featuresState.enable_voting = data.enable_voting;
                        renderFeatureButtons();
                    }
                }
            } catch (err) {
                console.error('Failed to load features:', err);
            }
        }

        function renderFeatureButtons() {
            const dBtn = document.getElementById('toggle-donate-btn');
            if (dBtn) {
                if (featuresState.enable_donate) {
                    dBtn.textContent = 'ВКЛ';
                    dBtn.style.background = '#112211';
                    dBtn.style.borderColor = '#224422';
                    dBtn.style.color = '#4ade80';
                } else {
                    dBtn.textContent = 'ВЫКЛ';
                    dBtn.style.background = '#221111';
                    dBtn.style.borderColor = '#442222';
                    dBtn.style.color = '#f87171';
                }
            }
            const vBtn = document.getElementById('toggle-voting-btn');
            if (vBtn) {
                if (featuresState.enable_voting) {
                    vBtn.textContent = 'ВКЛ';
                    vBtn.style.background = '#112211';
                    vBtn.style.borderColor = '#224422';
                    vBtn.style.color = '#4ade80';
                } else {
                    vBtn.textContent = 'ВЫКЛ';
                    vBtn.style.background = '#221111';
                    vBtn.style.borderColor = '#442222';
                    vBtn.style.color = '#f87171';
                }
            }
        }

        async function toggleFeature(name) {
            const nextVal = !featuresState[name];
            const dBtn = document.getElementById('toggle-donate-btn');
            const vBtn = document.getElementById('toggle-voting-btn');
            const targetBtn = (name === 'enable_donate') ? dBtn : vBtn;
            if (targetBtn) targetBtn.textContent = '...';

            try {
                let url = '/api/v1/admin/features';
                if (secretKey) url += '?key=' + encodeURIComponent(secretKey);
                const payload = {};
                payload[name] = nextVal;
                const res = await fetch(url, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                if (res.ok) {
                    const data = await res.json();
                    if (data && data.success) {
                        featuresState.enable_donate = data.enable_donate;
                        featuresState.enable_voting = data.enable_voting;
                    }
                }
            } catch (err) {
                console.error('Failed to update feature:', err);
            }
            renderFeatureButtons();
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

