package desync

import (
	"path/filepath"
	"strings"
)

type Preset struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

func commonHeader() []string {
	return []string{
		"--wf-tcp-out=80,443,2053,2083,2087,2096,8443,12",
		"--wf-udp-out=443,19294-19344,50000-50100,12",
		"--lua-init=@%LUA%zapret-lib.lua",
		"--lua-init=@%LUA%zapret-antidpi.lua",
	}
}

func commonUdpRules() []string {
	return []string{
		"--filter-udp=443",
		"--filter-l7=quic",
		"--hostlist=%LISTS%list-general.txt",
		"--hostlist=%LISTS%list-general-user.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=19294-19344,50000-50100",
		"--filter-l7=discord,stun",
		"--payload=wireguard_initiation,wireguard_cookie,stun,discord_ip_discovery",
		"--lua-desync=fake:blob=0x00000000000000000000000000000000:repeats=5",
		"--new",
		"--filter-tcp=2053,2083,2087,2096,8443",
		"--hostlist-domains=discord.media",
		"--out-range=-d10",
		"--payload=tls_client_hello",
		"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=8",
		"--lua-desync=multisplit:pos=1,midsld",
		"--new",
	}
}

func commonHttpRules() []string {
	return []string{
		"--filter-tcp=80",
		"--filter-l7=http",
		"--out-range=-d10",
		"--payload=http_req",
		"--lua-desync=fake:blob=fake_default_http:tcp_md5",
		"--lua-desync=fakedsplit:ip_autottl=-2,3-20:tcp_md5",
		"--new",
	}
}

func makeTlsRules(desyncActions ...string) []string {
	var rules []string

	// 1. Google & YouTube
	rules = append(rules,
		"--filter-tcp=443",
		"--filter-l7=tls",
		"--hostlist=%LISTS%list-google.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--out-range=-d10",
		"--payload=tls_client_hello",
	)
	rules = append(rules, desyncActions...)
	rules = append(rules, "--new")

	// 2. General blocked websites
	rules = append(rules,
		"--filter-tcp=443",
		"--filter-l7=tls",
		"--hostlist=%LISTS%list-general.txt",
		"--hostlist=%LISTS%list-general-user.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--out-range=-d10",
		"--payload=tls_client_hello",
	)
	rules = append(rules, desyncActions...)
	rules = append(rules, "--new")

	// 3. IP-based blocked destinations (ipset-all)
	rules = append(rules,
		"--filter-tcp=443,8443",
		"--filter-l7=tls",
		"--ipset=%LISTS%ipset-all.txt",
		"--hostlist-exclude=%LISTS%list-exclude.txt",
		"--hostlist-exclude=%LISTS%list-exclude-user.txt",
		"--ipset-exclude=%LISTS%ipset-exclude.txt",
		"--ipset-exclude=%LISTS%ipset-exclude-user.txt",
		"--out-range=-d10",
		"--payload=tls_client_hello",
	)
	rules = append(rules, desyncActions...)
	return rules
}

func buildPresetArgs(tlsDesyncActions ...string) []string {
	var res []string
	res = append(res, commonHeader()...)
	res = append(res, commonUdpRules()...)
	res = append(res, commonHttpRules()...)
	res = append(res, makeTlsRules(tlsDesyncActions...)...)
	return res
}

// BuiltinPresets provides all available DPI desync strategies for zapret2 (winws2).
var BuiltinPresets = []Preset{
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
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,dupsid,sni=www.google.com",
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
			"--lua-desync=fakedsplit:pos=1,midsld:tcp_md5:ip_autottl=-2,3-20",
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
			"--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=11:tls_mod=rnd,dupsid,sni=www.google.com",
			"--lua-desync=multidisorder:pos=1,midsld",
		),
	},
}

func GetPreset(name string) *Preset {
	norm := strings.TrimSuffix(strings.TrimSpace(name), ".bat")
	for i := range BuiltinPresets {
		if strings.EqualFold(BuiltinPresets[i].Name, norm) {
			return &BuiltinPresets[i]
		}
	}
	if len(BuiltinPresets) > 0 {
		return &BuiltinPresets[0]
	}
	return nil
}

func GetAvailablePresetNames() []string {
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
	// 1. Unblocks UDP 443 QUIC so Cloudflare WARP connects smoothly through ISP.
	// 2. Unblocks voice UDP ports (Discord voice and RTP).
	// WinDivert does NOT hook TCP 80/443, so browser and system traffic remain 100% direct!
	const udpPortsStr = "19294-19344,50000-50100"

	return []string{
		"--wf-tcp-out=80,443,2053,2083,2087,2096,8443,12",
		"--wf-udp-out=443," + udpPortsStr + ",12",
		"--lua-init=@" + luaSep + "zapret-lib.lua",
		"--lua-init=@" + luaSep + "zapret-antidpi.lua",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--hostlist=" + listsSep + "list-general.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=443",
		"--filter-l7=quic",
		"--ipset=" + listsSep + "ipset-all.txt",
		"--hostlist-exclude=" + listsSep + "list-exclude.txt",
		"--ipset-exclude=" + listsSep + "ipset-exclude.txt",
		"--payload=quic_initial",
		"--lua-desync=fake:blob=fake_default_quic:repeats=11",
		"--new",
		"--filter-udp=" + udpPortsStr,
		"--filter-l7=discord,stun",
		"--payload=wireguard_initiation,wireguard_cookie,stun,discord_ip_discovery",
		"--lua-desync=fake:blob=0x00000000000000000000000000000000:repeats=5",
	}
}

// BuildFilteredArgs is an alias to BuildModularArgs for backward compatibility.
func (p *Preset) BuildFilteredArgs(coreDir string, freeInternet bool) []string {
	return p.BuildModularArgs(coreDir, freeInternet)
}

