package crypto

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeyManager(t *testing.T) {
	km, err := NewKeyManager()
	require.NoError(t, err)
	assert.NotNil(t, km)
	assert.NotEmpty(t, km.GetEncryptionKey())
	assert.Len(t, km.GetEncryptionKey(), 32) // AES-256 key size
	assert.NotEmpty(t, km.GetKeyID())
	assert.False(t, km.ShouldRotate())
}

func TestNewKeyManager_WithEnvVar(t *testing.T) {
	// Test that environment variable setting works first
	testKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// Clean up any existing environment variable
	_ = os.Unsetenv("SHUGO_CLUSTER_KEY")

	// Set the environment variable
	err := os.Setenv("SHUGO_CLUSTER_KEY", testKey)
	require.NoError(t, err, "Setting environment variable should not fail")
	defer func() {
		err := os.Unsetenv("SHUGO_CLUSTER_KEY")
		require.NoError(t, err, "Unsetting environment variable should not fail")
	}()

	// Immediately verify the environment variable is accessible
	envValue := os.Getenv("SHUGO_CLUSTER_KEY")
	if envValue != testKey {
		t.Skipf("Environment variable handling not working in test environment. Expected %q, got %q", testKey, envValue)
	}

	km, err := NewKeyManager()
	require.NoError(t, err)
	assert.NotNil(t, km)

	// Verify the key properties
	assert.Len(t, km.GetEncryptionKey(), 32) // AES-256 key size
	assert.NotEmpty(t, km.GetKeyID())
	assert.Len(t, km.GetKeyID(), 16) // Key ID should be 16 characters (8 bytes hex)

	// Test deterministic behavior by creating a second key manager
	km2, err := NewKeyManager()
	require.NoError(t, err)

	// Both key managers should produce identical results when using the same cluster key
	assert.Equal(t, km.GetEncryptionKey(), km2.GetEncryptionKey(), "Same cluster key should produce same derived key")
	assert.Equal(t, km.GetKeyID(), km2.GetKeyID(), "Same cluster key should produce same key ID")
}

func TestKeyManager_KeyRotation(t *testing.T) {
	km, err := NewKeyManager()
	require.NoError(t, err)

	originalKey := km.GetEncryptionKey()
	originalKeyID := km.GetKeyID()

	// Should not rotate initially
	assert.False(t, km.ShouldRotate())

	// Force rotation
	err = km.RotateKey()
	require.NoError(t, err)

	// Key should be different after rotation
	assert.NotEqual(t, originalKey, km.GetEncryptionKey())
	assert.NotEqual(t, originalKeyID, km.GetKeyID())
	assert.False(t, km.ShouldRotate()) // Should reset timer
}

func TestDeriveKey(t *testing.T) {
	clusterKey := []byte("test-cluster-key-32-bytes-long!")
	salt := "test-salt"

	key1 := deriveKey(clusterKey, salt)
	key2 := deriveKey(clusterKey, salt)

	// Same inputs should produce same output
	assert.Equal(t, key1, key2)
	assert.Len(t, key1, 32) // SHA256 output size

	// Different salt should produce different output
	key3 := deriveKey(clusterKey, "different-salt")
	assert.NotEqual(t, key1, key3)
}

func TestGenerateKeyID(t *testing.T) {
	key1 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i)
	}

	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 1) // Different key
	}

	id1 := generateKeyID(key1)
	id2 := generateKeyID(key2)

	// Same key should produce same ID
	id1Again := generateKeyID(key1)
	assert.Equal(t, id1, id1Again)

	// Different keys should produce different IDs
	assert.NotEqual(t, id1, id2)

	// ID should be 16 characters (8 bytes hex encoded)
	assert.Len(t, id1, 16)
}

func TestHashKey(t *testing.T) {
	key1 := "test-key"
	key2 := "test-key"
	key3 := "different-key"

	hash1 := HashKey(key1)
	hash2 := HashKey(key2)
	hash3 := HashKey(key3)

	// Same input should produce same hash
	assert.Equal(t, hash1, hash2)

	// Different input should produce different hash
	assert.NotEqual(t, hash1, hash3)

	// Hash should be 64 characters (32 bytes hex encoded)
	assert.Len(t, hash1, 64)
}

func TestGenerateID(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()

	// IDs should be different (extremely unlikely to be the same)
	assert.NotEqual(t, id1, id2)

	// ID should be 64 characters (32 bytes hex encoded)
	assert.Len(t, id1, 64)
}

func TestCopyEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := "Hello, World! This is a test message for encryption."
	src := strings.NewReader(plaintext)

	var encrypted bytes.Buffer
	n, err := CopyEncrypt(key, src, &encrypted)
	require.NoError(t, err)
	assert.Greater(t, n, len(plaintext)) // Encrypted data should be larger due to nonce and tag

	var decrypted bytes.Buffer
	encryptedSrc := bytes.NewReader(encrypted.Bytes())
	m, err := CopyDecrypt(key, encryptedSrc, &decrypted)
	require.NoError(t, err)
	assert.Equal(t, len(plaintext), m)
	assert.Equal(t, plaintext, decrypted.String())
}

func TestCopyEncryptDecrypt_LargeData(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Create a large plaintext (1MB) — must stream as multiple chunks
	plaintext := make([]byte, 1024*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	src := bytes.NewReader(plaintext)

	var encrypted bytes.Buffer
	n, err := CopyEncryptChunked(key, src, &encrypted, 64*1024)
	require.NoError(t, err)
	assert.Greater(t, n, len(plaintext))
	assert.True(t, bytes.HasPrefix(encrypted.Bytes(), chunkedMagic))

	var decrypted bytes.Buffer
	encryptedSrc := bytes.NewReader(encrypted.Bytes())
	m, err := CopyDecrypt(key, encryptedSrc, &decrypted)
	require.NoError(t, err)
	assert.Equal(t, len(plaintext), m)
	assert.Equal(t, plaintext, decrypted.Bytes())
}

func TestCopyEncryptDecrypt_Empty(t *testing.T) {
	key := make([]byte, 32)
	var encrypted bytes.Buffer
	_, err := CopyEncrypt(key, bytes.NewReader(nil), &encrypted)
	require.NoError(t, err)

	var decrypted bytes.Buffer
	_, err = CopyDecrypt(key, bytes.NewReader(encrypted.Bytes()), &decrypted)
	require.NoError(t, err)
	assert.Equal(t, 0, decrypted.Len())
}

func TestCopyEncryptDecrypt_SmallChunkSize(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	var encrypted bytes.Buffer
	_, err := CopyEncryptChunked(key, bytes.NewReader(plaintext), &encrypted, 8)
	require.NoError(t, err)

	var decrypted bytes.Buffer
	_, err = CopyDecrypt(key, bytes.NewReader(encrypted.Bytes()), &decrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted.Bytes())
}

func TestCopyEncryptDecrypt_InvalidKey(t *testing.T) {
	key := make([]byte, 15) // Invalid key size (not 16, 24, or 32 bytes)
	plaintext := "test message"
	src := strings.NewReader(plaintext)

	var encrypted bytes.Buffer
	_, err := CopyEncrypt(key, src, &encrypted)
	assert.Error(t, err) // Should fail with invalid key size
}

func TestCopyDecrypt_InvalidData(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	// Try to decrypt invalid data (too short for nonce)
	invalidData := []byte("short")
	src := bytes.NewReader(invalidData)

	var decrypted bytes.Buffer
	_, err := CopyDecrypt(key, src, &decrypted)
	assert.Error(t, err) // Should fail with invalid data
}

func TestCopyDecrypt_WrongKey(t *testing.T) {
	correctKey := make([]byte, 32)
	for i := range correctKey {
		correctKey[i] = byte(i)
	}

	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(i + 1) // Different key
	}

	plaintext := "test message for wrong key"
	src := strings.NewReader(plaintext)

	var encrypted bytes.Buffer
	_, err := CopyEncrypt(correctKey, src, &encrypted)
	require.NoError(t, err)

	// Try to decrypt with wrong key
	encryptedSrc := bytes.NewReader(encrypted.Bytes())
	var decrypted bytes.Buffer
	_, err = CopyDecrypt(wrongKey, encryptedSrc, &decrypted)
	assert.Error(t, err) // Should fail with wrong key
}

func TestNewEncryptionKey_Deprecated(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key, 32)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 12, GCMNonceSize)
	assert.Equal(t, 16, GCMTagSize)
	assert.Equal(t, "shugo-cluster-salt-v1", KeyDerivationSalt)
	assert.Equal(t, 24*time.Hour, KeyRotationPeriod)
}
