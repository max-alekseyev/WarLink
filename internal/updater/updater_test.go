package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if res.Sha256URL != "http://example.com/WarLink.exe.sha256" {
		t.Errorf("expected Sha256URL, got %s", res.Sha256URL)
	}
}

func TestVerifySha256Strict(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.bin")
	content := []byte("WarLink binary test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	h := sha256.Sum256(content)
	validHash := hex.EncodeToString(h[:])

	// 1. Empty sha256URL must strictly fail
	ok, err := VerifySha256(testFile, "")
	if ok || err == nil {
		t.Errorf("expected error when sha256URL is empty, got ok=%v, err=%v", ok, err)
	}

	// 2. Matching sha256URL
	tsValid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  WarLink.exe\n", validHash)
	}))
	defer tsValid.Close()

	ok, err = VerifySha256(testFile, tsValid.URL)
	if !ok || err != nil {
		t.Errorf("expected verification success, got ok=%v, err=%v", ok, err)
	}

	// 3. Mismatched sha256URL
	tsMismatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  WarLink.exe\n")
	}))
	defer tsMismatch.Close()

	ok, err = VerifySha256(testFile, tsMismatch.URL)
	if ok || err == nil {
		t.Errorf("expected failure on hash mismatch, got ok=%v, err=%v", ok, err)
	}
}

func TestDownloadWithProgressChecksSize(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded.bin")

	// Server reports Content-Length 1000 but only sends 100 bytes
	tsIncomplete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 100))
	}))
	defer tsIncomplete.Close()

	err := DownloadWithProgress(tsIncomplete.URL, destPath, 1000, nil)
	if err == nil {
		t.Fatalf("expected error on incomplete download, got nil")
	}

	// Server sends complete file
	destPath2 := filepath.Join(tmpDir, "downloaded2.bin")
	payload := make([]byte, 2048)
	tsComplete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer tsComplete.Close()

	err = DownloadWithProgress(tsComplete.URL, destPath2, int64(len(payload)), nil)
	if err != nil {
		t.Fatalf("expected successful download, got %v", err)
	}
}
