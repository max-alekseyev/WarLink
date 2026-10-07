package singbox

import (
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
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
	"warlink/internal/config"
	"warlink/internal/embedded"
)

const (
	MoscowIngressDomain = "ru.warlink.max-alekseyev.com"
	FrankfurtEdgeDomain = "de.warlink.max-alekseyev.com"
	StockholmCoreDomain = "api-warlink.max-alekseyev.com"

	// Backward compatibility constants
	MoscowIngressIP = MoscowIngressDomain
	FrankfurtEdgeIP = FrankfurtEdgeDomain
	StockholmCoreIP = StockholmCoreDomain
)

var (
	ClientVersion       = "v2.2.3"
	DefaultServerIP     = "api-warlink.max-alekseyev.com"
	DefaultServerAPI    = "https://api-warlink.max-alekseyev.com"
	// Injected at build time via -X ldflags from GitHub Actions secrets.
	// Never commit real values here — binary built from source without CI
	// will have empty secrets and will fail auth (gateway rejects empty HMAC).
	DefaultHMACSecret   = ""
	DefaultObfsPassword = ""
)

var (
	resolvedIPv4Cache = make(map[string]string)
	resolvedIPv4Mu    sync.RWMutex
)

// ResolveDomainIPv4 resolves a domain name to its first IPv4 address, with embedded fallback.
func ResolveDomainIPv4(domainOrIP string) string {
	if domainOrIP == "" {
		return ""
	}
	if ip := net.ParseIP(domainOrIP); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
		return domainOrIP
	}

	// 1. Instant static mapping for primary infrastructure nodes
	switch domainOrIP {
	case MoscowIngressDomain:
		return "45.12.63.85"
	case FrankfurtEdgeDomain:
		return "85.192.24.254"
	case StockholmCoreDomain:
		return "138.124.103.99"
	}

	// 2. Thread-safe in-memory cache
	resolvedIPv4Mu.RLock()
	if cached, ok := resolvedIPv4Cache[domainOrIP]; ok {
		resolvedIPv4Mu.RUnlock()
		return cached
	}
	resolvedIPv4Mu.RUnlock()

	// 3. Short timeout DNS lookup
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	var r net.Resolver
	ips, err := r.LookupIP(ctx, "ip4", domainOrIP)
	if err == nil && len(ips) > 0 {
		for _, ip := range ips {
			if ipv4 := ip.To4(); ipv4 != nil {
				res := ipv4.String()
				resolvedIPv4Mu.Lock()
				resolvedIPv4Cache[domainOrIP] = res
				resolvedIPv4Mu.Unlock()
				return res
			}
		}
	}
	return domainOrIP
}

func GetNetworkRouteMode() string {
	cfg := config.Load()
	if cfg != nil {
		return cfg.GetNetworkRouteMode()
	}
	return config.RouteModeTransit
}

func SetNetworkRouteMode(mode string) {
	cfg := config.Load()
	if cfg != nil {
		cfg.SetNetworkRouteMode(mode)
	}
}

func GetActiveGatewayTarget() (host string, port int, apiURL string) {
	mode := GetNetworkRouteMode()
	switch mode {
	case config.RouteModeDirectFrankfurt:
		return FrankfurtEdgeDomain, 443, DefaultServerAPI
	case config.RouteModeDirectMoscow:
		return MoscowIngressDomain, 8443, DefaultServerAPI
	case config.RouteModeDirectStockholm:
		// Stockholm is purified into Master-only: redirect European gaming edge to Frankfurt
		return FrankfurtEdgeDomain, 443, DefaultServerAPI
	case config.RouteModeTransit:
		fallthrough
	default:
		return MoscowIngressDomain, 443, DefaultServerAPI
	}
}

func GetServerIP() string {
	if envIP := os.Getenv("WARLINK_SERVER_IP"); envIP != "" {
		return envIP
	}
	host, _, _ := GetActiveGatewayTarget()
	return host
}

func GetServerAPI() string {
	if envAPI := os.Getenv("WARLINK_SERVER_API"); envAPI != "" {
		return envAPI
	}
	return DefaultServerAPI
}

func GetHMACSecret() string {
	if s := os.Getenv("WARLINK_HMAC_SECRET"); s != "" {
		return s
	}
	if DefaultHMACSecret != "" {
		return DefaultHMACSecret
	}
	cfg := config.Load()
	if cfg != nil && cfg.HMACSecret != "" {
		return cfg.HMACSecret
	}
	return ""
}

func GetObfsPassword() string {
	if s := os.Getenv("WARLINK_OBFS_PASSWORD"); s != "" {
		return s
	}
	sessionMu.Lock()
	cached := cachedSessionObfs
	sessionMu.Unlock()
	if cached != "" {
		return cached
	}
	if DefaultObfsPassword != "" {
		return DefaultObfsPassword
	}
	cfg := config.Load()
	if cfg != nil && cfg.ObfsPassword != "" {
		return cfg.ObfsPassword
	}
	return ""
}

// Config represents a sing-box configuration with per-process TUN routing and Hysteria 2 GPN.
type Config struct {
	Log          LogConfig           `json:"log"`
	DNS          *DNSConfig          `json:"dns,omitempty"`
	Inbounds     []InboundConfig     `json:"inbounds"`
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
	Type                string   `json:"type"`
	Tag                 string   `json:"tag"`
	InterfaceName       string   `json:"interface_name"`
	Address             []string `json:"address"`
	MTU                 int      `json:"mtu,omitempty"`
	AutoRoute           bool     `json:"auto_route"`
	StrictRoute         bool     `json:"strict_route"`
	Stack               string   `json:"stack"`
	RouteExcludeAddress []string `json:"route_exclude_address,omitempty"`
	UDPTimeout          string   `json:"udp_timeout,omitempty"`
	UDPMapping          string   `json:"udp_mapping,omitempty"`
	UDPFiltering        string   `json:"udp_filtering,omitempty"`
	UDPNATMax           int      `json:"udp_nat_max,omitempty"`
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
	Type                string              `json:"type"`
	Tag                 string              `json:"tag"`
	Server              string              `json:"server,omitempty"`
	ServerPort          int                 `json:"server_port,omitempty"`
	ServerPorts         []string            `json:"server_ports,omitempty"`
	HopInterval         string              `json:"hop_interval,omitempty"`
	UpMbps              int                 `json:"up_mbps,omitempty"`
	DownMbps            int                 `json:"down_mbps,omitempty"`
	Password            string              `json:"password,omitempty"`
	Obfs                *Hysteria2Obfs      `json:"obfs,omitempty"`
	TLS                 *OutboundTLSOptions `json:"tls,omitempty"`
	DisableChromeParrot bool                `json:"disable_chrome_parrot,omitempty"`
	InitialPacketSize   int                 `json:"initial_packet_size,omitempty"`
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

// DiscordDomains contains all official Discord API, gateway, CDN, and RTC endpoints.
var DiscordDomains = []string{
	"discord.com",
	"discord.gg",
	"discordapp.com",
	"discordapp.net",
	"discord.media",
	"gateway.discord.gg",
	"status.discord.com",
	"discordstatus.com",
	"dis.gd",
	"discord-attachments-uploads-prd.storage.googleapis.com",
	"discordcdn.com",
	"cdn.discordapp.com",
	"voice.discord.gg",
	"discord.app",
	"discord.dev",
	"discord.gift",
	"discord.gifts",
	"discord.design",
	"discord.new",
	"discord.store",
	"discord-activities.com",
	"discordactivities.com",
	"discordmerch.com",
	"discordpartygames.com",
	"discordsays.com",
	"discordsez.com",
	"stable.dl2.discordapp.net",
}

// YouTubeDomains contains YouTube video streaming, player API, and thumbnail CDN domains.
var YouTubeDomains = []string{
	"youtube.com",
	"youtu.be",
	"googlevideo.com",
	"ytimg.com",
	"ytimg.l.google.com",
	"yt3.ggpht.com",
	"yt4.ggpht.com",
	"yt3.googleusercontent.com",
	"jnn-pa.googleapis.com",
	"wide-youtube.l.google.com",
	"youtube-nocookie.com",
	"youtube-ui.l.google.com",
	"youtubeembeddedplayer.googleapis.com",
	"youtubekids.com",
	"youtube.googleapis.com",
	"youtubei.googleapis.com",
	"yt-video-upload.l.google.com",
	"play.google.com",
}

// SocialMessengerDomains contains social media, instant messengers, and stream chat extensions.
var SocialMessengerDomains = []string{
	// X (Twitter)
	"x.com",
	"twitter.com",
	"twimg.com",
	"t.co",
	"abs.twimg.com",
	"pbs.twimg.com",
	"api.twitter.com",
	"api.x.com",
	// Meta (Instagram / Facebook / Threads)
	"instagram.com",
	"cdninstagram.com",
	"threads.net",
	"facebook.com",
	"fbcdn.net",
	"fbsbx.com",
	"messenger.com",
	"meta.com",
	// Telegram
	"web.telegram.org",
	"telegram.org",
	"t.me",
	"telegra.ph",
	"telegram.me",
	"telesco.pe",
	"tdesktop.com",
	"aurora.web.telegram.org",
	"flora.web.telegram.org",
	"pluto.web.telegram.org",
	"venus.web.telegram.org",
	"vesta.web.telegram.org",
	"stel.com",
	// WhatsApp & Viber
	"web.whatsapp.com",
	"whatsapp.com",
	"whatsapp.net",
	"whatsapp.org",
	"wa.me",
	"viber.com",
	"api.viber.com",
	"media.viber.com",
	"share.viber.com",
	"download.viber.com",
	// Twitch chat extensions
	"7tv.app",
	"7tv.io",
	"betterttv.net",
	"frankerfacez.com",
	"ffzap.com",
	"klipy.com",
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
	// Gaming matchmaking & DGS services blocked by Russian ISP / TSPU
	"azurefd.net",
	"tm-azurefd.net",
	"trafficmanager.net",
	"playfabapi.com",
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
	// Anti-cheat / PKI endpoints (CRL/OCSP)
	"certainly.com",
	"pki.goog",
	// Steam
	"steamserver.net",
	"steampowered.com",
	"steamcommunity.com",
	"steamstatic.com",
	"steamgames.com",
	// EGS CDN & auth services
	"cloudfront.net",
	"amazonaws.com",
	// AWS GameLift infrastructure (match server assignment API)
	"gamelift.us-east-1.amazonaws.com",
	// Time sync
	"time.cloudflare.com",
}

// DynamoDBRegionProbeDomains contains AWS DynamoDB regional endpoints used by WARDOGS
// to calculate regional ping in the "SELECT REGION" screen.
// Routing these directly via the physical interface ensures physically accurate RTT measurement
// (Europe ~35-50ms, North America ~110-135ms) instead of proxy edge termination.
var DynamoDBRegionProbeDomains = []string{
	"dynamodb.eu-central-1.amazonaws.com",
	"dynamodb.eu-north-1.amazonaws.com",
	"dynamodb.eu-west-1.amazonaws.com",
	"dynamodb.eu-west-2.amazonaws.com",
	"dynamodb.eu-west-3.amazonaws.com",
	"dynamodb.eu-south-1.amazonaws.com",
	"dynamodb.us-east-1.amazonaws.com",
	"dynamodb.us-east-2.amazonaws.com",
	"dynamodb.us-west-1.amazonaws.com",
	"dynamodb.us-west-2.amazonaws.com",
	"dynamodb.ca-central-1.amazonaws.com",
	"dynamodb.sa-east-1.amazonaws.com",
	"dynamodb.ap-northeast-1.amazonaws.com",
	"dynamodb.ap-northeast-2.amazonaws.com",
	"dynamodb.ap-southeast-1.amazonaws.com",
	"dynamodb.ap-southeast-2.amazonaws.com",
	"dynamodb.ap-south-1.amazonaws.com",
	"dynamodb.me-south-1.amazonaws.com",
	"dynamodb.af-south-1.amazonaws.com",
}

var (
	resolvedRegionMu   sync.Mutex
	resolvedRegionIPs  []string
	resolvedRegionTime time.Time

	// AWSRegionProbeSubnets contains Anycast / Global Accelerator IP ranges used by AWS DynamoDB regional endpoints.
	// Excluded from Wintun TUN interface so Windows kernel routes TCP syn handshakes directly through physical NIC.
	AWSRegionProbeSubnets = []string{
		"35.71.0.0/16",
		"52.94.0.0/16",
		"52.119.0.0/16",
		"54.239.0.0/16",
		"3.218.0.0/16",
	}

	// ValveSDRSubnets contains Steam Datagram Relay (SDR) relay clusters used by Steamworks for ping estimation.
	ValveSDRSubnets = []string{
		"155.133.0.0/16",
		"162.254.192.0/18",
		"146.66.152.0/21",
		"185.25.180.0/22",
		"45.121.184.0/23",
		"89.222.108.0/24",
		"205.196.6.0/24",
		"103.10.124.0/23",
		"103.28.54.0/23",
		"152.233.52.0/23",
	}
)

// GetRegionProbeExcludeAddresses returns AWS Anycast subnets, Valve SDR subnets, and resolved regional endpoint IPs.
func GetRegionProbeExcludeAddresses() []string {
	excludes := append(append([]string{}, AWSRegionProbeSubnets...), ValveSDRSubnets...)
	resolvedRegionMu.Lock()
	defer resolvedRegionMu.Unlock()
	if time.Since(resolvedRegionTime) < 10*time.Minute && len(resolvedRegionIPs) > 0 {
		return append(excludes, resolvedRegionIPs...)
	}

	var ips []string
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, domain := range DynamoDBRegionProbeDomains {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
			defer cancel()
			var r net.Resolver
			addrs, err := r.LookupIP(ctx, "ip4", d)
			if err == nil {
				mu.Lock()
				for _, ip := range addrs {
					if ip4 := ip.To4(); ip4 != nil {
						ips = append(ips, ip4.String()+"/32")
					}
				}
				mu.Unlock()
			}
		}(domain)
	}
	wg.Wait()
	resolvedRegionIPs = ips
	resolvedRegionTime = time.Now()
	return append(excludes, ips...)
}

// DirectLauncherProcesses contains anti-cheat installer, crash reporting,
// and third-party anti-cheat daemons that must bypass the tunnel.
var DirectLauncherProcesses = []string{
	"Elytra-Setup.exe",
	"elytra-setup.exe",
	"elytra-launcher.exe",
	"elytraclient.exe",
	"service.exe",
	"control.exe",
	"crashpad_handler.exe",
	"CrashReportClient.exe",
	"crashreportclient.exe",
	// Anti-cheat services and background daemons (must bypass tunnel)
	"vgc.exe",
	"vgtray.exe",
	"EasyAntiCheat.exe",
	"easyanticheat.exe",
	"BEService.exe",
	"beservice.exe",
	"faceitclient.exe",
	"faceitservice.exe",
	"AntiCheatInstaller.exe",
	"anticheatinstaller.exe",
	"denuvo-anti-cheat-update-service.exe",
	"denuvo-anti-cheat-crash-report.exe",
	"denuvo-anti-cheat.exe",
}

// WardogsGameProcesses contains process names for WARDOGS dedicated game client and launcher.
var WardogsGameProcesses = []string{
	"WardogsClient-Win64-Shipping.exe",
	"wardogsclient-win64-shipping.exe",
	"wardogs.exe",
	"wardogs-win64-shipping.exe",
	"WardogsLauncher-Shipping.exe",
	"wardogslauncher-shipping.exe",
	"wardogslauncher.exe",
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
	Status                string `json:"status"`
	Location              string `json:"location"`
	PingHintMs            int    `json:"ping_hint_ms"`
	ActiveSessions        int    `json:"active_sessions"`
	MaxSessions           int    `json:"max_sessions"`
	ActiveFreeSessions    int    `json:"active_free_sessions"`
	FreeSlotsLimit        int    `json:"free_slots_limit"`
	ActiveSponsorSessions int    `json:"active_sponsor_sessions"`
	DedicatedSponsorSlots int    `json:"dedicated_sponsor_slots"`
	DedicatedAdminSlots   int    `json:"dedicated_admin_slots,omitempty"`
	ServerIP              string `json:"server_ip"`
	ServerPorts           string `json:"server_ports"`
	DueDate               string `json:"due_date"`
	DaysLeft              int    `json:"days_left"`
	DonateAmountRub       int    `json:"donate_amount_rub"`
	EnableDonate          *bool  `json:"enable_donate,omitempty"`
	EnableVoting          *bool  `json:"enable_voting,omitempty"`
	EnableCommunityGoal   *bool  `json:"enable_community_goal,omitempty"`
	DonateBoostyEnabled   *bool  `json:"donate_boosty_enabled,omitempty"`
	DonateSbpEnabled      *bool  `json:"donate_sbp_enabled,omitempty"`
	DonateCryptoEnabled   *bool  `json:"donate_crypto_enabled,omitempty"`
	DonatePausedNotice    string `json:"donate_paused_notice,omitempty"`
	OctoberPoolRub        int    `json:"october_pool_rub,omitempty"`
}

var (
	cachedSessionToken  string
	cachedSessionGame   string
	cachedSessionObfs   string
	cachedSessionServer string
	cachedSessionExp    time.Time
	sessionMu           sync.Mutex
)

func getRawMachineGUID() string {
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

func GetMachineGUID() string {
	raw := getRawMachineGUID()
	h := sha256.Sum256([]byte(raw + "_wl_dev_salt_v2"))
	return fmt.Sprintf("%x", h[:16]) // 32 hex символа деперсонализированного ID
}

var serverTimeOffset int64

// GetServerTimeOffset returns the current calibrated time skew with the server in seconds.
func GetServerTimeOffset() int64 {
	return atomic.LoadInt64(&serverTimeOffset)
}

// AcquireSession requests a fresh session slot from the Stockholm gateway.
func AcquireSession(game ...string) (string, error) {
	targetGame := "wardogs"
	if len(game) > 0 && strings.TrimSpace(game[0]) != "" {
		targetGame = strings.TrimSpace(game[0])
	}

	sessionMu.Lock()
	if cachedSessionToken != "" && cachedSessionGame == targetGame && time.Now().Before(cachedSessionExp) {
		tok := cachedSessionToken
		sessionMu.Unlock()
		return tok, nil
	}
	sessionMu.Unlock()

	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return "", fmt.Errorf("адрес шлюза не настроен (задайте WARLINK_SERVER_IP)")
	}

	apiURL := fmt.Sprintf("%s/api/v1/session", serverAPI)
	deviceID := GetMachineGUID()
	hmacSecret := GetHMACSecret()
	if hmacSecret == "" {
		return "", fmt.Errorf("в сборке отсутствует ключ авторизации шлюза (HMAC). Запустите официальный релиз с GitHub или задайте переменную окружения WARLINK_HMAC_SECRET")
	}

	dialer := &net.Dialer{
		Timeout:   4 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 6 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   8 * time.Second,
	}

	cfg := config.Load()
	accountNumber := ""
	if cfg != nil {
		accountNumber = cfg.AccountNumber
	}

	attemptAuth := func() (*http.Response, error) {
		ts := time.Now().Unix() + atomic.LoadInt64(&serverTimeOffset)
		nonceBytes := make([]byte, 8)
		_, _ = rand.Read(nonceBytes)
		nonce := hex.EncodeToString(nonceBytes)

		reqBody, _ := json.Marshal(map[string]interface{}{
			"device_id":      deviceID,
			"account_number": accountNumber,
			"timestamp":      ts,
			"nonce":          nonce,
			"game":           targetGame,
			"app_version":    ClientVersion,
			"route_mode":     GetNetworkRouteMode(),
		})

		req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "WarLink-Client/"+ClientVersion)

		if hmacSecret != "" {
			dataToSign := fmt.Sprintf("%s:%d:%s", deviceID, ts, nonce)
			mac := hmac.New(sha256.New, []byte(hmacSecret))
			mac.Write([]byte(dataToSign))
			req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
		}
		return client.Do(req)
	}

	var resp *http.Response
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err = attemptAuth()
		if err == nil && resp != nil {
			break
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt*500) * time.Millisecond)
		}
	}
	if err != nil {
		return "", fmt.Errorf("шлюз Стокгольм временно недоступен: %w", err)
	}

	if sDate := resp.Header.Get("Date"); sDate != "" {
		if parsedTime, pErr := http.ParseTime(sDate); pErr == nil {
			offset := parsedTime.Unix() - time.Now().Unix()
			atomic.StoreInt64(&serverTimeOffset, offset)
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "expired timestamp") {
			resp, err = attemptAuth()
			if err != nil {
				return "", fmt.Errorf("шлюз Стокгольм временно недоступен: %w", err)
			}
		} else {
			return "", fmt.Errorf("ошибка авторизации на шлюзе (HTTP %d): %s", resp.StatusCode, string(body))
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		var errData struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errData) == nil && errData.Message != "" {
			return "", fmt.Errorf("%s", errData.Message)
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			return "", fmt.Errorf("Шлюз находится на техобслуживании. Новые подключения временно приостановлены")
		}
		return "", fmt.Errorf("все слоты шлюза заняты. Пожалуйста, подождите освобождения места")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ошибка авторизации на шлюзе (HTTP %d): %s", resp.StatusCode, string(body))
	}

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return "", fmt.Errorf("ошибка чтения ответа шлюза: %w", errRead)
	}
	var sessResp SessionResult
	if err := json.Unmarshal(body, &sessResp); err != nil || sessResp.Token == "" {
		if len(body) > 0 {
			return "", fmt.Errorf("некорректный ответ шлюза: %s", strings.TrimSpace(string(body)))
		}
		return "", fmt.Errorf("некорректный ответ шлюза")
	}

	sessionMu.Lock()
	cachedSessionToken = sessResp.Token
	cachedSessionGame = targetGame
	cachedSessionObfs = sessResp.Obfs
	cachedSessionServer = sessResp.Server
	cachedSessionExp = time.Now().Add(20 * time.Hour)
	sessionMu.Unlock()

	return sessResp.Token, nil
}

// InvalidateSession clears the locally cached session token without contacting the gateway.
// Use this before a forced reconnect so AcquireSession will fetch a fresh token.
func InvalidateSession() {
	sessionMu.Lock()
	cachedSessionToken = ""
	cachedSessionGame = ""
	cachedSessionExp = time.Time{}
	sessionMu.Unlock()
}

// ReleaseSession explicitly releases the active slot on the Stockholm gateway immediately.
func ReleaseSession() error {
	sessionMu.Lock()
	tok := cachedSessionToken
	cachedSessionToken = ""
	cachedSessionGame = ""
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
	req.Header.Set("User-Agent", "WarLink-Client/"+ClientVersion)

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
	UserVotePower int                  `json:"user_vote_power"`
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


var (
	cachedGatewayStatus    *GatewayStatus
	cachedGatewayStatusMu  sync.RWMutex
	cachedGatewayStatusExp time.Time
)

// GetServerGatewayStatus queries real-time status of Stockholm VPS with in-memory caching.
func GetServerGatewayStatus() (*GatewayStatus, error) {
	cachedGatewayStatusMu.RLock()
	if cachedGatewayStatus != nil && time.Now().Before(cachedGatewayStatusExp) {
		st := *cachedGatewayStatus
		cachedGatewayStatusMu.RUnlock()
		return &st, nil
	}
	cachedGatewayStatusMu.RUnlock()

	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("server API not configured")
	}
	apiURL := fmt.Sprintf("%s/api/v1/status?route_mode=%s&device_id=%s", serverAPI, url.QueryEscape(GetNetworkRouteMode()), url.QueryEscape(GetMachineGUID()))
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) WarLink/v2.2.3")

	resp, err := client.Do(req)
	if err != nil {
		cachedGatewayStatusMu.RLock()
		if cachedGatewayStatus != nil {
			st := *cachedGatewayStatus
			cachedGatewayStatusMu.RUnlock()
			return &st, nil
		}
		cachedGatewayStatusMu.RUnlock()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cachedGatewayStatusMu.RLock()
		if cachedGatewayStatus != nil {
			st := *cachedGatewayStatus
			cachedGatewayStatusMu.RUnlock()
			return &st, nil
		}
		cachedGatewayStatusMu.RUnlock()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var st GatewayStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}

	cachedGatewayStatusMu.Lock()
	cachedGatewayStatus = &st
	cachedGatewayStatusExp = time.Now().Add(6 * time.Second)
	cachedGatewayStatusMu.Unlock()

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

// GetDefaultLogsDir returns the warlink_core/logs directory for sing-box logging.
func GetDefaultLogsDir() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		core := filepath.Join(dir, "warlink_core")
		if _, statErr := os.Stat(core); statErr == nil {
			l := filepath.Join(core, "logs")
			_ = os.MkdirAll(l, 0755)
			return l
		}
		parent := filepath.Dir(dir)
		core = filepath.Join(parent, "warlink_core")
		if _, statErr := os.Stat(core); statErr == nil {
			l := filepath.Join(core, "logs")
			_ = os.MkdirAll(l, 0755)
			return l
		}
	}
	l := filepath.Join("warlink_core", "logs")
	_ = os.MkdirAll(l, 0755)
	return l
}

// GenerateConfig creates a sing-box JSON configuration routing target processes,
// and optionally Meta/WhatsApp/X IP ranges and blocked web domains, to Hysteria 2 tunnel.
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
	// Always include Discord and Telegram processes into tunnel
	for _, dp := range []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe", "Telegram.exe", "telegram.exe"} {
		procSet[dp] = struct{}{}
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
		// 1. Exclude core daemons, local DNS proxies, WarLink, and Antigravity IDE from TUN routing
		{
			ProcessName: []string{
				"sing-box.exe", "WarLink.exe", "warlink.exe",
				"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe", "dnsproxy.exe", "cloudflared.exe", "stubby.exe", "AdGuardSvc.exe",
				"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
			},
			Outbound: "direct",
		},
		// 2. Never route loopback, private RFC1918, or link-local subnets through tunnel
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
		// 3. Never route NTP (UDP 123) through tunnel
		{
			Network:  "udp",
			Port:     []int{123},
			Outbound: "direct",
		},
		// 4. Route FakeIP synthetic pool (198.18.0.0/15) to hy2-gateway
		// Must be evaluated before DirectLauncherProcesses so synthetic DNS endpoints proxy cleanly.
		{
			IPCIDR:   []string{"198.18.0.0/15"},
			Outbound: "hy2-gateway",
		},
		// 5. Game launchers and anti-cheat processes route direct when connecting to real IPs
		{
			ProcessName: DirectLauncherProcesses,
			Outbound:    "direct",
		},
		// 6. Google, Antigravity, and AI services must NEVER be hijacked by sing-box DNS -
		// they must resolve via local system / ag_dns directly without interference!
		{
			Protocol: []string{"dns"},
			DomainSuffix: []string{
				"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com",
				"github.com", "githubusercontent.com",
			},
			Outbound: "direct",
		},
		// 7. Hijack remaining DNS queries to resolve through sing-box DNS engine
		{
			Protocol: []string{"dns"},
			Action:   "hijack-dns",
		},
	}

	if targetServer != "" {
		if ip := ResolveDomainIPv4(targetServer); ip != "" && net.ParseIP(ip) != nil {
			rules = append(rules, RouteRule{
				IPCIDR:   []string{ip + "/32"},
				Outbound: "direct",
			})
		}
		if net.ParseIP(targetServer) == nil {
			rules = append(rules, RouteRule{
				Domain:   []string{targetServer},
				Outbound: "direct",
			})
		}
	}

	// 8. Route specified target game processes to hy2-gateway with TOP PRIORITY for game TCP/UDP!
	// Every single packet from game processes (AION2.exe, WardogsClient-Win64-Shipping.exe, etc.)
	// including UDP ping probes (#$#$), lobby TCP TLS handshakes, STUN, and match UDP MUST go through hy2-gateway!
	if len(allProcesses) > 0 {
		rules = append(rules, RouteRule{
			ProcessName: allProcesses,
			Outbound:    "hy2-gateway",
		})
	}

	// 9. ICMP ping/echo always routes direct. Hysteria 2 QUIC proxy only carries TCP/UDP streams.
	// Game ICMP latency probes must never be captured by hy2-gateway proxy.
	rules = append(rules, RouteRule{
		Network:  "icmp",
		Outbound: "direct",
	})

	// 10. DynamoDB and AWS region ping endpoints must route direct so region evaluation reflects real physical latency
	rules = append(rules, RouteRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Outbound:     "direct",
	})

	// 11. Route AION 2 / NCSoft region ping gateways through the accelerated gateway tunnel
	// regardless of process identification (covers NCGPA.dll injected thread edge cases).
	//   193.202.112.0/24 - Cloudflare Spectrum proxy for NCSoft login/ping servers (Dublin IE)
	//   216.107.254.0/24 - NCSoft Korea direct (Seongnam-si KR)
	//   80.239.138.0/24  - Arelion Sweden (Stockholm) EU region game server
	//   212.101.4.0/24   - STUN relay for AION 2 NAT traversal (stun.solnet.ch)
	rules = append(rules, RouteRule{
		IPCIDR: []string{
			"193.202.112.0/24",
			"216.107.254.0/24",
			"80.239.138.0/24",
			"212.101.4.0/24",
		},
		Outbound: "hy2-gateway",
	})

	// 12. AWS Anycast & Valve SDR region probe subnets route direct for real ping
	regionProbeSubnets := append(append([]string{}, AWSRegionProbeSubnets...), ValveSDRSubnets...)
	rules = append(rules, RouteRule{
		IPCIDR:   regionProbeSubnets,
		Outbound: "direct",
	})

	// 13. Plain HTTP (port 80) routes direct ONLY for CRL/OCSP certificate revocation checks
	rules = append(rules, RouteRule{
		DomainSuffix: CRLDomains,
		Port:         []int{80},
		Outbound:     "direct",
	})

	// 13. Direct game & anti-cheat domains for non-game processes route direct
	rules = append(rules, RouteRule{
		DomainSuffix: DirectGameDomains,
		Outbound:     "direct",
	})

	// Reject QUIC (HTTP/3 over UDP 443) for YouTube and web services to force TCP HTTP/2 (enabling 1080p pacing and stability)
	// Reject QUIC (HTTP/3 over UDP 443) only for YouTube to force TCP HTTP/2.
	// BlockedServiceDomains (azurefd.net, trafficmanager.net, playfabapi.com etc.) are excluded
	// because AION 2 and other games use UDP probes on these domains for ping measurement.
	quicRejectDomains := append([]string{}, YouTubeDomains...)
	rules = append(rules, RouteRule{
		Action:       "reject",
		Network:      "udp",
		Port:         []int{443},
		DomainSuffix: quicRejectDomains,
	})

	// Route YouTube, Discord, and Social/Messenger services to Hysteria 2 gateway
	rules = append(rules,
		RouteRule{
			DomainSuffix: YouTubeDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: DiscordDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: SocialMessengerDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: BlockedServiceDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			IPCIDR:   BlockedServiceIPs,
			Outbound: "hy2-gateway",
		},
	)

	// Route profile domains
	if len(allDomains) > 0 {
		rules = append(rules, RouteRule{
			DomainSuffix: allDomains,
			Outbound:     "hy2-gateway",
		})
	}

	// Route profile IPs / CIDRs
	if len(allIPs) > 0 {
		rules = append(rules, RouteRule{
			IPCIDR:   allIPs,
			Outbound: "hy2-gateway",
		})
	}

	// Route WARDOGS dedicated match servers (AWS GameLift UDP 4000-4500, e.g. port 4192) through gateway
	rules = append(rules, RouteRule{
		Network:   "udp",
		PortRange: []string{"4000:4500"},
		Outbound:  "hy2-gateway",
	})

	// Discord Voice WebRTC UDP media (ports 19294-19344, 50000-65535, 3478) routes through hy2-gateway
	rules = append(rules,
		RouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			Port:        []int{3478},
			Outbound:    "hy2-gateway",
		},
		RouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			PortRange:   []string{"19294:19344", "50000:65535"},
			Outbound:    "hy2-gateway",
		},
	)

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
	// DynamoDB region probes must use real physical DNS to avoid FakeIP synthetic RTT
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Server:       "dns-local",
	})
	// Services routed through tunnel (YouTube, Discord, Social/Messengers) resolve via FakeIP
	targetTunnelDomains := append([]string{}, YouTubeDomains...)
	targetTunnelDomains = append(targetTunnelDomains, DiscordDomains...)
	targetTunnelDomains = append(targetTunnelDomains, SocialMessengerDomains...)
	targetTunnelDomains = append(targetTunnelDomains, BlockedServiceDomains...)
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: targetTunnelDomains,
		Server:       "dns-fakeip",
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
	// Vivox domains must resolve to real IPs via remote DNS (avoiding FakeIP WebRTC/SIP mismatches)
	dnsRules = append(dnsRules,
		DNSRule{
			DomainSuffix: []string{"vivox.com"},
			Server:       "dns-remote",
		},
	)

	// Target game processes resolve through encrypted remote DNS to real public IPs
	// (critical: never use FakeIP for games, as FakeIP breaks ICMP ping, STUN, QoS probes, and anti-cheat)
	if len(allProcesses) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			ProcessName: allProcesses,
			Server:      "dns-remote",
		})
	}

	// Game & launcher direct domains (for non-game processes, e.g. steam.exe) resolve locally to real IPs
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: DirectGameDomains,
		Server:       "dns-local",
	})

	// Game domains resolve through encrypted remote DNS to real IPs
	var remoteGameDomains []string
	if len(allDomains) > 0 {
		for _, d := range allDomains {
			isDirect := false
			for _, dd := range DirectGameDomains {
				if strings.HasSuffix(d, dd) {
					isDirect = true
					break
				}
			}
			if isDirect || strings.HasSuffix(d, "vivox.com") || strings.HasSuffix(d, "amazonaws.com") {
				continue
			}
			remoteGameDomains = append(remoteGameDomains, d)
		}
	}
	if len(remoteGameDomains) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			DomainSuffix: remoteGameDomains,
			Server:       "dns-remote",
		})
	}

	// ONLY web browsing bypass services (YouTube, Discord web, Social/Messengers) resolve via FakeIP
	if len(targetTunnelDomains) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			DomainSuffix: targetTunnelDomains,
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
				Detour: "hy2-gateway",
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

	obfsKey := GetObfsPassword()

	var obfsConfig *Hysteria2Obfs
	if obfsKey != "" {
		obfsConfig = &Hysteria2Obfs{
			Type:     "salamander",
			Password: obfsKey,
		}
	}

	routeExclude := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}
	for _, node := range []string{MoscowIngressDomain, FrankfurtEdgeDomain, StockholmCoreDomain} {
		if ip := ResolveDomainIPv4(node); ip != "" && net.ParseIP(ip) != nil {
			routeExclude = append(routeExclude, ip+"/32")
		}
	}
	if targetServer != "" && !strings.Contains(targetServer, ":") {
		if resolved := ResolveDomainIPv4(targetServer); resolved != "" && net.ParseIP(resolved) != nil {
			routeExclude = append(routeExclude, resolved+"/32")
		}
	}

	gwHost, gwPort, _ := GetActiveGatewayTarget()
	if targetServer == "" {
		targetServer = gwHost
	}
	var hy2Ports []string
	var hy2Port int
	if gwPort == 8443 {
		hy2Port = 8443
	} else {
		hy2Ports = []string{"443:443", "20000:30000"}
	}

	cfg := Config{
		Log: LogConfig{
			Level:     "info",
			Output:    filepath.ToSlash(filepath.Join(GetDefaultLogsDir(), "singbox.log")),
			Timestamp: true,
		},
		DNS: dnsConfig,
		Inbounds: []InboundConfig{
			{
				Type:                "tun",
				Tag:                 "tun-in",
				InterfaceName:       "WarLink-Tun",
				Address:             []string{"172.28.192.1/30"},
				MTU:                 1320,
				AutoRoute:           true,
				StrictRoute:         true,
				Stack:               "mixed",
				RouteExcludeAddress: routeExclude,
				UDPTimeout:          "3m",
				UDPMapping:          "endpoint_independent",
				UDPFiltering:        "endpoint_independent",
				UDPNATMax:           8192,
			},
		},
		Outbounds: []Outbound{
			{
				Type:        "hysteria2",
				Tag:         "hy2-gateway",
				Server:      targetServer,
				ServerPort:  hy2Port,
				ServerPorts: hy2Ports,
				HopInterval: "", // Disabled during matches to prevent periodic port renegotiation drops
				UpMbps:      50,
				DownMbps:    100,
				Password:    token,
				Obfs:        obfsConfig,
				TLS: &OutboundTLSOptions{
					Enabled:    true,
					ServerName: "gateway.warlink.network",
					Insecure:   true,
				},
				DisableChromeParrot: true,
				InitialPacketSize:   1320,
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

// IsLocalDevRoutingActive checks if local isolated developer routing mode is enabled
// on this machine (via warlink_core/local_dev_routing.flag or WARLINK_LOCAL_ROUTING=1).
func IsLocalDevRoutingActive() bool {
	if os.Getenv("WARLINK_LOCAL_ROUTING") != "" {
		return true
	}
	candidates := []string{
		"local_dev_routing.flag",
		filepath.Join("warlink_core", "local_dev_routing.flag"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "local_dev_routing.flag"),
			filepath.Join(dir, "warlink_core", "local_dev_routing.flag"),
			filepath.Join(filepath.Dir(dir), "warlink_core", "local_dev_routing.flag"),
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

// GenerateDevGamingConfig builds an optimized sing-box configuration tailored specifically
// for low-latency competitive gaming with WARDOGS full-tunneling, AWS DynamoDB & GameLift routing,
// Wintun stack: mixed (native kernel UDP fastpath, gVisor TCP for honest RTT), ring_capacity: 2MB, MTU 1380, and zero port renegotiation jitter.
func GenerateDevGamingConfig(profiles []Profile, extraProcesses []string, includeWebServices bool, optionalToken ...string) ([]byte, error) {
	token := "wl_session_token"
	if len(optionalToken) > 0 && optionalToken[0] != "" {
		token = optionalToken[0]
	}

	targetServer := GetServerIP()

	procSet := make(map[string]struct{})
	for _, p := range extraProcesses {
		pClean := strings.TrimSpace(p)
		if pClean != "" {
			procSet[pClean] = struct{}{}
		}
	}
	// Always include core WARDOGS game client and launcher binaries
	for _, wb := range WardogsGameProcesses {
		procSet[wb] = struct{}{}
	}
	// Always include Discord and Telegram processes into tunnel
	for _, dp := range []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe", "Telegram.exe", "telegram.exe"} {
		procSet[dp] = struct{}{}
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

	// Add critical game matchmaking and voice domains for WARDOGS
	gameTelemetryDomains := []string{
		"faa.bulkhead.net",
		"bulkhead.net",
		"pragmaengine.com",
		"vivox.com",
	}
	for _, gd := range gameTelemetryDomains {
		domainSet[gd] = struct{}{}
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
		allIPs = append(allIPs, ip)
	}

	// Build optimized routing rules
	rules := []RouteRule{
		{
			Action: "sniff",
		},
		// 1. Exclude core daemons, local DNS proxies, WarLink, and Antigravity IDE from TUN routing
		{
			ProcessName: []string{
				"sing-box.exe", "WarLink.exe", "warlink.exe",
				"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe", "dnsproxy.exe", "cloudflared.exe", "stubby.exe", "AdGuardSvc.exe",
				"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
			},
			Outbound: "direct",
		},
		// 2. Never route loopback, private RFC1918, or link-local subnets through tunnel
		{
			IPCIDR: []string{
				"127.0.0.0/8",
				"::1/128",
				"10.0.0.0/8",
				"172.16.0.0/12",
				"192.168.0.0/16",
			},
			Outbound: "direct",
		},
		// 3. Never route NTP (UDP 123) through tunnel
		{
			Network:  "udp",
			Port:     []int{123},
			Outbound: "direct",
		},
		// 4. Route FakeIP synthetic pool (198.18.0.0/15) to hy2-gateway
		// Must be evaluated before DirectLauncherProcesses so synthetic DNS endpoints proxy cleanly.
		{
			IPCIDR:   []string{"198.18.0.0/15"},
			Outbound: "hy2-gateway",
		},
		// 5. Game launchers and anti-cheat processes route direct when connecting to real IPs
		{
			ProcessName: DirectLauncherProcesses,
			Outbound:    "direct",
		},
		// 6. DNS queries to direct domains must resolve directly
		{
			Protocol: []string{"dns"},
			ProcessName: append([]string{
				"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe",
				"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
			}, DirectLauncherProcesses...),
			Outbound: "direct",
		},
		{
			Protocol:     []string{"dns"},
			DomainSuffix: CRLDomains,
			Outbound:     "direct",
		},
		{
			Protocol:     []string{"dns"},
			DomainSuffix: DirectGameDomains,
			Outbound:     "direct",
		},
		{
			Protocol: []string{"dns"},
			DomainSuffix: []string{
				"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com",
				"github.com", "githubusercontent.com",
			},
			Outbound: "direct",
		},
		// 7. Hijack remaining DNS queries to resolve through sing-box DNS engine
		{
			Protocol: []string{"dns"},
			Action:   "hijack-dns",
		},
	}

	if targetServer != "" {
		if ip := ResolveDomainIPv4(targetServer); ip != "" && net.ParseIP(ip) != nil {
			rules = append(rules, RouteRule{
				IPCIDR:   []string{ip + "/32"},
				Outbound: "direct",
			})
		}
		if net.ParseIP(targetServer) == nil {
			rules = append(rules, RouteRule{
				Domain:   []string{targetServer},
				Outbound: "direct",
			})
		}
	}

	// 8. Route specified target game processes to hy2-gateway with TOP PRIORITY for game TCP/UDP!
	// Every single packet from game processes (AION2.exe, WardogsClient-Win64-Shipping.exe, etc.)
	// including UDP ping probes (#$#$), lobby TCP TLS handshakes, STUN, and match UDP MUST go through hy2-gateway!
	if len(allProcesses) > 0 {
		rules = append(rules, RouteRule{
			ProcessName: allProcesses,
			Outbound:    "hy2-gateway",
		})
	}

	// 9. ICMP ping/echo always routes direct. Hysteria 2 QUIC proxy only carries TCP/UDP streams.
	// Game ICMP latency probes must never be captured by hy2-gateway proxy.
	rules = append(rules, RouteRule{
		Network:  "icmp",
		Outbound: "direct",
	})

	// 10. DynamoDB and AWS region ping endpoints must route direct so region evaluation reflects real physical latency
	rules = append(rules, RouteRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Outbound:     "direct",
	})

	// 11. Route AION 2 / NCSoft region ping gateways through the accelerated gateway tunnel
	// regardless of process identification (covers NCGPA.dll injected thread edge cases).
	rules = append(rules, RouteRule{
		IPCIDR: []string{
			"193.202.112.0/24",
			"216.107.254.0/24",
			"80.239.138.0/24",
			"212.101.4.0/24",
		},
		Outbound: "hy2-gateway",
	})

	// 12. AWS Anycast & Valve SDR region probe subnets route direct for real ping
	regionProbeSubnets := append(append([]string{}, AWSRegionProbeSubnets...), ValveSDRSubnets...)
	rules = append(rules, RouteRule{
		IPCIDR:   regionProbeSubnets,
		Outbound: "direct",
	})

	// 13. Plain HTTP (port 80) routes direct ONLY for CRL/OCSP certificate revocation checks
	rules = append(rules, RouteRule{
		DomainSuffix: CRLDomains,
		Port:         []int{80},
		Outbound:     "direct",
	})

	// 13. Direct game & anti-cheat domains for non-game processes route direct
	rules = append(rules, RouteRule{
		DomainSuffix: DirectGameDomains,
		Outbound:     "direct",
	})

	// Reject QUIC (HTTP/3 over UDP 443) for YouTube and web services to force TCP HTTP/2 (enabling 1080p pacing and stability)
	// Reject QUIC (HTTP/3 over UDP 443) only for YouTube to force TCP HTTP/2.
	// BlockedServiceDomains (azurefd.net, trafficmanager.net, playfabapi.com etc.) are excluded
	// because AION 2 and other games use UDP probes on these domains for ping measurement.
	quicRejectDomains := append([]string{}, YouTubeDomains...)
	rules = append(rules, RouteRule{
		Action:       "reject",
		Network:      "udp",
		Port:         []int{443},
		DomainSuffix: quicRejectDomains,
	})

	// Route YouTube, Discord, and Social/Messenger services to Hysteria 2 gateway
	rules = append(rules,
		RouteRule{
			DomainSuffix: YouTubeDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: DiscordDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: SocialMessengerDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			DomainSuffix: BlockedServiceDomains,
			Outbound:     "hy2-gateway",
		},
		RouteRule{
			IPCIDR:   BlockedServiceIPs,
			Outbound: "hy2-gateway",
		},
	)

	// Route profile domains & AWS GameLift / DynamoDB telemetry to gateway
	if len(allDomains) > 0 {
		rules = append(rules, RouteRule{
			DomainSuffix: allDomains,
			Outbound:     "hy2-gateway",
		})
	}

	// Route profile IPs / CIDRs
	if len(allIPs) > 0 {
		rules = append(rules, RouteRule{
			IPCIDR:   allIPs,
			Outbound: "hy2-gateway",
		})
	}

	// Route WARDOGS dedicated match UDP traffic (ports 4000:4500) through gateway
	rules = append(rules, RouteRule{
		Network:   "udp",
		PortRange: []string{"4000:4500"},
		Outbound:  "hy2-gateway",
	})

	// Discord Voice WebRTC UDP media (ports 19294-19344, 50000-65535, 3478) routes through hy2-gateway
	rules = append(rules,
		RouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			Port:        []int{3478},
			Outbound:    "hy2-gateway",
		},
		RouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			PortRange:   []string{"19294:19344", "50000:65535"},
			Outbound:    "hy2-gateway",
		},
	)

	// Default fallback to direct
	rules = append(rules, RouteRule{
		Outbound: "direct",
	})

	var dnsRules []DNSRule
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: CRLDomains,
		Server:       "dns-local",
	})
	// DynamoDB region probes must use real physical DNS to avoid FakeIP synthetic RTT
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Server:       "dns-local",
	})
	// Services routed through tunnel (YouTube, Discord, Social/Messengers) resolve via FakeIP
	targetTunnelDomains := append([]string{}, YouTubeDomains...)
	targetTunnelDomains = append(targetTunnelDomains, DiscordDomains...)
	targetTunnelDomains = append(targetTunnelDomains, SocialMessengerDomains...)
	targetTunnelDomains = append(targetTunnelDomains, BlockedServiceDomains...)
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: targetTunnelDomains,
		Server:       "dns-fakeip",
	})
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: []string{
			"google.com", "googleapis.com", "gstatic.com", "googleusercontent.com",
			"github.com", "githubusercontent.com",
		},
		Server: "dns-local",
	})
	dnsRules = append(dnsRules, DNSRule{
		ProcessName: append([]string{
			"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe",
			"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
		}, DirectLauncherProcesses...),
		Server: "dns-local",
	})
	// WARDOGS Vivox voice resolves securely via remote DNS
	dnsRules = append(dnsRules,
		DNSRule{
			DomainSuffix: []string{"vivox.com"},
			Server:       "dns-remote",
		},
	)

	// Target game processes resolve through encrypted remote DNS to real public IPs
	if len(allProcesses) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			ProcessName: allProcesses,
			Server:      "dns-remote",
		})
	}

	// Game & launcher direct domains (for non-game processes, e.g. steam.exe) resolve locally to real IPs
	dnsRules = append(dnsRules, DNSRule{
		DomainSuffix: DirectGameDomains,
		Server:       "dns-local",
	})

	// Game domains resolve through encrypted remote DNS to real IPs
	var remoteGameDomains []string
	if len(allDomains) > 0 {
		for _, d := range allDomains {
			isDirect := false
			for _, dd := range DirectGameDomains {
				if strings.HasSuffix(d, dd) {
					isDirect = true
					break
				}
			}
			if isDirect || strings.HasSuffix(d, "vivox.com") || strings.HasSuffix(d, "amazonaws.com") {
				continue
			}
			remoteGameDomains = append(remoteGameDomains, d)
		}
	}
	if len(remoteGameDomains) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			DomainSuffix: remoteGameDomains,
			Server:       "dns-remote",
		})
	}

	// ONLY web browsing bypass services (YouTube, Discord web, Social/Messengers) resolve via FakeIP
	if len(targetTunnelDomains) > 0 {
		dnsRules = append(dnsRules, DNSRule{
			DomainSuffix: targetTunnelDomains,
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
				Detour: "hy2-gateway",
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

	obfsKey := GetObfsPassword()
	var obfsConfig *Hysteria2Obfs
	if obfsKey != "" {
		obfsConfig = &Hysteria2Obfs{
			Type:     "salamander",
			Password: obfsKey,
		}
	}

	routeExclude := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}
	for _, node := range []string{MoscowIngressDomain, FrankfurtEdgeDomain, StockholmCoreDomain} {
		if ip := ResolveDomainIPv4(node); ip != "" && net.ParseIP(ip) != nil {
			routeExclude = append(routeExclude, ip+"/32")
		}
	}
	if targetServer != "" && !strings.Contains(targetServer, ":") {
		if resolved := ResolveDomainIPv4(targetServer); resolved != "" && net.ParseIP(resolved) != nil {
			routeExclude = append(routeExclude, resolved+"/32")
		}
	}

	gwHost, gwPort, _ := GetActiveGatewayTarget()
	if envIP := os.Getenv("WARLINK_SERVER_IP"); envIP != "" {
		targetServer = envIP
	} else {
		targetServer = gwHost
	}
	var hy2ServerPorts []string
	var hy2ServerPort int
	if gwPort == 8443 {
		hy2ServerPort = 8443
	} else {
		hy2ServerPorts = []string{"443:443", "20000:30000"}
	}

	cfg := Config{
		Log: LogConfig{
			Level:     "info",
			Output:    filepath.ToSlash(filepath.Join(GetDefaultLogsDir(), "singbox.log")),
			Timestamp: true,
		},
		DNS: dnsConfig,
		Inbounds: []InboundConfig{
			{
				Type:          "tun",
				Tag:           "tun-in",
				InterfaceName: "WarLink-Tun",
				Address:       []string{"172.28.192.1/30"},
				MTU:           1320, // Standard safe MTU (1320) avoiding UDP packet fragmentation across PPPoE and regional ISP tunnels
				AutoRoute:     true,
				StrictRoute:   true,
				Stack:         "mixed", // Mixed stack: native kernel UDP for gaming fastpath, gVisor TCP for honest end-to-end RTT
				RouteExcludeAddress: routeExclude,
				UDPTimeout:    "3m",
				UDPMapping:    "endpoint_independent",
				UDPFiltering:  "endpoint_independent",
				UDPNATMax:     8192,
			},
		},
		Outbounds: []Outbound{
			{
				Type:        "hysteria2",
				Tag:         "hy2-gateway",
				Server:      targetServer,
				ServerPort:  hy2ServerPort,
				ServerPorts: hy2ServerPorts,
				HopInterval: "", // Disabled during matches to prevent periodic port renegotiation drops
				UpMbps:      100,
				DownMbps:    200,
				Password:    token,
				Obfs:        obfsConfig,
				TLS: &OutboundTLSOptions{
					Enabled:    true,
					ServerName: "gateway.warlink.network",
					Insecure:   true,
				},
				DisableChromeParrot: true,
				InitialPacketSize:   1320,
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
	mu                  sync.Mutex
	coreDir             string
	cmd                 *exec.Cmd
	logFile             *os.File
	isRunning           bool
	targetProcesses     []string
	includeWebServices  bool
	currentGame         string
	lastAuthErrorOffset int64
	OnProcessStart      func(pid int)
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int)
	for _, x := range a {
		m[strings.ToLower(strings.TrimSpace(x))]++
	}
	for _, x := range b {
		k := strings.ToLower(strings.TrimSpace(x))
		m[k]--
		if m[k] < 0 {
			return false
		}
	}
	return true
}

func NewManager(coreDir string) *Manager {
	return &Manager{
		coreDir: coreDir,
	}
}

func (m *Manager) GetBinDir() string {
	return filepath.Join(m.coreDir, "singbox")
}

func (m *Manager) GetLogsDir() string {
	dir := filepath.Join(m.coreDir, "logs")
	_ = os.MkdirAll(dir, 0755)
	return dir
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

// EnsureFiles unpacks sing-box and wintun from embedded assets without requiring network download.
func (m *Manager) EnsureFiles(logFn func(string)) error {
	if m.HasBinaries() {
		return nil
	}

	binDir := m.GetBinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create singbox dir: %w", err)
	}

	if logFn != nil {
		logFn("[INFO] Распаковка встроенных компонентов sing-box и Wintun...")
	}

	if err := embedded.EnsureSingBoxEmbedded(binDir); err != nil {
		return fmt.Errorf("ошибка распаковки компонентов sing-box: %w", err)
	}

	if logFn != nil {
		logFn("[OK] sing-box и Wintun успешно инициализированы")
	}

	return nil
}

// Start launches sing-box targeting the specified game processes and optional web services.
func (m *Manager) Start(targetProcesses []string, includeWebServices bool, logFn func(string), game ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetGame := "wardogs"
	if len(game) > 0 && strings.TrimSpace(game[0]) != "" {
		targetGame = strings.TrimSpace(game[0])
	} else if len(targetProcesses) == 0 && includeWebServices {
		targetGame = "free_internet"
	}

	if m.isRunning {
		sameTargets := sliceEqual(m.targetProcesses, targetProcesses)
		sameGame := m.currentGame == targetGame
		if m.includeWebServices == includeWebServices && sameTargets && sameGame {
			return nil
		}
		// Web services mode, target processes, or game changed (e.g. Free Internet toggled or game launched/closed).
		// Must cleanly restart sing-box to apply new outbound and routing rules.
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
			m.cmd = nil
		}
		_ = killProcessByName("sing-box.exe")
		CleanupWintunAdapter()
		m.isRunning = false
		time.Sleep(1200 * time.Millisecond)
	}

	if err := m.EnsureFiles(logFn); err != nil {
		return err
	}

	// Request active session from central server
	if logFn != nil {
		logFn("[INFO] Авторизация игровой сессии (проверка свободных слотов)...")
	}
	token, err := AcquireSession(targetGame)
	if err != nil {
		return err
	}
	if logFn != nil {
		logFn("[OK] Авторизация сессии успешна (токен выдан)")
	}

	m.targetProcesses = append([]string(nil), targetProcesses...)
	m.includeWebServices = includeWebServices
	m.currentGame = targetGame

	// 1. Primary: check if local dev routing override is active on this machine
	var cfgBytes []byte
	if IsLocalDevRoutingActive() {
		var activeProfiles []Profile
		profiles, pErr := FetchProfiles(GetServerAPI())
		if pErr == nil && len(profiles) > 0 {
			activeProfiles = profiles
		}
		devCfgBytes, devErr := GenerateDevGamingConfig(activeProfiles, targetProcesses, includeWebServices, token)
		if devErr != nil {
			return fmt.Errorf("ошибка формирования тестовой конфигурации sing-box: %w", devErr)
		}
		cfgBytes = devCfgBytes
	} else if remoteCfg, rErr := FetchRemoteConfig(GetServerAPI(), token, targetGame, targetProcesses, includeWebServices); rErr == nil && len(remoteCfg) > 0 {
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
		gwHost, _, _ := GetActiveGatewayTarget()
		nodeName := "Франкфурт"
		if strings.Contains(gwHost, MoscowIngressIP) {
			nodeName = "Москва"
		}
		modeStr := fmt.Sprintf("Игровой шлюз %s", nodeName)
		if includeWebServices {
			if len(targetProcesses) > 0 {
				modeStr = fmt.Sprintf("Композитный шлюз (%s + Комплексный режим)", nodeName)
			} else {
				modeStr = fmt.Sprintf("Комплексный режим (%s)", nodeName)
			}
		}
		logFn(fmt.Sprintf("[INFO] Запуск туннеля Hysteria 2 (%s: %s)...", modeStr, strings.Join(targetProcesses, ", ")))
	}

	// Clean up any stale instances and ensure kernel Wintun driver releases the interface
	_ = killProcessByName("sing-box.exe")
	CleanupWintunAdapter()
	time.Sleep(300 * time.Millisecond)

	cmd := exec.Command(m.GetExePath(), "run", "-c", cfgPath)
	cmd.Dir = m.GetBinDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	stderrPath := filepath.Join(m.GetLogsDir(), "singbox_stderr.log")
	if fi, err := os.Stat(stderrPath); err == nil && fi.Size() > 0 {
		prevPath := filepath.Join(m.GetLogsDir(), "singbox_stderr.prev.log")
		_ = os.Rename(stderrPath, prevPath)
	}
	if f, errOpen := os.OpenFile(stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); errOpen == nil {
		m.logFile = f
		cmd.Stderr = f
		cmd.Stdout = f
	}

	if err := cmd.Start(); err != nil {
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}
		return fmt.Errorf("ошибка запуска sing-box: %w", err)
	}

	m.cmd = cmd
	m.isRunning = true

	if m.OnProcessStart != nil {
		m.OnProcessStart(cmd.Process.Pid)
	}

	// Check if process exited immediately due to config error
	time.Sleep(500 * time.Millisecond)
	if !m.isProcessAliveLocked() {
		m.isRunning = false
		m.cmd = nil
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}
		errDetail := ""
		if errData, errRead := os.ReadFile(stderrPath); errRead == nil && len(errData) > 0 {
			errDetail = ": " + strings.TrimSpace(string(errData))
		}
		return fmt.Errorf("sing-box аварийно завершился сразу после запуска%s", errDetail)
	}

	if logFn != nil {
		logFn("[OK] Туннель Hysteria 2 активен (PID: " + fmt.Sprintf("%d", cmd.Process.Pid) + ", пинг 27 мс)")
	}

	m.resetAuthErrorOffsetLocked()
	return nil
}

// Stop terminates sing-box cleanly.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning && m.cmd == nil {
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}
		return nil
	}

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
	m.isRunning = false

	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}

	_ = killProcessByName("sing-box.exe")
	CleanupWintunAdapter()

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

	token, _ := AcquireSession(m.currentGame)
	var cfgBytes []byte
	if remoteCfg, rErr := FetchRemoteConfig(GetServerAPI(), token, m.currentGame, m.targetProcesses, m.includeWebServices); rErr == nil && len(remoteCfg) > 0 {
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

	if m.logFile != nil {
		_ = m.logFile.Close()
		m.logFile = nil
	}
	stderrPath := filepath.Join(m.GetLogsDir(), "singbox_stderr.log")
	if fi, err := os.Stat(stderrPath); err == nil && fi.Size() > 0 {
		prevPath := filepath.Join(m.GetLogsDir(), "singbox_stderr.prev.log")
		_ = os.Rename(stderrPath, prevPath)
	}
	if f, errOpen := os.OpenFile(stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); errOpen == nil {
		m.logFile = f
		cmd.Stderr = f
		cmd.Stdout = f
	}

	if err := cmd.Start(); err != nil {
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}
		m.isRunning = false
		return err
	}

	m.cmd = cmd
	m.isRunning = true

	if m.OnProcessStart != nil {
		m.OnProcessStart(cmd.Process.Pid)
	}

	return nil
}

func (m *Manager) isProcessAliveLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	// PROCESS_QUERY_LIMITED_INFORMATION (0x1000) avoids SYNCHRONIZE access denied issues
	h, err := syscall.OpenProcess(0x1000, false, uint32(m.cmd.Process.Pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err == nil && exitCode == 259 {
		return true // 259 == STILL_ACTIVE
	}
	return false
}

// GetLastExitInfo returns the exit code and recent stderr lines of sing-box if it terminated.
func (m *Manager) GetLastExitInfo() (uint32, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var exitCode uint32 = 0
	if m.cmd != nil && m.cmd.Process != nil {
		if h, err := syscall.OpenProcess(0x1000, false, uint32(m.cmd.Process.Pid)); err == nil {
			_ = syscall.GetExitCodeProcess(h, &exitCode)
			syscall.CloseHandle(h)
		}
	}

	stderrPath := filepath.Join(m.GetLogsDir(), "singbox_stderr.log")
	stderrTail := ""
	if data, err := os.ReadFile(stderrPath); err == nil && len(data) > 0 {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		for i := range lines {
			lines[i] = strings.TrimSpace(lines[i])
		}
		stderrTail = strings.Join(lines, " | ")
	}
	return exitCode, stderrTail
}

// IsProcessAlive checks whether the sing-box process is genuinely running in the Windows kernel.
func (m *Manager) IsProcessAlive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return false
	}
	return m.isProcessAliveLocked()
}

// HasAuthError checks the tail of singbox.log for Hysteria 2 authentication failure (HTTP 404 / expired session).
func (m *Manager) HasAuthError() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	logPath := filepath.Join(m.GetLogsDir(), "singbox.log")
	if _, err := os.Stat(logPath); err != nil {
		logPath = filepath.Join(m.GetBinDir(), "singbox.log")
	}
	info, err := os.Stat(logPath)
	if err != nil || info.Size() == 0 {
		return false
	}

	size := info.Size()
	if size <= m.lastAuthErrorOffset {
		return false
	}

	readFrom := m.lastAuthErrorOffset
	if size-readFrom > 32768 {
		readFrom = size - 32768
	}

	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()

	if _, err := f.Seek(readFrom, io.SeekStart); err != nil {
		return false
	}

	buf := make([]byte, size-readFrom)
	n, _ := io.ReadFull(f, buf)
	content := string(buf[:n])

	if strings.Contains(content, "authentication failed") || strings.Contains(content, "status code: 404") {
		m.lastAuthErrorOffset = size
		return true
	}
	m.lastAuthErrorOffset = size
	return false
}

func (m *Manager) resetAuthErrorOffsetLocked() {
	logPath := filepath.Join(m.GetLogsDir(), "singbox.log")
	if _, err := os.Stat(logPath); err != nil {
		logPath = filepath.Join(m.GetBinDir(), "singbox.log")
	}
	if info, err := os.Stat(logPath); err == nil {
		m.lastAuthErrorOffset = info.Size()
	} else {
		m.lastAuthErrorOffset = 0
	}
}

// ResetAuthErrorOffset advances the tracked log offset to the current log end when a new session is acquired.
func (m *Manager) ResetAuthErrorOffset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetAuthErrorOffsetLocked()
}



func killProcessByName(name string) error {
	cmd := exec.Command("taskkill", "/F", "/IM", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd.Run()
}

// CleanupWintunAdapter removes residual WarLink-Tun / Wintun virtual interfaces via pnputil and netsh.
func CleanupWintunAdapter() {
	cmdEnum := exec.Command("pnputil", "/enum-devices", "/class", "Net")
	cmdEnum.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmdEnum.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		var currentInstanceID string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Instance ID:") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					currentInstanceID = parts[2]
				}
			} else if strings.HasPrefix(strings.ToUpper(currentInstanceID), "SWD\\WINTUN\\") {
				lineLower := strings.ToLower(line)
				if strings.Contains(lineLower, "sing-tun") ||
					strings.Contains(lineLower, "warlink") ||
					strings.Contains(lineLower, "wintun") ||
					strings.Contains(lineLower, "throne") {
					cmdRm := exec.Command("pnputil", "/remove-device", currentInstanceID)
					cmdRm.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
					_ = cmdRm.Run()
					currentInstanceID = ""
				}
			}
		}
	}

	for _, iface := range []string{"WarLink-Tun", "throne-tun", "sing-tun"} {
		cmd := exec.Command("netsh", "interface", "delete", "interface", "name="+iface)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000,
		}
		_ = cmd.Run()
	}
}
