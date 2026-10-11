package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTOTPGenerationAndValidation(t *testing.T) {
	secret := GenerateTOTPSecret()
	if len(secret) == 0 {
		t.Fatalf("expected non-empty TOTP secret")
	}

	// Generate expected code for current counter
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("failed to decode base32 secret: %v", err)
	}

	counter := uint64(time.Now().Unix() / 30)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	codeInt := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	codeStr := fmt.Sprintf("%06d", codeInt)

	if !ValidateTOTP(secret, codeStr) {
		t.Fatalf("expected valid code %s to pass validation", codeStr)
	}

	// Bad code should fail
	if ValidateTOTP(secret, "000000") && codeStr != "000000" {
		t.Fatalf("expected invalid code to fail validation")
	}
	if ValidateTOTP(secret, "12345") {
		t.Fatalf("expected code with invalid length to fail validation")
	}
}

func TestBackupCodesGeneration(t *testing.T) {
	codes := GenerateBackupCodes(10)
	if len(codes) != 10 {
		t.Fatalf("expected 10 backup codes, got %d", len(codes))
	}
	seen := make(map[string]bool)
	for _, c := range codes {
		if len(c) != 12 || c[0:3] != "WL-" {
			t.Fatalf("unexpected backup code format: %s", c)
		}
		if seen[c] {
			t.Fatalf("duplicate backup code generated: %s", c)
		}
		seen[c] = true
	}
}

func TestGetCleanIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"127.0.0.1:54321", "127.0.0.1"},
		{"[::1]:8080", "::1"},
		{"192.168.1.1", "192.168.1.1"},
		{"138.124.103.99:443", "138.124.103.99"},
	}

	for _, tt := range tests {
		got := getCleanIP(tt.input)
		if got != tt.expected {
			t.Errorf("getCleanIP(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGenerateQRCodeSVG(t *testing.T) {
	svg, err := generateQRCodeSVG("otpauth://totp/WarLink:admin?secret=JBSWY3DPEHPK3PXP&issuer=WarLink")
	if err != nil {
		t.Fatalf("failed to generate QR SVG: %v", err)
	}
	if len(svg) == 0 {
		t.Fatalf("expected non-empty SVG")
	}
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("invalid SVG structure: %s", svg[:50])
	}
	if !strings.Contains(svg, "<path fill=\"#000000\"") {
		t.Fatalf("expected black fill path in SVG")
	}
}
