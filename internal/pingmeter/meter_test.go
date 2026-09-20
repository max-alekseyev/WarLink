package pingmeter

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// constructMockUDPPacket builds a raw IPv4/UDP packet slice for testing.
func constructMockUDPPacket(srcIP string, srcPort int, dstIP string, dstPort int) []byte {
	pkt := make([]byte, 28)

	// IPv4 Header (20 bytes)
	pkt[0] = 0x45 // Version 4, IHL 5 (20 bytes)
	pkt[1] = 0x00 // DSCP / ECN
	binary.BigEndian.PutUint16(pkt[2:4], 28) // Total Length
	pkt[9] = 17 // UDP protocol

	copy(pkt[12:16], net.ParseIP(srcIP).To4())
	copy(pkt[16:20], net.ParseIP(dstIP).To4())

	// UDP Header (8 bytes)
	binary.BigEndian.PutUint16(pkt[20:22], uint16(srcPort))
	binary.BigEndian.PutUint16(pkt[22:24], uint16(dstPort))
	binary.BigEndian.PutUint16(pkt[24:26], 8)
	binary.BigEndian.PutUint16(pkt[26:28], 0)

	return pkt
}

func TestMeterPacketProcessing(t *testing.T) {
	m := New()

	clientIP := "192.168.1.100"
	clientPort := 54255
	matchServerIP := "54.115.8.196"
	matchServerPort := 4192

	// 1. Send outbound packet to match server
	outPkt := constructMockUDPPacket(clientIP, clientPort, matchServerIP, matchServerPort)
	m.processPacket(outPkt)

	// Verify server endpoint was recognized
	if m.activeServer != matchServerIP || m.activePort != matchServerPort {
		t.Fatalf("Expected activeServer %s:%d, got %s:%d", matchServerIP, matchServerPort, m.activeServer, m.activePort)
	}

	// Fake an artificial delay
	time.Sleep(30 * time.Millisecond)

	// 2. Send inbound packet from match server
	inPkt := constructMockUDPPacket(matchServerIP, matchServerPort, clientIP, clientPort)
	m.processPacket(inPkt)

	srv, wireRtt, inGame, active := m.GetActivePing()
	if !active {
		t.Fatalf("Expected active ping, got inactive")
	}

	if srv != "54.115.8.196:4192" {
		t.Errorf("Expected server 54.115.8.196:4192, got %s", srv)
	}

	if wireRtt < 25 || wireRtt > 200 {
		t.Errorf("Expected wire RTT ~30-50ms, got %d ms", wireRtt)
	}

	if inGame != wireRtt+30 {
		t.Errorf("Expected inGame estimate wireRtt+30 (%d), got %d", wireRtt+30, inGame)
	}
}

func TestMeterSlidingAverage(t *testing.T) {
	m := New()

	samples := []int{40, 50, 60, 45, 55}
	for _, s := range samples {
		m.addSample(s)
	}

	// (40+50+60+45+55)/5 = 250/5 = 50
	if m.latestRTT != 50 {
		t.Errorf("Expected average 50, got %d", m.latestRTT)
	}
}
