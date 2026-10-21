package system

import (
	"net"
)

// DetectOptimalWintunMTU inspects active physical network interfaces and returns
// the optimal MTU for Wintun/TUN tunnel to prevent UDP packet fragmentation.
//
// Baseline calculations:
// - Standard Ethernet (MTU >= 1500) -> 1380 (1500 - 120 bytes for QUIC/IP/Salamander headers)
// - PPPoE / DSL (1450 <= MTU < 1500) -> 1340
// - Constrained / Mobile / Cellular (MTU < 1450) -> 1280 (guaranteed IPv6/UDP MTU floor)
// - Fallback default -> 1320
func DetectOptimalWintunMTU() int {
	ifaces, err := net.Interfaces()
	if err != nil {
		return 1320
	}

	minActiveMTU := 0

	for _, iface := range ifaces {
		// Ignore down or loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Ignore virtual interfaces that might have abnormal MTUs (e.g. wintun, tap, loopback)
		if iface.MTU <= 0 || iface.MTU > 9000 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}

		hasIPv4 := false
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				hasIPv4 = true
				break
			}
		}

		if hasIPv4 {
			if minActiveMTU == 0 || iface.MTU < minActiveMTU {
				minActiveMTU = iface.MTU
			}
		}
	}

	if minActiveMTU <= 0 {
		return 1320
	}

	switch {
	case minActiveMTU >= 1500:
		return 1380
	case minActiveMTU >= 1450:
		return 1340
	default:
		return 1280
	}
}
