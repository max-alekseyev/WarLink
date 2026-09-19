package engine

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
	"warlink/internal/singbox"
)

// TelemetryMonitor tracks real-time physical ping and packet loss over a sliding window.
type TelemetryMonitor struct {
	mu             sync.Mutex
	stopChan       chan struct{}
	isRunning      bool
	pingMs         int
	packetLoss     int
	history        []bool // true = success, false = dropped/timeout
	rtts           []int  // rtt in ms for successful probes
	windowSize     int
	useSocksProxy  bool
	socksAddr      string
	directTarget   string
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
		windowSize:    15,
		socksAddr:     "127.0.0.1:40000",
		directTarget:  target,
		history:       make([]bool, 0, 15),
		rtts:          make([]int, 0, 15),
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
func (tm *TelemetryMonitor) Start(useSocks bool) {
	tm.mu.Lock()
	if tm.isRunning {
		tm.mu.Unlock()
		return
	}
	tm.isRunning = true
	tm.useSocksProxy = useSocks
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
	useSocks := tm.useSocksProxy
	socksAddr := tm.socksAddr
	directTarget := tm.directTarget
	tm.mu.Unlock()

	var rtt int
	var success bool

	if useSocks {
		rtt, success = probeViaSocks5(socksAddr, "1.1.1.1", 443, 1800*time.Millisecond)
	} else {
		rtt, success = probeDirect(directTarget, 1800*time.Millisecond)
	}

	// Fallback to direct probe if socks fails or is not ready yet
	if !success && useSocks {
		rtt, success = probeDirect(directTarget, 1500*time.Millisecond)
	}

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
	if elapsed <= 1 {
		// Connection was intercepted or answered locally by loopback/TUN driver, discard false 1ms reading
		return 0, false
	}
	return elapsed, true
}

// probeViaSocks5 performs an RFC 1928 SOCKS5 handshake through the local proxy
func probeViaSocks5(socksAddr, destHost string, destPort int, timeout time.Duration) (int, bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return 0, false
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	// 1. Send SOCKS5 Greeting (VER 5, 1 Method: NO_AUTH)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return 0, false
	}

	// 2. Read Server Method Selection
	buf := make([]byte, 2)
	if _, err := conn.Read(buf); err != nil || buf[0] != 0x05 || buf[1] != 0x00 {
		return 0, false
	}

	// 3. Send SOCKS5 Connect Request (IPv4 or Domain)
	req := make([]byte, 0, 10)
	req = append(req, 0x05, 0x01, 0x00) // VER 5, CMD 1 (CONNECT), RSV 0

	ip := net.ParseIP(destHost).To4()
	if ip != nil {
		req = append(req, 0x01) // ATYP 1 (IPv4)
		req = append(req, ip...)
	} else {
		req = append(req, 0x03, byte(len(destHost))) // ATYP 3 (Domain)
		req = append(req, []byte(destHost)...)
	}
	req = append(req, byte(destPort>>8), byte(destPort&0xFF))

	if _, err := conn.Write(req); err != nil {
		return 0, false
	}

	// 4. Read Connection Response
	resp := make([]byte, 10)
	if _, err := conn.Read(resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		return 0, false
	}

	elapsed := int(time.Since(start).Milliseconds())
	if elapsed < 1 {
		elapsed = 1
	}
	return elapsed, true
}
