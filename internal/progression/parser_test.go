package progression

import (
	"image"
	"path/filepath"
	"testing"
)

func TestParseScreenshotAllResolutions(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
	}{
		{"Reference_2560x1440", "1867240_1.jpg"},
		{"FHD_1920x1080", "test_1920x1080.jpg"},
		{"HD_1280x720", "test_1280x720.jpg"},
		{"UHD_3840x2160", "test_3840x2160.jpg"},
		{"Ultrawide_3440x1440", "test_3440x1440_ultrawide.jpg"},
	}

	expectedCareer := 110
	expectedRoles := map[string]int{
		"assault": 11,
		"medic":   22,
		"recon":   12,
		"support": 28,
		"driver":  21,
		"pilot":   16,
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "progression", tc.fileName)
			res, err := ParseScreenshotFile(path)
			if err != nil {
				t.Fatalf("Failed to parse %s: %v", tc.fileName, err)
			}

			if res.CareerLevel != expectedCareer {
				t.Errorf("CareerLevel got %d, want %d", res.CareerLevel, expectedCareer)
			}

			for role, expLvl := range expectedRoles {
				actual, ok := res.Roles[role]
				if !ok {
					t.Errorf("Missing role %s", role)
					continue
				}
				if actual.Level != expLvl {
					t.Errorf("Role %s level got %d, want %d", role, actual.Level, expLvl)
				}
			}

			if !res.Valid {
				t.Errorf("Expected valid invariant sum(Roles) == CareerLevel, got sum=%d career=%d", res.SumRoles, res.CareerLevel)
			}
		})
	}
}

func TestDatabaseUnlocks(t *testing.T) {
	db := GetDatabase()
	all := db.GetAllUnlocks()
	if len(all) != 233 {
		t.Fatalf("Expected 233 unlocks, got %d", len(all))
	}

	for _, it := range all {
		if it.NameRU == "" {
			t.Errorf("Item %s has empty NameRU", it.UnlockID)
		}
	}

	// Test NextUnlock for Assault at level 11
	nextAssault := db.GetNextUnlock("assault", 11)
	if nextAssault == nil || nextAssault.NextItem == nil {
		t.Fatalf("Expected next unlock for assault level 11")
	}
	if nextAssault.NextItem.Level <= 11 {
		t.Errorf("Next unlock level should be > 11, got %d", nextAssault.NextItem.Level)
	}
	if nextAssault.LevelsRemaining != nextAssault.NextItem.Level-11 {
		t.Errorf("LevelsRemaining mismatch: %d != %d", nextAssault.LevelsRemaining, nextAssault.NextItem.Level-11)
	}
}

func TestParseScreenshotEdgeCases(t *testing.T) {
	// 1. Corrupted bytes
	_, err := ParseScreenshotBytes([]byte("not an image"))
	if err == nil {
		t.Errorf("Expected error for corrupted bytes, got nil")
	}

	// 2. Non-existent file
	_, err = ParseScreenshotFile("non_existent_file_12345.png")
	if err == nil {
		t.Errorf("Expected error for non-existent file, got nil")
	}

	// 3. Image too small (< 640x360)
	smallImg := image.NewRGBA(image.Rect(0, 0, 320, 240))
	_, err = ParseScreenshot(smallImg)
	if err == nil {
		t.Errorf("Expected error for image too small, got nil")
	}

	// 4. Blank/black image (no digits detected)
	blackImg := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	res, err := ParseScreenshot(blackImg)
	if err != nil {
		t.Fatalf("Unexpected error for valid 1080p black canvas: %v", err)
	}
	if res.Valid {
		t.Errorf("Expected blank image to have Valid == false, got true")
	}
	if res.CareerLevel != 0 || res.SumRoles != 0 {
		t.Errorf("Expected 0 career and 0 sumRoles on blank image, got career=%d sum=%d", res.CareerLevel, res.SumRoles)
	}
}

func TestParseUserScreenshots(t *testing.T) {
	shotsDir := `C:\Program Files (x86)\Steam\userdata\358788399\760\remote\1867240\screenshots`
	files, err := filepath.Glob(filepath.Join(shotsDir, "*.jpg"))
	if err != nil || len(files) == 0 {
		t.Skip("No screenshots found")
	}
	for _, f := range files {
		res, err := ParseScreenshotFile(f)
		if err != nil {
			t.Fatalf("[%s] ERROR: %v", filepath.Base(f), err)
		}
		t.Logf("[%s] Valid=%v Career=%d SumRoles=%d Roles=%v", filepath.Base(f), res.Valid, res.CareerLevel, res.SumRoles, res.Roles)
		if !res.Valid {
			t.Errorf("[%s] Expected Valid=true, got Career=%d SumRoles=%d", filepath.Base(f), res.CareerLevel, res.SumRoles)
		}
	}
}


