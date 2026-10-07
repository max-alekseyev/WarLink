package embedded

import (
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed assets/bin/*
var AssetsFS embed.FS

// EnsureSingBoxEmbedded unpacks wintun.dll and decompresses sing-box.exe.gz
// into singboxDir (e.g. warlink_core/singbox) 100% offline.
func EnsureSingBoxEmbedded(singboxDir string) error {
	if err := os.MkdirAll(singboxDir, 0755); err != nil {
		return fmt.Errorf("failed to create singbox dir: %w", err)
	}

	// 1. Extract wintun.dll if missing or invalid size
	wintunDst := filepath.Join(singboxDir, "wintun.dll")
	extractWintun := true
	if fi, err := os.Stat(wintunDst); err == nil && fi.Size() > 100000 {
		extractWintun = false
	}
	if extractWintun {
		src, err := AssetsFS.Open("assets/bin/wintun.dll")
		if err != nil {
			return fmt.Errorf("failed to open embedded wintun.dll: %w", err)
		}
		dst, err := os.OpenFile(wintunDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			src.Close()
			return fmt.Errorf("failed to create target wintun.dll: %w", err)
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to extract wintun.dll: %w", copyErr)
		}
	}

	// 2. Decompress sing-box.exe.gz if missing or invalid size
	singboxDst := filepath.Join(singboxDir, "sing-box.exe")
	extractSingbox := true
	if fi, err := os.Stat(singboxDst); err == nil && fi.Size() > 10000000 {
		extractSingbox = false
	}
	if extractSingbox {
		src, err := AssetsFS.Open("assets/bin/sing-box.exe.gz")
		if err != nil {
			return fmt.Errorf("failed to open embedded sing-box.exe.gz: %w", err)
		}

		gzReader, err := gzip.NewReader(src)
		if err != nil {
			src.Close()
			return fmt.Errorf("failed to create gzip reader for sing-box.exe.gz: %w", err)
		}

		dst, err := os.OpenFile(singboxDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			gzReader.Close()
			src.Close()
			return fmt.Errorf("failed to create target sing-box.exe: %w", err)
		}

		_, copyErr := io.Copy(dst, gzReader)
		gzReader.Close()
		src.Close()
		dst.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to decompress sing-box.exe: %w", copyErr)
		}
	}

	return nil
}
