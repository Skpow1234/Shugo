package security

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

func TestTOTPAcceptsCurrentCode(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := hotp(raw, uint64(now.Unix()/totpStep))
	if err != nil {
		t.Fatal(err)
	}
	if !verifyTOTP(secret, code, now) {
		t.Fatal("current TOTP code was rejected")
	}
	if verifyTOTP(secret, "00000", now) {
		t.Fatal("short code was accepted")
	}
}
