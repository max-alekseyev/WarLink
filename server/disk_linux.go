//go:build linux

package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func getDiskUsage() (usedGB float64, totalGB float64, percent float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0, 0
	}
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if totalBytes == 0 {
		return 0, 0, 0
	}
	usedBytes := totalBytes - freeBytes
	totalGB = float64(totalBytes) / (1024 * 1024 * 1024)
	usedGB = float64(usedBytes) / (1024 * 1024 * 1024)
	percent = (float64(usedBytes) / float64(totalBytes)) * 100.0
	return usedGB, totalGB, percent
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
	var stats UDPStats
	f, err := os.Open("/proc/net/snmp")
	if err != nil {
		return stats
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var headers []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Udp: ") {
			fields := strings.Fields(line[5:])
			if headers == nil {
				headers = fields
			} else {
				for i, h := range headers {
					if i >= len(fields) {
						break
					}
					val, _ := strconv.ParseUint(fields[i], 10, 64)
					switch h {
					case "InDatagrams":
						stats.InDatagrams = val
					case "NoPorts":
						stats.NoPorts = val
					case "InErrors":
						stats.InErrors = val
					case "OutDatagrams":
						stats.OutDatagrams = val
					case "RcvbufErrors":
						stats.RcvbufErrors = val
					case "SndbufErrors":
						stats.SndbufErrors = val
					}
				}
				break
			}
		}
	}
	return stats
}

var (
	storageSizesCache     map[string]int64
	storageSizesCacheTime time.Time
	storageSizesMu        sync.Mutex
)

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func getStorageComponentSizes() map[string]int64 {
	storageSizesMu.Lock()
	defer storageSizesMu.Unlock()

	if storageSizesCache != nil && time.Since(storageSizesCacheTime) < 60*time.Second {
		return storageSizesCache
	}

	res := make(map[string]int64)
	res["postgres"] = dirSize("/var/lib/postgresql")
	res["victoriametrics"] = dirSize("/opt/victoriametrics/data")
	res["system_logs"] = dirSize("/var/log/journal")
	res["avatars"] = dirSize("/opt/warlink-server/avatars")
	if res["avatars"] == 0 {
		res["avatars"] = dirSize("./avatars")
	}

	storageSizesCache = res
	storageSizesCacheTime = time.Now()
	return res
}
