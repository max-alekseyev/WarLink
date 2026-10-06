package aclgen

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// BlockedServiceIPs contains IP ranges blocked at Layer 3/4 by Russian ISPs (TSPU).
var BlockedServiceIPs = []string{
	"157.240.0.0/16",
	"31.13.64.0/18",
	"179.60.192.0/22",
	"185.89.216.0/22",
	"57.144.0.0/14",
	"69.63.176.0/20",
	"69.171.224.0/19",
	"66.220.144.0/20",
	"204.15.20.0/22",
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

// CRLDomains contains Certificate Revocation List (CRL) and OCSP domains that must bypass tunnel over port 80.
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

// DirectGameDomains contains domains that must route directly (bypassing Hysteria tunnel and FakeIP)
// for maximum speed, compatibility, and anti-cheat validation.
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
	// EGS CDN & AWS services
	"cloudfront.net",
	"amazonaws.com",
	// AWS GameLift infrastructure (match server assignment API)
	"gamelift.us-east-1.amazonaws.com",
	// Time sync
	"time.cloudflare.com",
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

type SingBoxLogConfig struct {
	Disabled bool   `json:"disabled,omitempty"`
	Level    string `json:"level,omitempty"`
	Output   string `json:"output,omitempty"`
}

type SingBoxInbound struct {
	Type                string   `json:"type"`
	Tag                 string   `json:"tag"`
	InterfaceName       string   `json:"interface_name"`
	Address             []string `json:"address"`
	MTU                 int      `json:"mtu,omitempty"`
	AutoRoute           bool     `json:"auto_route"`
	StrictRoute         bool     `json:"strict_route"`
	Stack               string   `json:"stack"`
	RouteExcludeAddress []string `json:"route_exclude_address,omitempty"`
}

type SingBoxHysteria2Obfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}

type SingBoxOutboundTLSOptions struct {
	Enabled    bool     `json:"enabled"`
	ServerName string   `json:"server_name,omitempty"`
	Insecure   bool     `json:"insecure"`
	ALPN       []string `json:"alpn,omitempty"`
}

type SingBoxOutbound struct {
	Type        string                     `json:"type"`
	Tag         string                     `json:"tag"`
	Server      string                     `json:"server,omitempty"`
	ServerPort  int                        `json:"server_port,omitempty"`
	ServerPorts []string                   `json:"server_ports,omitempty"`
	HopInterval string                     `json:"hop_interval,omitempty"`
	UpMbps      int                        `json:"up_mbps,omitempty"`
	DownMbps    int                        `json:"down_mbps,omitempty"`
	Password    string                     `json:"password,omitempty"`
	Obfs        *SingBoxHysteria2Obfs      `json:"obfs,omitempty"`
	TLS         *SingBoxOutboundTLSOptions `json:"tls,omitempty"`
}

type SingBoxRouteRule struct {
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

type SingBoxRouteConfig struct {
	DefaultDomainResolver string             `json:"default_domain_resolver,omitempty"`
	FindProcess           bool               `json:"find_process"`
	AutoDetectInterface   bool               `json:"auto_detect_interface"`
	Rules                 []SingBoxRouteRule `json:"rules"`
}

type SingBoxDNSServer struct {
	Tag        string `json:"tag"`
	Type       string `json:"type,omitempty"`
	Address    string `json:"address,omitempty"`
	Server     string `json:"server,omitempty"`
	Detour     string `json:"detour,omitempty"`
	Inet4Range string `json:"inet4_range,omitempty"`
}

type SingBoxDNSRule struct {
	ProcessName  []string `json:"process_name,omitempty"`
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	Domain       []string `json:"domain,omitempty"`
	Server       string   `json:"server"`
}

type SingBoxDNSConfig struct {
	Servers []SingBoxDNSServer `json:"servers"`
	Rules   []SingBoxDNSRule   `json:"rules"`
	Final   string             `json:"final"`
}

type SingBoxCacheFile struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

type SingBoxExperimental struct {
	CacheFile *SingBoxCacheFile `json:"cache_file,omitempty"`
}

type SingBoxFullConfig struct {
	Log          SingBoxLogConfig     `json:"log"`
	Inbounds     []SingBoxInbound     `json:"inbounds"`
	Outbounds    []SingBoxOutbound    `json:"outbounds"`
	Route        SingBoxRouteConfig   `json:"route"`
	DNS          SingBoxDNSConfig     `json:"dns"`
	Experimental *SingBoxExperimental `json:"experimental,omitempty"`
}

// GenerateSingBoxConfig constructs a pure, validated sing-box JSON configuration from profiles.
func GenerateSingBoxConfig(profiles []Profile, extraProcesses []string, includeWebServices bool, serverIP string, serverPorts string, obfsPassword string, token string) ([]byte, error) {
	if token == "" {
		token = "wl_session_token"
	}
	if serverPorts == "" {
		serverPorts = "443,20000-30000"
	}

	procSet := make(map[string]struct{})
	for _, p := range extraProcesses {
		pClean := strings.TrimSpace(p)
		if pClean != "" {
			procSet[pClean] = struct{}{}
		}
	}
	if includeWebServices {
		procSet["Telegram.exe"] = struct{}{}
		procSet["telegram.exe"] = struct{}{}
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
		allIPs = append(allIPs, ip)
	}

	targetServer := serverIP
	if strings.Contains(targetServer, ":") {
		h := strings.Split(targetServer, ":")[0]
		if h != "" {
			targetServer = h
		}
	}

	rules := []SingBoxRouteRule{
		{
			Action: "sniff",
		},
		// 1. Exclude core daemons, local DNS proxies, WarLink, and Antigravity IDE from TUN routing
		{
			ProcessName: []string{
				"sing-box.exe", "winws2.exe", "winws.exe", "WarLink.exe", "warlink.exe",
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
		// 6. Hijack remaining DNS queries to resolve through sing-box DNS engine
		{
			Protocol: []string{"dns"},
			Action:   "hijack-dns",
		},
	}

	if targetServer != "" {
		rules = append(rules, SingBoxRouteRule{
			IPCIDR:   []string{targetServer + "/32"},
			Outbound: "direct",
		})
	}

	// 7. Route specified target game processes to hy2-gateway with HIGHEST PRIORITY!
	// All game TCP and UDP traffic (game servers, STUN, Vivox voice, match lobbies, HTTP 80/443 auth, EOS)
	// MUST go through tunnel!
	if len(allProcesses) > 0 {
		rules = append(rules, SingBoxRouteRule{
			ProcessName: allProcesses,
			Outbound:    "hy2-gateway",
		})
	}

	// 8. Plain HTTP (port 80) routes direct ONLY for CRL/OCSP certificate revocation checks
	rules = append(rules, SingBoxRouteRule{
		DomainSuffix: CRLDomains,
		Port:         []int{80},
		Outbound:     "direct",
	})

	// 9. Direct game domains (Steam downloads, CDN) for non-game processes route direct
	rules = append(rules, SingBoxRouteRule{
		DomainSuffix: DirectGameDomains,
		Outbound:     "direct",
	})

	if includeWebServices {
		rules = append(rules, SingBoxRouteRule{
			Action:       "reject",
			Network:      "udp",
			Port:         []int{443},
			DomainSuffix: BlockedServiceDomains,
		})

		rules = append(rules,
			SingBoxRouteRule{
				DomainSuffix: BlockedServiceDomains,
				Outbound:     "hy2-gateway",
			},
			SingBoxRouteRule{
				IPCIDR:   BlockedServiceIPs,
				Outbound: "hy2-gateway",
			},
		)
	}

	// Route profile domains
	if len(allDomains) > 0 {
		rules = append(rules, SingBoxRouteRule{
			DomainSuffix: allDomains,
			Outbound:     "hy2-gateway",
		})
	}

	// Route profile IPs / CIDRs
	if len(allIPs) > 0 {
		rules = append(rules, SingBoxRouteRule{
			IPCIDR:   allIPs,
			Outbound: "hy2-gateway",
		})
	}

	// Dedicated match UDP ports for WARDOGS
	rules = append(rules, SingBoxRouteRule{
		Network:   "udp",
		PortRange: []string{"4000:4500"},
		Outbound:  "hy2-gateway",
	})

	// Discord Voice WebRTC UDP media strictly for Discord processes routes direct
	rules = append(rules,
		SingBoxRouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			Port:        []int{3478},
			Outbound:    "direct",
		},
		SingBoxRouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			PortRange:   []string{"19294:19344", "50000:65535"},
			Outbound:    "direct",
		},
	)

	// Steam Datagram Relay (SDR) ping relays for Steam client
	rules = append(rules, SingBoxRouteRule{
		Network:   "udp",
		PortRange: []string{"27000:27200"},
		Outbound:  "direct",
	})

	// Default fallback to direct
	rules = append(rules, SingBoxRouteRule{
		Outbound: "direct",
	})

	var dnsRules []SingBoxDNSRule
	// 1. Antigravity & local daemons -> dns-local
	dnsRules = append(dnsRules, SingBoxDNSRule{
		ProcessName: append([]string{
			"Antigravity.exe", "antigravity.exe", "antigravity-tools.exe", "language_server.exe",
			"ag_dns.exe", "agunlocker.exe", "AGUnlocker.exe",
		}, DirectLauncherProcesses...),
		Server: "dns-local",
	})
	// 2. CRL domains -> dns-local
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: CRLDomains,
		Server:       "dns-local",
	})
	// 3. Vivox voice chat -> dns-remote (resolves real IP of voice servers via 1.1.1.1 through tunnel)
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: []string{"vivox.com"},
		Server:       "dns-remote",
	})
	// 4. Target game processes -> dns-fakeip (all queries from game EXEs get FakeIP and resolve remotely)
	if len(allProcesses) > 0 {
		dnsRules = append(dnsRules, SingBoxDNSRule{
			ProcessName: allProcesses,
			Server:      "dns-fakeip",
		})
	}
	// 5. Direct game domains (for non-game processes, e.g. steam.exe) -> dns-local
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: DirectGameDomains,
		Server:       "dns-local",
	})

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
		dnsRules = append(dnsRules, SingBoxDNSRule{
			DomainSuffix: fakeDomains,
			Server:       "dns-fakeip",
		})
	}

	dnsConfig := SingBoxDNSConfig{
		Servers: []SingBoxDNSServer{
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

	hy2Outbound := SingBoxOutbound{
		Type:        "hysteria2",
		Tag:         "hy2-gateway",
		Server:      targetServer,
		ServerPorts: normalizeServerPorts(serverPorts),
		HopInterval: "", // Disabled during matches to prevent periodic port renegotiation drops
		UpMbps:      100,
		DownMbps:    100,
		Password:    token,
		TLS: &SingBoxOutboundTLSOptions{
			Enabled:    true,
			ServerName: "gateway.warlink.network",
			Insecure:   true,
		},
	}

	if obfsPassword != "" {
		hy2Outbound.Obfs = &SingBoxHysteria2Obfs{
			Type:     "salamander",
			Password: obfsPassword,
		}
	}

	fullConfig := SingBoxFullConfig{
		Log: SingBoxLogConfig{
			Disabled: false,
			Level:    "info",
			Output:   "singbox.log",
		},
		Inbounds: []SingBoxInbound{
			{
				Type:          "tun",
				Tag:           "tun-in",
				InterfaceName: "WarLink-Tun",
				Address:       []string{"172.28.192.1/30"},
				MTU:           1320,
				AutoRoute:           true,
				StrictRoute:         false,
				Stack:               "mixed",
				RouteExcludeAddress: []string{"162.159.0.0/16"},
			},
		},
		Outbounds: []SingBoxOutbound{
			hy2Outbound,
			{
				Type: "direct",
				Tag:  "direct",
			},
		},
		Route: SingBoxRouteConfig{
			DefaultDomainResolver: "dns-local",
			FindProcess:           true,
			AutoDetectInterface:   true,
			Rules:                 rules,
		},
		DNS: dnsConfig,
		Experimental: &SingBoxExperimental{
			CacheFile: &SingBoxCacheFile{
				Enabled: true,
				Path:    "cache.db",
			},
		},
	}

	return json.MarshalIndent(fullConfig, "", "  ")
}

func normalizeServerPorts(rawPorts string) []string {
	if rawPorts == "" {
		return []string{"443:443", "20000:30000"}
	}
	parts := strings.Split(rawPorts, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			p = strings.ReplaceAll(p, "-", ":")
		}
		if !strings.Contains(p, ":") {
			p = fmt.Sprintf("%s:%s", p, p)
		}
		result = append(result, p)
	}
	if len(result) == 0 {
		return []string{"443:443", "20000:30000"}
	}
	return result
}

