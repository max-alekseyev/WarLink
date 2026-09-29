//go:build !linux

package main

func getDiskUsage() (usedGB float64, totalGB float64, percent float64) {
	return 6.8, 9.8, 69.4
}

type UDPStats struct {
	InDatagrams  uint64
	NoPorts      uint64
	InErrors     uint64
	OutDatagrams uint64
	RcvbufErrors uint64
	SndbufErrors uint64
}

func getUDPBufferMetrics() UDPStats {
	return UDPStats{
		InDatagrams:  363352367,
		OutDatagrams: 354906693,
		RcvbufErrors: 0,
		SndbufErrors: 0,
		InErrors:     2,
	}
}

func getStorageComponentSizes() map[string]int64 {
	return map[string]int64{
		"postgres":        70015266,
		"victoriametrics": 46973586,
		"system_logs":     436207616,
		"avatars":         16429,
	}
}
