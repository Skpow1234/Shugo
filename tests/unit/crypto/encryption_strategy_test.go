package main

import (
	"bytes"
	"testing"

	"github.com/Skpow1234/Shugo/internal/crypto"
)

// TestEncryptionAtRest verifies that data is encrypted when stored locally
func TestEncryptionAtRest(t *testing.T) {
	// Test data
	originalData := []byte("test encryption at rest data")

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Simulate storing data with encryption at rest
	reader := bytes.NewReader(originalData)
	var encryptedBuffer bytes.Buffer

	// Encrypt data (simulating storage)
	_, err = crypto.CopyEncrypt(key, reader, &encryptedBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt data: %v", err)
	}

	// Verify that encrypted data is different from original
	if bytes.Equal(originalData, encryptedBuffer.Bytes()) {
		t.Error("encrypted data should be different from original data")
	}

	// Verify that encrypted data is larger (due to AES-GCM overhead)
	if len(encryptedBuffer.Bytes()) <= len(originalData) {
		t.Error("encrypted data should be larger than original due to AES-GCM overhead")
	}

	t.Logf("Original size: %d, Encrypted size: %d", len(originalData), len(encryptedBuffer.Bytes()))
}

// TestDecryptionAtRest verifies that encrypted data can be decrypted correctly
func TestDecryptionAtRest(t *testing.T) {
	// Test data
	originalData := []byte("test decryption at rest data")

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt data
	reader := bytes.NewReader(originalData)
	var encryptedBuffer bytes.Buffer
	_, err = crypto.CopyEncrypt(key, reader, &encryptedBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt data: %v", err)
	}

	// Decrypt data (simulating retrieval)
	encryptedReader := bytes.NewReader(encryptedBuffer.Bytes())
	var decryptedBuffer bytes.Buffer
	_, err = crypto.CopyDecrypt(key, encryptedReader, &decryptedBuffer)
	if err != nil {
		t.Fatalf("failed to decrypt data: %v", err)
	}

	// Verify that decrypted data matches original
	if !bytes.Equal(originalData, decryptedBuffer.Bytes()) {
		t.Error("decrypted data does not match original data")
	}

	t.Log("Encryption and decryption at rest works correctly")
}

// TestEncryptionConsistency verifies that the same data encrypts consistently
func TestEncryptionConsistency(t *testing.T) {
	// Test data
	originalData := []byte("test consistency data")

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt the same data twice
	var encrypted1, encrypted2 bytes.Buffer

	reader1 := bytes.NewReader(originalData)
	_, err = crypto.CopyEncrypt(key, reader1, &encrypted1)
	if err != nil {
		t.Fatalf("failed to encrypt data first time: %v", err)
	}

	reader2 := bytes.NewReader(originalData)
	_, err = crypto.CopyEncrypt(key, reader2, &encrypted2)
	if err != nil {
		t.Fatalf("failed to encrypt data second time: %v", err)
	}

	// Verify that encrypted data is different each time (due to random nonce)
	if bytes.Equal(encrypted1.Bytes(), encrypted2.Bytes()) {
		t.Error("encrypted data should be different each time due to random nonce")
	}

	// But both should decrypt to the same original data
	var decrypted1, decrypted2 bytes.Buffer

	reader1 = bytes.NewReader(encrypted1.Bytes())
	_, err = crypto.CopyDecrypt(key, reader1, &decrypted1)
	if err != nil {
		t.Fatalf("failed to decrypt first encrypted data: %v", err)
	}

	reader2 = bytes.NewReader(encrypted2.Bytes())
	_, err = crypto.CopyDecrypt(key, reader2, &decrypted2)
	if err != nil {
		t.Fatalf("failed to decrypt second encrypted data: %v", err)
	}

	// Both should match original
	if !bytes.Equal(originalData, decrypted1.Bytes()) {
		t.Error("first decryption does not match original")
	}

	if !bytes.Equal(originalData, decrypted2.Bytes()) {
		t.Error("second decryption does not match original")
	}

	t.Log("Encryption consistency verified - different ciphertexts, same plaintext")
}

// TestLargeDataEncryption verifies that large data can be encrypted/decrypted
func TestLargeDataEncryption(t *testing.T) {
	// Create large test data (1MB)
	originalData := make([]byte, 1024*1024)
	for i := range originalData {
		originalData[i] = byte(i % 256)
	}

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Encrypt large data
	reader := bytes.NewReader(originalData)
	var encryptedBuffer bytes.Buffer
	_, err = crypto.CopyEncrypt(key, reader, &encryptedBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt large data: %v", err)
	}

	// Decrypt large data
	encryptedReader := bytes.NewReader(encryptedBuffer.Bytes())
	var decryptedBuffer bytes.Buffer
	_, err = crypto.CopyDecrypt(key, encryptedReader, &decryptedBuffer)
	if err != nil {
		t.Fatalf("failed to decrypt large data: %v", err)
	}

	// Verify that decrypted data matches original
	if !bytes.Equal(originalData, decryptedBuffer.Bytes()) {
		t.Error("large data decryption does not match original")
	}

	t.Logf("Large data encryption/decryption successful: %d bytes", len(originalData))
}
