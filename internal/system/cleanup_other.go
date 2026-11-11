//go:build !windows

package system

// CleanupLegacyWardogsTweaks is a no-op stub on non-Windows platforms.
func CleanupLegacyWardogsTweaks(logFn func(string)) {}
