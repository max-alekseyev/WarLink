package embedded

import (
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
