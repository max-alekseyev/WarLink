package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"warlink/internal/adminweb"
)

// Brute-force protection for login
var (
	loginAttemptsMu sync.Mutex
	loginAttempts   = make(map[string]*loginAttemptTracker)
)

type loginAttemptTracker struct {
	Count        int
	BlockedUntil time.Time
}

// GenerateTOTPSecret generates a cryptographically secure 20-byte base32 TOTP secret.
func GenerateTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// ValidateTOTP validates a 6-digit TOTP code against a base32 secret (RFC 6238).
func ValidateTOTP(secret string, code string) bool {
	cleanCode := strings.TrimSpace(code)
	if len(cleanCode) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}

	t := time.Now().Unix() / 30
	// Tolerant window: -1, 0, +1 (±30 seconds)
	for i := int64(-1); i <= 1; i++ {
		step := t + i
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(step))

		mac := hmac.New(sha1.New, key)
		mac.Write(buf[:])
		hash := mac.Sum(nil)

		offset := hash[len(hash)-1] & 0x0f
		truncated := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff
		otp := truncated % 1000000
		otpStr := fmt.Sprintf("%06d", otp)

		if hmac.Equal([]byte(otpStr), []byte(cleanCode)) {
			return true
		}
	}
	return false
}

// GenerateBackupCodes generates 5 random one-time recovery codes formatted as WL-XXXX-XXXX.
func GenerateBackupCodes(count int) []string {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		h := hex.EncodeToString(b)
		codes[i] = fmt.Sprintf("WL-%s-%s", strings.ToUpper(h[0:4]), strings.ToUpper(h[4:8]))
	}
	return codes
}

func getCleanIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return strings.TrimSpace(ip)
}

// checkAdminAuth verifies whether a request comes from an authenticated admin session
// or from an internal localhost service (Discord bot).
func (s *AppState) checkAdminAuth(r *http.Request) bool {
	ok, _ := s.checkAdminAuthWithKind(r)
	return ok
}

func (s *AppState) checkAdminAuthWithKind(r *http.Request) (bool, string) {
	// 1. Check human admin session cookie
	if cookie, err := r.Cookie("admin_session_token"); err == nil && cookie.Value != "" {
		if s.validateAdminSession(cookie.Value) {
			return true, "session"
		}
	}

	// 2. Check legacy admin_key cookie or DashboardKey header (fallback)
	if cookie, err := r.Cookie("admin_key"); err == nil && cookie.Value != "" {
		if s.cfg.DashboardKey != "" && cookie.Value == s.cfg.DashboardKey {
			return true, "admin_key"
		}
	}
	if qk := r.Header.Get("X-Dashboard-Key"); qk != "" && s.cfg.DashboardKey != "" && qk == s.cfg.DashboardKey {
		return true, "dashboard_key_header"
	}
	if auth := r.Header.Get("Authorization"); auth != "" && s.cfg.DashboardKey != "" {
		if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == s.cfg.DashboardKey {
			return true, "bearer_token"
		}
	}

	// 3. Check internal service token strictly from localhost (127.0.0.1 or ::1)
	clientIP := getCleanIP(r.RemoteAddr)
	if clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" {
		serviceToken := r.Header.Get("X-Service-Token")
		if serviceToken == "" {
			serviceToken = r.Header.Get("X-Dashboard-Key")
		}
		expectedToken := s.cfg.InternalServiceToken
		if expectedToken == "" {
			expectedToken = s.cfg.DashboardKey
		}
		if expectedToken != "" && serviceToken == expectedToken {
			return true, "internal_service"
		}
	}

	return false, ""
}

// validateAdminSession verifies if a session token exists in admin_sessions and has not expired.
func (s *AppState) validateAdminSession(token string) bool {
	if s.db == nil || token == "" {
		return false
	}
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM admin_sessions
		WHERE id = $1 AND expires_at > NOW()
	`, token).Scan(&count)
	return err == nil && count > 0
}

// handleAdminAuthStatus returns setup state and current auth state.
func (s *AppState) handleAdminAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.db == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"needs_setup": false, "authenticated": false})
		return
	}

	var userCount int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&userCount)
	if userCount == 0 {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"needs_setup":   true,
			"authenticated": false,
		})
		return
	}

	authOK := s.checkAdminAuth(r)
	var username string
	if authOK {
		if cookie, err := r.Cookie("admin_session_token"); err == nil {
			_ = s.db.QueryRow(`
				SELECT u.username FROM admin_sessions s
				JOIN admin_users u ON s.user_id = u.id
				WHERE s.id = $1 AND s.expires_at > NOW()
			`, cookie.Value).Scan(&username)
		}
		if username == "" {
			username = "admin"
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"needs_setup":   false,
		"authenticated": authOK,
		"username":      username,
	})
}

// handleAdminAuthSetupInit initializes the TOTP setup wizard.
func (s *AppState) handleAdminAuthSetupInit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if s.db == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&count)
	if count > 0 {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Мастер настройки уже был завершен ранее. Доступ закрыт.",
		})
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "admin"
	}

	totpSecret := GenerateTOTPSecret()
	backupCodes := GenerateBackupCodes(5)
	otpAuthURL := fmt.Sprintf("otpauth://totp/WarLink:%s?secret=%s&issuer=WarLink", username, totpSecret)

	qrSVG, errSVG := generateQRCodeSVG(otpAuthURL)
	if errSVG != nil {
		log.Printf("[ADMIN-AUTH] Warning: failed to generate QR SVG: %v", errSVG)
	}

	var qrPNG string
	pngBytes, errPNG := qrcode.Encode(otpAuthURL, qrcode.Medium, 256)
	if errPNG == nil {
		qrPNG = "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"totp_secret":  totpSecret,
		"backup_codes": backupCodes,
		"otpauth_url":  otpAuthURL,
		"qr_svg":       qrSVG,
		"qr_png":       qrPNG,
	})
}

// generateQRCodeSVG generates a clean inline vector SVG for a QR code string.
func generateQRCodeSVG(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bitmap := qr.Bitmap()
	size := len(bitmap)
	if size == 0 {
		return "", fmt.Errorf("empty qr bitmap")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" style="width:100%%;height:100%%;display:block;">`, size, size))
	sb.WriteString(`<path fill="#000000" d="`)
	for y, row := range bitmap {
		for x, isBlack := range row {
			if isBlack {
				sb.WriteString(fmt.Sprintf("M%d %dh1v1h-1z", x, y))
			}
		}
	}
	sb.WriteString(`"/></svg>`)
	return sb.String(), nil
}

// handleAdminAuthSetupVerify finalizes the admin setup and verifies the first OTP code.
func (s *AppState) handleAdminAuthSetupVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if s.db == nil {
		http.Error(w, `{"error":"database_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&count)
	if count > 0 {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Мастер настройки уже завершен.",
		})
		return
	}

	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		TotpSecret  string   `json:"totp_secret"`
		BackupCodes []string `json:"backup_codes"`
		Code        string   `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Логин или пароль не удовлетворяют требованиям"})
		return
	}

	if !ValidateTOTP(req.TotpSecret, req.Code) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Неверный проверочный 6-значный код TOTP"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Ошибка хэширования пароля"})
		return
	}

	bcJSON, _ := json.Marshal(req.BackupCodes)

	var userID int
	err = s.db.QueryRow(`
		INSERT INTO admin_users (username, password_hash, totp_secret, backup_codes, created_at, last_login_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		RETURNING id
	`, req.Username, string(hash), req.TotpSecret, string(bcJSON)).Scan(&userID)
	if err != nil {
		log.Printf("[ADMIN-AUTH] Error creating admin user: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Ошибка сохранения администратора"})
		return
	}

	// Create initial session token
	sessionToken := generateSessionToken()
	_, _ = s.db.Exec(`
		INSERT INTO admin_sessions (id, user_id, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + INTERVAL '30 days')
	`, sessionToken, userID, r.UserAgent(), getCleanIP(r.RemoteAddr))

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   30 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // set to true via reverse proxy
	})

	log.Printf("[ADMIN-AUTH] Initial setup wizard successfully completed for admin '%s'", req.Username)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleAdminAuthLogin authenticates an admin using Login + Password (bcrypt) + OTP (TOTP or Backup Code).
func (s *AppState) handleAdminAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	clientIP := getCleanIP(r.RemoteAddr)
	loginAttemptsMu.Lock()
	tracker, exists := loginAttempts[clientIP]
	if exists && tracker.BlockedUntil.After(time.Now()) {
		loginAttemptsMu.Unlock()
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Слишком много неверных попыток. Доступ заблокирован на 15 минут.",
		})
		return
	}
	loginAttemptsMu.Unlock()

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	username := strings.TrimSpace(req.Username)
	code := strings.TrimSpace(req.Code)

	var (
		userID      int
		passHash    string
		totpSecret  string
		backupCodes []string
		bcRaw       []byte
	)

	err := s.db.QueryRow(`
		SELECT id, password_hash, totp_secret, backup_codes
		FROM admin_users
		WHERE username = $1
	`, username).Scan(&userID, &passHash, &totpSecret, &bcRaw)
	if err != nil {
		s.recordFailedLoginAttempt(clientIP)
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Неверный логин или пароль"})
		return
	}

	// Verify Password with bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(passHash), []byte(req.Password)); err != nil {
		s.recordFailedLoginAttempt(clientIP)
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Неверный логин или пароль"})
		return
	}

	// Verify 2FA (TOTP RFC 6238) or Backup Code
	totpValid := ValidateTOTP(totpSecret, code)
	backupUsed := false

	if !totpValid {
		if len(bcRaw) > 0 {
			_ = json.Unmarshal(bcRaw, &backupCodes)
		}
		for i, bc := range backupCodes {
			if strings.EqualFold(bc, code) {
				backupUsed = true
				// Remove used backup code
				backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
				newBCRaw, _ := json.Marshal(backupCodes)
				_, _ = s.db.Exec(`UPDATE admin_users SET backup_codes = $1 WHERE id = $2`, string(newBCRaw), userID)
				break
			}
		}
	}

	if !totpValid && !backupUsed {
		s.recordFailedLoginAttempt(clientIP)
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Неверный код 2FA (TOTP)"})
		return
	}

	// Login successful: reset failed attempts
	loginAttemptsMu.Lock()
	delete(loginAttempts, clientIP)
	loginAttemptsMu.Unlock()

	sessionToken := generateSessionToken()
	_, _ = s.db.Exec(`
		INSERT INTO admin_sessions (id, user_id, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + INTERVAL '30 days')
	`, sessionToken, userID, r.UserAgent(), clientIP)
	_, _ = s.db.Exec(`UPDATE admin_users SET last_login_at = NOW() WHERE id = $1`, userID)

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session_token",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   30 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("[ADMIN-AUTH] Successful login for admin '%s' from IP %s", username, clientIP)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (s *AppState) recordFailedLoginAttempt(ip string) {
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	t, exists := loginAttempts[ip]
	if !exists {
		t = &loginAttemptTracker{}
		loginAttempts[ip] = t
	}
	t.Count++
	if t.Count >= 5 {
		t.BlockedUntil = time.Now().Add(15 * time.Minute)
		log.Printf("[ADMIN-AUTH] IP %s blocked for 15 minutes due to 5 failed login attempts", ip)
	}
}

// handleAdminAuthLogout clears the session.
func (s *AppState) handleAdminAuthLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if cookie, err := r.Cookie("admin_session_token"); err == nil && cookie.Value != "" {
		if s.db != nil {
			_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE id = $1`, cookie.Value)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleAdminAuthKeyLogin authenticates an admin directly using the Master Dashboard Key.
func (s *AppState) handleAdminAuthKeyLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" || s.cfg.DashboardKey == "" || key != s.cfg.DashboardKey {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Неверный секретный ключ"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_key",
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   30 * 24 * 3600,
		SameSite: http.SameSiteLaxMode,
	})
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "username": "admin"})
}

// handleAdminWebSPA serves the native embedded control panel SPA.
func (s *AppState) handleAdminWebSPA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if qk := strings.TrimSpace(r.URL.Query().Get("key")); qk != "" {
		if s.cfg.DashboardKey != "" && qk == s.cfg.DashboardKey {
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_key",
				Value:    qk,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   30 * 24 * 3600,
				SameSite: http.SameSiteLaxMode,
			})
		}
	}

	if strings.HasSuffix(r.URL.Path, "style.css") {
		css, err := adminweb.GetStyleCSS()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(css)
		return
	}
	if strings.HasSuffix(r.URL.Path, "app.js") {
		js, err := adminweb.GetAppJS()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(js)
		return
	}

	html, err := adminweb.GetIndexHTML()
	if err != nil {
		http.Error(w, "Admin Web Panel not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

// handleGrafanaRedirect handles backwards-compatible redirects from Grafana links to native SPA views.
func (s *AppState) handleGrafanaRedirect(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	targetHash := "#view-special-projects"

	if strings.Contains(path, "support_tickets") || strings.Contains(path, "dialogs") {
		targetHash = "#view-support"
	} else if strings.Contains(path, "accounts") || strings.Contains(path, "sponsors") {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		accountID := ""
		for i, part := range parts {
			if (part == "accounts" || part == "sponsors") && i+1 < len(parts) {
				accountID = strings.TrimSpace(parts[i+1])
				break
			}
		}
		if accountID != "" {
			targetHash = fmt.Sprintf("#view-account?account=%s", accountID)
		} else {
			targetHash = "#view-monetization"
		}
	} else if strings.Contains(path, "routing_feedback") {
		targetHash = "#view-support"
	} else if strings.Contains(path, "game_suggestions") {
		targetHash = "#view-games-kanban"
	} else if strings.Contains(path, "cluster_nodes") || strings.Contains(path, "nodes") {
		targetHash = "#view-nodes"
	}

	http.Redirect(w, r, "/admin"+targetHash, http.StatusFound)
}

// handleAdminGamesKanbanMove moves a game suggestion between Kanban columns.
func (s *AppState) handleAdminGamesKanbanMove(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		SteamAppID int    `json:"steam_app_id"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SteamAppID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	validStatuses := map[string]bool{
		"voting":            true,
		"queue_integration": true,
		"supported":         true,
		"forbidden":         true,
	}
	if !validStatuses[req.Status] {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_status"})
		return
	}

	// Update game_suggestions status.
	// PostgreSQL trigger trg_fn_game_suggestions_after_sync automatically adds/removes
	// entries in supported_games and forbidden_steam_games!
	_, err := s.db.Exec(`
		UPDATE game_suggestions
		SET status = $1, updated_at = NOW()
		WHERE steam_app_id = $2
	`, req.Status, req.SteamAppID)
	if err != nil {
		log.Printf("[KANBAN] Error updating game status: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
		return
	}

	// Hot reload in-memory game filters in server
	s.loadBlockedSteamGames()
	s.loadSupportedGames()

	log.Printf("[KANBAN] Game AppID %d transitioned to status '%s' (auto-sync triggers executed)", req.SteamAppID, req.Status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleAdminGoldSyncTelemetry returns 30-day delay history relative to 03:00 MSK in-game reset.
func (s *AppState) handleAdminGoldSyncTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	mskZone := time.FixedZone("MSK", 3*3600)
	type SyncItem struct {
		TargetDate      string `json:"target_date"`
		DetectedTimeMSK string `json:"detected_time_msk"`
		DelaySeconds    int    `json:"delay_seconds"`
		PriceUSD        int    `json:"price_usd"`
		CreatedAt       string `json:"created_at"`
	}

	var history []SyncItem
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT TO_CHAR(target_date, 'DD.MM.YYYY'),
			       TO_CHAR(detected_time_msk AT TIME ZONE 'Europe/Moscow', 'HH24:MI:SS МСК'),
			       delay_seconds, price_usd,
			       TO_CHAR(created_at AT TIME ZONE 'Europe/Moscow', 'DD.MM.YYYY HH24:MI')
			FROM gold_market_sync_log
			ORDER BY target_date DESC
			LIMIT 30
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var it SyncItem
				if errScan := rows.Scan(&it.TargetDate, &it.DetectedTimeMSK, &it.DelaySeconds, &it.PriceUSD, &it.CreatedAt); errScan == nil {
					history = append(history, it)
				}
			}
		}
	}

	var current *SyncItem
	if len(history) > 0 {
		current = &history[0]
	} else {
		nowMSK := time.Now().In(mskZone)
		current = &SyncItem{
			TargetDate:      nowMSK.Format("02.01.2006"),
			DetectedTimeMSK: "03:00:00 МСК",
			DelaySeconds:    0,
			PriceUSD:        182000,
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"current": current,
		"history": history,
	})
}

// handleAdminNodes returns cluster global slot stats and 2 gaming gateway node statuses.
func (s *AppState) handleAdminNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	s.mu.RLock()
	totalActive := len(s.sessions)
	directMoscowPlayers := 0
	directFrankfurtPlayers := 0
	transitPlayers := 0
	for _, sess := range s.sessions {
		rm := strings.ToLower(sess.RouteMode)
		switch {
		case strings.Contains(rm, "transit"):
			transitPlayers++
		case strings.Contains(rm, "moscow"):
			directMoscowPlayers++
		default:
			directFrankfurtPlayers++
		}
	}
	s.mu.RUnlock()

	s.featureMu.RLock()
	currentDrain := s.drainMode
	s.featureMu.RUnlock()

	s.moscowPingMu.RLock()
	moscowRTT := s.moscowPingRTT
	s.moscowPingMu.RUnlock()

	maxSlots := s.cfg.MaxSessions
	if maxSlots <= 0 {
		maxSlots = 100
	}
	sponsorSlots := s.cfg.DedicatedSponsorSlots
	if sponsorSlots <= 0 {
		sponsorSlots = 10
	}
	freeSlots := maxSlots - sponsorSlots - totalActive
	if freeSlots < 0 {
		freeSlots = 0
	}

	cluster := map[string]interface{}{
		"total_online":     totalActive,
		"max_slots":        maxSlots,
		"sponsor_reserved": sponsorSlots,
		"free_available":   freeSlots,
		"drain_mode":       currentDrain,
	}

	// 2 Dedicated Gaming Gateway Nodes (Stockholm is NOT a proxy node)
	type GatewayNode struct {
		NodeID         string  `json:"node_id"`
		Name           string  `json:"name"`
		Host           string  `json:"host"`
		Port           int     `json:"port"`
		IP             string  `json:"ip"`
		IsHealthy      bool    `json:"is_healthy"`
		ActivePlayers  int     `json:"active_players"`
		ActiveSessions int     `json:"active_sessions"`
		DirectPlayers  int     `json:"direct_players"`
		TransitPlayers int     `json:"transit_players"`
		RTTMs          float64 `json:"rtt_ms"`
		Role           string  `json:"role"`
	}

	nodes := []GatewayNode{
		{
			NodeID:         "moscow-gateway",
			Name:           "Москва (Игровой шлюз РФ)",
			Host:           "ru.warlink.max-alekseyev.com",
			Port:           8443,
			IP:             "45.12.63.85",
			IsHealthy:      true,
			ActivePlayers:  totalActive,
			ActiveSessions: totalActive,
			DirectPlayers:  totalActive,
			TransitPlayers: 0,
			RTTMs:          moscowRTT,
			Role:           "Игровой шлюз РФ (UDP Ingress & Direct)",
		},
	}

	// Active Client Routing Modes
	type RouteModeInfo struct {
		ID            string  `json:"id"`
		Name          string  `json:"name"`
		Description   string  `json:"description"`
		GatewayPath   string  `json:"gateway_path"`
		ActivePlayers int     `json:"active_players"`
		RTTMs         float64 `json:"rtt_ms"`
	}

	routes := []RouteModeInfo{
		{
			ID:            "direct_moscow",
			Name:          "Прямая Москва",
			Description:   "Прямой игровой маршрут через шлюз в РФ (минимальный пинг для игроков из России)",
			GatewayPath:   "ru.warlink.max-alekseyev.com (Москва)",
			ActivePlayers: totalActive,
			RTTMs:         moscowRTT,
		},
	}

	masterNode := map[string]interface{}{
		"node_id":    "stockholm-master",
		"name":       "Стокгольм (Швеция, Master Control Plane)",
		"host":       "api-warlink.max-alekseyev.com",
		"port":       443,
		"ip":         "138.124.103.99",
		"is_healthy": true,
		"role":       "Центральный мастер-сервер (API, DB, HMAC Auth, Grafana — трафик игроков НЕ проксирует)",
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"cluster":     cluster,
		"nodes":       nodes,
		"routes":      routes,
		"master_node": masterNode,
	})
}

// handleAdminNodesDrain toggles global cluster drain mode.
func (s *AppState) handleAdminNodesDrain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		DrainMode bool `json:"drain_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[NODES-DRAIN] decode error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload: " + err.Error()})
		return
	}

	s.featureMu.Lock()
	s.drainMode = req.DrainMode
	s.featureMu.Unlock()

	if s.db != nil {
		_, _ = s.db.Exec(`
			INSERT INTO server_settings (key, value) VALUES ('drain_mode', $1)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
		`, fmt.Sprintf("%v", req.DrainMode))
	}

	log.Printf("[NODES] Cluster global drain_mode set to %v", req.DrainMode)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "drain_mode": req.DrainMode})
}

// handleAdminNodesSlots updates max sessions and dedicated sponsor slots.
func (s *AppState) handleAdminNodesSlots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		MaxSessions           int `json:"max_sessions"`
		DedicatedSponsorSlots int `json:"dedicated_sponsor_slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload", "details": err.Error()})
		return
	}

	if req.MaxSessions <= 0 {
		req.MaxSessions = 100
	}
	if req.DedicatedSponsorSlots < 0 {
		req.DedicatedSponsorSlots = 10
	}

	s.mu.Lock()
	s.cfg.MaxSessions = req.MaxSessions
	s.cfg.DedicatedSponsorSlots = req.DedicatedSponsorSlots
	s.mu.Unlock()

	if s.db != nil {
		_, _ = s.db.Exec(`
			INSERT INTO server_settings (key, value) VALUES 
				('max_sessions', $1),
				('dedicated_sponsor_slots', $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
		`, strconv.Itoa(req.MaxSessions), strconv.Itoa(req.DedicatedSponsorSlots))
	}

	log.Printf("[NODES-SLOTS] Updated cluster slots: MaxSessions=%d, DedicatedSponsorSlots=%d", req.MaxSessions, req.DedicatedSponsorSlots)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":                 true,
		"max_sessions":            req.MaxSessions,
		"dedicated_sponsor_slots": req.DedicatedSponsorSlots,
	})
}

// handleAdminMonetization gets or updates monetization parameters.
func (s *AppState) handleAdminMonetization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			BoostyEnabled bool   `json:"boosty_enabled"`
			SbpEnabled    bool   `json:"sbp_enabled"`
			UsdtEnabled   bool   `json:"usdt_enabled"`
			UsdtWallet    string `json:"usdt_wallet"`
			PauseReason   string `json:"pause_reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
			return
		}

		s.featureMu.Lock()
		s.donateBoostyEnabled = req.BoostyEnabled
		s.donateSbpEnabled = req.SbpEnabled
		s.donateCryptoEnabled = req.UsdtEnabled
		s.donatePausedNotice = req.PauseReason
		s.featureMu.Unlock()

		if s.db != nil {
			_, _ = s.db.Exec(`
				INSERT INTO server_settings (key, value) VALUES
					('donate_boosty_enabled', $1),
					('donate_sbp_enabled', $2),
					('donate_crypto_enabled', $3),
					('donate_paused_notice', $4),
					('crypto_usdt_trc20_address', $5),
					('enable_boosty', $1),
					('enable_sbp', $2),
					('enable_usdt', $3)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
			`, fmt.Sprintf("%v", req.BoostyEnabled), fmt.Sprintf("%v", req.SbpEnabled), fmt.Sprintf("%v", req.UsdtEnabled), req.PauseReason, req.UsdtWallet)
		}

		log.Printf("[ADMIN-MONETIZATION] Updated live: Boosty=%v, SBP=%v, Crypto=%v, Notice='%s', USDT='%s'",
			req.BoostyEnabled, req.SbpEnabled, req.UsdtEnabled, req.PauseReason, req.UsdtWallet)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		return
	}

	// GET: read monetization settings live from AppState and server_settings
	s.featureMu.RLock()
	curBoosty := s.donateBoostyEnabled
	curSbp := s.donateSbpEnabled
	curCrypto := s.donateCryptoEnabled
	curNotice := s.donatePausedNotice
	s.featureMu.RUnlock()

	var curWallet string = "TE1hrh5yy7hnHpCX7fz51rPvWubpcJJgaM"
	if s.db != nil {
		_ = s.db.QueryRow(`SELECT value FROM server_settings WHERE key = 'crypto_usdt_trc20_address'`).Scan(&curWallet)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"boosty_enabled": curBoosty,
		"sbp_enabled":    curSbp,
		"usdt_enabled":   curCrypto,
		"usdt_wallet":    curWallet,
		"pause_reason":   curNotice,
	})
}

// handleAdminGamesSuggestions returns all game suggestions for the 4-column Kanban board.
func (s *AppState) handleAdminGamesSuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	if s.db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "database_not_connected"})
		return
	}

	rows, err := s.db.Query(`
		SELECT steam_app_id, title, COALESCE(icon_url, ''), votes_count, status, TO_CHAR(created_at, 'DD.MM.YYYY')
		FROM game_suggestions
		ORDER BY 
			CASE 
				WHEN status = 'queue_integration' THEN 1 
				WHEN status = 'voting' THEN 2 
				WHEN status = 'supported' THEN 3 
				ELSE 4 
			END,
			votes_count DESC, 
			created_at ASC
	`)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	type SuggestionItem struct {
		SteamAppID int    `json:"steam_app_id"`
		Title      string `json:"title"`
		IconURL    string `json:"icon_url"`
		VotesCount int    `json:"votes_count"`
		Status     string `json:"status"`
		CreatedAt  string `json:"created_at"`
	}

	list := make([]SuggestionItem, 0)
	for rows.Next() {
		var it SuggestionItem
		if errScan := rows.Scan(&it.SteamAppID, &it.Title, &it.IconURL, &it.VotesCount, &it.Status, &it.CreatedAt); errScan == nil {
			list = append(list, it)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"suggestions": list,
		"count":       len(list),
	})
}

// handleAdminSponsorsGrant manually grants sponsor status to an account.
func (s *AppState) handleAdminSponsorsGrant(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		AccountNumber  string `json:"account_number"`
		DurationMonths int    `json:"duration_months"`
		AmountRub      int    `json:"amount_rub"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AccountNumber == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid_payload"})
		return
	}

	if req.DurationMonths <= 0 {
		req.DurationMonths = 1
	}

	if s.db != nil {
		_, err := s.db.Exec(`
			INSERT INTO accounts (account_number, is_sponsor, sponsor_until, total_donated_rub, created_at)
			VALUES ($1, TRUE, NOW() + ($2 || ' months')::INTERVAL, $3, NOW())
			ON CONFLICT (account_number) DO UPDATE SET
				is_sponsor = TRUE,
				sponsor_until = GREATEST(COALESCE(accounts.sponsor_until, NOW()), NOW()) + ($2 || ' months')::INTERVAL,
				total_donated_rub = accounts.total_donated_rub + $3,
				updated_at = NOW()
		`, req.AccountNumber, req.DurationMonths, req.AmountRub)
		if err != nil {
			log.Printf("[SPONSORS] Error granting sponsor status: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "db_error"})
			return
		}
	}

	log.Printf("[SPONSORS] Granted %d months sponsor status to account %s (amount: %d RUB)", req.DurationMonths, req.AccountNumber, req.AmountRub)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// generateSessionToken generates a 256-bit cryptographically secure random session ID.
func generateSessionToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *AppState) getActiveSessionsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// handleAdminAccountRouter handles unified account operations:
// GET /api/v1/admin/accounts - search / list accounts
// GET /api/v1/admin/accounts/{account_number} - full account card data
// POST /api/v1/admin/accounts/kick - kick active session
// POST /api/v1/admin/accounts/ban - ban account and device
// POST /api/v1/admin/accounts/unban - remove ban
func (s *AppState) handleAdminAccountRouter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.checkAdminAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	subAction := ""
	if len(parts) >= 5 {
		subAction = parts[4]
	}

	if r.Method == http.MethodPost {
		switch subAction {
		case "kick":
			var req struct {
				AccountNumber string `json:"account_number"`
				DeviceID      string `json:"device_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			kicked := 0
			s.mu.Lock()
			for tok, sess := range s.sessions {
				if (req.AccountNumber != "" && sess.AccountNumber == req.AccountNumber) ||
					(req.DeviceID != "" && sess.DeviceID == req.DeviceID) {
					delete(s.sessions, tok)
					kicked++
				}
			}
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "kicked": kicked})
			return

		case "ban":
			var req struct {
				AccountNumber string `json:"account_number"`
				DeviceID      string `json:"device_id"`
				Reason        string `json:"reason"`
				Duration      string `json:"duration"` // "1h", "24h", "7d", "perm"
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Reason == "" {
				req.Reason = "Блокировка администратором панели управления"
			}
			var expiresAt *time.Time
			switch req.Duration {
			case "1h":
				tExp := time.Now().Add(1 * time.Hour)
				expiresAt = &tExp
			case "24h":
				tExp := time.Now().Add(24 * time.Hour)
				expiresAt = &tExp
			case "7d":
				tExp := time.Now().Add(7 * 24 * time.Hour)
				expiresAt = &tExp
			}

			if s.db != nil {
				if req.AccountNumber != "" {
					_, _ = s.db.Exec(`
						INSERT INTO security_bans (target_type, target_value, reason, banned_by, expires_at)
						VALUES ('account', $1, $2, 'admin', $3)
					`, req.AccountNumber, req.Reason, expiresAt)
				}
				if req.DeviceID != "" {
					_, _ = s.db.Exec(`
						INSERT INTO security_bans (target_type, target_value, reason, banned_by, expires_at)
						VALUES ('device', $1, $2, 'admin', $3)
					`, req.DeviceID, req.Reason, expiresAt)
				}
			}

			// Drop active sessions
			s.mu.Lock()
			for tok, sess := range s.sessions {
				if (req.AccountNumber != "" && sess.AccountNumber == req.AccountNumber) ||
					(req.DeviceID != "" && sess.DeviceID == req.DeviceID) {
					delete(s.sessions, tok)
				}
			}
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return

		case "unban":
			var req struct {
				AccountNumber string `json:"account_number"`
				DeviceID      string `json:"device_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if s.db != nil {
				_, _ = s.db.Exec(`DELETE FROM security_bans WHERE (target_value = $1 AND target_value != '') OR (target_value = $2 AND target_value != '')`, req.AccountNumber, req.DeviceID)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
	}

	// GET request
	accountNumber := ""
	if subAction != "" && subAction != "kick" && subAction != "ban" && subAction != "unban" {
		accountNumber = subAction
	}
	if accountNumber == "" {
		accountNumber = strings.TrimSpace(r.URL.Query().Get("account"))
	}
	if accountNumber == "" {
		accountNumber = strings.TrimSpace(r.URL.Query().Get("q"))
	}

	if accountNumber != "" {
		s.getAdminAccountDetails(w, accountNumber)
		return
	}

	// List recent accounts if no specific account requested
	type AccountBrief struct {
		AccountNumber   string     `json:"account_number"`
		Nickname        string     `json:"nickname"`
		Tier            string     `json:"tier"`
		IsSponsor       bool       `json:"is_sponsor"`
		SponsorUntil    *time.Time `json:"sponsor_until"`
		TotalDonatedRub int        `json:"total_donated_rub"`
		CreatedAt       time.Time  `json:"created_at"`
	}

	accounts := make([]AccountBrief, 0)
	if s.db != nil {
		rows, err := s.db.Query(`
			SELECT account_number, nickname, tier, sponsor_until, total_donated_rub, created_at
			FROM accounts
			ORDER BY updated_at DESC LIMIT 50
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var a AccountBrief
				_ = rows.Scan(&a.AccountNumber, &a.Nickname, &a.Tier, &a.SponsorUntil, &a.TotalDonatedRub, &a.CreatedAt)
				if a.SponsorUntil != nil && a.SponsorUntil.After(time.Now()) {
					a.IsSponsor = true
				}
				accounts = append(accounts, a)
			}
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"accounts": accounts,
	})
}

func (s *AppState) getAdminAccountDetails(w http.ResponseWriter, accountIdentifier string) {
	if s.db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "database_not_connected"})
		return
	}

	accountIdentifier = strings.TrimSpace(accountIdentifier)

	type AccountRecord struct {
		AccountNumber      string     `json:"account_number"`
		Nickname           string     `json:"nickname"`
		AvatarURL          string     `json:"avatar_url"`
		Tier               string     `json:"tier"`
		SponsorUntil       *time.Time `json:"sponsor_until"`
		TotalDonatedRub    int        `json:"total_donated_rub"`
		SteamID            string     `json:"steam_id"`
		Motto              string     `json:"motto"`
		HideDonationAmount bool       `json:"hide_donation_amount"`
		CreatedAt          time.Time  `json:"created_at"`
		UpdatedAt          time.Time  `json:"updated_at"`
		IsSponsor          bool       `json:"is_sponsor"`
	}

	var acc AccountRecord
	err := s.db.QueryRow(`
		SELECT account_number, nickname, avatar_url, tier, sponsor_until, total_donated_rub,
		       COALESCE(steam_id, ''), COALESCE(motto, ''), hide_donation_amount, created_at, updated_at
		FROM accounts
		WHERE account_number = $1
		   OR account_number IN (SELECT account_number FROM account_devices WHERE device_id = $1)
		   OR nickname ILIKE $1
		LIMIT 1
	`, accountIdentifier).Scan(
		&acc.AccountNumber, &acc.Nickname, &acc.AvatarURL, &acc.Tier, &acc.SponsorUntil, &acc.TotalDonatedRub,
		&acc.SteamID, &acc.Motto, &acc.HideDonationAmount, &acc.CreatedAt, &acc.UpdatedAt,
	)

	// If not found in accounts, check if identifier exists in user_connection_history or account_devices
	if err != nil {
		var foundAcc, foundDev string
		_ = s.db.QueryRow(`
			SELECT COALESCE(account_number, ''), device_id FROM user_connection_history
			WHERE account_number = $1 OR device_id = $1
			ORDER BY connected_at DESC LIMIT 1
		`, accountIdentifier).Scan(&foundAcc, &foundDev)

		if foundAcc == "" && foundDev == "" {
			_ = s.db.QueryRow(`
				SELECT account_number, device_id FROM account_devices
				WHERE account_number = $1 OR device_id = $1
				LIMIT 1
			`, accountIdentifier).Scan(&foundAcc, &foundDev)
		}

		if foundAcc != "" || foundDev != "" {
			finalAcc := foundAcc
			if finalAcc == "" {
				finalAcc = accountIdentifier
			}
			acc = AccountRecord{
				AccountNumber: finalAcc,
				Nickname:      "Игрок (профиль не заполнен)",
				Tier:          "free",
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}
		} else {
			// Account not found anywhere
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "account_not_found"})
			return
		}
	}

	if acc.SponsorUntil != nil && acc.SponsorUntil.After(time.Now()) {
		acc.IsSponsor = true
	}

	// 1. Linked Devices
	type DeviceRecord struct {
		DeviceID  string    `json:"device_id"`
		CreatedAt time.Time `json:"created_at"`
	}
	devices := make([]DeviceRecord, 0)
	rowsDev, errDev := s.db.Query(`
		SELECT device_id, created_at
		FROM account_devices
		WHERE account_number = $1
		ORDER BY created_at DESC
	`, acc.AccountNumber)
	if errDev == nil {
		defer rowsDev.Close()
		for rowsDev.Next() {
			var d DeviceRecord
			_ = rowsDev.Scan(&d.DeviceID, &d.CreatedAt)
			devices = append(devices, d)
		}
	}

	// 2. Recent Connections (up to 20)
	type ConnRecord struct {
		ID            int64     `json:"id"`
		DeviceID      string    `json:"device_id"`
		ClientIP      string    `json:"client_ip"`
		Country       string    `json:"country"`
		City          string    `json:"city"`
		AppVersion    string    `json:"app_version"`
		RouteMode     string    `json:"route_mode"`
		ConnectedNode string    `json:"connected_node"`
		ConnectedAt   time.Time `json:"connected_at"`
	}
	connections := make([]ConnRecord, 0)
	rowsConn, errConn := s.db.Query(`
		SELECT id, device_id, client_ip, country, city,
		       COALESCE(app_version, ''), COALESCE(route_mode, ''), COALESCE(connected_node, ''),
		       connected_at
		FROM user_connection_history
		WHERE account_number = $1
		   OR device_id IN (SELECT device_id FROM account_devices WHERE account_number = $1)
		ORDER BY connected_at DESC LIMIT 20
	`, acc.AccountNumber)
	if errConn == nil {
		defer rowsConn.Close()
		for rowsConn.Next() {
			var c ConnRecord
			_ = rowsConn.Scan(&c.ID, &c.DeviceID, &c.ClientIP, &c.Country, &c.City, &c.AppVersion, &c.RouteMode, &c.ConnectedNode, &c.ConnectedAt)
			connections = append(connections, c)
		}
	}

	// 3. Support Tickets (up to 20)
	type TicketBrief struct {
		ID          int64     `json:"id"`
		Category    string    `json:"category"`
		Status      string    `json:"status"`
		UserComment string    `json:"user_comment"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	tickets := make([]TicketBrief, 0)
	rowsTick, errTick := s.db.Query(`
		SELECT id, category, status, user_comment, created_at, updated_at
		FROM support_tickets
		WHERE account_number = $1
		   OR device_id IN (SELECT device_id FROM account_devices WHERE account_number = $1)
		ORDER BY created_at DESC LIMIT 20
	`, acc.AccountNumber)
	if errTick == nil {
		defer rowsTick.Close()
		for rowsTick.Next() {
			var t TicketBrief
			_ = rowsTick.Scan(&t.ID, &t.Category, &t.Status, &t.UserComment, &t.CreatedAt, &t.UpdatedAt)
			tickets = append(tickets, t)
		}
	}

	// 4. Security Bans
	type BanRecord struct {
		ID          int64      `json:"id"`
		TargetType  string     `json:"target_type"`
		TargetValue string     `json:"target_value"`
		Reason      string     `json:"reason"`
		BannedBy    string     `json:"banned_by"`
		ExpiresAt   *time.Time `json:"expires_at"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	bans := make([]BanRecord, 0)
	rowsBans, errBans := s.db.Query(`
		SELECT id, target_type, target_value, reason, banned_by, expires_at, created_at
		FROM security_bans
		WHERE (target_value = $1 OR target_value IN (SELECT device_id FROM account_devices WHERE account_number = $1))
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
	`, acc.AccountNumber)
	if errBans == nil {
		defer rowsBans.Close()
		for rowsBans.Next() {
			var b BanRecord
			_ = rowsBans.Scan(&b.ID, &b.TargetType, &b.TargetValue, &b.Reason, &b.BannedBy, &b.ExpiresAt, &b.CreatedAt)
			bans = append(bans, b)
		}
	}

	// 5. Check real-time active session in RAM
	isOnline := false
	var activeSession *SessionInfo
	s.mu.RLock()
	for _, sess := range s.sessions {
		matched := false
		if sess.AccountNumber != "" && sess.AccountNumber == acc.AccountNumber {
			matched = true
		}
		if !matched {
			for _, d := range devices {
				if d.DeviceID != "" && sess.DeviceID == d.DeviceID {
					matched = true
					break
				}
			}
		}
		if matched {
			isOnline = true
			activeSession = sess
			break
		}
	}
	s.mu.RUnlock()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"account":        acc,
		"devices":        devices,
		"connections":    connections,
		"tickets":        tickets,
		"bans":           bans,
		"is_banned":      len(bans) > 0,
		"is_online":      isOnline,
		"active_session": activeSession,
	})
}
