package pingmeter

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	minPlausibleRTT = 15
)

// Meter tracks UDP latency to game match servers on ports 4000-4500.
type Meter struct {
	mu           sync.RWMutex
	stopChan     chan struct{}
	isRunning    bool
	activeServer string
	activePort   int
	latestRTT    int // in milliseconds
	samples      []int

	burstStart    map[string]time.Time
	awaitingReply map[string]bool

	lastCallback time.Time
	onPingUpdate func(server string, wireRttMs int, inGameEstMs int)
}

func New(_ ...string) *Meter {
	return &Meter{
		burstStart:    make(map[string]time.Time),
		awaitingReply: make(map[string]bool),
		samples:       make([]int, 0, 10),
	}
}

// SetUpdateCallback registers a listener for ping updates.
func (m *Meter) SetUpdateCallback(cb func(server string, wireRttMs int, inGameEstMs int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPingUpdate = cb
}

// Start marks the meter as running.
func (m *Meter) Start() error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return nil
	}
	m.stopChan = make(chan struct{})
	m.isRunning = true
	m.mu.Unlock()
	return nil
}

// Stop stops packet monitoring.
func (m *Meter) Stop() {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return
	}
	m.isRunning = false
	if m.stopChan != nil {
		close(m.stopChan)
	}
	m.activeServer = ""
	m.activePort = 0
	m.latestRTT = 0
	m.samples = m.samples[:0]
	m.burstStart = make(map[string]time.Time)
	m.awaitingReply = make(map[string]bool)
	m.mu.Unlock()
}

// GetActivePing returns the currently tracked game server and ping in ms.
func (m *Meter) GetActivePing() (server string, wireRttMs int, inGameEstMs int, active bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.latestRTT <= 0 || m.activeServer == "" {
		return "", 0, 0, false
	}
	srv := fmt.Sprintf("%s:%d", m.activeServer, m.activePort)
	inGame := m.latestRTT + 30 // Approximate Unreal Engine 30Hz tick delay
	return srv, m.latestRTT, inGame, true
}

// GetDetailedStats returns detailed match latency metrics including jitter, min, and max RTT.
func (m *Meter) GetDetailedStats() (server string, wireRttMs int, inGameEstMs int, jitterMs int, minRttMs int, maxRttMs int, active bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.latestRTT <= 0 || m.activeServer == "" || len(m.samples) == 0 {
		return "", 0, 0, 0, 0, 0, false
	}
	srv := fmt.Sprintf("%s:%d", m.activeServer, m.activePort)
	inGame := m.latestRTT + 30

	jitter := 0
	if len(m.samples) >= 2 {
		diffSum := 0
		for i := 1; i < len(m.samples); i++ {
			d := m.samples[i] - m.samples[i-1]
			if d < 0 {
				d = -d
			}
			diffSum += d
		}
		jitter = diffSum / (len(m.samples) - 1)
	}

	minRTT := m.samples[0]
	maxRTT := m.samples[0]
	for _, s := range m.samples[1:] {
		if s < minRTT {
			minRTT = s
		}
		if s > maxRTT {
			maxRTT = s
		}
	}

	return srv, m.latestRTT, inGame, jitter, minRTT, maxRTT, true
}

// processPacket parses IPv4/UDP headers and calculates round-trip latency.
func (m *Meter) processPacket(pkt []byte) {
	if len(pkt) < 28 {
		return
	}

	// IPv4 check
	version := pkt[0] >> 4
	if version != 4 {
		return
	}
	ihl := int(pkt[0] & 0x0F)
	ipHeaderLen := ihl * 4
	if len(pkt) < ipHeaderLen+8 {
		return
	}

	protocol := pkt[9]
	if protocol != 17 { // UDP = 17
		return
	}

	srcIP := net.IP(pkt[12:16]).String()
	dstIP := net.IP(pkt[16:20]).String()

	udpHeader := pkt[ipHeaderLen : ipHeaderLen+8]
	srcPort := int(binary.BigEndian.Uint16(udpHeader[0:2]))
	dstPort := int(binary.BigEndian.Uint16(udpHeader[2:4]))

	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Outbound: client sending to match server.
	if dstPort >= 4000 && dstPort <= 4500 {
		key := fmt.Sprintf("%s:%d", dstIP, dstPort)
		if !m.awaitingReply[key] || now.Sub(m.burstStart[key]) > time.Second {
			m.burstStart[key] = now
			m.awaitingReply[key] = true
		}
		m.activeServer = dstIP
		m.activePort = dstPort
		return
	}

	// Inbound: match server responding to client.
	if srcPort >= 4000 && srcPort <= 4500 {
		key := fmt.Sprintf("%s:%d", srcIP, srcPort)
		if !m.awaitingReply[key] {
			return
		}
		sentAt, ok := m.burstStart[key]
		if !ok {
			return
		}

		m.awaitingReply[key] = false

		if now.Sub(sentAt) > 2500*time.Millisecond {
			delete(m.burstStart, key)
			return
		}

		rtt := now.Sub(sentAt)
		delete(m.burstStart, key)

		if rtt > 0 && rtt < 2*time.Second {
			rttMs := int(rtt.Milliseconds())
			if rttMs < minPlausibleRTT {
				return
			}
			m.addSample(rttMs)

			if now.Sub(m.lastCallback) >= 1200*time.Millisecond {
				m.lastCallback = now
				if cb := m.onPingUpdate; cb != nil {
					srv := fmt.Sprintf("%s:%d", srcIP, srcPort)
					inGame := m.latestRTT + 30
					go cb(srv, m.latestRTT, inGame)
				}
			}
		}
	}
}

func (m *Meter) addSample(rttMs int) {
	m.samples = append(m.samples, rttMs)
	if len(m.samples) > 10 {
		m.samples = m.samples[1:]
	}

	sum := 0
	for _, s := range m.samples {
		sum += s
	}
	m.latestRTT = sum / len(m.samples)
}
