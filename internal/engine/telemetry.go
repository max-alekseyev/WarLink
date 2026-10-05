package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"warlink/internal/config"
	"warlink/internal/singbox"
)

// ReportEngineCrash sends a background crash/incident report to the telemetry endpoint.
func ReportEngineCrash(errorType, message string, contextData map[string]interface{}) {
	payload := map[string]interface{}{
		"device_id":   singbox.GetMachineGUID(),
		"app_version": singbox.ClientVersion,
		"error_type":  errorType,
		"message":     message,
		"context":     contextData,
	}
	cfg := config.Load()
	if cfg != nil {
		payload["account_number"] = cfg.AccountNumber
	}
	go func() {
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		apiURL := fmt.Sprintf("http://%s/api/v1/telemetry/crash", singbox.StockholmCoreIP)
		c := &http.Client{Timeout: 5 * time.Second}
		resp, postErr := c.Post(apiURL, "application/json", bytes.NewReader(body))
		if postErr == nil {
			_ = resp.Body.Close()
		}
	}()
}

// TelemetryMonitor tracks real-time physical ping and packet loss over a sliding window.
type TelemetryMonitor struct {
	mu           sync.Mutex
	stopChan     chan struct{}
	isRunning    bool
	pingMs       int
	packetLoss   int
	history      []bool // true = success, false = dropped/timeout
	rtts         []int  // rtt in ms for successful probes
	windowSize   int
	directTarget string
}

func NewTelemetryMonitor() *TelemetryMonitor {
	target := "1.1.1.1:80"
	if gwIP := singbox.GetServerIP(); gwIP != "" {
		if !strings.Contains(gwIP, ":") {
			target = net.JoinHostPort(gwIP, "80")
		} else {
			target = gwIP
		}
	}
	return &TelemetryMonitor{
		windowSize:   15,
		directTarget: target,
		history:      make([]bool, 0, 15),
		rtts:         make([]int, 0, 15),
	}
}

// SetDirectTarget updates the direct probe target IP or IP:port.
func (tm *TelemetryMonitor) SetDirectTarget(target string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	if !strings.Contains(target, ":") {
		target = net.JoinHostPort(target, "80")
	}
	tm.directTarget = target
}

// Start begins the background probing loop while the tunnel is connected.
func (tm *TelemetryMonitor) Start(_ ...bool) {
	tm.mu.Lock()
	if tm.isRunning {
		tm.mu.Unlock()
		return
	}
	tm.isRunning = true
	tm.stopChan = make(chan struct{})
	tm.history = tm.history[:0]
	tm.rtts = tm.rtts[:0]
	tm.mu.Unlock()

	go tm.loop()
}

// Stop stops the telemetry probing loop and resets metrics.
func (tm *TelemetryMonitor) Stop() {
	tm.mu.Lock()
	if !tm.isRunning {
		tm.mu.Unlock()
		return
	}
	tm.isRunning = false
	close(tm.stopChan)
	tm.pingMs = 0
	tm.packetLoss = 0
	tm.history = tm.history[:0]
	tm.rtts = tm.rtts[:0]
	tm.mu.Unlock()
}

// Get returns the current measured ping (ms) and packet loss percentage.
func (tm *TelemetryMonitor) Get() (int, int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.pingMs, tm.packetLoss
}

// GetDetailed returns the current measured ping (ms), packet loss percentage, and sliding jitter (ms).
func (tm *TelemetryMonitor) GetDetailed() (pingMs int, lossPct int, jitterMs int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	jitter := 0
	if len(tm.rtts) >= 2 {
		diffSum := 0
		for i := 1; i < len(tm.rtts); i++ {
			d := tm.rtts[i] - tm.rtts[i-1]
			if d < 0 {
				d = -d
			}
			diffSum += d
		}
		jitter = diffSum / (len(tm.rtts) - 1)
	}
	return tm.pingMs, tm.packetLoss, jitter
}

func (tm *TelemetryMonitor) loop() {
	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	// Initial probe immediately
	tm.probe()

	for {
		select {
		case <-tm.stopChan:
			return
		case <-ticker.C:
			tm.probe()
		}
	}
}

func (tm *TelemetryMonitor) probe() {
	tm.mu.Lock()
	directTarget := tm.directTarget
	tm.mu.Unlock()

	rtt, success := probeDirect(directTarget, 1800*time.Millisecond)

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Update sliding window
	tm.history = append(tm.history, success)
	if len(tm.history) > tm.windowSize {
		tm.history = tm.history[1:]
	}

	if success && rtt > 0 {
		tm.rtts = append(tm.rtts, rtt)
		if len(tm.rtts) > tm.windowSize {
			tm.rtts = tm.rtts[1:]
		}
	}

	// Calculate packet loss percentage
	failed := 0
	for _, ok := range tm.history {
		if !ok {
			failed++
		}
	}
	if len(tm.history) > 0 {
		tm.packetLoss = int((float64(failed) / float64(len(tm.history))) * 100.0)
	} else {
		tm.packetLoss = 0
	}

	// Calculate average ping from successful probes
	if len(tm.rtts) > 0 {
		sum := 0
		for _, v := range tm.rtts {
			sum += v
		}
		tm.pingMs = sum / len(tm.rtts)
	} else if success {
		tm.pingMs = rtt
	}
}

// probeDirect measures TCP connection RTT to target
func probeDirect(target string, timeout time.Duration) (int, bool) {
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", target)
	if err != nil {
		return 0, false
	}
	_ = conn.Close()
	elapsed := int(time.Since(start).Milliseconds())
	if elapsed < 1 {
		elapsed = 1
	}
	return elapsed, true
}

