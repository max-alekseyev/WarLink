//go:build windows

package system

import (
	"os"
	"path/filepath"
	"syscall"
)

// CleanupLegacyWardogsTweaks checks if WarLink previously created a backup file (GameUserSettings.ini.warlink.bak).
// If found (meaning the user previously enabled shadow tweaks in WarLink), it clears any ReadOnly attribute on
// GameUserSettings.ini, restores the original config content from the backup, and deletes the backup file.
// If the backup file does not exist, no action is taken.
func CleanupLegacyWardogsTweaks(logFn func(string)) {
	if logFn == nil {
		logFn = func(string) {}
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	configDir := filepath.Join(localAppData, "Wardogs", "Saved", "Config", "WindowsClient")
	backupFile := filepath.Join(configDir, "GameUserSettings.ini.warlink.bak")
	configFile := filepath.Join(configDir, "GameUserSettings.ini")

	// Only act if WarLink's specific backup file exists (fingerprint of WarLink tweak usage)
	bakData, err := os.ReadFile(backupFile)
	if err != nil {
		return
	}

	// Clear ReadOnly attribute on GameUserSettings.ini
	if pathPtr, err := syscall.UTF16PtrFromString(configFile); err == nil {
		if attrs, err := syscall.GetFileAttributes(pathPtr); err == nil {
			if attrs&syscall.FILE_ATTRIBUTE_READONLY != 0 {
				attrs &^= syscall.FILE_ATTRIBUTE_READONLY
				_ = syscall.SetFileAttributes(pathPtr, attrs)
			}
		}
	}

	// Restore original config from backup
	if len(bakData) > 0 {
		_ = os.WriteFile(configFile, bakData, 0644)
	}

	// Remove WarLink backup file
	_ = os.Remove(backupFile)

	logFn("[CLEANUP] Восстановлен оригинальный GameUserSettings.ini из резервной копии WarLink")
}
