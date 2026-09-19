package updater

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareSemVer(t *testing.T) {
	cases := []struct {
		v1, v2   string
		expected int
	}{
		{"v1.0.2", "v1.0.1", 1},
		{"v1.0.1", "v1.0.2", -1},
		{"v1.0.2", "v1.0.2", 0},
		{"v1.1.0", "v1.0.9", 1},
		{"v2.0.0", "v1.9.9", 1},
	}

	for _, c := range cases {
		res := CompareSemVer(c.v1, c.v2)
		if res != c.expected {
			t.Errorf("CompareSemVer(%s, %s) = %d, expected %d", c.v1, c.v2, res, c.expected)
		}
	}
}

func TestCheckForUpdateMock(t *testing.T) {
	mockJSON := `{
		"tag_name": "v1.1.0",
		"name": "Release v1.1.0",
		"body": "Update notes",
		"assets": [
			{
				"name": "WarLink.exe",
				"browser_download_url": "http://example.com/WarLink.exe",
				"size": 15000000
			},
			{
				"name": "WarLink.exe.sha256",
				"browser_download_url": "http://example.com/WarLink.exe.sha256",
				"size": 64
			}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockJSON))
	}))
	defer ts.Close()

	t.Setenv("WARLINK_UPDATE_URL", ts.URL)

	res, err := CheckForUpdate("v1.0.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UpdateAvailable {
		t.Errorf("expected update to be available for v1.0.3 vs v1.1.0")
	}
	if res.LatestVersion != "v1.1.0" {
		t.Errorf("expected latest version v1.1.0, got %s", res.LatestVersion)
	}
	if res.ExeURL != "http://example.com/WarLink.exe" {
		t.Errorf("expected ExeURL http://example.com/WarLink.exe, got %s", res.ExeURL)
	}
}
