package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const totpStep = 30

func generateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mfa secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func verifyTOTP(secret, code string, now time.Time) bool {
	if len(code) != 6 {
		return false
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return false
		}
	}
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(raw) == 0 {
		return false
	}
	counter := uint64(now.Unix() / totpStep)
	start := counter
	if start > 0 {
		start--
	}
	matched := false
	for c := start; c <= counter+1; c++ {
		got, err := hotp(raw, c)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(code)) == 1 {
			matched = true
		}
	}
	return matched
}

func hotp(secret []byte, counter uint64) (string, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(buf); err != nil {
		return "", err
	}
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 0x0f)
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", bin%1000000), nil
}
