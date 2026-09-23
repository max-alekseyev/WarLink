package embedded

import (
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets/bin/* assets/lists/* assets/lua/*
var AssetsFS embed.FS

// EnsureCoreFiles unpacks embedded binaries and lists into coreDir (e.g. warlink_core)
// without requiring any network download from GitHub.
func EnsureCoreFiles(coreDir string) error {
	if err := os.MkdirAll(coreDir, 0755); err != nil {
		return fmt.Errorf("failed to create core dir: %w", err)
	}

	subDirs := []string{"bin", "lists", "lua"}
	for _, sub := range subDirs {
		embeddedPath := "assets/" + sub
		targetSubDir := filepath.Join(coreDir, sub)
		if err := os.MkdirAll(targetSubDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s dir: %w", sub, err)
		}

		entries, err := fs.ReadDir(AssetsFS, embeddedPath)
		if err != nil {
			return fmt.Errorf("failed to read embedded dir %s: %w", embeddedPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// sing-box and wintun are dedicated to the singbox directory, not zapret
			if entry.Name() == "sing-box.exe.gz" || entry.Name() == "wintun.dll" {
				continue
			}

			srcFile := embeddedPath + "/" + entry.Name()
			dstFile := filepath.Join(targetSubDir, entry.Name())

			// Check if file exists with same size
			embedInfo, err := entry.Info()
			if err == nil {
				if destInfo, statErr := os.Stat(dstFile); statErr == nil {
					if destInfo.Size() == embedInfo.Size() {
						continue // Already valid and up to date
					}
				}
			}

			// Extract file
			src, err := AssetsFS.Open(srcFile)
			if err != nil {
				return fmt.Errorf("failed to open embedded file %s: %w", srcFile, err)
			}

			dst, err := os.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				src.Close()
				return fmt.Errorf("failed to create target file %s: %w", dstFile, err)
			}

			_, copyErr := io.Copy(dst, src)
			src.Close()
			dst.Close()
			if copyErr != nil {
				return fmt.Errorf("failed to extract file %s: %w", dstFile, copyErr)
			}
		}
	}

	return nil
}

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
