package singbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
	"warlink/internal/config"
)

const (
	SingBoxDownloadURL = "https://github.com/shtorm-7/sing-box-extended/releases/download/v1.14.0-extended-2.7.1/sing-box-1.14.0-extended-2.7.1-windows-amd64.zip"
	WintunDownloadURL  = "https://www.wintun.net/builds/wintun-0.14.1.zip"
)

var (
	DefaultServerIP     = "138.124.103.99"
	DefaultServerAPI    = "http://138.124.103.99"
	// Injected at build time via -X ldflags from GitHub Actions secrets.
	// Never commit real values here — binary built from source without CI
	// will have empty secrets and will fail auth (gateway rejects empty HMAC).
	DefaultHMACSecret   = ""
	DefaultObfsPassword = ""
)

func GetServerIP() string {
	if DefaultServerIP != "" {
		return DefaultServerIP
	}
	if envIP := os.Getenv("WARLINK_SERVER_IP"); envIP != "" {
		return envIP
	}
	cfg := config.Load()
	if cfg != nil && cfg.ServerIP != "" {
		return cfg.ServerIP
	}
	return ""
}

func GetServerAPI() string {
	if DefaultServerAPI != "" {
		return DefaultServerAPI
	}
	if envAPI := os.Getenv("WARLINK_SERVER_API"); envAPI != "" {
		return envAPI
	}
	ip := GetServerIP()
	if ip != "" {
		return fmt.Sprintf("http://%s", ip)
	}
	return ""
}

// Config represents a sing-box configuration with per-process TUN routing and Hysteria 2 GPN.
type Config struct {
	Log          LogConfig           `json:"log"`
	DNS          *DNSConfig          `json:"dns,omitempty"`
	Inbounds     []InboundConfig     `json:"inbounds"`
	Endpoints    []Endpoint          `json:"endpoints,omitempty"`
	Outbounds    []Outbound          `json:"outbounds"`
	Route        RouteConfig         `json:"route"`
	Experimental *ExperimentalConfig `json:"experimental,omitempty"`
}

type ExperimentalConfig struct {
	CacheFile *CacheFileConfig `json:"cache_file,omitempty"`
}

type CacheFileConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

type DNSConfig struct {
	Servers []DNSServer `json:"servers"`
	Rules   []DNSRule   `json:"rules,omitempty"`
	Final   string      `json:"final,omitempty"`
}

type DNSServer struct {
	Tag        string `json:"tag"`
	Type       string `json:"type"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Path       string `json:"path,omitempty"`
	Detour     string `json:"detour,omitempty"`
	Inet4Range string `json:"inet4_range,omitempty"`
}

type DNSRule struct {
	ProcessName  []string `json:"process_name,omitempty"`
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	Server       string   `json:"server"`
}

type LogConfig struct {
	Level     string `json:"level"`
	Output    string `json:"output,omitempty"`
	Timestamp bool   `json:"timestamp"`
}

type InboundConfig struct {
	Type          string   `json:"type"`
	Tag           string   `json:"tag"`
	InterfaceName string   `json:"interface_name"`
	Address       []string `json:"address"`
	MTU           int      `json:"mtu,omitempty"`
	AutoRoute     bool     `json:"auto_route"`
	StrictRoute   bool     `json:"strict_route"`
	Stack         string   `json:"stack"`
}

type Endpoint struct {
	Type       string   `json:"type"`
	Tag        string   `json:"tag"`
	Address    []string `json:"address"`
	PrivateKey string   `json:"private_key"`
	MTU        int      `json:"mtu"`
	Peers      []Peer   `json:"peers"`
}

type Peer struct {
	Address    string   `json:"address"`
	Port       int      `json:"port"`
	PublicKey  string   `json:"public_key"`
	AllowedIPs []string `json:"allowed_ips"`
	Reserved   []int    `json:"reserved"`
}

type OutboundTLSOptions struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name,omitempty"`
	Insecure   bool   `json:"insecure"`
}

type Hysteria2Obfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}

type Outbound struct {
	Type        string              `json:"type"`
	Tag         string              `json:"tag"`
	Server      string              `json:"server,omitempty"`
	ServerPort  int                 `json:"server_port,omitempty"`
	ServerPorts []string            `json:"server_ports,omitempty"`
	HopInterval string              `json:"hop_interval,omitempty"`
	UpMbps      int                 `json:"up_mbps,omitempty"`
	DownMbps    int                 `json:"down_mbps,omitempty"`
	Password    string              `json:"password,omitempty"`
	Obfs        *Hysteria2Obfs      `json:"obfs,omitempty"`
	TLS         *OutboundTLSOptions `json:"tls,omitempty"`
}

type RouteConfig struct {
	DefaultDomainResolver string      `json:"default_domain_resolver,omitempty"`
	FindProcess           bool        `json:"find_process"`
	AutoDetectInterface   bool        `json:"auto_detect_interface"`
	Rules                 []RouteRule `json:"rules"`
}

// BlockedServiceIPs contains IP ranges blocked at Layer 3/4 by Russian ISPs (TSPU)
// such that DPI evasion cannot reach them without a Layer 3 proxy/tunnel.
var BlockedServiceIPs = []string{
	// Meta / Instagram / WhatsApp subnets blocked by TSPU (AS32934)
	"157.240.0.0/16",
	"31.13.64.0/18",
	"179.60.192.0/22",
	"185.89.216.0/22",
	"57.144.0.0/14",
	"69.63.176.0/20",
	"69.171.224.0/19",
	"66.220.144.0/20",
	"204.15.20.0/22",
	// Telegram subnets blocked by TSPU (AS44907, AS62041)
	"149.154.160.0/20",
	"91.108.4.0/22",
	"91.108.8.0/22",
	"91.108.12.0/22",
	"91.108.16.0/22",
	"91.108.20.0/22",
	"91.108.56.0/22",
	"91.105.192.0/23",
}

// BlockedServiceDomains contains domains that require tunneling through Stockholm GPN.
var BlockedServiceDomains = []string{
	"web.telegram.org",
	"telegram.org",
	"t.me",
	"telegra.ph",
	"telegram.me",
	"telesco.pe",
	"tdesktop.com",
	"web.whatsapp.com",
	"whatsapp.com",
	"whatsapp.net",
	"instagram.com",
	"cdninstagram.com",
	"threads.net",
	"facebook.com",
	"fbcdn.net",
	"x.com",
	"twitter.com",
	"twimg.com",
	"t.co",
}

// CRLDomains contains Certificate Revocation List (CRL) and OCSP domains that must
// bypass GPN tunnel directly over port 80 to ensure anti-cheat and TLS certificate validation never fail.
var CRLDomains = []string{
	"digicert.com",
	"certainly.com",
	"pki.goog",
	"verisign.com",
	"sectigo.com",
	"globalsign.com",
	"identrust.com",
	"microsoft.com",
	"symcd.com",
	"entrust.net",
	"amazontrust.com",
	"letsencrypt.org",
	"godaddy.com",
	"usertrust.com",
	"comodoca.com",
	"swisssign.net",
}

// DirectGameDomains contains Valve/Steam domains and game launcher/anti-cheat CDN endpoints
// that must route directly (bypassing the tunnel and FakeIP) for maximum speed and compatibility.
var DirectGameDomains = []string{
	// Anti-cheat / game CDN
	"elytra.ac",
	"certainly.com",
	"pki.goog",
	// Steam
	"steamserver.net",
	"steampowered.com",
	"steamcommunity.com",
	"steamstatic.com",
	"steamgames.com",
	// Epic Games Store — WARDOGS lobby auth & backend
	"epicgames.com",
	"epicgames.dev",
	"epicgames.net",
	"unrealengine.com",
	"ol.epicgames.com",
	"api.epicgames.dev",
	// EGS CDN & auth services
	"cloudfront.net",
	"amazonaws.com",
	// AWS GameLift infrastructure (match server assignment API)
	"gamelift.us-east-1.amazonaws.com",
	// Time sync
	"time.cloudflare.com",
}

// DirectLauncherProcesses contains launcher, anti-cheat installer, and background crash reporting
// processes that should always route directly without tunnel encapsulation.
var DirectLauncherProcesses = []string{
	"WardogsLauncher-Shipping.exe",
	"wardogslauncher-shipping.exe",
	"wardogslauncher.exe",
	"Elytra-Setup.exe",
	"elytra-setup.exe",
	"service.exe",
	"control.exe",
	"crashpad_handler.exe",
	"CrashReportClient.exe",
	"crashreportclient.exe",
}

type RouteRule struct {
	Action          string   `json:"action,omitempty"`
	Protocol        []string `json:"protocol,omitempty"`
	Network         string   `json:"network,omitempty"`
	Port            []int    `json:"port,omitempty"`
	PortRange       []string `json:"port_range,omitempty"`
	ProcessName     []string `json:"process_name,omitempty"`
	IPCIDR          []string `json:"ip_cidr,omitempty"`
	DomainSuffix    []string `json:"domain_suffix,omitempty"`
	Domain          []string `json:"domain,omitempty"`
	OverrideAddress string   `json:"override_address,omitempty"`
	Outbound        string   `json:"outbound,omitempty"`
}

type SessionResult struct {
	Token        string `json:"token"`
	Server       string `json:"server"`
	ServerPorts  string `json:"server_ports"`
	Obfs         string `json:"obfs"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

type GatewayStatus struct {
	Status          string `json:"status"`
	Location        string `json:"location"`
	PingHintMs      int    `json:"ping_hint_ms"`
	ActiveSessions  int    `json:"active_sessions"`
	MaxSessions     int    `json:"max_sessions"`
	ServerIP        string `json:"server_ip"`
	ServerPorts     string `json:"server_ports"`
	DueDate         string `json:"due_date"`
	DaysLeft        int    `json:"days_left"`
	DonateAmountRub int    `json:"donate_amount_rub"`
	EnableDonate    *bool  `json:"enable_donate,omitempty"`
	EnableVoting    *bool  `json:"enable_voting,omitempty"`
}

var (
	cachedSessionToken  string
	cachedSessionObfs   string
	cachedSessionServer string
	cachedSessionPorts  string
	cachedSessionExp    time.Time
	sessionMu           sync.Mutex
)

func GetMachineGUID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		s, _, err := k.GetStringValue("MachineGuid")
		if err == nil && s != "" {
			return s
		}
	}
	h, _ := os.Hostname()
	if h != "" {
		return "host-" + h
	}
	return "warlink-device-win"
}

// AcquireSession negotiates a valid Hysteria 2 session token with the Stockholm VPS.
func AcquireSession(game ...string) (string, error) {
	targetGame := "wardogs"
	if len(game) > 0 && strings.TrimSpace(game[0]) != "" {
		targetGame = strings.TrimSpace(game[0])
	}

	sessionMu.Lock()
	if cachedSessionToken != "" && time.Now().Before(cachedSessionExp) {
		tok := cachedSessionToken
		sessionMu.Unlock()
		return tok, nil
	}
	sessionMu.Unlock()

	deviceID := GetMachineGUID()
	ts := time.Now().Unix()
	nonceBytes := make([]byte, 8)
	_, _ = rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"device_id": deviceID,
		"timestamp": ts,
		"nonce":     nonce,
		"game":      targetGame,
	})

	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return "", fmt.Errorf("адрес шлюза не настроен (задайте WARLINK_SERVER_IP)")
	}

	apiURL := fmt.Sprintf("%s/api/v1/session", serverAPI)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса сессии: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	hmacSecret := os.Getenv("WARLINK_HMAC_SECRET")
	if hmacSecret == "" {
		hmacSecret = DefaultHMACSecret
	}
	if hmacSecret != "" {
		dataToSign := fmt.Sprintf("%s:%d:%s", deviceID, ts, nonce)
		mac := hmac.New(sha256.New, []byte(hmacSecret))
		mac.Write([]byte(dataToSign))
		req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	client := &http.Client{Timeout: 7 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("шлюз Стокгольм временно недоступен (%s): %w", GetServerIP(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("Все 100 слотов шлюза заняты. Пожалуйста, подождите освобождения места.")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ошибка авторизации на шлюзе (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var sessResp SessionResult
	if err := json.NewDecoder(resp.Body).Decode(&sessResp); err != nil || sessResp.Token == "" {
		return "", fmt.Errorf("некорректный ответ шлюза")
	}

	sessionMu.Lock()
	cachedSessionToken = sessResp.Token
	cachedSessionObfs = sessResp.Obfs
	cachedSessionServer = sessResp.Server
	cachedSessionPorts = sessResp.ServerPorts
	cachedSessionExp = time.Now().Add(20 * time.Hour)
	sessionMu.Unlock()

	return sessResp.Token, nil
}

// InvalidateSession clears the locally cached session token without contacting the gateway.
// Use this before a forced reconnect so AcquireSession will fetch a fresh token.
func InvalidateSession() {
	sessionMu.Lock()
	cachedSessionToken = ""
	cachedSessionExp = time.Time{}
	sessionMu.Unlock()
}

// ReleaseSession explicitly releases the active slot on the Stockholm gateway immediately.
func ReleaseSession() error {
	sessionMu.Lock()
	tok := cachedSessionToken
	cachedSessionToken = ""
	cachedSessionExp = time.Time{}
	sessionMu.Unlock()

	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil
	}

	deviceID := GetMachineGUID()
	reqBody, _ := json.Marshal(map[string]interface{}{
		"device_id": deviceID,
		"token":     tok,
	})

	apiURL := fmt.Sprintf("%s/api/v1/session/release", serverAPI)
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 2500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

type VotesResponse struct {
	Success       bool                 `json:"success"`
	TargetVotes   int                  `json:"target_votes"`
	MaxUserVotes  int                  `json:"max_user_votes"`
	UserVotesUsed int                  `json:"user_votes_used"`
	Games         []GameSuggestionItem `json:"games"`
	Error         string               `json:"error,omitempty"`
}

type GameSuggestionItem struct {
	SteamAppID int    `json:"steam_app_id"`
	Title      string `json:"title"`
	IconURL    string `json:"icon_url"`
	VotesCount int    `json:"votes_count"`
	Status     string `json:"status"`
	UserVoted  bool   `json:"user_voted"`
}

func FetchVotes(deviceID string) (*VotesResponse, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("адрес шлюза не настроен")
	}
	url := fmt.Sprintf("%s/api/v1/votes?device_id=%s", serverAPI, deviceID)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var vResp VotesResponse
	if err := json.NewDecoder(resp.Body).Decode(&vResp); err != nil {
		return nil, err
	}
	return &vResp, nil
}

func SubmitVote(deviceID string, appID int, title, iconURL string) (bool, string, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return false, "", fmt.Errorf("адрес шлюза не настроен")
	}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"device_id":    deviceID,
		"steam_app_id": appID,
		"title":        title,
		"icon_url":     iconURL,
	})
	url := fmt.Sprintf("%s/api/v1/votes", serverAPI)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	var res struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	return res.Success, res.Error, nil
}

func RetractVote(deviceID string, appID int) (bool, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return false, fmt.Errorf("адрес шлюза не настроен")
	}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"device_id":    deviceID,
		"steam_app_id": appID,
	})
	url := fmt.Sprintf("%s/api/v1/votes", serverAPI)
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(reqBody))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var res struct {
		Success bool `json:"success"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	return res.Success, nil
}

// RequestServerDonate calls the Stockholm server to generate an official Aeza invoice.
func RequestServerDonate() (string, error) {
	apiURL := fmt.Sprintf("%s/api/v1/donate", GetServerAPI())
	req, err := http.NewRequest(http.MethodPost, apiURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Success bool   `json:"success"`
		PayURL  string `json:"pay_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.PayURL != "" {
		return result.PayURL, nil
	}
	return "", fmt.Errorf("сервер не предоставил платежную ссылку")
}

// GetServerGatewayStatus queries real-time status of Stockholm VPS.
func GetServerGatewayStatus() (*GatewayStatus, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("server API not configured")
	}
	apiURL := fmt.Sprintf("%s/api/v1/status", serverAPI)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var st GatewayStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Profile represents game/service routing rules received from /api/v1/profiles
type Profile struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Processes []string `json:"processes"`
	Domains   []string `json:"domains"`
	IPs       []string `json:"ips"`
	UDPRanges []string `json:"udp_ranges"`
}

// FetchProfiles queries the server API for active profiles.
func FetchProfiles(optionalServerAPI ...string) ([]Profile, error) {
	serverAPI := ""
	if len(optionalServerAPI) > 0 {
		serverAPI = optionalServerAPI[0]
	}
	if serverAPI == "" {
		serverAPI = GetServerAPI()
	}
	if serverAPI == "" {
		return nil, fmt.Errorf("server API not configured")
	}
	url := fmt.Sprintf("%s/api/v1/profiles", strings.TrimRight(serverAPI, "/"))
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profiles from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d for profiles", resp.StatusCode)
	}

	var res struct {
		Success  bool      `json:"success"`
		Profiles []Profile `json:"profiles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode profiles JSON: %w", err)
	}
	return res.Profiles, nil
}

// FetchRemoteConfig queries the server API for full dynamic sing-box JSON configuration.
func FetchRemoteConfig(serverAPI string, token string, game string, targetProcesses []string, includeWebServices bool) ([]byte, error) {
	if serverAPI == "" {
		serverAPI = GetServerAPI()
	}
	if serverAPI == "" {
		return nil, fmt.Errorf("server API not configured")
	}
	if token == "" {
		return nil, fmt.Errorf("session token required")
	}

	webVal := "0"
	if includeWebServices {
		webVal = "1"
	}
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/singbox/config", strings.TrimRight(serverAPI, "/")))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", token)
	if game != "" {
		q.Set("game", game)
	}
	q.Set("web", webVal)
	if len(targetProcesses) > 0 {
		q.Set("procs", strings.Join(targetProcesses, ","))
	}
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote sing-box config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d for singbox config", resp.StatusCode)
	}

	var res struct {
		Success bool            `json:"success"`
		Error   string          `json:"error,omitempty"`
		Game    string          `json:"game,omitempty"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode remote singbox config response: %w", err)
	}
	if !res.Success || len(res.Config) == 0 {
		return nil, fmt.Errorf("server returned unsuccessful singbox config: %s", res.Error)
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, res.Config, "", "  "); err == nil {
		return formatted.Bytes(), nil
	}
	return res.Config, nil
}

// GenerateConfig creates a sing-box JSON configuration routing target processes,
// and optionally Meta/WhatsApp/X IP ranges and blocked web domains, to Hysteria 2 Stockholm tunnel.
func GenerateConfig(targetProcesses []string, includeWebServices bool, optionalToken ...string) ([]byte, error) {
	return GenerateConfigFromProfiles(nil, targetProcesses, includeWebServices, optionalToken...)
}

// GenerateConfigFromProfiles constructs a sing-box JSON configuration dynamically using loaded profiles.
func GenerateConfigFromProfiles(profiles []Profile, extraProcesses []string, includeWebServices bool, optionalToken ...string) ([]byte, error) {
	token := "wl_session_token"
	if len(optionalToken) > 0 && optionalToken[0] != "" {
		token = optionalToken[0]
	}

	// Collect unique processes, domains and IPs
	procSet := make(map[string]struct{})
	for _, p := range extraProcesses {
		pClean := strings.TrimSpace(p)
		if pClean != "" {
			procSet[pClean] = struct{}{}
		}
	}

	domainSet := make(map[string]struct{})
	ipSet := make(map[string]struct{})

	for _, prof := range profiles {
		for _, p := range prof.Processes {
			pClean := strings.TrimSpace(p)
			if pClean != "" {
				procSet[pClean] = struct{}{}
			}
		}
		for _, d := range prof.Domains {
			dClean := strings.ToLower(strings.TrimSpace(d))
			if dClean != "" {
				domainSet[dClean] = struct{}{}
			}
		}
		for _, ip := range prof.IPs {
			ipClean := strings.TrimSpace(ip)
			if ipClean != "" {
				if strings.Contains(ipClean, ",") {
					parts := strings.Split(ipClean, ",")
					ipClean = strings.TrimSpace(parts[0])
				}
				if !strings.Contains(ipClean, "/") {
					if strings.Contains(ipClean, ":") {
						ipClean = ipClean + "/128"
					} else {
						ipClean = ipClean + "/32"
					}
				}
				if _, _, err := net.ParseCIDR(ipClean); err == nil {
					ipSet[ipClean] = struct{}{}
				}
			}
		}
	}

	var allProcesses []string
	for p := range procSet {
		allProcesses = append(allProcesses, p)
	}
	var allDomains []string
	for d := range domainSet {
		allDomains = append(allDomains, d)
	}
	var allIPs []string
	for ip := range ipSet {
		ipClean := strings.TrimSpace(ip)
		if strings.Contains(ipClean, ",") {
			parts := strings.Split(ipClean, ",")
			ipClean = strings.TrimSpace(parts[0])
		}
		if !strings.Contains(ipClean, "/") {
			if strings.Contains(ipClean, ":") {
				ipClean = ipClean + "/128"
			} else {
				ipClean = ipClean + "/32"
			}
		}
		if _, _, err := net.ParseCIDR(ipClean); err == nil {
			allIPs = append(allIPs, ipClean)
		}
	}

	targetServer := GetServerIP()
	if targetServer == "" {
		sessionMu.Lock()
		targetServer = cachedSessionServer
		sessionMu.Unlock()
	}
	if strings.Contains(targetServer, ":") {
		h := strings.Split(targetServer, ":")[0]
		if h != "" {
			targetServer = h
		} else {
			targetServer = GetServerIP()
		}
	}

	rules := []RouteRule{
		{
			Action: "sniff",
		},
		// 1. Exclude core daemons, local DNS proxies, WarLink, Antigravity IDE, and game launchers/anti-cheat from TUN routing
		{
			ProcessName: append([]string{
				"sing-box.exe", "winws2.exe", "winws.exe", "WarLink.exe", "warlink.exe",
				"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe", "dnsproxy.exe", "cloudflared.exe", "stubby.exe", "AdGuardSvc.exe",
				"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
			}, DirectLauncherProcesses...),
			Outbound: "direct",
		},
		// 2. All plain HTTP (port 80) routes direct for instant CRL/OCSP revocation checks
		{
			Port:     []int{80},
			Outbound: "direct",
		},
		// 3. Direct game & anti-cheat domains route direct
		{
			DomainSuffix: DirectGameDomains,
			Outbound:     "direct",
		},
		// 4. Never route loopback, private RFC1918, or link-local subnets through tunnel
		{
			IPCIDR: []string{
				"127.0.0.0/8",
				"::1/128",
				"10.0.0.0/8",
				"172.16.0.0/12",
				"192.168.0.0/16",
				"169.254.0.0/16",
				"fc00::/7",
				"fe80::/10",
			},
			Outbound: "direct",
		},
		// 4b. Never route NTP (UDP 123) through tunnel
		{
			Network:  "udp",
			Port:     []int{123},
			Outbound: "direct",
		},
		// 5. Route FakeIP synthetic pool (198.18.0.0/15) to hy2-stockholm
		{
			IPCIDR:   []string{"198.18.0.0/15"},
			Outbound: "hy2-stockholm",
		},
		// 5b. Google, Antigravity, and AI services must NEVER be hijacked by sing-box DNS -
		// they must resolve via local system / ag_dns directly without interference!
		{
			Protocol: []string{"dns"},
			DomainSuffix: []string{
				"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com",
				"github.com", "githubusercontent.com",
			},
			Outbound: "direct",
		},
		// 6. Hijack remaining DNS queries to resolve through sing-box DNS engine
		{
			Protocol: []string{"dns"},
			Action:   "hijack-dns",
		},
	}

	if targetServer != "" {
		rules = append(rules, RouteRule{
			IPCIDR:   []string{targetServer + "/32"},
			Outbound: "direct",
		})
	}

	if includeWebServices {
		// Reject QUIC (HTTP/3 over UDP 443) only for target blocked web domains to force TCP HTTP/2
		rules = append(rules, RouteRule{
			Action:       "reject",
			Network:      "udp",
			Port:         []int{443},
			DomainSuffix: BlockedServiceDomains,
		})

		rules = append(rules,
			RouteRule{
				DomainSuffix: BlockedServiceDomains,
				Outbound:     "hy2-stockholm",
			},
			RouteRule{
				IPCIDR:   BlockedServiceIPs,
				Outbound: "hy2-stockholm",
			},
		)
	}

	// Route profile domains
	if len(allDomains) > 0 {
		rules = append(rules, RouteRule{
			DomainSuffix: allDomains,
			Outbound:     "hy2-stockholm",
		})
	}

	// Route profile IPs / CIDRs
	if len(allIPs) > 0 {
		rules = append(rules, RouteRule{
			IPCIDR:   allIPs,
			Outbound: "hy2-stockholm",
		})
	}

	// Route WARDOGS dedicated match servers (AWS GameLift UDP 4000-4500, e.g. port 4192) through Stockholm gateway
	// to bypass Russian TSPU/ISP packet drops and ensure stable match connectivity.
	// Steam Datagram Relay (SDR) ping relays stay direct.
	rules = append(rules,
		RouteRule{
			Network:   "udp",
			PortRange: []string{"4000:4500"},
			Outbound:  "hy2-stockholm",
		},
		RouteRule{
			Network:   "udp",
			PortRange: []string{"27000:27200"},
			Outbound:  "direct",
		},
		RouteRule{
			DomainSuffix: DirectGameDomains,
			Outbound:     "direct",
		},
	)

	// Route specified target processes to hy2-stockholm
	if len(allProcesses) > 0 {
		rules = append(rules, RouteRule{
			ProcessName: allProcesses,
			Outbound:    "hy2-stockholm",
		})
	}

	// Default fallback to direct
	rules = append(rules, RouteRule{
		Outbound: "direct",
	})

	var dnsRules []DNSRule
	// Certificate validation and CRL lookups must resolve directly to real IPs
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: CRLDomains,
		Server:       "dns-local",
	})
	// Game & launcher direct domains must resolve locally to real IPs
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: DirectGameDomains,
		Server:       "dns-local",
	})
	// Google / Antigravity / AI domains must resolve locally to real IPs without FakeIP
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: []string{
			"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com",
			"github.com", "githubusercontent.com",
		},
		Server: "dns-local",
	})
	// ag_dns.exe / AGUnlocker / Antigravity handle DNS themselves and must always use local resolver.
	// Launcher / anti-cheat processes also need direct local DNS.
	dnsRules = append(dnsRules, DNSRule{
		ProcessName: append([]string{
			"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe",
			"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
		}, DirectLauncherProcesses...),
		Server: "dns-local",
	})
	// Vivox domains must resolve to real IPs via remote DNS (avoiding FakeIP SIP/SDP mismatches)
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: []string{"vivox.com"},
		Server:       "dns-remote",
	})

	if len(allProcesses) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			ProcessName: allProcesses,
			Server:      "dns-fakeip",
		})
	}

	var fakeDomains []string
	if includeWebServices {
		fakeDomains = append(fakeDomains, BlockedServiceDomains...)
	}
	if len(allDomains) > 0 {
		for _, d := range allDomains {
			isDirect := false
			for _, dd := range DirectGameDomains {
				if strings.HasSuffix(d, dd) {
					isDirect = true
					break
				}
			}
			if isDirect || strings.HasSuffix(d, "vivox.com") {
				continue
			}
			fakeDomains = append(fakeDomains, d)
		}
	}
	if len(fakeDomains) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			DomainSuffix: fakeDomains,
			Server:       "dns-fakeip",
		})
	}

	dnsConfig := &DNSConfig{
		Servers: []DNSServer{
			{
				Tag:        "dns-fakeip",
				Type:       "fakeip",
				Inet4Range: "198.18.0.0/15",
			},
			{
				Tag:    "dns-remote",
				Type:   "tcp",
				Server: "1.1.1.1",
				Detour: "hy2-stockholm",
			},
			{
				Tag:    "dns-local",
				Type:   "local",
				Detour: "direct",
			},
		},
		Rules: dnsRules,
		Final: "dns-local",
	}

	obfsKey := os.Getenv("WARLINK_OBFS_PASSWORD")
	if obfsKey == "" {
		sessionMu.Lock()
		obfsKey = cachedSessionObfs
		sessionMu.Unlock()
	}
	if obfsKey == "" {
		obfsKey = DefaultObfsPassword
	}

	var obfsConfig *Hysteria2Obfs
	if obfsKey != "" {
		obfsConfig = &Hysteria2Obfs{
			Type:     "salamander",
			Password: obfsKey,
		}
	}

	cfg := Config{
		Log: LogConfig{
			Level:     "info",
			Output:    "singbox.log",
			Timestamp: true,
		},
		DNS: dnsConfig,
		Inbounds: []InboundConfig{
			{
				Type:          "tun",
				Tag:           "tun-in",
				InterfaceName: "WarLink-Tun",
				Address:       []string{"172.19.0.1/30"},
				MTU:           1400,
				AutoRoute:     true,
				StrictRoute:   false,
				Stack:         "mixed",
			},
		},
		Outbounds: []Outbound{
			{
				Type:        "hysteria2",
				Tag:         "hy2-stockholm",
				Server:      targetServer,
				ServerPorts: []string{"443:443", "20000:30000"},
				HopInterval: "10m",
				UpMbps:      50,
				DownMbps:    100,
				Password:    token,
				Obfs:        obfsConfig,
				TLS: &OutboundTLSOptions{
					Enabled:    true,
					ServerName: "gateway.warlink.network",
					Insecure:   true,
				},
			},
			{
				Type: "direct",
				Tag:  "direct",
			},
		},
		Route: RouteConfig{
			DefaultDomainResolver: "dns-local",
			FindProcess:           true,
			AutoDetectInterface:   true,
			Rules:                 rules,
		},
		Experimental: &ExperimentalConfig{
			CacheFile: &CacheFileConfig{
				Enabled: true,
				Path:    "cache.db",
			},
		},
	}

	return json.MarshalIndent(cfg, "", "  ")
}

// Manager controls the lifecycle of sing-box.
type Manager struct {
	mu                 sync.Mutex
	coreDir            string
	cmd                *exec.Cmd
	isRunning          bool
	targetProcesses    []string
	includeWebServices bool
}

func NewManager(coreDir string) *Manager {
	return &Manager{
		coreDir: coreDir,
	}
}

func (m *Manager) GetBinDir() string {
	return filepath.Join(m.coreDir, "singbox")
}

func (m *Manager) GetExePath() string {
	return filepath.Join(m.GetBinDir(), "sing-box.exe")
}

func (m *Manager) GetWintunPath() string {
	return filepath.Join(m.GetBinDir(), "wintun.dll")
}

func (m *Manager) HasBinaries() bool {
	if _, err := os.Stat(m.GetExePath()); err != nil {
		return false
	}
	if _, err := os.Stat(m.GetWintunPath()); err != nil {
		return false
	}
	// Verify that the existing sing-box binary supports QUIC
	cmd := exec.Command(m.GetExePath(), "version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "with_quic") {
		return false
	}
	return true
}

// EnsureFiles downloads and extracts sing-box and wintun if missing or outdated.
func (m *Manager) EnsureFiles(logFn func(string)) error {
	if m.HasBinaries() {
		return nil
	}

	binDir := m.GetBinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create singbox dir: %w", err)
	}

	// 1. Check sing-box with with_quic
	needDownload := false
	if _, err := os.Stat(m.GetExePath()); err != nil {
		needDownload = true
	} else {
		cmd := exec.Command(m.GetExePath(), "version")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		out, err := cmd.Output()
		if err != nil || !strings.Contains(string(out), "with_quic") {
			needDownload = true
		}
	}

	if needDownload {
		if logFn != nil {
			logFn("[INFO] Загрузка сетевого роутера sing-box с поддержкой Hysteria 2 / QUIC...")
		}
		_ = os.Remove(m.GetExePath())
		if err := downloadAndExtractZip(SingBoxDownloadURL, binDir, "sing-box.exe", m.GetExePath()); err != nil {
			return fmt.Errorf("ошибка скачивания sing-box: %w", err)
		}
		if logFn != nil {
			logFn("[OK] sing-box успешно установлен")
		}
	}

	// 2. Check/Copy/Download wintun.dll
	if _, err := os.Stat(m.GetWintunPath()); err != nil {
		// First check local system installations for existing signed wintun.dll
		localCandidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Cloudflare", "Cloudflare WARP", "wintun.dll"),
			filepath.Join(os.Getenv("ProgramFiles"), "WireGuard", "wintun.dll"),
			filepath.Join(os.Getenv("ProgramFiles"), "FlyFrogLLC", "Happ", "core", "wintun.dll"),
		}
		copied := false
		for _, cand := range localCandidates {
			if data, readErr := os.ReadFile(cand); readErr == nil && len(data) > 100000 {
				if writeErr := os.WriteFile(m.GetWintunPath(), data, 0755); writeErr == nil {
					if logFn != nil {
						logFn("[OK] Драйвер WinTun успешно инициализирован из локальной системы")
					}
					copied = true
					break
				}
			}
		}
		if !copied {
			if logFn != nil {
				logFn("[INFO] Скачивание официального драйвера WinTun...")
			}
			if err := downloadAndExtractZip(WintunDownloadURL, binDir, "wintun.dll", m.GetWintunPath()); err != nil {
				return fmt.Errorf("ошибка скачивания wintun.dll: %w", err)
			}
			if logFn != nil {
				logFn("[OK] wintun.dll готов к работе")
			}
		}
	}

	return nil
}

// Start launches sing-box targeting the specified game processes and optional web services.
func (m *Manager) Start(targetProcesses []string, includeWebServices bool, logFn func(string), game ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return nil
	}

	if err := m.EnsureFiles(logFn); err != nil {
		return err
	}

	targetGame := "wardogs"
	if len(game) > 0 && strings.TrimSpace(game[0]) != "" {
		targetGame = strings.TrimSpace(game[0])
	} else if len(targetProcesses) == 0 && includeWebServices {
		targetGame = "free_internet"
	}

	// Request active session from Stockholm server
	if logFn != nil {
		logFn(fmt.Sprintf("[INFO] Авторизация на шлюзе Стокгольм (%s, проверка свободных слотов)...", GetServerIP()))
	}
	token, err := AcquireSession(targetGame)
	if err != nil {
		return fmt.Errorf("ошибка шлюза: %w", err)
	}
	if logFn != nil {
		logFn("[OK] Авторизация на шлюзе Стокгольм успешна (токен выдан)")
	}

	m.targetProcesses = append([]string(nil), targetProcesses...)
	m.includeWebServices = includeWebServices

	// 1. Primary: fetch dynamic sing-box configuration directly from remote server
	var cfgBytes []byte
	if remoteCfg, rErr := FetchRemoteConfig(GetServerAPI(), token, targetGame, targetProcesses, includeWebServices); rErr == nil && len(remoteCfg) > 0 {
		cfgBytes = remoteCfg
		if logFn != nil {
			logFn("[OK] Получена динамическая конфигурация sing-box со шлюза")
		}
	} else {
		// 2. Fallback: local profile-based generation
		if logFn != nil && rErr != nil {
			logFn(fmt.Sprintf("[WARN] Загрузка удаленной конфигурации (%v), переход на локальную генерацию", rErr))
		}
		var activeProfiles []Profile
		profiles, pErr := FetchProfiles(GetServerAPI())
		if pErr == nil && len(profiles) > 0 {
			activeProfiles = profiles
			if logFn != nil {
				logFn(fmt.Sprintf("[OK] Загружено %d профилей маршрутизации с сервера (игры, соцсети)", len(profiles)))
			}
		}

		genBytes, genErr := GenerateConfigFromProfiles(activeProfiles, targetProcesses, includeWebServices, token)
		if genErr != nil {
			return fmt.Errorf("ошибка формирования конфигурации sing-box: %w", genErr)
		}
		cfgBytes = genBytes
	}

	cfgPath := filepath.Join(m.GetBinDir(), "config.json")
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		return fmt.Errorf("ошибка сохранения config.json: %w", err)
	}

	if logFn != nil {
		modeStr := "Игровой шлюз Стокгольм (27 мс)"
		if includeWebServices {
			if len(targetProcesses) > 0 {
				modeStr = "Композитный шлюз (Игра + Свободный интернет)"
			} else {
				modeStr = "Свободный интернет (Стокгольм)"
			}
		}
		logFn(fmt.Sprintf("[INFO] Запуск туннеля Hysteria 2 (%s: %s)...", modeStr, strings.Join(targetProcesses, ", ")))
	}

	// Clean up any stale instances
	_ = killProcessByName("sing-box.exe")

	cmd := exec.Command(m.GetExePath(), "run", "-c", cfgPath)
	cmd.Dir = m.GetBinDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	stderrPath := filepath.Join(m.GetBinDir(), "singbox_stderr.log")
	if stderrFile, errOpen := os.OpenFile(stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); errOpen == nil {
		cmd.Stderr = stderrFile
		cmd.Stdout = stderrFile
		defer stderrFile.Close()
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ошибка запуска sing-box: %w", err)
	}

	m.cmd = cmd
	m.isRunning = true

	// Check if process exited immediately due to config error
	time.Sleep(500 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		m.isRunning = false
		errDetail := ""
		if errData, errRead := os.ReadFile(stderrPath); errRead == nil && len(errData) > 0 {
			errDetail = ": " + strings.TrimSpace(string(errData))
		}
		return fmt.Errorf("sing-box аварийно завершился сразу после запуска%s", errDetail)
	}

	if logFn != nil {
		logFn("[OK] Туннель Hysteria 2 активен (PID: " + fmt.Sprintf("%d", cmd.Process.Pid) + ", пинг 27 мс)")
	}

	return nil
}

// Stop terminates sing-box cleanly.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning && m.cmd == nil {
		return nil
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
	m.isRunning = false

	_ = killProcessByName("sing-box.exe")

	return nil
}

func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}

// AddTargetProcess dynamically adds a newly discovered process to the routing list and reloads sing-box.
func (m *Manager) AddTargetProcess(proc string, logFn func(string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc = strings.TrimSpace(proc)
	if proc == "" {
		return nil
	}
	for _, p := range m.targetProcesses {
		if strings.EqualFold(p, proc) {
			return nil
		}
	}
	m.targetProcesses = append(m.targetProcesses, proc)
	if !m.isRunning {
		return nil
	}

	token, _ := AcquireSession()
	var cfgBytes []byte
	if remoteCfg, rErr := FetchRemoteConfig(GetServerAPI(), token, "", m.targetProcesses, m.includeWebServices); rErr == nil && len(remoteCfg) > 0 {
		cfgBytes = remoteCfg
	} else {
		activeProfiles, _ := FetchProfiles()
		genBytes, genErr := GenerateConfigFromProfiles(activeProfiles, m.targetProcesses, m.includeWebServices, token)
		if genErr != nil {
			return genErr
		}
		cfgBytes = genBytes
	}
	cfgPath := filepath.Join(m.GetBinDir(), "config.json")
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		return err
	}

	if logFn != nil {
		logFn(fmt.Sprintf("[INFO] Добавлен дочерний процесс игры в шлюз: %s", proc))
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
	_ = killProcessByName("sing-box.exe")

	cmd := exec.Command(m.GetExePath(), "run", "-c", cfgPath)
	cmd.Dir = m.GetBinDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	if stderrFile, errOpen := os.OpenFile(filepath.Join(m.GetBinDir(), "singbox_stderr.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); errOpen == nil {
		cmd.Stderr = stderrFile
		cmd.Stdout = stderrFile
		defer stderrFile.Close()
	}
	if err := cmd.Start(); err != nil {
		m.isRunning = false
		return err
	}
	m.cmd = cmd
	m.isRunning = true
	return nil
}

// IsProcessAlive checks whether the sing-box process is genuinely running in the Windows kernel.
func (m *Manager) IsProcessAlive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning || m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	h, err := syscall.OpenProcess(0x1000, false, uint32(m.cmd.Process.Pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}

func downloadAndExtractZip(url, destDir, targetFileName, finalDestPath string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) WarLink/2.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	for _, file := range zipReader.File {
		if strings.EqualFold(filepath.Base(file.Name), targetFileName) {
			// If it's wintun, ensure we pick amd64 if path has amd64
			if targetFileName == "wintun.dll" && !strings.Contains(strings.ToLower(file.Name), "amd64") {
				continue
			}

			rc, err := file.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			outFile, err := os.OpenFile(finalDestPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, rc); err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("файл %s не найден в архиве", targetFileName)
}

func killProcessByName(name string) error {
	cmd := exec.Command("taskkill", "/F", "/IM", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd.Run()
}
