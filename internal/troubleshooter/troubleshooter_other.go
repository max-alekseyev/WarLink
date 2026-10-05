//go:build !windows

package troubleshooter

// checkDriversAndServices is a stub for non-Windows systems.
func checkDriversAndServices() []CheckResult {
	return []CheckResult{
		{
			ID:       "driver_stub",
			Category: "driver",
			Title:    "Системный стек сети",
			Status:   "ok",
			Message:  "Платформа отлична от Windows",
		},
	}
}

// fixPlatformIssues is a stub for non-Windows systems.
func fixPlatformIssues() []string {
	return []string{"Платформа отлична от Windows (коррекция не требуется)"}
}
