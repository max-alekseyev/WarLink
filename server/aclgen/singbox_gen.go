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

// DynamoDBRegionProbeDomains contains AWS DynamoDB regional endpoints used by WARDOGS
// and Unreal Engine games to calculate regional latency in region/server select screens.
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

// AWSRegionProbeSubnets contains Anycast / Global Accelerator IP ranges used by AWS DynamoDB regional endpoints.
var AWSRegionProbeSubnets = []string{
	"35.71.0.0/16",
	"52.94.0.0/16",
	"52.119.0.0/16",
	"54.239.0.0/16",
	"3.218.0.0/16",
}

// ValveSDRSubnets contains Steam Datagram Relay (SDR) relay clusters used by Steamworks for ping estimation.
var ValveSDRSubnets = []string{
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
	UDPTimeout          string   `json:"udp_timeout,omitempty"`
	UDPMapping          string   `json:"udp_mapping,omitempty"`
	UDPFiltering        string   `json:"udp_filtering,omitempty"`
	UDPNATMax           int      `json:"udp_nat_max,omitempty"`
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
	Type                string                     `json:"type"`
	Tag                 string                     `json:"tag"`
	Server              string                     `json:"server,omitempty"`
	ServerPort          int                        `json:"server_port,omitempty"`
	ServerPorts         []string                   `json:"server_ports,omitempty"`
	HopInterval         string                     `json:"hop_interval,omitempty"`
	UpMbps              int                        `json:"up_mbps,omitempty"`
	DownMbps            int                        `json:"down_mbps,omitempty"`
	Password            string                     `json:"password,omitempty"`
	Obfs                *SingBoxHysteria2Obfs      `json:"obfs,omitempty"`
	TLS                 *SingBoxOutboundTLSOptions `json:"tls,omitempty"`
	DisableChromeParrot bool                       `json:"disable_chrome_parrot,omitempty"`
	InitialPacketSize   int                        `json:"initial_packet_size,omitempty"`
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

	// 7. Route specified target game processes to hy2-gateway with TOP PRIORITY for game TCP/UDP!
	// Every single packet from game processes (AION2.exe, WardogsClient-Win64-Shipping.exe, etc.)
	// including UDP ping probes (#$#$), lobby TCP TLS handshakes, STUN, and match UDP MUST go through hy2-gateway!
	if len(allProcesses) > 0 {
		rules = append(rules, SingBoxRouteRule{
			ProcessName: allProcesses,
			Outbound:    "hy2-gateway",
		})
	}

	// 8. ICMP ping/echo always routes direct. Hysteria 2 QUIC proxy only carries TCP/UDP streams.
	// Game ICMP latency probes must never be captured by hy2-gateway proxy.
	rules = append(rules, SingBoxRouteRule{
		Network:  "icmp",
		Outbound: "direct",
	})

	// 9. DynamoDB and AWS region ping endpoints must route direct so region evaluation reflects real physical latency
	rules = append(rules, SingBoxRouteRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Outbound:     "direct",
	})

	// 10. Route AION 2 / Throne and Liberty region ping gateways and NCSoft server subnets
	// through the accelerated gateway tunnel regardless of process identification.
	// These IP rules ensure ping probes reach game servers even when sing-box cannot
	// identify the originating process (NCGPA.dll injected threads, Access is denied).
	//   193.202.112.0/24 - Cloudflare Spectrum proxy for NCSoft login / ping servers (IE, Dublin)
	//   216.107.254.0/24 - NCSoft Korea direct (Seongnam-si, KR)
	//   80.239.138.0/24  - Arelion Sweden (Stockholm) EU region game server
	//   212.101.4.0/24   - STUN relay for AION 2 NAT traversal (stun.solnet.ch)
	rules = append(rules, SingBoxRouteRule{
		IPCIDR: []string{
			"193.202.112.0/24",
			"216.107.254.0/24",
			"80.239.138.0/24",
			"212.101.4.0/24",
		},
		Outbound: "hy2-gateway",
	})

	// 11. AWS Anycast & Valve SDR region probe subnets route direct for real ping
	regionProbeSubnets := append(append([]string{}, AWSRegionProbeSubnets...), ValveSDRSubnets...)
	rules = append(rules, SingBoxRouteRule{
		IPCIDR:   regionProbeSubnets,
		Outbound: "direct",
	})

	// 12. Plain HTTP (port 80) routes direct ONLY for CRL/OCSP certificate revocation checks
	rules = append(rules, SingBoxRouteRule{
		DomainSuffix: CRLDomains,
		Port:         []int{80},
		Outbound:     "direct",
	})

	// 13. Direct game domains (Steam downloads, CDN) for non-game processes route direct
	rules = append(rules, SingBoxRouteRule{
		DomainSuffix: DirectGameDomains,
		Outbound:     "direct",
	})

	// Reject QUIC (HTTP/3 over UDP 443) only for YouTube to force TCP HTTP/2.
	// BlockedServiceDomains (azurefd.net, trafficmanager.net, playfabapi.com etc.) are excluded
	// because AION 2 and other games use UDP probes on these domains for ping measurement.
	quicRejectDomains := append([]string{}, YouTubeDomains...)
	rules = append(rules, SingBoxRouteRule{
		Action:       "reject",
		Network:      "udp",
		Port:         []int{443},
		DomainSuffix: quicRejectDomains,
	})

	// Route YouTube, Discord, and Social/Messenger services to Hysteria 2 gateway
	rules = append(rules,
		SingBoxRouteRule{
			DomainSuffix: YouTubeDomains,
			Outbound:     "hy2-gateway",
		},
		SingBoxRouteRule{
			DomainSuffix: DiscordDomains,
			Outbound:     "hy2-gateway",
		},
		SingBoxRouteRule{
			DomainSuffix: SocialMessengerDomains,
			Outbound:     "hy2-gateway",
		},
		SingBoxRouteRule{
			DomainSuffix: BlockedServiceDomains,
			Outbound:     "hy2-gateway",
		},
		SingBoxRouteRule{
			IPCIDR:   BlockedServiceIPs,
			Outbound: "hy2-gateway",
		},
	)

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

	// Discord Voice WebRTC UDP media (ports 19294-19344, 50000-65535, 3478) routes through hy2-gateway
	rules = append(rules,
		SingBoxRouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			Port:        []int{3478},
			Outbound:    "hy2-gateway",
		},
		SingBoxRouteRule{
			ProcessName: []string{"Discord.exe", "discord.exe", "DiscordCanary.exe", "DiscordPTB.exe"},
			Network:     "udp",
			PortRange:   []string{"19294:19344", "50000:65535"},
			Outbound:    "hy2-gateway",
		},
	)

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
	// 2.1 DynamoDB regional probe domains -> dns-local (real physical IPs for true RTT measurement)
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: DynamoDBRegionProbeDomains,
		Server:       "dns-local",
	})
	// 3. Services routed through tunnel (YouTube, Discord, Social/Messengers) resolve via FakeIP
	targetTunnelDomains := append([]string{}, YouTubeDomains...)
	targetTunnelDomains = append(targetTunnelDomains, DiscordDomains...)
	targetTunnelDomains = append(targetTunnelDomains, SocialMessengerDomains...)
	targetTunnelDomains = append(targetTunnelDomains, BlockedServiceDomains...)
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: targetTunnelDomains,
		Server:       "dns-fakeip",
	})
	// 4. Vivox voice chat -> dns-remote (resolves real IP of voice servers via 1.1.1.1 through tunnel)
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: []string{"vivox.com"},
		Server:       "dns-remote",
	})
	// 5. Target game processes resolve through encrypted remote DNS to real public IPs
	// (critical: never use FakeIP for games, as FakeIP breaks ICMP ping, STUN, QoS probes, and anti-cheat)
	if len(allProcesses) > 0 {
		dnsRules = append(dnsRules, SingBoxDNSRule{
			ProcessName: allProcesses,
			Server:      "dns-remote",
		})
	}

	// 6. Game & launcher direct domains (for non-game processes, e.g. steam.exe) -> dns-local
	dnsRules = append(dnsRules, SingBoxDNSRule{
		DomainSuffix: DirectGameDomains,
		Server:       "dns-local",
	})

	// 7. Game domains resolve through encrypted remote DNS to real IPs
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
		dnsRules = append(dnsRules, SingBoxDNSRule{
			DomainSuffix: remoteGameDomains,
			Server:       "dns-remote",
		})
	}

	// 8. ONLY web browsing bypass services (YouTube, Discord web, Social/Messengers) resolve via FakeIP
	if len(targetTunnelDomains) > 0 {
		dnsRules = append(dnsRules, SingBoxDNSRule{
			DomainSuffix: targetTunnelDomains,
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
		DisableChromeParrot: true,
		InitialPacketSize:   1320,
	}

	if obfsPassword != "" {
		hy2Outbound.Obfs = &SingBoxHysteria2Obfs{
			Type:     "salamander",
			Password: obfsPassword,
		}
	}

	routeExclude := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}
	if targetServer != "" && !strings.Contains(targetServer, ":") {
		routeExclude = append(routeExclude, targetServer+"/32")
	}

	fullConfig := SingBoxFullConfig{
		Log: SingBoxLogConfig{
			Disabled: false,
			Level:    "info",
			Output:   "singbox.log",
		},
		Inbounds: []SingBoxInbound{
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

