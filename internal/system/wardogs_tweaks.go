//go:build windows

package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"warlink/internal/watcher"
)

// WardogsShadowStatus represents current tweak and file state for WARDOGS shadows.
type WardogsShadowStatus struct {
	ConfigFound     bool   `json:"config_found"`
	Path            string `json:"path"`
	ShadowQuality   int    `json:"shadow_quality"`
	ShadowsDisabled bool   `json:"shadows_disabled"`
	IsReadOnly      bool   `json:"is_read_only"`
	HasBackup       bool   `json:"has_backup"`
	BackupPath      string `json:"backup_path"`
	GameRunning     bool   `json:"game_running"`
}

// GetWardogsConfigDir returns the directory containing GameUserSettings.ini.
func GetWardogsConfigDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(localAppData, "Wardogs", "Saved", "Config", "WindowsClient")
}

// GetWardogsConfigFile returns the full path to GameUserSettings.ini.
func GetWardogsConfigFile() string {
	return filepath.Join(GetWardogsConfigDir(), "GameUserSettings.ini")
}

// GetWardogsBackupFile returns the full path to the tweak backup file.
func GetWardogsBackupFile() string {
	return filepath.Join(GetWardogsConfigDir(), "GameUserSettings.ini.warlink.bak")
}

// isFileReadOnly checks if a Windows file has the FILE_ATTRIBUTE_READONLY attribute.
func isFileReadOnly(path string) bool {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(pathPtr)
	if err != nil {
		return false
	}
	return attrs&syscall.FILE_ATTRIBUTE_READONLY != 0
}

// setFileReadOnly sets or unsets the FILE_ATTRIBUTE_READONLY attribute.
func setFileReadOnly(path string, readOnly bool) error {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(pathPtr)
	if err != nil {
		return err
	}
	if readOnly {
		attrs |= syscall.FILE_ATTRIBUTE_READONLY
	} else {
		attrs &^= syscall.FILE_ATTRIBUTE_READONLY
	}
	return syscall.SetFileAttributes(pathPtr, attrs)
}

// isWardogsGameRunning checks if WARDOGS game process is active without spawning any console windows.
func isWardogsGameRunning() bool {
	running, _ := watcher.IsAnyProcessRunning([]string{
		"wardogsclient-win64-shipping.exe",
		"wardogslauncher-shipping.exe",
	}, "")
	return running
}

// GetWardogsShadowStatus inspects GameUserSettings.ini and returns current state.
func GetWardogsShadowStatus() WardogsShadowStatus {
	configPath := GetWardogsConfigFile()
	backupPath := GetWardogsBackupFile()

	status := WardogsShadowStatus{
		Path:        configPath,
		BackupPath:  backupPath,
		GameRunning: isWardogsGameRunning(),
	}

	fi, err := os.Stat(configPath)
	if err != nil || fi.IsDir() {
		status.ConfigFound = false
		return status
	}
	status.ConfigFound = true
	status.IsReadOnly = isFileReadOnly(configPath)

	if bFi, bErr := os.Stat(backupPath); bErr == nil && !bFi.IsDir() {
		status.HasBackup = true
	}

	// Read and parse sg.ShadowQuality under [ScalabilityGroups]
	f, err := os.Open(configPath)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		inScalability := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				inScalability = strings.EqualFold(line, "[ScalabilityGroups]")
				continue
			}
			if inScalability && strings.HasPrefix(line, "sg.ShadowQuality=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					val, pErr := strconv.Atoi(strings.TrimSpace(parts[1]))
					if pErr == nil {
						status.ShadowQuality = val
						if val == 4 {
							status.ShadowsDisabled = true
						}
						break
					}
				}
			}
		}
	}

	return status
}

// SetWardogsShadowsDisabled modifies GameUserSettings.ini to disable or restore shadows.
func SetWardogsShadowsDisabled(disable bool) error {
	configPath := GetWardogsConfigFile()
	backupPath := GetWardogsBackupFile()

	fi, err := os.Stat(configPath)
	if err != nil || fi.IsDir() {
		return fmt.Errorf("файл GameUserSettings.ini не найден (%s)", configPath)
	}

	// 1. If disabling shadows: make backup if not already present
	if disable {
		if _, bErr := os.Stat(backupPath); os.IsNotExist(bErr) {
			raw, rErr := os.ReadFile(configPath)
			if rErr == nil {
				_ = os.WriteFile(backupPath, raw, 0644)
			}
		}
	}

	// 2. Clear ReadOnly attribute so we can write to the file
	_ = setFileReadOnly(configPath, false)

	// 3. Read current file content
	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать GameUserSettings.ini: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(contentBytes), "\r\n", "\n"), "\n")

	// Determine target shadow value
	targetVal := 4
	if !disable {
		// Restore original value from backup if available, otherwise default to 3
		targetVal = 3
		if bData, bErr := os.ReadFile(backupPath); bErr == nil {
			bLines := strings.Split(strings.ReplaceAll(string(bData), "\r\n", "\n"), "\n")
			inScale := false
			for _, bLine := range bLines {
				trimmed := strings.TrimSpace(bLine)
				if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
					inScale = strings.EqualFold(trimmed, "[ScalabilityGroups]")
					continue
				}
				if inScale && strings.HasPrefix(trimmed, "sg.ShadowQuality=") {
					parts := strings.SplitN(trimmed, "=", 2)
					if len(parts) == 2 {
						if v, convErr := strconv.Atoi(strings.TrimSpace(parts[1])); convErr == nil && v >= 0 && v <= 3 {
							targetVal = v
							break
						}
					}
				}
			}
		}
	}

	// 4. Update sg.ShadowQuality under [ScalabilityGroups]
	inScalability := false
	scalabilityFound := false
	shadowLineFound := false
	var newLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inScalability && !shadowLineFound {
				// Insert before the next section begins
				newLines = append(newLines, fmt.Sprintf("sg.ShadowQuality=%d", targetVal))
				shadowLineFound = true
			}
			inScalability = strings.EqualFold(trimmed, "[ScalabilityGroups]")
			if inScalability {
				scalabilityFound = true
			}
			newLines = append(newLines, line)
			continue
		}

		if inScalability && strings.HasPrefix(trimmed, "sg.ShadowQuality=") {
			newLines = append(newLines, fmt.Sprintf("sg.ShadowQuality=%d", targetVal))
			shadowLineFound = true
			continue
		}

		newLines = append(newLines, line)
	}

	// If file ended while still inside [ScalabilityGroups] and line wasn't added
	if inScalability && !shadowLineFound {
		newLines = append(newLines, fmt.Sprintf("sg.ShadowQuality=%d", targetVal))
		shadowLineFound = true
	}

	// If [ScalabilityGroups] was never found in the file
	if !scalabilityFound {
		newLines = append(newLines, "", "[ScalabilityGroups]", fmt.Sprintf("sg.ShadowQuality=%d", targetVal))
	}

	// 5. Write back with Windows CRLF
	output := strings.Join(newLines, "\r\n")
	if err := os.WriteFile(configPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("ошибка записи GameUserSettings.ini: %w", err)
	}

	// 6. Set ReadOnly if shadows are disabled (to protect from UE5 overwrite),
	// or leave normal if shadows were restored
	if disable {
		if err := setFileReadOnly(configPath, true); err != nil {
			return fmt.Errorf("не удалось установить атрибут 'Только для чтения': %w", err)
		}
	} else {
		_ = setFileReadOnly(configPath, false)
	}

	return nil
}

// OpenWardogsConfigFolder opens the config folder in Windows File Explorer.
func OpenWardogsConfigFolder() error {
	dir := GetWardogsConfigDir()
	if _, err := os.Stat(dir); err != nil {
		_ = os.MkdirAll(dir, 0755)
	}
	cmd := exec.Command("explorer.exe", dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}
