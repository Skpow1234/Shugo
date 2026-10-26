package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

const sealedPrefix = "enc1:"

// SealString encrypts plaintext with AES-GCM. The key comes from
// SHUGO_WALLET_KEY, SHUGO_DATA_KEY, or SHUGO_CLUSTER_KEY.
func SealString(plaintext string) (string, error) {
	key, err := sealKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// OpenString decrypts a value produced by SealString. Values without the
// enc1 prefix are returned unchanged so older plaintext records can be read
// and rewritten.
func OpenString(sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealedPrefix) {
		return sealed, nil
	}
	key, err := sealKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealedPrefix))
	if err != nil {
		return "", fmt.Errorf("decode sealed value: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("sealed value is too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt sealed value: %w", err)
	}
	return string(plain), nil
}

func sealKey() ([]byte, error) {
	for _, name := range []string{"SHUGO_WALLET_KEY", "SHUGO_DATA_KEY", "SHUGO_CLUSTER_KEY"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		if decoded, err := hex.DecodeString(value); err == nil && (len(decoded) == 16 || len(decoded) == 24 || len(decoded) == 32) {
			return decoded, nil
		}
		sum := sha256.Sum256([]byte(value))
		return sum[:], nil
	}
	return nil, fmt.Errorf("set SHUGO_WALLET_KEY or SHUGO_CLUSTER_KEY to protect private keys")
}
