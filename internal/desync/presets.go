package desync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Preset struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

const discordDomainsStr = "discord.com,discord.gg,discordapp.com,discordapp.net,discord.media,gateway.discord.gg,status.discord.com,dis.gd,discord-attachments-uploads-prd.storage.googleapis.com,discordcdn.com,cdn.discordapp.com,voice.discord.gg"

func commonHeader() []string {
	return []string{
		"--blob=fake_discord:@%BIN%ACTIVE_DISCORD_UDP.bin",
		"--wf-tcp-out=80,443,2053,2083,2087,2096,8443",
		"--wf-tcp-in=80,443",
		"--wf-tcp-empty=0",
		"--wf-udp-out=443,19294-19344,50000-65535",
		"--ctrack-timeouts=60:300:60:3600",
		"--lua-init=@%LUA%zapret-lib.lua",
		"--lua-init=@%LUA%zapret-antidpi.lua",
		"--lua-init=@%LUA%zapret-auto.lua",
	}
}

func commonUdpRules() []string {
	return []string{
		"--filter-udp=443",
		"--filter-l7=quic",
		"--hostlist=%LISTS%list-general.txt",
		"--hostlist=%LISTS%list-general-user.txt",
		"--hostlist=%LISTS%list-auto.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=19294-19344,50000-65535",
		"--filter-l7=discord,stun",
		"--payload=wireguard_initiation,wireguard_cookie,stun,discord_ip_discovery",
		"--out-range=-d3",
		"--lua-desync=fake:blob=fake_discord:repeats=6",
		"--new",
		"--filter-tcp=2053,2083,2087,2096,8443",
		"--hostlist-domains=" + discordDomainsStr,
		"--payload=tls_client_hello",
		"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
		"--lua-desync=multisplit:pos=1,midsld",
		"--new",
	}
}

func commonHttpRules() []string {
	return []string{
		"--filter-tcp=80",
		"--filter-l7=http",
		"--hostlist=%LISTS%list-general.txt",
		"--hostlist=%LISTS%list-general-user.txt",
		"--hostlist=%LISTS%list-auto.txt",
		"--hostlist-exclude=%LISTS%list-google.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--payload=http_req",
		"--lua-desync=fake:blob=fake_default_http:tcp_ts=-1000",
		"--lua-desync=multisplit:pos=2",
		"--new",
	}
}

func makeTlsRules(googleDesync []string, generalDesync []string) []string {
	var rules []string

	hasCircular := false
	for _, a := range generalDesync {
		if strings.Contains(a, "circular") {
			hasCircular = true
			break
		}
	}

	if hasCircular {
		// 1. Google & YouTube with adaptive circular evasion chain (safe for GFE: SeqOverlap -> TS shift -> AutoTTL -> OOB)
		rules = append(rules,
			"--filter-tcp=443",
			"--filter-l7=tls",
			"--hostlist=%LISTS%list-google.txt",
			"--hostlist-exclude=%LISTS%list-exclude.txt",
			"--hostlist-exclude=%LISTS%list-exclude-user.txt",
			"--ipset-exclude=%LISTS%ipset-exclude.txt",
			"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
			"--in-range=-s34228",
			"--lua-desync=circular:fails=3:key=google:time=60:retrans=3:maxseq=32768:inseq=4096:reset",
			"--payload=tls_client_hello",
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000:strategy=1",
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6:strategy=2",
			"--lua-desync=multisplit:pos=1,midsld:strategy=2",
			"--lua-desync=fake:blob=fake_default_tls:ip_ttl=8:ip_autottl=-2,3-20:repeats=8:strategy=3",
			"--lua-desync=multidisorder:pos=1,midsld:strategy=3",
			"--lua-desync=oob:pos=1:strategy=4:final",
			"--lua-desync=multisplit:pos=1,midsld:strategy=4:final",
			"--new",
		)

		// 2. Discord services with adaptive circular evasion chain (safe for Cloudflare: SeqOverlap -> TS shift -> AutoTTL -> OOB)
		rules = append(rules,
			"--filter-tcp=443",
			"--filter-l7=tls",
			"--hostlist-domains="+discordDomainsStr,
			"--hostlist-exclude=%LISTS%list-exclude.txt",
			"--hostlist-exclude=%LISTS%list-exclude-user.txt",
			"--ipset-exclude=%LISTS%ipset-exclude.txt",
			"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
			"--in-range=-s34228",
			"--lua-desync=circular:fails=3:key=discord:time=60:retrans=3:maxseq=32768:inseq=4096:reset",
			"--payload=tls_client_hello",
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000:strategy=1",
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6:strategy=2",
			"--lua-desync=multisplit:pos=1,midsld:strategy=2",
			"--lua-desync=fake:blob=fake_default_tls:ip_ttl=8:ip_autottl=-2,3-20:repeats=8:strategy=3",
			"--lua-desync=multidisorder:pos=1,midsld:strategy=3",
			"--lua-desync=oob:pos=1:strategy=4:final",
			"--lua-desync=multisplit:pos=1,midsld:strategy=4:final",
			"--new",
		)
	} else {
		// Non-circular preset: determine safest action for Google and Discord matching the preset style
		var safeGoogleDesync []string
		hasSeqOvl := false
		hasTtl := false
		for _, a := range generalDesync {
			if strings.Contains(a, "seqovl") {
				hasSeqOvl = true
			}
			if strings.Contains(a, "ip_ttl") || strings.Contains(a, "ip_autottl") {
				hasTtl = true
			}
		}

		if len(googleDesync) > 0 {
			safeGoogleDesync = googleDesync
		} else if hasSeqOvl {
			safeGoogleDesync = []string{
				"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000",
			}
		} else if hasTtl {
			safeGoogleDesync = []string{
				"--lua-desync=fake:blob=fake_default_tls:ip_ttl=8:ip_autottl=-2,3-20:repeats=8",
				"--lua-desync=multidisorder:pos=1,midsld",
			}
		} else {
			safeGoogleDesync = []string{
				"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
				"--lua-desync=multisplit:pos=1,midsld",
			}
		}

		// 1. Google & YouTube
		rules = append(rules,
			"--filter-tcp=443",
			"--filter-l7=tls",
			"--hostlist=%LISTS%list-google.txt",
			"--hostlist-exclude=%LISTS%list-exclude.txt",
			"--hostlist-exclude=%LISTS%list-exclude-user.txt",
			"--ipset-exclude=%LISTS%ipset-exclude.txt",
			"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
			"--payload=tls_client_hello",
		)
		rules = append(rules, safeGoogleDesync...)
		rules = append(rules, "--new")

		// 2. Discord services
		rules = append(rules,
			"--filter-tcp=443",
			"--filter-l7=tls",
			"--hostlist-domains="+discordDomainsStr,
			"--hostlist-exclude=%LISTS%list-exclude.txt",
			"--hostlist-exclude=%LISTS%list-exclude-user.txt",
			"--ipset-exclude=%LISTS%ipset-exclude.txt",
			"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
			"--payload=tls_client_hello",
		)
		rules = append(rules, safeGoogleDesync...)
		rules = append(rules, "--new")
	}

	// 3. General blocked websites (strictly excludes Google and Discord lists)
	rules = append(rules,
		"--filter-tcp=443",
		"--filter-l7=tls",
		"--hostlist=%LISTS%list-general.txt",
		"--hostlist=%LISTS%list-general-user.txt",
		"--hostlist=%LISTS%list-auto.txt",
		"--hostlist-auto=%LISTS%list-auto.txt",
		"--hostlist-auto-fail-threshold=3",
		"--hostlist-auto-fail-time=60",
		"--hostlist-auto-retrans-threshold=3",
		"--hostlist-auto-retrans-maxseq=32768",
		"--hostlist-auto-retrans-reset=1",
		"--hostlist-auto-incoming-maxseq=4096",
		"--hostlist-exclude=%LISTS%list-google.txt",
		"--hostlist-exclude-domains="+discordDomainsStr,
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
	)

	if hasCircular {
		for _, a := range generalDesync {
			if strings.HasPrefix(a, "--in-range") || strings.Contains(a, "circular") {
				rules = append(rules, a)
			}
		}
		rules = append(rules, "--payload=tls_client_hello")
		for _, a := range generalDesync {
			if !strings.HasPrefix(a, "--in-range") && !strings.Contains(a, "circular") {
				rules = append(rules, a)
			}
		}
	} else {
		rules = append(rules, "--payload=tls_client_hello")
		rules = append(rules, generalDesync...)
	}
	rules = append(rules, "--new")

	// 4. IP-based blocked destinations (ipset-all)
	rules = append(rules,
		"--filter-tcp=443,8443",
		"--filter-l7=tls",
		"--ipset=%LISTS%ipset-all.txt",
		"--hostlist-exclude=%LISTS%list-google.txt",
		"--hostlist-exclude-domains="+discordDomainsStr,
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
	)
	if hasCircular {
		for _, a := range generalDesync {
			if strings.HasPrefix(a, "--in-range") || strings.Contains(a, "circular") {
				rules = append(rules, a)
			}
		}
		rules = append(rules, "--payload=tls_client_hello")
		for _, a := range generalDesync {
			if !strings.HasPrefix(a, "--in-range") && !strings.Contains(a, "circular") {
				rules = append(rules, a)
			}
		}
	} else {
		rules = append(rules, "--payload=tls_client_hello")
		rules = append(rules, generalDesync...)
	}
	return rules
}

func buildPresetArgs(tlsDesyncActions ...string) []string {
	return buildPresetArgsWithGoogle(nil, tlsDesyncActions...)
}

func buildPresetArgsWithGoogle(googleDesync []string, generalDesync ...string) []string {
	var res []string
	res = append(res, commonHeader()...)
	res = append(res, commonUdpRules()...)
	res = append(res, commonHttpRules()...)
	res = append(res, makeTlsRules(googleDesync, generalDesync)...)
	return res
}

// BuiltinPresets provides the Russian TSPU evasion strategies for zapret2 (winws2).
var BuiltinPresets = []Preset{
	{
		Name: "Автокалибровка (Circular Adaptive)",
		Args: buildPresetArgs(
			"--in-range=-s34228",
			"--lua-desync=circular:fails=3:key=general:time=60:retrans=3:maxseq=32768:inseq=4096:reset",
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000:strategy=1",
			"--lua-desync=fake:blob=fake_default_tls:ip_ttl=8:ip_autottl=-2,3-20:repeats=8:strategy=2",
			"--lua-desync=multidisorder:pos=1,midsld:strategy=2",
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-10000:repeats=6:tls_mod=rnd,dupsid:strategy=3",
			"--lua-desync=multisplit:pos=1,midsld:strategy=3",
			"--lua-desync=oob:pos=1:strategy=4",
			"--lua-desync=multisplit:pos=1,midsld:strategy=4",
			"--lua-desync=fake:blob=fake_default_tls:badsum:ip_ttl=8:ip_autottl=-2,3-20:repeats=11:tls_mod=rndsni:strategy=5",
			"--lua-desync=multisplit:pos=1,midsld:strategy=5",
			"--lua-desync=fake:blob=fake_default_tls:tls_mod=sni=gosuslugi.ru:tcp_ts=-1000:repeats=6:strategy=6:final",
			"--lua-desync=multisplit:pos=1,midsld:strategy=6:final",
		),
	},
	{
		Name: "Стратегия 1 (EcoFilter SeqOverlap)",
		Args: buildPresetArgs(
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000",
		),
	},
	{
		Name: "Стратегия 2 (MultiDisorder TTL)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:ip_ttl=8:ip_autottl=-2,3-20:repeats=8",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 3 (PAWS TimestampShift)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-10000:repeats=6:tls_mod=rnd,dupsid",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 4 (FakedDisorder Interleaved)",
		Args: buildPresetArgs(
			"--lua-desync=fakeddisorder:pos=midsld:repeats=4:tcp_ts=-1000",
		),
	},
	{
		Name: "Стратегия 5 (L4 BadSum Saturator)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:badsum:ip_ttl=8:ip_autottl=-2,3-20:repeats=11:tls_mod=rndsni",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 6 (WhiteSNI Mimicry)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tls_mod=sni=gosuslugi.ru:tcp_ts=-1000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 7 (IPFrag L3 Bypass)",
		Args: buildPresetArgs(
			"--lua-desync=multisplit:pos=1,midsld:ipfrag2:ipfrag_pos_tcp=32:ipfrag_disorder",
		),
	},
	{
		Name: "Стратегия 8 (TCP MD5 BGP Mask)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=8:tls_mod=rnd,dupsid",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 9 (Combined Burst DoubleFooling)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=6",
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6:tls_mod=rnd,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "Стратегия 10 (Stealth MicroSplit)",
		Args: buildPresetArgs(
			"--lua-desync=multisplit:pos=1,midsld,endsld:nodrop",
		),
	},
}

// legacyPresets provides backward-compatible aliases for previously saved user profiles.
var legacyPresets = []Preset{
	{
		Name: "general",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,dupsid",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (EXP)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=11:tls_mod=rnd,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "general (FAKE TLS AUTO ALT)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,rndsni,dupsid",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (FAKE TLS AUTO)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,rndsni,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "general (SIMPLE FAKE ALT)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (SIMPLE FAKE)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=6",
			"--lua-desync=multidisorder:pos=midsld",
		),
	},
	{
		Name: "general (ALT2)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "general (FAKE TLS AUTO ALT2)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=8",
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=4",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (SIMPLE FAKE ALT2)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT3)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=6",
			"--lua-desync=multisplit:pos=midsld",
		),
	},
	{
		Name: "general (FAKE TLS AUTO ALT3)",
		Args: buildPresetArgs(
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000",
		),
	},
	{
		Name: "general (ALT4)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT5)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=6",
			"--lua-desync=multidisorder:pos=midsld",
		),
	},
	{
		Name: "general (ALT6)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT7)",
		Args: buildPresetArgs(
			"--lua-desync=multisplit:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000",
		),
	},
	{
		Name: "general (ALT8)",
		Args: buildPresetArgs(
			"--lua-desync=multidisorder:pos=1,midsld:seqovl=568:seqovl_pattern=0x1603030000",
		),
	},
	{
		Name: "general (ALT9)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,rndsni,dupsid",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT10)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,rndsni,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT11)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=11:tls_mod=rnd,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT12)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=8",
			"--lua-desync=fake:blob=fake_default_tls:tcp_seq=-10000:repeats=4",
			"--lua-desync=multisplit:pos=1,midsld",
		),
	},
	{
		Name: "general (ALT13)",
		Args: buildPresetArgs(
			"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6:tls_mod=rnd,dupsid",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
}

var (
	dynamicPresetsMu sync.RWMutex
	dynamicPresets   []Preset
)

// SetDynamicPresets replaces current active presets with remotely fetched presets.
func SetDynamicPresets(presets []Preset) {
	dynamicPresetsMu.Lock()
	defer dynamicPresetsMu.Unlock()
	if len(presets) > 0 {
		dynamicPresets = presets
	}
}

// GetDynamicPresets returns active dynamic presets or falls back to BuiltinPresets.
func GetDynamicPresets() []Preset {
	dynamicPresetsMu.RLock()
	defer dynamicPresetsMu.RUnlock()
	if len(dynamicPresets) > 0 {
		return dynamicPresets
	}
	return BuiltinPresets
}

// FetchRemoteDesyncConfig queries the server API for updated desync presets.
func FetchRemoteDesyncConfig(serverAPI string) ([]Preset, error) {
	if serverAPI == "" {
		return nil, fmt.Errorf("server API not provided")
	}
	apiURL := fmt.Sprintf("%s/api/v1/desync/config", strings.TrimRight(serverAPI, "/"))
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote desync config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d for desync config", resp.StatusCode)
	}

	var res struct {
		Success bool     `json:"success"`
		Version string   `json:"version,omitempty"`
		Presets []Preset `json:"presets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode desync config JSON: %w", err)
	}
	if !res.Success || len(res.Presets) == 0 {
		return nil, fmt.Errorf("server returned empty or unsuccessful desync config")
	}

	SetDynamicPresets(res.Presets)
	return res.Presets, nil
}

func GetPreset(name string) *Preset {
	norm := strings.TrimSuffix(strings.TrimSpace(name), ".bat")

	dynamicPresetsMu.RLock()
	for i := range dynamicPresets {
		if strings.EqualFold(dynamicPresets[i].Name, norm) {
			p := dynamicPresets[i]
			dynamicPresetsMu.RUnlock()
			return &p
		}
	}
	dynamicPresetsMu.RUnlock()

	for i := range BuiltinPresets {
		if strings.EqualFold(BuiltinPresets[i].Name, norm) {
			return &BuiltinPresets[i]
		}
	}
	for i := range legacyPresets {
		if strings.EqualFold(legacyPresets[i].Name, norm) {
			return &legacyPresets[i]
		}
	}
	if len(BuiltinPresets) > 0 {
		return &BuiltinPresets[0]
	}
	return nil
}

func GetAvailablePresetNames() []string {
	dynamicPresetsMu.RLock()
	if len(dynamicPresets) > 0 {
		res := make([]string, len(dynamicPresets))
		for i, p := range dynamicPresets {
			res[i] = p.Name
		}
		dynamicPresetsMu.RUnlock()
		return res
	}
	dynamicPresetsMu.RUnlock()

	res := make([]string, len(BuiltinPresets))
	for i, p := range BuiltinPresets {
		res[i] = p.Name
	}
	return res
}


// BuildArgs returns all arguments with paths expanded (full preset).
func (p *Preset) BuildArgs(coreDir string) []string {
	return p.BuildModularArgs(coreDir, true)
}

// BuildModularArgs returns arguments tailored for game-only or web desynchronization.
func (p *Preset) BuildModularArgs(coreDir string, freeInternet bool) []string {
	binDir := filepath.Join(coreDir, "bin")
	listsDir := filepath.Join(coreDir, "lists")
	luaDir := filepath.Join(coreDir, "lua")
	binSep := binDir + string(filepath.Separator)
	listsSep := listsDir + string(filepath.Separator)
	luaSep := luaDir + string(filepath.Separator)

	if freeInternet {
		var res []string
		for _, a := range p.Args {
			v := strings.ReplaceAll(a, "%BIN%", binSep)
			v = strings.ReplaceAll(v, "%LISTS%", listsSep)
			v = strings.ReplaceAll(v, "%LUA%", luaSep)
			v = strings.ReplaceAll(v, "%CORE%", coreDir+string(filepath.Separator))

			res = append(res, v)
			if strings.HasPrefix(v, "--hostlist=") && strings.Contains(v, "list-general.txt") {
				res = append(res, "--hostlist="+listsSep+"list-free-internet.txt")
			}
		}
		return res
	}

	// Game-Only Selective Filtering (Free Internet disabled):
	// 1. Unblocks UDP 443 QUIC so Hysteria 2 connects smoothly through ISP.
	// 2. Unblocks voice UDP ports (Discord voice and RTP).
	// 3. Selectively unblocks game auth & backend HTTPS (TCP 443) via list-general.txt.
	// Browser, banking, and general web traffic remain 100% direct and untouched!
	const udpPortsStr = "19294-19344,50000-65535"

	return []string{
		"--blob=fake_discord:@" + binSep + "ACTIVE_DISCORD_UDP.bin",
		"--wf-tcp-out=443",
		"--wf-udp-out=443," + udpPortsStr,
		"--ctrack-timeouts=60:300:60:3600",
		"--lua-init=@" + luaSep + "zapret-lib.lua",
		"--lua-init=@" + luaSep + "zapret-antidpi.lua",
		"--filter-tcp=443",
		"--filter-l7=tls",
		"--hostlist=" + listsSep + "list-general.txt",
		"--hostlist=" + listsSep + "list-general-user.txt",
		"--hostlist=" + listsSep + "list-auto.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude-user.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude-user.txt",
		"--payload=tls_client_hello",
		"--lua-desync=fake:blob=fake_default_tls:tcp_ts=-1000:repeats=6",
		"--lua-desync=multisplit:pos=1,midsld",
		"--new",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--hostlist=" + listsSep + "list-general.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude-user.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude-user.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--ipset=" + listsSep + "ipset-all.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude-user.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude-user.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=" + udpPortsStr,
		"--filter-l7=discord,stun",
		"--payload=wireguard_initiation,wireguard_cookie,stun,discord_ip_discovery",
		"--out-range=-d3",
		"--lua-desync=fake:blob=fake_discord:repeats=6",
	}
}

// BuildFilteredArgs is an alias to BuildModularArgs for backward compatibility.
func (p *Preset) BuildFilteredArgs(coreDir string, freeInternet bool) []string {
	return p.BuildModularArgs(coreDir, freeInternet)
}

