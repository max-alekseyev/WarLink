package scanner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"warlink/internal/desync"
)

// EndpointCheck defines a target service to probe during the benchmark.
type EndpointCheck struct {
	Name      string
	Host      string
	Port      int
	CheckHTTP bool
	CheckTLS2 bool
	CheckTLS3 bool
	IsDNS     bool
	IsUDP     bool
}

// CheckResult holds the status of probes for a single endpoint.
type CheckResult struct {
	Name    string
	HTTPOk  bool
	TLS2Ok  bool
	TLS3Ok  bool
	PingMs  int
	Success bool
}

// PresetScore records the benchmark result for one DPI preset.
type PresetScore struct {
	PresetName   string
	PassedChecks int
	TotalChecks  int
	AvgPingMs    int
	Results      []CheckResult
}

// Standard benchmark targets requested for comprehensive DPI bypassing evaluation
var BenchmarkTargets = []EndpointCheck{
	{Name: "DiscordMain", Host: "discord.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "DiscordGateway", Host: "gateway.discord.gg", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "DiscordCDN", Host: "cdn.discordapp.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "DiscordUpdates", Host: "updates.discord.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "DiscordVoiceRTC", Host: "voice.discord.gg", Port: 443, CheckHTTP: false, CheckTLS2: false, CheckTLS3: true},
	{Name: "SteamCommunity", Host: "steamcommunity.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "EpicGamesEOS", Host: "api.epicgames.dev", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "EasyAntiCheat", Host: "modules.easyanticheat.net", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: false},
	{Name: "CloudflareWarpUDP", Host: "162.159.192.1", Port: 2408, IsUDP: true},
	{Name: "YouTubeWeb", Host: "www.youtube.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "YouTubeShort", Host: "youtu.be", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "YouTubeImage", Host: "i.ytimg.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "YouTubeVideoRedirect", Host: "redirector.googlevideo.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "TwitchWeb", Host: "www.twitch.tv", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "GoogleMain", Host: "www.google.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "GoogleGstatic", Host: "fonts.gstatic.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "CloudflareWeb", Host: "www.cloudflare.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "CloudflareCDN", Host: "cdnjs.cloudflare.com", Port: 443, CheckHTTP: true, CheckTLS2: true, CheckTLS3: true},
	{Name: "CloudflareDoH", Host: "cloudflare-dns.com", Port: 443, CheckHTTP: true, CheckTLS2: false, CheckTLS3: true},
	{Name: "CloudflareDNS1111", Host: "1.1.1.1", Port: 53, IsDNS: true},
	{Name: "CloudflareDNS1001", Host: "1.0.0.1", Port: 53, IsDNS: true},
	{Name: "GoogleDNS8888", Host: "8.8.8.8", Port: 53, IsDNS: true},
	{Name: "GoogleDNS8844", Host: "8.8.4.4", Port: 53, IsDNS: true},
}

// RunFullBenchmark sequentially evaluates all 22 builtin presets and returns the winning optimal profile.
func RunFullBenchmark(
	coreDir string,
	progressCb func(curr, total int, presetName, logLine string),
	logCb func(string),
) (string, *PresetScore, error) {
	// Clean up stale winws instances and leftover drivers before starting
	_ = KillProcess("winws2.exe")
	_ = KillProcess("winws.exe")
	StopWinDivertService()
	time.Sleep(200 * time.Millisecond)

	winwsPath := filepath.Join(coreDir, "bin", "winws2.exe")
	presets := desync.BuiltinPresets
	totalPresets := len(presets)

	var scores []PresetScore

	logCb(fmt.Sprintf("[BENCHMARK] Старт глобального тестирования %d профилей обхода DPI...", totalPresets))

	for i, preset := range presets {
		idx := i + 1
		if progressCb != nil {
			progressCb(idx, totalPresets, preset.Name, fmt.Sprintf("Тестирование профиля %s [%d/%d]...", preset.Name, idx, totalPresets))
		}

		// 1. Build arguments and start winws with current preset
		args := preset.BuildModularArgs(coreDir, true)
		cmd := exec.Command(winwsPath, args...)
		cmd.Dir = filepath.Join(coreDir, "bin")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}

		if err := cmd.Start(); err != nil {
			logCb(fmt.Sprintf("[WARN] Ошибка запуска winws для %s: %v", preset.Name, err))
			continue
		}

		// Wait briefly for WinDivert driver filter hook
		time.Sleep(120 * time.Millisecond)

		// 2. Concurrently probe all target endpoints
		results := probeAllEndpoints(BenchmarkTargets)

		// 3. Stop winws process and clean handles
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = KillProcess("winws2.exe")
		_ = KillProcess("winws.exe")
		time.Sleep(50 * time.Millisecond)

		// 4. Calculate score and log formatted table
		passed := 0
		totalChecks := 0
		sumPing := 0
		pingCount := 0

		var logLines []string
		logLines = append(logLines, fmt.Sprintf("[BENCHMARK] Результаты для профиля %s [%d/%d]:", preset.Name, idx, totalPresets))

		for _, r := range results {
			line := formatResultLine(r)
			logLines = append(logLines, "  "+line)

			if r.CheckHTTPCount() > 0 {
				totalChecks += r.CheckHTTPCount()
				if r.HTTPOk {
					passed++
				}
			}
			if r.CheckTLS2Count() > 0 {
				totalChecks += r.CheckTLS2Count()
				if r.TLS2Ok {
					passed++
				}
			}
			if r.CheckTLS3Count() > 0 {
				totalChecks += r.CheckTLS3Count()
				if r.TLS3Ok {
					passed++
				}
			}
			if r.IsPingOnly() {
				totalChecks++
				if r.Success {
					passed++
				}
			}

			if r.PingMs > 0 {
				sumPing += r.PingMs
				pingCount++
			}
		}

		avgPing := 999
		if pingCount > 0 {
			avgPing = sumPing / pingCount
		}

		score := PresetScore{
			PresetName:   preset.Name,
			PassedChecks: passed,
			TotalChecks:  totalChecks,
			AvgPingMs:    avgPing,
			Results:      results,
		}
		scores = append(scores, score)

		// Print formatted log block
		for _, l := range logLines {
			logCb(l)
		}
	}

	if len(scores) == 0 {
		return "general (ALT13)", nil, fmt.Errorf("не удалось протестировать ни один профиль")
	}

	// Sort scores: Highest passed checks first, then lowest ping
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].PassedChecks != scores[j].PassedChecks {
			return scores[i].PassedChecks > scores[j].PassedChecks
		}
		return scores[i].AvgPingMs < scores[j].AvgPingMs
	})

	winner := scores[0]
	logCb(fmt.Sprintf("[BENCHMARK] Победитель автотестирования: %s (Пройдено: %d/%d проверок, ср. пинг: %d ms)",
		winner.PresetName, winner.PassedChecks, winner.TotalChecks, winner.AvgPingMs))

	StopWinDivertService()
	return winner.PresetName, &winner, nil
}

func (r CheckResult) CheckHTTPCount() int {
	return 1
}

func (r CheckResult) CheckTLS2Count() int {
	return 1
}

func (r CheckResult) CheckTLS3Count() int {
	return 1
}

func (r CheckResult) IsPingOnly() bool {
	return !r.HTTPOk && !r.TLS2Ok && !r.TLS3Ok
}

func formatResultLine(r CheckResult) string {
	// Pad name to 25 chars
	namePad := r.Name
	if len(namePad) < 25 {
		namePad = namePad + strings.Repeat(" ", 25-len(namePad))
	}

	var parts []string
	if r.HTTPOk {
		parts = append(parts, "HTTP:OK ")
	} else if r.CheckHTTPCount() > 0 {
		parts = append(parts, "HTTP:ERR")
	}

	if r.TLS2Ok {
		parts = append(parts, "TLS1.2:OK ")
	} else if r.CheckTLS2Count() > 0 {
		parts = append(parts, "TLS1.2:ERR")
	}

	if r.TLS3Ok {
		parts = append(parts, "TLS1.3:OK ")
	} else if r.CheckTLS3Count() > 0 {
		parts = append(parts, "TLS1.3:ERR")
	}

	protoStr := strings.Join(parts, "   ")
	if len(protoStr) < 32 {
		protoStr = protoStr + strings.Repeat(" ", 32-len(protoStr))
	}

	pingStr := "-- ms"
	if r.PingMs > 0 {
		pingStr = fmt.Sprintf("%d ms", r.PingMs)
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%sPing: %s", namePad, pingStr)
	}
	return fmt.Sprintf("%s%s | Ping: %s", namePad, protoStr, pingStr)
}

func probeAllEndpoints(targets []EndpointCheck) []CheckResult {
	results := make([]CheckResult, len(targets))
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func(idx int, tgt EndpointCheck) {
			defer wg.Done()
			results[idx] = probeSingle(tgt)
		}(i, t)
	}

	wg.Wait()
	return results
}

func probeSingle(t EndpointCheck) CheckResult {
	res := CheckResult{Name: t.Name}
	const timeout = 1000 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// DNS or pure ping probe
	if t.IsDNS || t.IsUDP {
		start := time.Now()
		targetAddr := fmt.Sprintf("%s:%d", t.Host, t.Port)
		network := "tcp"
		if t.IsUDP {
			network = "udp"
		}
		d := &net.Dialer{Timeout: timeout}
		conn, err := d.DialContext(ctx, network, targetAddr)
		if err == nil {
			_ = conn.Close()
			res.PingMs = int(time.Since(start).Milliseconds())
			if res.PingMs < 1 {
				res.PingMs = 1
			}
			res.Success = true
		}
		return res
	}

	var mu sync.Mutex
	updatePing := func(elapsed int) {
		mu.Lock()
		defer mu.Unlock()
		if res.PingMs == 0 || elapsed < res.PingMs {
			res.PingMs = elapsed
		}
	}

	var wg sync.WaitGroup

	// 1. HTTP Probe
	if t.CheckHTTP {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s/", t.Host), nil)
			if err != nil {
				return
			}
			tr := &http.Transport{
				DisableKeepAlives: true,
				DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
			}
			client := &http.Client{
				Transport: tr,
				Timeout:   timeout,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				mu.Lock()
				res.HTTPOk = true
				mu.Unlock()
				updatePing(int(time.Since(start).Milliseconds()))
			}
		}()
	}

	// Helper for TLS probes with explicit handshake timeout
	probeTLS := func(version uint16, setOk func()) {
		defer wg.Done()
		start := time.Now()
		dialer := &net.Dialer{Timeout: timeout}
		rawConn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:443", t.Host))
		if err != nil {
			return
		}
		_ = rawConn.SetDeadline(time.Now().Add(timeout))
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName:         t.Host,
			MinVersion:         version,
			MaxVersion:         version,
			InsecureSkipVerify: true,
		})
		err = tlsConn.HandshakeContext(ctx)
		_ = tlsConn.Close()
		if err == nil {
			mu.Lock()
			setOk()
			mu.Unlock()
			updatePing(int(time.Since(start).Milliseconds()))
		}
	}

	// 2. TLS 1.2 Probe
	if t.CheckTLS2 {
		wg.Add(1)
		go probeTLS(tls.VersionTLS12, func() { res.TLS2Ok = true })
	}

	// 3. TLS 1.3 Probe
	if t.CheckTLS3 {
		wg.Add(1)
		go probeTLS(tls.VersionTLS13, func() { res.TLS3Ok = true })
	}

	wg.Wait()
	res.Success = (res.HTTPOk || res.TLS2Ok || res.TLS3Ok)
	return res
}
