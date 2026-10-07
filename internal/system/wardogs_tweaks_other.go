//go:build !windows

package system

import "fmt"

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

func GetWardogsConfigDir() string {
	return ""
}

func GetWardogsConfigFile() string {
	return ""
}

func GetWardogsBackupFile() string {
	return ""
}

func GetWardogsShadowStatus() WardogsShadowStatus {
	return WardogsShadowStatus{}
}

func SetWardogsShadowsDisabled(disable bool) error {
	return fmt.Errorf("wardogs tweaks are only supported on Windows")
}

func OpenWardogsConfigFolder() error {
	return fmt.Errorf("not supported on this OS")
}
