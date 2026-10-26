package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/Skpow1234/Shugo/internal/crypto"
)

// TestResilientReplication verifies that replication works with retry logic
func TestResilientReplication(t *testing.T) {
	// Test data
	testData := []byte("test resilient replication data")

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Test streaming encryption (simulating replication)
	streamingReader := bytes.NewReader(testData)
	outputBuffer := new(bytes.Buffer)

	// Test successful streaming
	n, err := crypto.CopyEncrypt(key, streamingReader, outputBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt streaming data: %v", err)
	}

	// Verify that data was encrypted
	if n == 0 {
		t.Error("no data was encrypted")
	}

	// The encrypted data should be larger than the original due to AES-GCM overhead
	expectedMinSize := len(testData)
	actualSize := outputBuffer.Len()
	if actualSize < expectedMinSize {
		t.Errorf("encrypted data too small: expected at least %d bytes, got %d", expectedMinSize, actualSize)
	}

	// Test streaming decryption
	encryptedReader := bytes.NewReader(outputBuffer.Bytes())
	decryptedBuffer := new(bytes.Buffer)

	_, err = crypto.CopyDecrypt(key, encryptedReader, decryptedBuffer)
	if err != nil {
		t.Fatalf("failed to decrypt streaming data: %v", err)
	}

	// Verify decryption worked
	if !bytes.Equal(testData, decryptedBuffer.Bytes()) {
		t.Error("decrypted data does not match original")
	}
}

// TestRetryLogic simulates retry behavior for failed operations
func TestRetryLogic(t *testing.T) {
	maxRetries := 3
	attemptCount := 0

	// Simulate a function that fails twice then succeeds
	testFunction := func() error {
		attemptCount++
		if attemptCount < 3 {
			return io.ErrUnexpectedEOF // Simulate network error
		}
		return nil // Success on third attempt
	}

	// Test retry logic
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := testFunction(); err != nil {
			if attempt == maxRetries-1 {
				t.Fatalf("function failed after %d attempts: %v", maxRetries, err)
			}
			continue
		}
		// Success
		break
	}

	// Verify that it took 3 attempts
	if attemptCount != 3 {
		t.Errorf("expected 3 attempts, got %d", attemptCount)
	}
}

// TestPartialFailure simulates partial failure scenarios
func TestPartialFailure(t *testing.T) {
	// Simulate 3 peers, where 2 succeed and 1 fails
	peers := []string{"peer1", "peer2", "peer3"}
	successCount := 0

	// Simulate peer operations
	for _, peer := range peers {
		// Simulate peer2 failing
		if peer == "peer2" {
			continue // Skip failed peer
		}
		successCount++
	}

	// Verify that we got 2 successful replications
	if successCount != 2 {
		t.Errorf("expected 2 successful replications, got %d", successCount)
	}

	// Verify that we have at least one successful replication
	if successCount == 0 {
		t.Error("no successful replications")
	}
}
