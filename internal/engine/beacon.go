package engine

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
	"warlink/internal/config"
	"warlink/internal/singbox"
)

// AutoBeacon quietly collects and sends telemetry samples to the WarLink server.
type AutoBeacon struct {
	mu                 sync.Mutex
	stopChan           chan struct{}
	isRunning          bool
	engine             *Engine
	interval           time.Duration
	startTime          time.Time
	lastBeaconTime     time.Time
	currentGameID      string
	currentProcessName string
	lastMatchServer    string

	// Cumulative match statistics
	samplesCount int
	pingSum      int64
	minPing      int
	maxPing      int
	jitterSum    int64
	lossSum      int64

	logCb func(string)
}

// NewAutoBeacon initializes the background auto-telemetry worker.
func NewAutoBeacon(eng *Engine, logCb func(string)) *AutoBeacon {
	return &AutoBeacon{
		engine:   eng,
		interval: 60 * time.Second,
		logCb:    logCb,
	}
}

func (b *AutoBeacon) log(msg string) {
	if b.logCb != nil {
		b.logCb(msg)
	}
}

// Start begins the auto-beacon polling loop.
func (b *AutoBeacon) Start(gameID, procName string) {
	b.mu.Lock()
	if b.isRunning {
		if gameID != "" {
			b.currentGameID = gameID
		}
		if procName != "" {
			b.currentProcessName = procName
		}
		b.mu.Unlock()
		return
	}

	b.isRunning = true
	b.stopChan = make(chan struct{})
	b.startTime = time.Now()
	b.lastBeaconTime = time.Now()
	b.currentGameID = gameID
	b.currentProcessName = procName
	b.lastMatchServer = ""
	b.samplesCount = 0
	b.pingSum = 0
	b.minPing = 0
	b.maxPing = 0
	b.jitterSum = 0
	b.lossSum = 0
	b.mu.Unlock()

	go b.loop()
}

// Stop terminates the background beacon loop.
func (b *AutoBeacon) Stop() {
	b.mu.Lock()
	if !b.isRunning {
		b.mu.Unlock()
		return
	}
	b.isRunning = false
	if b.stopChan != nil {
		close(b.stopChan)
	}
	b.mu.Unlock()
}

// SetGame updates the current active game ID and process name.
func (b *AutoBeacon) SetGame(gameID, procName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if gameID != "" {
		b.currentGameID = gameID
	}
	if procName != "" {
		b.currentProcessName = procName
	}
}

func (b *AutoBeacon) loop() {
	// First initial probe after 12 seconds to allow the network adapter to fully stabilize
	select {
	case <-b.stopChan:
		return
	case <-time.After(12 * time.Second):
		b.sendBeacon(false)
	}

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopChan:
			return
		case <-ticker.C:
			b.sendBeacon(false)
		}
	}
}

// SendMatchSummary dispatches the final session report and resets match counters.
func (b *AutoBeacon) SendMatchSummary() {
	b.sendBeacon(true)
}

// probeEndpoint attempts a quick TCP connection to one of the target ports to measure latency.
func probeEndpoint(ip string, ports []string, timeout time.Duration) (int, bool) {
	if ip == "" {
		return 0, false
	}
	for _, port := range ports {
		target := net.JoinHostPort(ip, port)
		d := net.Dialer{Timeout: timeout}
		start := time.Now()
		conn, err := d.Dial("tcp", target)
		if err == nil {
			_ = conn.Close()
			elapsed := int(time.Since(start).Milliseconds())
			if elapsed < 1 {
				elapsed = 1
			}
			return elapsed, true
		}
	}
	return 0, false
}

// CollectSnapshot probes all gateways and active game meter in parallel.
func (b *AutoBeacon) CollectSnapshot() (moscowPing int, moscowOK bool, frankfurtPing int, frankfurtOK bool, stockholmPing int, stockholmOK bool, inGamePing int, jitter int, lossPct int, matchServer string, routeMode string) {
	routeMode = singbox.GetNetworkRouteMode()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		moscowPing, moscowOK = probeEndpoint(singbox.MoscowIngressIP, []string{"80", "443"}, 1500*time.Millisecond)
	}()

	go func() {
		defer wg.Done()
		frankfurtPing, frankfurtOK = probeEndpoint(singbox.FrankfurtEdgeIP, []string{"80", "443"}, 1500*time.Millisecond)
	}()

	go func() {
		defer wg.Done()
		stockholmPing, stockholmOK = probeEndpoint(singbox.StockholmCoreIP, []string{"80", "443"}, 1500*time.Millisecond)
	}()

	wg.Wait()

	// 2. Query in-game passive UDP sniffer
	var isGameActive bool
	if b.engine != nil && b.engine.pingMeter != nil {
		var wireRtt, minR, maxR int
		matchServer, wireRtt, inGamePing, jitter, minR, maxR, isGameActive = b.engine.pingMeter.GetDetailedStats()
		_ = wireRtt
		_ = minR
		_ = maxR
	}

	// 3. Query tunnel ping monitor for background jitter and packet loss
	var tunnelPing, tunnelLoss, tunnelJitter int
	if b.engine != nil && b.engine.telemetry != nil {
		tunnelPing, tunnelLoss, tunnelJitter = b.engine.telemetry.GetDetailed()
	}

	if isGameActive && inGamePing > 0 {
		lossPct = tunnelLoss
		if tunnelLoss >= 80 {
			// If game UDP packets are actively flowing with valid inGamePing,
			// a high TCP probe loss indicates a probe firewall drop, not real game packet loss.
			lossPct = 0
		}
		if jitter <= 0 {
			jitter = tunnelJitter
		}
	} else {
		// No active match packet sniffed: use active route gateway baseline ping
		lossPct = tunnelLoss
		jitter = tunnelJitter
		matchServer = ""
		switch routeMode {
		case config.RouteModeDirectMoscow:
			if moscowOK && moscowPing > 0 {
				inGamePing = moscowPing + 30
			} else if tunnelPing > 0 {
				inGamePing = tunnelPing + 30
			}
		case config.RouteModeDirectFrankfurt:
			if frankfurtOK && frankfurtPing > 0 {
				inGamePing = frankfurtPing + 25
			} else if tunnelPing > 0 {
				inGamePing = tunnelPing + 25
			}
		case config.RouteModeDirectStockholm:
			if stockholmOK && stockholmPing > 0 {
				inGamePing = stockholmPing + 30
			} else if tunnelPing > 0 {
				inGamePing = tunnelPing + 30
			}
		case config.RouteModeTransit:
			fallthrough
		default:
			if moscowOK && moscowPing > 0 {
				inGamePing = moscowPing + 35
			} else if frankfurtOK && frankfurtPing > 0 {
				inGamePing = frankfurtPing + 25
			} else if tunnelPing > 0 {
				inGamePing = tunnelPing + 30
			}
		}
	}

	return
}

func (b *AutoBeacon) sendBeacon(isFinal bool) {
	moscowPing, moscowOK, frankfurtPing, frankfurtOK, stockholmPing, stockholmOK, inGamePing, jitter, lossPct, matchServer, routeMode := b.CollectSnapshot()

	b.mu.Lock()
	gameID := b.currentGameID
	procName := b.currentProcessName
	startTime := b.startTime
	durSec := int(time.Since(startTime).Seconds())
	if durSec < 0 {
		durSec = 0
	}

	if inGamePing > 0 {
		b.samplesCount++
		b.pingSum += int64(inGamePing)
		if b.minPing == 0 || inGamePing < b.minPing {
			b.minPing = inGamePing
		}
		if inGamePing > b.maxPing {
			b.maxPing = inGamePing
		}
		b.jitterSum += int64(jitter)
		b.lossSum += int64(lossPct)
		if matchServer != "" {
			b.lastMatchServer = matchServer
		}
	}

	avgPing := inGamePing
	minPing := b.minPing
	maxPing := b.maxPing
	avgJitter := jitter
	avgLoss := lossPct
	if b.samplesCount > 0 {
		avgPing = int(b.pingSum / int64(b.samplesCount))
		avgJitter = int(b.jitterSum / int64(b.samplesCount))
		avgLoss = int(b.lossSum / int64(b.samplesCount))
	}
	if matchServer == "" && b.lastMatchServer != "" {
		matchServer = b.lastMatchServer
	}

	status := "beacon"
	if isFinal {
		status = "match_summary"
		// Reset counters for next match
		b.samplesCount = 0
		b.pingSum = 0
		b.minPing = 0
		b.maxPing = 0
		b.jitterSum = 0
		b.lossSum = 0
		b.startTime = time.Now()
	}
	b.mu.Unlock()

	cfg := config.Load()
	accountNumber := ""
	if cfg != nil {
		accountNumber = cfg.AccountNumber
	}
	deviceID := singbox.GetMachineGUID()
	appVersion := singbox.ClientVersion

	hTrace := sha256.Sum256([]byte(accountNumber + deviceID))
	traceID := fmt.Sprintf("trc-%x-%d", hTrace[:4], time.Now().Unix())

	// Build telemetry payload compatible with both /telemetry/beacon and /routing-feedback
	payload := map[string]interface{}{
		"trace_id":             traceID,
		"account_number":       accountNumber,
		"device_id":            deviceID,
		"app_version":          appVersion,
		"route_mode":           routeMode,
		"status":               status,
		"ping_moscow_ms":       moscowPing,
		"ping_frankfurt_ms":    frankfurtPing,
		"ping_stockholm_ms":    stockholmPing,
		"in_game_ping":         inGamePing,
		"jitter_ms":            jitter,
		"packet_loss_pct":      lossPct,
		"game_id":              gameID,
		"process_name":         procName,
		"match_server":         matchServer,
		"is_final_report":      isFinal,
		"session_duration_sec": durSec,
		"min_ping_ms":          minPing,
		"max_ping_ms":          maxPing,
		"avg_ping_ms":          avgPing,
		"timestamp":            time.Now().Unix(),
		"telemetry_data": map[string]interface{}{
			"moscow_ok":       moscowOK,
			"frankfurt_ok":    frankfurtOK,
			"stockholm_ok":    stockholmOK,
			"avg_ping_ms":     avgPing,
			"min_ping_ms":     minPing,
			"max_ping_ms":     maxPing,
			"avg_jitter_ms":   avgJitter,
			"avg_loss_pct":    avgLoss,
			"duration_sec":    durSec,
			"is_final_report": isFinal,
			"match_server":    matchServer,
			"game_id":         gameID,
			"process_name":    procName,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Prepare signature if HMAC secret is available
	hmacSecret := singbox.GetHMACSecret()
	ts := time.Now().Unix() + singbox.GetServerTimeOffset()
	nonceBytes := make([]byte, 8)
	_, _ = rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	var sig string
	if hmacSecret != "" {
		dataToSign := fmt.Sprintf("%s:%d:%s", deviceID, ts, nonce)
		mac := hmac.New(sha256.New, []byte(hmacSecret))
		mac.Write([]byte(dataToSign))
		sig = hex.EncodeToString(mac.Sum(nil))
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Primary target: Active Server Gateway
	serverAPI := singbox.GetServerAPI()
	if serverAPI == "" {
		serverAPI = "http://" + singbox.StockholmCoreIP
	}

	apiEndpoints := []string{
		fmt.Sprintf("%s/api/v1/telemetry/beacon", serverAPI),
		fmt.Sprintf("http://%s/api/v1/telemetry/beacon", singbox.StockholmCoreIP),
		fmt.Sprintf("%s/api/v1/routing-feedback", serverAPI),
		fmt.Sprintf("http://%s/api/v1/routing-feedback", singbox.StockholmCoreIP),
	}

	var postErr error
	var sentOK bool
	for _, url := range apiEndpoints {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			postErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "WarLink-Client/"+appVersion)
		if sig != "" {
			req.Header.Set("X-Signature", sig)
			req.Header.Set("X-Device-ID", deviceID)
			req.Header.Set("X-Timestamp", fmt.Sprintf("%d", ts))
			req.Header.Set("X-Nonce", nonce)
		}

		resp, err := client.Do(req)
		if err != nil {
			postErr = err
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			sentOK = true
			break
		}
		// If 404 on /telemetry/beacon, try next fallback in loop
		postErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if sentOK {
		if isFinal {
			b.log(fmt.Sprintf("[BEACON] Отправлен финальный отчет матча: Игра=%s, Сервер=%s, Средний=%d мс (Мин %d / Макс %d), Джиттер=%d мс, Потери=%d%%, Длительность=%d сек (Маршрут: %s)",
				gameID, matchServer, avgPing, minPing, maxPing, avgJitter, avgLoss, durSec, routeMode))
		} else {
			b.log(fmt.Sprintf("[BEACON] Автоматическая телеметрия: Маршрут=%s, Москва=%d мс, Стокгольм=%d мс, Игра=%d мс, Джиттер=%d мс, Потери=%d%%",
				routeMode, moscowPing, stockholmPing, inGamePing, jitter, lossPct))
		}
	} else if postErr != nil {
		// Silent non-critical log
		_ = postErr
	}
}
