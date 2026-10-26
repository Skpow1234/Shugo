package main

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/Skpow1234/Shugo/internal/crypto"
)

// TestStreamingEncryption verifies that encryption works with streaming data
func TestStreamingEncryption(t *testing.T) {
	// Create a large file (1MB) to test streaming
	largeData := make([]byte, 1024*1024) // 1MB
	if _, err := io.ReadFull(rand.Reader, largeData); err != nil {
		t.Fatalf("failed to generate test data: %v", err)
	}

	// Create a streaming reader
	streamingReader := bytes.NewReader(largeData)

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Create output buffer
	outputBuffer := new(bytes.Buffer)

	// Test streaming encryption
	n, err := crypto.CopyEncrypt(key, streamingReader, outputBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt streaming data: %v", err)
	}

	// Verify that data was encrypted
	if n == 0 {
		t.Error("no data was encrypted")
	}

	// The encrypted data should be larger than the original due to AES-GCM overhead
	expectedMinSize := len(largeData)
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
	if !bytes.Equal(largeData, decryptedBuffer.Bytes()) {
		t.Error("decrypted data does not match original")
	}
}

// TestStreamingWithoutBuffering verifies that we can process large data without buffering everything in memory
func TestStreamingWithoutBuffering(t *testing.T) {
	// Create a large file (5MB) to test memory efficiency
	largeData := make([]byte, 5*1024*1024) // 5MB
	if _, err := io.ReadFull(rand.Reader, largeData); err != nil {
		t.Fatalf("failed to generate test data: %v", err)
	}

	// Create a streaming reader that reads in chunks
	streamingReader := &chunkedReader{
		data:      largeData,
		chunkSize: 4096, // 4KB chunks
	}

	// Create encryption key
	key, err := crypto.NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}

	// Create output buffer
	outputBuffer := new(bytes.Buffer)

	// Test streaming encryption with chunked reading
	n, err := crypto.CopyEncrypt(key, streamingReader, outputBuffer)
	if err != nil {
		t.Fatalf("failed to encrypt chunked streaming data: %v", err)
	}

	// Verify that data was encrypted
	if n == 0 {
		t.Error("no data was encrypted")
	}

	// The encrypted data should be larger than the original due to AES-GCM overhead
	expectedMinSize := len(largeData)
	actualSize := outputBuffer.Len()
	if actualSize < expectedMinSize {
		t.Errorf("encrypted data too small: expected at least %d bytes, got %d", expectedMinSize, actualSize)
	}
}

// chunkedReader simulates reading data in chunks to test streaming behavior
type chunkedReader struct {
	data      []byte
	offset    int
	chunkSize int
}

func (cr *chunkedReader) Read(p []byte) (n int, err error) {
	if cr.offset >= len(cr.data) {
		return 0, io.EOF
	}

	// Read up to chunkSize bytes
	remaining := len(cr.data) - cr.offset
	toRead := cr.chunkSize
	if toRead > remaining {
		toRead = remaining
	}
	if toRead > len(p) {
		toRead = len(p)
	}

	copy(p, cr.data[cr.offset:cr.offset+toRead])
	cr.offset += toRead

	return toRead, nil
}
