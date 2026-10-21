//go:build !windows

package system

func ApplyCompetitiveGamingTweaks(logFn func(string)) {
	// No-op on non-Windows platforms
}

func GetCompetitiveGamingTweaksStatus() map[string]interface{} {
	return map[string]interface{}{
		"supported": false,
	}
}
