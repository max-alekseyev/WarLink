//go:build !windows

package system

func ApplyCompetitiveGamingTweaks(logFn func(string)) {
	// No-op on non-Windows platforms
}
