package scanner

import (
	"strings"
	"testing"
)

func TestConflictingProcessNames(t *testing.T) {
	required := []string{
		"gearup_booster.exe",
		"gearup_ball.exe",
		"RedShieldVPN.exe",
		"exitlag.exe",
		"lagofast.exe",
		"warp-svc.exe",
	}

	for _, req := range required {
		found := false
		for _, name := range ConflictingProcessNames {
			if strings.EqualFold(name, req) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in ConflictingProcessNames", req)
		}
	}
}
