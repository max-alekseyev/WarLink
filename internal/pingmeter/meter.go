package pingmeter

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	WINDIVERT_LAYER_NETWORK = 0
	WINDIVERT_FLAG_SNIFF    = 0x0001
	// Minimum physically plausible RTT for our topology:
	// client -> Stockholm VPS (27ms) + VPS -> AWS match server (~50ms) = ~77ms minimum.
	// We use 15ms as lower bound to reject pure TUN loopback artifacts while allowing legitimate fast replies.
	minPlausibleRTT = 15
)

// MatchSample represents a measured live match round-trip time.
type MatchSample struct {
	MeasuredAt   time.Time
}

// Meter passively tracks UDP latency to game match servers on ports 4000-4500.
type Meter struct {
	mu           sync.RWMutex
	stopChan     chan struct{}
	isRunning    bool
	activeServer string
	activePort   int
	latestRTT    int // in milliseconds
	samples      []int

	// burstStart records when the client began sending this burst of packets.
	// We preserve the initial outbound timestamp until a matching inbound packet
	// arrives, calculating true wire RTT instead of overwriting with sub-frame noise.
	burstStart map[string]time.Time
	// awaitingReply indicates an outbound burst is in-flight awaiting server response.
	awaitingReply map[string]bool

	dllDir   string
	handle   uintptr

	onPingUpdate func(server string, wireRttMs int, inGameEstMs int)
}

func New(optionalDllDir ...string) *Meter {
	dir := ""
	if len(optionalDllDir) > 0 {
		dir = optionalDllDir[0]
	}
	if dir == "" {
		// Default to embedded zapret bin directory if exists
		candidates := []string{
			filepath.Join("warlink_core", "zapret", "bin"),
			filepath.Join("..", "..", "warlink_core", "zapret", "bin"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(filepath.Join(c, "WinDivert.dll")); err == nil {
				dir = c
				break
			}
		}
	}

	return &Meter{
		dllDir:        dir,
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

// Start begins passive UDP sniffing for match servers on ports 4000-4500.
func (m *Meter) Start() error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return nil
	}
	m.stopChan = make(chan struct{})
	m.isRunning = true
	m.mu.Unlock()

	go m.sniffLoop()
	return nil
}

// Stop stops packet monitoring and closes open handles.
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
	if m.handle != 0 && m.handle != ^uintptr(0) {
		m.closeHandle(m.handle)
		m.handle = 0
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

func (m *Meter) sniffLoop() {
	dllPath := filepath.Join(m.dllDir, "WinDivert.dll")
	if _, err := os.Stat(dllPath); err != nil {
		// WinDivert DLL not found, sniffing inactive
		return
	}

	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return
	}
	defer dll.Release()

	procOpen, err := dll.FindProc("WinDivertOpen")
	if err != nil {
		return
	}
	procRecv, err := dll.FindProc("WinDivertRecv")
	if err != nil {
		return
	}
	procClose, err := dll.FindProc("WinDivertClose")
	if err != nil {
		return
	}

	filter := "udp and (udp.DstPort >= 4000 and udp.DstPort <= 4500 or udp.SrcPort >= 4000 and udp.SrcPort <= 4500)"
	filterPtr, _ := syscall.BytePtrFromString(filter)

	prio := int64(-1000) // Lower priority so filter engines run first
	r1, _, _ := procOpen.Call(
		uintptr(unsafe.Pointer(filterPtr)),
		uintptr(WINDIVERT_LAYER_NETWORK),
		uintptr(prio),
		uintptr(WINDIVERT_FLAG_SNIFF),
	)

	handle := r1
	if handle == 0 || handle == ^uintptr(0) {
		// Typically fails if not elevated; silently return
		return
	}

	m.mu.Lock()
	m.handle = handle
	m.mu.Unlock()

	packetBuf := make([]byte, 2048)
	addrBuf := make([]byte, 128)
	var readLen uint32

	defer func() {
		procClose.Call(handle)
	}()

	for {
		select {
		case <-m.stopChan:
			return
		default:
		}

		r1, _, _ := procRecv.Call(
			handle,
			uintptr(unsafe.Pointer(&packetBuf[0])),
			uintptr(len(packetBuf)),
			uintptr(unsafe.Pointer(&readLen)),
			uintptr(unsafe.Pointer(&addrBuf[0])),
		)

		if r1 == 0 {
			// Read error or closed
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if readLen > 0 {
			m.processPacket(packetBuf[:readLen])
		}
	}
}

func (m *Meter) closeHandle(h uintptr) {
	dllPath := filepath.Join(m.dllDir, "WinDivert.dll")
	if dll, err := syscall.LoadDLL(dllPath); err == nil {
		defer dll.Release()
		if proc, err := dll.FindProc("WinDivertClose"); err == nil {
			proc.Call(h)
		}
	}
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
		// Only start a new burst if we are not currently awaiting a reply or if the in-flight burst has timed out.
		// This preserves the initial outbound packet timestamp of the transaction,
		// measuring true network round-trip time instead of sub-tick overwrite artifacts.
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

		// Clear awaiting flag so the next client outbound starts a fresh round-trip sample.
		m.awaitingReply[key] = false

		// Guard: discard if the burst is too old (> 2.5s)
		if now.Sub(sentAt) > 2500*time.Millisecond {
			delete(m.burstStart, key)
			return
		}

		rtt := now.Sub(sentAt)
		delete(m.burstStart, key)

		if rtt > 0 && rtt < 2*time.Second {
			rttMs := int(rtt.Milliseconds())
			// Filter out sub-threshold values (< 15ms)
			if rttMs < minPlausibleRTT {
				return
			}
			m.addSample(rttMs)

			if m.onPingUpdate != nil {
				srv := fmt.Sprintf("%s:%d", srcIP, srcPort)
				inGame := m.latestRTT + 30
				go m.onPingUpdate(srv, m.latestRTT, inGame)
			}
		}
	}
}

func (m *Meter) addSample(rttMs int) {
	m.samples = append(m.samples, rttMs)
	if len(m.samples) > 10 {
		m.samples = m.samples[1:]
	}

	// Calculate average of sliding window
	sum := 0
	for _, s := range m.samples {
		sum += s
	}
	m.latestRTT = sum / len(m.samples)
}
